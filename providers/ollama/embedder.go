package ollama

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector"
)

// Compile-time checks: OllamaEmbedder implements both Embedder and BatchEmbedder.
var (
	_ goagent.Embedder      = (*OllamaEmbedder)(nil)
	_ goagent.BatchEmbedder = (*OllamaEmbedder)(nil)
)

// EmbedderOption configures an OllamaEmbedder.
type EmbedderOption func(*OllamaEmbedder)

// WithEmbedModel sets the Ollama embedding model (e.g. "nomic-embed-text").
// Required: Embed returns an error if no model is configured.
func WithEmbedModel(model string) EmbedderOption {
	return func(e *OllamaEmbedder) { e.model = model }
}

// WithMaxChars sets the maximum number of runes sent to Ollama per call.
// Text exceeding this limit is truncated at the last word boundary.
// Default: 30000 (~7500 tokens — safe margin for nomic-embed-text).
func WithMaxChars(n int) EmbedderOption {
	return func(e *OllamaEmbedder) { e.maxChars = n }
}

// OllamaEmbedder implements goagent.Embedder using a local embedding model
// served by Ollama. It extracts only ContentText blocks — images and
// documents are ignored. For long texts use a Chunker before calling Embed.
type OllamaEmbedder struct {
	client   *OllamaClient
	model    string
	maxChars int
}

// NewEmbedder creates an OllamaEmbedder with a default OllamaClient targeting
// localhost:11434. For custom HTTP settings, create a client with NewClient
// and pass it to NewEmbedderWithClient instead.
func NewEmbedder(opts ...EmbedderOption) *OllamaEmbedder {
	return NewEmbedderWithClient(NewClient(), opts...)
}

// NewEmbedderWithClient creates an OllamaEmbedder that uses the given shared
// client. Use this when you need to share a client between Provider and
// OllamaEmbedder, or when the default OllamaClient settings are not sufficient.
func NewEmbedderWithClient(client *OllamaClient, opts ...EmbedderOption) *OllamaEmbedder {
	e := &OllamaEmbedder{
		client:   client,
		maxChars: 30000,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Embed returns the embedding vector for the text content of blocks.
// Returns an error when no model is set or when blocks contain no text
// (vector.ErrNoEmbeddeableContent).
func (e *OllamaEmbedder) Embed(ctx context.Context, blocks []goagent.ContentBlock) ([]float32, error) {
	if e.model == "" {
		return nil, fmt.Errorf("ollama embedder: model not set; use WithEmbedModel")
	}

	text := embedExtractText(blocks)
	if text == "" {
		return nil, vector.ErrNoEmbeddeableContent
	}
	if e.maxChars > 0 && len([]rune(text)) > e.maxChars {
		text = embedTruncateAtWord(text, e.maxChars)
	}

	reqBody := map[string]string{
		"model":  e.model,
		"prompt": text,
	}
	var result struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := e.client.do(ctx, "/api/embeddings", reqBody, &result); err != nil {
		return nil, fmt.Errorf("ollama embedder: %w", err)
	}
	return result.Embedding, nil
}

// BatchEmbed embeds multiple content slices concurrently, sending each to
// Ollama in a separate goroutine. This reduces total latency from
// N×embed_latency to ~max(embed_latency) when the inputs are independent.
//
// A nil vector at position i means that the input at that position contained
// no text blocks (equivalent to Embed returning ErrNoEmbeddeableContent).
// A non-nil error means at least one Ollama call failed; the batch is aborted.
func (e *OllamaEmbedder) BatchEmbed(ctx context.Context, inputs [][]goagent.ContentBlock) ([][]float32, error) {
	vecs := make([][]float32, len(inputs))
	errs := make([]error, len(inputs))
	var wg sync.WaitGroup
	for i, blocks := range inputs {
		wg.Add(1)
		go func(idx int, blks []goagent.ContentBlock) {
			defer wg.Done()
			vec, err := e.Embed(ctx, blks)
			if err != nil {
				if errors.Is(err, vector.ErrNoEmbeddeableContent) {
					return // vecs[idx] stays nil — signal to caller to skip
				}
				errs[idx] = err
				return
			}
			vecs[idx] = vec
		}(i, blocks)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return vecs, nil
}

// embedExtractText concatenates all ContentText blocks separated by a single
// space. Blocks of other types are silently ignored.
func embedExtractText(blocks []goagent.ContentBlock) string {
	var parts []string
	for _, b := range blocks {
		if b.Type == goagent.ContentText && strings.TrimSpace(b.Text) != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, " ")
}

// embedTruncateAtWord truncates text at the last word boundary before
// maxChars runes. Hard-cuts at maxChars when no space is found before it.
func embedTruncateAtWord(text string, maxChars int) string {
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}
	cut := maxChars
	for cut > 0 && runes[cut] != ' ' {
		cut--
	}
	if cut == 0 {
		return string(runes[:maxChars])
	}
	return string(runes[:cut])
}
