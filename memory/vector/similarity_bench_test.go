package vector_test

import (
	"testing"

	"github.com/Germanblandin1/goagent/memory/vector"
)

// BenchmarkCosineSimilarity_384 benchmarks CosineSimilarity with typical
// sentence-embedding dimensions (e.g. nomic-embed-text, all-MiniLM-L6-v2).
func BenchmarkCosineSimilarity_384(b *testing.B) {
	a := makeVec(384)
	bv := makeVec(384)
	for b.Loop() {
		_ = vector.CosineSimilarity(a, bv)
	}
}

// BenchmarkCosineSimilarity_1536 benchmarks CosineSimilarity with OpenAI
// text-embedding-3-small / text-embedding-ada-002 dimensions.
func BenchmarkCosineSimilarity_1536(b *testing.B) {
	a := makeVec(1536)
	bv := makeVec(1536)
	for b.Loop() {
		_ = vector.CosineSimilarity(a, bv)
	}
}

// BenchmarkCosineSimilarityRaw_384 benchmarks the raw (non-normalised) variant.
func BenchmarkCosineSimilarityRaw_384(b *testing.B) {
	a := makeVec(384)
	bv := makeVec(384)
	for b.Loop() {
		_ = vector.CosineSimilarityRaw(a, bv)
	}
}

// BenchmarkNormalize_384 benchmarks vector normalization at sentence-embedding dim.
func BenchmarkNormalize_384(b *testing.B) {
	v := makeVec(384)
	for b.Loop() {
		_ = vector.Normalize(v)
	}
}

// makeVec generates a non-zero float32 slice of length n with deterministic values.
func makeVec(n int) []float32 {
	v := make([]float32, n)
	for i := range v {
		v[i] = float32(i+1) / float32(n)
	}
	return v
}
