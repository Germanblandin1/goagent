package voyage

import (
	"context"
	"fmt"
	"strings"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector"
)

// Compile-time check: Embedder implements goagent.Embedder.
var _ goagent.Embedder = (*Embedder)(nil)

// EmbedderOption configures an Embedder.
type EmbedderOption func(*Embedder)

// WithEmbedModel sets the Voyage AI embedding model (e.g. "voyage-3").
// Required: Embed returns an error if no model is configured.
func WithEmbedModel(model string) EmbedderOption {
	return func(e *Embedder) { e.model = model }
}

// WithInputType sets the input_type hint sent to the Voyage AI API.
// Use "document" when embedding corpus entries and "query" when embedding
// search queries. Omit (default) for symmetric tasks such as clustering.
func WithInputType(t string) EmbedderOption {
	return func(e *Embedder) { e.inputType = t }
}

// WithMaxChars sets the maximum number of runes sent to Voyage AI per call.
// Text exceeding this limit is truncated at the last word boundary.
// Default: 30000 (~7500 tokens — conservative safe margin).
func WithMaxChars(n int) EmbedderOption {
	return func(e *Embedder) { e.maxChars = n }
}

// Embedder implements goagent.Embedder using the Voyage AI embeddings API.
// It extracts only ContentText blocks — images and documents are ignored.
// For long texts use a Chunker before calling Embed.
type Embedder struct {
	client    *VoyageClient
	model     string
	inputType string
	maxChars  int
}

// NewEmbedder creates an Embedder with a default VoyageClient.
// The API key is read from the VOYAGE_API_KEY environment variable by default.
// For explicit credentials or custom HTTP settings, create a client with
// NewClient and pass it to NewEmbedderWithClient instead.
func NewEmbedder(opts ...EmbedderOption) *Embedder {
	return NewEmbedderWithClient(NewClient(), opts...)
}

// NewEmbedderWithClient creates an Embedder that uses the given shared client.
// Use this when you need to share a client across multiple Embedder instances
// or when the default VoyageClient settings are not sufficient.
func NewEmbedderWithClient(client *VoyageClient, opts ...EmbedderOption) *Embedder {
	e := &Embedder{
		client:   client,
		maxChars: 30000,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// embeddingRequest is the Voyage AI /embeddings request body.
type embeddingRequest struct {
	Input     []string `json:"input"`
	Model     string   `json:"model"`
	InputType string   `json:"input_type,omitempty"`
}

// embeddingResponse is the Voyage AI /embeddings response body.
type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

// Embed returns the embedding vector for the text content of blocks.
// Returns an error when no model is set or when blocks contain no text
// (vector.ErrNoEmbeddeableContent).
func (e *Embedder) Embed(ctx context.Context, blocks []goagent.ContentBlock) ([]float32, error) {
	if e.model == "" {
		return nil, fmt.Errorf("voyage embedder: model not set; use WithEmbedModel")
	}

	text := embedExtractText(blocks)
	if text == "" {
		return nil, vector.ErrNoEmbeddeableContent
	}
	if e.maxChars > 0 && len([]rune(text)) > e.maxChars {
		text = embedTruncateAtWord(text, e.maxChars)
	}

	reqBody := embeddingRequest{
		Input:     []string{text},
		Model:     e.model,
		InputType: e.inputType,
	}

	var result embeddingResponse
	if err := e.client.do(ctx, "/embeddings", reqBody, &result); err != nil {
		return nil, fmt.Errorf("voyage embedder: %w", err)
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("voyage embedder: empty embedding data in response")
	}
	return result.Data[0].Embedding, nil
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
