package vector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Germanblandin1/goagent"
)

// ErrNoEmbeddeableContent is returned when none of the input blocks can be
// processed by the Embedder (e.g. a text-only embedder receives only images).
// This is not a fatal error in Store — callers may skip the chunk with errors.Is.
var ErrNoEmbeddeableContent = errors.New("vector: no embeddeable content in blocks")

// EmbedAll embeds each ChunkResult and returns the vectors in the same order.
// If any chunk fails, it returns an error identifying the failing chunk index.
// Does not parallelize — the caller is responsible for concurrency.
func EmbedAll(ctx context.Context, e goagent.Embedder, chunks []ChunkResult) ([][]float32, error) {
	vecs := make([][]float32, len(chunks))
	for i, chunk := range chunks {
		vec, err := e.Embed(ctx, chunk.Blocks)
		if err != nil {
			return nil, fmt.Errorf("embedding chunk %d: %w", i, err)
		}
		vecs[i] = vec
	}
	return vecs, nil
}

// ── OllamaEmbedder ───────────────────────────────────────────────────────────

// Compile-time check: OllamaEmbedder implements goagent.Embedder.
var _ goagent.Embedder = (*OllamaEmbedder)(nil)

// OllamaOption configures an OllamaEmbedder.
type OllamaOption func(*OllamaEmbedder)

// WithOllamaBaseURL overrides the default Ollama server URL (http://localhost:11434).
func WithOllamaBaseURL(url string) OllamaOption {
	return func(e *OllamaEmbedder) { e.baseURL = url }
}

// WithOllamaMaxChars sets the maximum number of runes sent to Ollama per call.
// Text exceeding this limit is truncated at the last word boundary.
// Default: 30000 (~7500 tokens with the heuristic — safe margin for nomic-embed-text).
func WithOllamaMaxChars(n int) OllamaOption {
	return func(e *OllamaEmbedder) { e.maxChars = n }
}

// OllamaEmbedder implements goagent.Embedder using a local model served by Ollama.
// It extracts only ContentText blocks — images and documents are ignored.
// For long texts use a Chunker before calling Embed.
type OllamaEmbedder struct {
	baseURL  string
	model    string
	maxChars int
	client   *http.Client
}

// NewOllamaEmbedder creates an OllamaEmbedder for the given model name
// (e.g. "nomic-embed-text").
func NewOllamaEmbedder(model string, opts ...OllamaOption) *OllamaEmbedder {
	e := &OllamaEmbedder{
		baseURL:  "http://localhost:11434",
		model:    model,
		maxChars: 30000,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Embed returns the embedding vector for the text content of blocks.
// Returns ErrNoEmbeddeableContent when blocks contain no text.
func (e *OllamaEmbedder) Embed(ctx context.Context, blocks []goagent.ContentBlock) ([]float32, error) {
	text := extractText(blocks)
	if text == "" {
		return nil, ErrNoEmbeddeableContent
	}
	if e.maxChars > 0 && len([]rune(text)) > e.maxChars {
		text = truncateAtWord(text, e.maxChars)
	}

	body, err := json.Marshal(map[string]string{
		"model":  e.model,
		"prompt": text,
	})
	if err != nil {
		return nil, fmt.Errorf("ollama embedder marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		e.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama embedder request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embedder do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama embedder: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ollama embedder decode: %w", err)
	}
	return result.Embedding, nil
}

// ── FallbackEmbedder ─────────────────────────────────────────────────────────

// Compile-time check: FallbackEmbedder implements goagent.Embedder.
var _ goagent.Embedder = (*FallbackEmbedder)(nil)

// FallbackEmbedderOption configures a FallbackEmbedder.
type FallbackEmbedderOption func(*FallbackEmbedder)

// WithSupportedType sets the content type that the primary Embedder can handle.
// Blocks of other types are filtered out before calling Embed.
// Default: ContentText.
func WithSupportedType(t goagent.ContentType) FallbackEmbedderOption {
	return func(e *FallbackEmbedder) { e.supportedType = t }
}

// WithOnSkipped registers a callback invoked for each block filtered out
// because its type is not the supported type. May be nil (default).
func WithOnSkipped(fn func(goagent.ContentBlock)) FallbackEmbedderOption {
	return func(e *FallbackEmbedder) { e.onSkipped = fn }
}

// FallbackEmbedder wraps a primary Embedder and filters out blocks whose type
// is not the configured supported type. If at least one block survives the
// filter, it delegates to the primary Embedder. If none survive, it returns
// ErrNoEmbeddeableContent.
// Facilitates the transition from text-only to multimodal embedders without
// changing the calling API.
type FallbackEmbedder struct {
	primary       goagent.Embedder
	supportedType goagent.ContentType
	onSkipped     func(goagent.ContentBlock)
}

// NewFallbackEmbedder creates a FallbackEmbedder wrapping primary.
// Default supported type: ContentText.
func NewFallbackEmbedder(primary goagent.Embedder, opts ...FallbackEmbedderOption) *FallbackEmbedder {
	e := &FallbackEmbedder{
		primary:       primary,
		supportedType: goagent.ContentText,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Embed filters blocks by the configured supported type and delegates to the
// primary Embedder. Returns ErrNoEmbeddeableContent when no blocks survive the filter.
func (e *FallbackEmbedder) Embed(ctx context.Context, blocks []goagent.ContentBlock) ([]float32, error) {
	var supported []goagent.ContentBlock
	for _, b := range blocks {
		if b.Type == e.supportedType {
			supported = append(supported, b)
		} else if e.onSkipped != nil {
			e.onSkipped(b)
		}
	}
	if len(supported) == 0 {
		return nil, ErrNoEmbeddeableContent
	}
	return e.primary.Embed(ctx, supported)
}

