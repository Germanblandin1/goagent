package vector_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector"
)

// ── SentenceChunker ──────────────────────────────────────────────────────────

// charEst is a deterministic SizeEstimator that returns the byte length of the
// text, making chunk sizes predictable in tests without relying on the heuristic.
type charEst struct{}

func (charEst) Measure(_ context.Context, text string) int { return len(text) }

func TestSentenceChunker_SingleShortText(t *testing.T) {
	c := vector.NewSentenceChunker(
		vector.WithSentenceMaxSize(200),
		vector.WithSentenceEstimator(charEst{}),
	)
	got, err := c.Chunk(context.Background(), textContent("Hello world."))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	// Single chunk: no chunk_index / chunk_total added.
	if _, ok := got[0].Metadata["chunk_index"]; ok {
		t.Error("single chunk must not have chunk_index in metadata")
	}
	if _, ok := got[0].Metadata["chunk_total"]; ok {
		t.Error("single chunk must not have chunk_total in metadata")
	}
}

func TestSentenceChunker_MultipleSentencesWithinLimit(t *testing.T) {
	text := "First sentence. Second sentence. Third sentence."
	c := vector.NewSentenceChunker(
		vector.WithSentenceMaxSize(200),
		vector.WithSentenceEstimator(charEst{}),
	)
	got, err := c.Chunk(context.Background(), textContent(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (all sentences fit)", len(got))
	}
}

func TestSentenceChunker_SplitsAtSentenceBoundary(t *testing.T) {
	// Three sentences. MaxSize=26 fits one sentence at a time (each ~20-25 chars).
	// "The quick brown fox."    = 20 bytes
	// "Jumped over the dog."   = 20 bytes
	// "And ran away fast."     = 18 bytes
	text := "The quick brown fox. Jumped over the dog. And ran away fast."
	c := vector.NewSentenceChunker(
		vector.WithSentenceMaxSize(26),
		vector.WithSentenceOverlap(0),
		vector.WithSentenceEstimator(charEst{}),
	)
	chunks, err := c.Chunk(context.Background(), textContent(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 3 {
		t.Fatalf("want 3 chunks, got %d", len(chunks))
	}
	// Verify no chunk contains a partial sentence by checking each chunk ends with
	// a sentence-terminating character or is the last chunk.
	for i, ch := range chunks {
		ct := ch.Blocks[0].Text
		trimmed := strings.TrimSpace(ct)
		last := trimmed[len(trimmed)-1]
		if last != '.' && last != '!' && last != '?' {
			t.Errorf("chunk[%d] %q does not end with sentence punctuation", i, ct)
		}
	}
}

func TestSentenceChunker_OverlapOneSentence(t *testing.T) {
	// Four sentences of ~15-16 bytes each. MaxSize=32 fits exactly two.
	// "First sentence."  = 15
	// "Second sentence." = 16
	// "Third sentence."  = 15
	// "Fourth sentence." = 16
	text := "First sentence. Second sentence. Third sentence. Fourth sentence."
	c := vector.NewSentenceChunker(
		vector.WithSentenceMaxSize(32),
		vector.WithSentenceOverlap(1),
		vector.WithSentenceEstimator(charEst{}),
	)
	chunks, err := c.Chunk(context.Background(), textContent(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("want at least 2 chunks, got %d", len(chunks))
	}
	// Last sentence of chunk N must appear in chunk N+1.
	for i := 0; i < len(chunks)-1; i++ {
		sentences1 := strings.Split(chunks[i].Blocks[0].Text, ". ")
		last := strings.TrimSpace(sentences1[len(sentences1)-1])
		if !strings.Contains(chunks[i+1].Blocks[0].Text, last) {
			t.Errorf("chunk[%d] last sentence %q not found in chunk[%d]: %q",
				i, last, i+1, chunks[i+1].Blocks[0].Text)
		}
	}
}

func TestSentenceChunker_OverlapZeroNoRepeat(t *testing.T) {
	text := "First sentence. Second sentence. Third sentence. Fourth sentence."
	c := vector.NewSentenceChunker(
		vector.WithSentenceMaxSize(32),
		vector.WithSentenceOverlap(0),
		vector.WithSentenceEstimator(charEst{}),
	)
	chunks, err := c.Chunk(context.Background(), textContent(text))
	if err != nil {
		t.Fatal(err)
	}

	// Collect all sentences per chunk and ensure none appears in two chunks.
	seen := map[string]int{}
	for i, ch := range chunks {
		for _, s := range strings.Split(ch.Blocks[0].Text, ". ") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if prev, ok := seen[s]; ok {
				t.Errorf("sentence %q appears in both chunk[%d] and chunk[%d]", s, prev, i)
			}
			seen[s] = i
		}
	}
}

func TestSentenceChunker_SingleLongSentenceIsNotDropped(t *testing.T) {
	long := strings.Repeat("word ", 50) // ~250 bytes, exceeds MaxSize=30
	long = strings.TrimSpace(long) + "."
	c := vector.NewSentenceChunker(
		vector.WithSentenceMaxSize(30),
		vector.WithSentenceOverlap(0),
		vector.WithSentenceEstimator(charEst{}),
	)
	got, err := c.Chunk(context.Background(), textContent(long))
	if err != nil {
		t.Fatal(err)
	}
	// The sentence exceeds MaxSize but must still be emitted as its own chunk.
	if len(got) != 1 {
		t.Fatalf("want 1 chunk for oversized single sentence, got %d", len(got))
	}
	if got[0].Blocks[0].Text != long {
		t.Errorf("oversized sentence was truncated or modified")
	}
}

func TestSentenceChunker_EmptyContent(t *testing.T) {
	c := vector.NewSentenceChunker()
	got, err := c.Chunk(context.Background(), vector.ChunkContent{
		Blocks: []goagent.ContentBlock{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("want 0 chunks for empty content, got %d", len(got))
	}
}

func TestSentenceChunker_IgnoresNonTextBlocks(t *testing.T) {
	c := vector.NewSentenceChunker()
	got, err := c.Chunk(context.Background(), vector.ChunkContent{
		Blocks: []goagent.ContentBlock{
			goagent.ImageBlock([]byte{0xFF}, "image/png"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("want 0 chunks for image-only content, got %d", len(got))
	}
}

func TestSentenceChunker_ChunkIndexMetadata(t *testing.T) {
	text := "First sentence. Second sentence. Third sentence. Fourth sentence."
	c := vector.NewSentenceChunker(
		vector.WithSentenceMaxSize(32),
		vector.WithSentenceOverlap(0),
		vector.WithSentenceEstimator(charEst{}),
	)
	chunks, err := c.Chunk(context.Background(), textContent(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("need multiple chunks to verify metadata, got %d", len(chunks))
	}
	total := len(chunks)
	for i, ch := range chunks {
		if ch.Metadata["chunk_index"] != i {
			t.Errorf("chunk[%d].chunk_index = %v, want %d", i, ch.Metadata["chunk_index"], i)
		}
		if ch.Metadata["chunk_total"] != total {
			t.Errorf("chunk[%d].chunk_total = %v, want %d", i, ch.Metadata["chunk_total"], total)
		}
	}
}

func TestSentenceChunker_MetadataIsNotMutated(t *testing.T) {
	original := map[string]any{"source": "doc.txt"}
	text := "First sentence. Second sentence. Third sentence. Fourth sentence."
	c := vector.NewSentenceChunker(
		vector.WithSentenceMaxSize(32),
		vector.WithSentenceOverlap(0),
		vector.WithSentenceEstimator(charEst{}),
	)
	_, err := c.Chunk(context.Background(), vector.ChunkContent{
		Blocks:   []goagent.ContentBlock{goagent.TextBlock(text)},
		Metadata: original,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Chunk adds chunk_index/chunk_total to copies, not to the original map.
	if _, ok := original["chunk_index"]; ok {
		t.Error("original metadata was mutated: chunk_index added")
	}
}

func TestSentenceChunker_ParagraphBreakSplits(t *testing.T) {
	// \n\n should also trigger a sentence boundary.
	text := "First paragraph.\n\nSecond paragraph."
	c := vector.NewSentenceChunker(
		vector.WithSentenceMaxSize(18),
		vector.WithSentenceOverlap(0),
		vector.WithSentenceEstimator(charEst{}),
	)
	// "First paragraph." = 16 bytes ≤ 18; "Second paragraph." = 17 bytes.
	// 16+17 = 33 > 18 → two chunks.
	chunks, err := c.Chunk(context.Background(), textContent(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 {
		t.Fatalf("want 2 chunks split at paragraph break, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0].Blocks[0].Text, "First paragraph") {
		t.Errorf("chunk[0] = %q, want 'First paragraph'", chunks[0].Blocks[0].Text)
	}
	if !strings.Contains(chunks[1].Blocks[0].Text, "Second paragraph") {
		t.Errorf("chunk[1] = %q, want 'Second paragraph'", chunks[1].Blocks[0].Text)
	}
}
