package vector_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector"
)

// wordContent builds a ChunkContent with a single text block of approximately
// wordCount words (each word is "lorem ").
func wordContent(wordCount int) vector.ChunkContent {
	return vector.ChunkContent{
		Blocks: []goagent.ContentBlock{goagent.TextBlock(strings.Repeat("lorem ", wordCount))},
	}
}

// BenchmarkTextChunker_NoSplit measures Chunk when the document fits in a
// single chunk — the fast path that skips splitWithOverlap entirely.
func BenchmarkTextChunker_NoSplit(b *testing.B) {
	c := vector.NewTextChunker(vector.WithMaxSize(1000))
	content := wordContent(100) // ~100 heuristic tokens, well under 1 000
	ctx := context.Background()
	for b.Loop() {
		_, _ = c.Chunk(ctx, content)
	}
}

// BenchmarkTextChunker_MediumDoc measures Chunk on a document that produces
// several chunks. Each word triggers an Estimator.Measure call inside
// splitWithOverlap.
func BenchmarkTextChunker_MediumDoc(b *testing.B) {
	c := vector.NewTextChunker(vector.WithMaxSize(500), vector.WithOverlap(50))
	content := wordContent(2_000) // ~2 000 tokens → ~4 chunks at 500 max
	ctx := context.Background()
	for b.Loop() {
		_, _ = c.Chunk(ctx, content)
	}
}

// BenchmarkTextChunker_LargeDoc measures Chunk on a large document to stress
// the splitWithOverlap loop at scale (~20+ chunks).
func BenchmarkTextChunker_LargeDoc(b *testing.B) {
	c := vector.NewTextChunker(vector.WithMaxSize(500), vector.WithOverlap(50))
	content := wordContent(10_000)
	ctx := context.Background()
	for b.Loop() {
		_, _ = c.Chunk(ctx, content)
	}
}

// BenchmarkRecursiveChunker_MediumDoc measures RecursiveChunker on a document
// with paragraph and sentence breaks. Compare with TextChunker to quantify the
// separator hierarchy traversal overhead.
func BenchmarkRecursiveChunker_MediumDoc(b *testing.B) {
	c := vector.NewRecursiveChunker(
		vector.WithRCMaxSize(500),
		vector.WithRCOverlap(50),
	)
	para := strings.Repeat("lorem ipsum dolor sit amet. ", 20) + "\n\n"
	content := vector.ChunkContent{
		Blocks: []goagent.ContentBlock{goagent.TextBlock(strings.Repeat(para, 5))},
	}
	ctx := context.Background()
	for b.Loop() {
		_, _ = c.Chunk(ctx, content)
	}
}

// BenchmarkSentenceChunker_MediumDoc measures SentenceChunker on a document
// with many sentence boundaries. The regex FindAllStringIndex call is the
// dominant cost in this path.
func BenchmarkSentenceChunker_MediumDoc(b *testing.B) {
	c := vector.NewSentenceChunker(
		vector.WithSentenceMaxSize(500),
		vector.WithSentenceOverlap(2),
	)
	sentence := "The quick brown fox jumps over the lazy dog. "
	content := vector.ChunkContent{
		Blocks: []goagent.ContentBlock{goagent.TextBlock(strings.Repeat(sentence, 200))},
	}
	ctx := context.Background()
	for b.Loop() {
		_, _ = c.Chunk(ctx, content)
	}
}
