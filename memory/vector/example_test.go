package vector_test

import (
	"context"
	"fmt"
	"log"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/internal/testutil"
	"github.com/Germanblandin1/goagent/memory/vector"
)

// Example demonstrates splitting a text document into chunks with the default
// TextChunker. Short texts that fit within MaxSize are returned as a single chunk.
func Example() {
	ctx := context.Background()
	chunker := vector.NewTextChunker()

	content := vector.ChunkContent{
		Blocks: []goagent.ContentBlock{
			goagent.TextBlock("Go is a compiled, statically typed language designed for simplicity."),
		},
	}
	chunks, err := chunker.Chunk(ctx, content)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(chunks))
	// Output: 1
}

// ExampleNewNoOpChunker shows a chunker that returns its input as a single
// unchanged chunk — useful when the content already fits within model limits.
func ExampleNewNoOpChunker() {
	ctx := context.Background()
	chunker := vector.NewNoOpChunker()

	content := vector.ChunkContent{
		Blocks:   []goagent.ContentBlock{goagent.TextBlock("original text")},
		Metadata: map[string]any{"source": "doc.md"},
	}
	chunks, err := chunker.Chunk(ctx, content)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(chunks))
	fmt.Println(vector.ExtractText(chunks[0].Blocks))
	// Output:
	// 1
	// original text
}

// ExampleNewTextChunker shows splitting text at word boundaries when it exceeds
// MaxSize, with zero overlap between adjacent chunks.
func ExampleNewTextChunker() {
	ctx := context.Background()
	chunker := vector.NewTextChunker(
		vector.WithMaxSize(10),
		vector.WithOverlap(0),
		vector.WithEstimator(&vector.CharEstimator{}),
	)

	content := vector.ChunkContent{
		Blocks: []goagent.ContentBlock{goagent.TextBlock("hello world foo bar")},
	}
	chunks, err := chunker.Chunk(ctx, content)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(chunks))
	fmt.Println(vector.ExtractText(chunks[0].Blocks))
	fmt.Println(vector.ExtractText(chunks[1].Blocks))
	// Output:
	// 2
	// hello world
	// foo bar
}

// ExampleNewRecursiveChunker shows splitting a two-paragraph document at the
// paragraph boundary (\n\n) before falling back to finer separators.
func ExampleNewRecursiveChunker() {
	ctx := context.Background()
	chunker := vector.NewRecursiveChunker(
		vector.WithRCMaxSize(30),
		vector.WithRCOverlap(0),
		vector.WithRCEstimator(&vector.CharEstimator{}),
	)

	content := vector.ChunkContent{
		Blocks: []goagent.ContentBlock{
			goagent.TextBlock("First paragraph text here.\n\nSecond paragraph text here."),
		},
	}
	chunks, err := chunker.Chunk(ctx, content)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(chunks))
	fmt.Println(vector.ExtractText(chunks[0].Blocks))
	fmt.Println(vector.ExtractText(chunks[1].Blocks))
	// Output:
	// 2
	// First paragraph text here.
	// Second paragraph text here.
}

// ExampleNewSentenceChunker shows grouping sentences into chunks that respect
// MaxSize, with one overlapping sentence between adjacent chunks for context.
func ExampleNewSentenceChunker() {
	ctx := context.Background()
	chunker := vector.NewSentenceChunker(
		vector.WithSentenceMaxSize(20),
		vector.WithSentenceOverlap(0),
		vector.WithSentenceEstimator(&vector.CharEstimator{}),
	)

	content := vector.ChunkContent{
		Blocks: []goagent.ContentBlock{
			goagent.TextBlock("Go is fast. It uses goroutines."),
		},
	}
	chunks, err := chunker.Chunk(ctx, content)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(chunks))
	fmt.Println(vector.ExtractText(chunks[0].Blocks))
	fmt.Println(vector.ExtractText(chunks[1].Blocks))
	// Output:
	// 2
	// Go is fast.
	// It uses goroutines.
}

// ExampleExtractText shows concatenating all ContentText blocks while ignoring
// non-text blocks such as images.
func ExampleExtractText() {
	blocks := []goagent.ContentBlock{
		goagent.TextBlock("hello"),
		goagent.ImageBlock([]byte{0xFF, 0xD8}, "image/jpeg"), // non-text, silently ignored
		goagent.TextBlock("world"),
	}
	fmt.Println(vector.ExtractText(blocks))
	// Output: hello world
}

// ExampleChunkToMessage shows building a Message from a ChunkResult while
// preserving the original Role.
func ExampleChunkToMessage() {
	orig := goagent.AssistantMessage("original content")
	chunk := vector.ChunkResult{
		Blocks:   []goagent.ContentBlock{goagent.TextBlock("chunk content")},
		Metadata: map[string]any{"chunk_index": 0},
	}
	msg := vector.ChunkToMessage(orig, chunk)
	fmt.Println(msg.TextContent())
	// Output: chunk content
}

// ExampleCharEstimator shows measuring text size in Unicode code points.
// Useful when the model limit is expressed in characters rather than tokens.
func ExampleCharEstimator() {
	ctx := context.Background()
	est := &vector.CharEstimator{}
	fmt.Println(est.Measure(ctx, "hello"))
	fmt.Println(est.Measure(ctx, "日本語"))
	// Output:
	// 5
	// 3
}

// ExampleHeuristicTokenEstimator shows the fast token estimator that uses
// len(text)/4 + 4. Typical error is ±15% compared to a real tokenizer.
func ExampleHeuristicTokenEstimator() {
	ctx := context.Background()
	est := &vector.HeuristicTokenEstimator{}
	fmt.Println(est.Measure(ctx, ""))           // 0/4 + 4 = 4
	fmt.Println(est.Measure(ctx, "hello world")) // 11/4 + 4 = 6
	// Output:
	// 4
	// 6
}

// ExampleNewOllamaTokenEstimator shows constructing an estimator that calls the
// Ollama tokenize API for exact token counts.
// No Output: because it makes HTTP requests to a local Ollama server.
func ExampleNewOllamaTokenEstimator() {
	est := vector.NewOllamaTokenEstimator("llama3.2")
	_ = est
}

// ExampleCosineSimilarity shows computing the dot-product similarity between
// two unit-length vectors — equivalent to cosine similarity for normalized vectors.
func ExampleCosineSimilarity() {
	a := []float32{1, 0, 0}
	b := []float32{1, 0, 0}
	fmt.Printf("%.1f\n", vector.CosineSimilarity(a, b)) // identical direction

	c := []float32{1, 0, 0}
	d := []float32{0, 1, 0}
	fmt.Printf("%.1f\n", vector.CosineSimilarity(c, d)) // orthogonal
	// Output:
	// 1.0
	// 0.0
}

// ExampleNormalize shows scaling a vector to unit length (L2 norm = 1).
func ExampleNormalize() {
	v := []float32{3, 4} // norm = 5
	n := vector.Normalize(v)
	fmt.Printf("%.1f\n", n[0]) // 3/5
	fmt.Printf("%.1f\n", n[1]) // 4/5
	// Output:
	// 0.6
	// 0.8
}

// ExampleNewInMemoryStore shows upserting a vector, counting entries, and
// searching for the nearest neighbour.
func ExampleNewInMemoryStore() {
	ctx := context.Background()
	store := vector.NewInMemoryStore()

	msg := goagent.AssistantMessage("vector databases store embeddings")
	vec := []float32{1, 0, 0} // unit-length vector

	if err := store.Upsert(ctx, "doc:0", vec, msg); err != nil {
		log.Fatal(err)
	}

	count, _ := store.Count(ctx)
	fmt.Println(count)

	results, err := store.Search(ctx, []float32{1, 0, 0}, 1)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(results[0].Message.TextContent())
	// Output:
	// 1
	// vector databases store embeddings
}

// ExampleNewFallbackEmbedder shows filtering out non-text blocks before
// delegating to the primary embedder, and counting skipped blocks.
func ExampleNewFallbackEmbedder() {
	skipped := 0
	emb := vector.NewFallbackEmbedder(
		&testutil.MockEmbedder{},
		vector.WithSupportedType(goagent.ContentText),
		vector.WithOnSkipped(func(_ goagent.ContentBlock) { skipped++ }),
	)

	blocks := []goagent.ContentBlock{
		goagent.TextBlock("embeddable text"),
		goagent.ImageBlock([]byte{0xFF}, "image/png"), // skipped — not ContentText
	}
	vec, err := emb.Embed(context.Background(), blocks)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(vec) > 0)
	fmt.Println(skipped)
	// Output:
	// true
	// 1
}
