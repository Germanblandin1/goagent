package vector

import (
	"context"
	"errors"
	"fmt"

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
