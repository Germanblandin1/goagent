package testutil

import (
	"context"
	"hash/fnv"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector"
)

// Compile-time check: MockEmbedder implements goagent.Embedder.
var _ goagent.Embedder = (*MockEmbedder)(nil)

// MockEmbedder generates deterministic embeddings via an LCG seeded with the
// FNV-32a hash of the text content. Makes no API calls.
// The same text always produces the same normalized vector.
// Use in unit tests that need similarity search without external services.
type MockEmbedder struct {
	Dim int // vector dimension; defaults to 16 when zero
}

// Embed returns a deterministic unit-length vector for the text content of blocks.
// Returns vector.ErrNoEmbeddeableContent when blocks contain no text.
func (m *MockEmbedder) Embed(_ context.Context, blocks []goagent.ContentBlock) ([]float32, error) {
	var text string
	for _, b := range blocks {
		if b.Type == goagent.ContentText {
			text += b.Text
		}
	}
	if text == "" {
		return nil, vector.ErrNoEmbeddeableContent
	}

	dim := m.Dim
	if dim == 0 {
		dim = 16
	}

	h := fnv.New32a()
	h.Write([]byte(text))
	seed := h.Sum32()

	vec := make([]float32, dim)
	for i := range vec {
		// Classic LCG — deterministic, no external state.
		seed = seed*1664525 + 1013904223
		vec[i] = float32(seed>>16)/float32(1<<16)*2 - 1 // range [-1, 1]
	}

	return vector.Normalize(vec), nil
}
