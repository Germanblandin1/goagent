package vector_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func textContent(s string) vector.ChunkContent {
	return vector.ChunkContent{
		Blocks: []goagent.ContentBlock{goagent.TextBlock(s)},
	}
}

// ── NoOpChunker ──────────────────────────────────────────────────────────────

func TestNoOpChunker(t *testing.T) {
	c := vector.NewNoOpChunker()
	blocks := []goagent.ContentBlock{
		goagent.TextBlock("hello"),
		goagent.ImageBlock([]byte{0xFF}, "image/png"),
	}
	meta := map[string]any{"key": "val"}

	got, err := c.Chunk(context.Background(), vector.ChunkContent{
		Blocks:   blocks,
		Metadata: meta,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if len(got[0].Blocks) != 2 {
		t.Errorf("blocks len = %d, want 2", len(got[0].Blocks))
	}
	if got[0].Metadata["key"] != "val" {
		t.Errorf("metadata not preserved")
	}
}

// ── TextChunker ──────────────────────────────────────────────────────────────

func TestTextChunker_ShortText_SingleChunk(t *testing.T) {
	c := vector.NewTextChunker(vector.WithMaxSize(500))
	got, err := c.Chunk(context.Background(), textContent("hello world"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(got))
	}
	if got[0].Blocks[0].Text != "hello world" {
		t.Errorf("text = %q, want %q", got[0].Blocks[0].Text, "hello world")
	}
}

func TestTextChunker_NeverSplitsWords(t *testing.T) {
	// Build a sentence where every word is longer than 1 token.
	words := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel"}
	text := strings.Join(words, " ")

	// MaxSize=10 with heuristic means roughly 2-3 words per chunk.
	c := vector.NewTextChunker(
		vector.WithMaxSize(10),
		vector.WithOverlap(0),
		vector.WithEstimator(&vector.HeuristicTokenEstimator{}),
	)
	chunks, err := c.Chunk(context.Background(), textContent(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}

	for _, ch := range chunks {
		chunkText := ch.Blocks[0].Text
		chunkWords := strings.Fields(chunkText)
		for _, w := range chunkWords {
			found := false
			for _, orig := range words {
				if w == orig {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("chunk contains split word %q not in original words", w)
			}
		}
	}
}

func TestTextChunker_OverlapCorrect(t *testing.T) {
	// Build text with clearly distinct words.
	words := []string{"one", "two", "three", "four", "five", "six", "seven", "eight"}
	text := strings.Join(words, " ")

	c := vector.NewTextChunker(
		vector.WithMaxSize(12), // ~3 words
		vector.WithOverlap(4),  // ~1 word overlap
		vector.WithEstimator(&vector.HeuristicTokenEstimator{}),
	)
	chunks, err := c.Chunk(context.Background(), textContent(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}

	// The last word of chunk N should appear at the start of chunk N+1.
	for i := 0; i < len(chunks)-1; i++ {
		words1 := strings.Fields(chunks[i].Blocks[0].Text)
		words2 := strings.Fields(chunks[i+1].Blocks[0].Text)
		lastWordOfChunk := words1[len(words1)-1]
		found := false
		for _, w := range words2 {
			if w == lastWordOfChunk {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("chunk %d last word %q not found in chunk %d: %v",
				i, lastWordOfChunk, i+1, words2)
		}
	}
}

func TestTextChunker_EmptyBlocks(t *testing.T) {
	c := vector.NewTextChunker()
	got, err := c.Chunk(context.Background(), vector.ChunkContent{
		Blocks: []goagent.ContentBlock{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 chunks for empty blocks, got %d", len(got))
	}
}

func TestTextChunker_IgnoresNonTextBlocks(t *testing.T) {
	c := vector.NewTextChunker()
	content := vector.ChunkContent{
		Blocks: []goagent.ContentBlock{
			goagent.ImageBlock([]byte{0xFF}, "image/png"),
		},
	}
	got, err := c.Chunk(context.Background(), content)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 chunks for image-only content, got %d", len(got))
	}
}

func TestTextChunker_ChunkIndexMetadata(t *testing.T) {
	words := make([]string, 20)
	for i := range words {
		words[i] = "word"
	}
	text := strings.Join(words, " ")

	c := vector.NewTextChunker(
		vector.WithMaxSize(10),
		vector.WithOverlap(0),
	)
	chunks, err := c.Chunk(context.Background(), textContent(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, ch := range chunks {
		if ch.Metadata["chunk_index"] != i {
			t.Errorf("chunk[%d].chunk_index = %v, want %d", i, ch.Metadata["chunk_index"], i)
		}
		if ch.Metadata["chunk_total"] != len(chunks) {
			t.Errorf("chunk[%d].chunk_total = %v, want %d", i, ch.Metadata["chunk_total"], len(chunks))
		}
	}
}

// ── BlockChunker ─────────────────────────────────────────────────────────────

func TestBlockChunker_ImagePassesThrough(t *testing.T) {
	c := vector.NewBlockChunker()
	imgData := []byte{0xFF, 0xD8}
	content := vector.ChunkContent{
		Blocks: []goagent.ContentBlock{
			goagent.ImageBlock(imgData, "image/jpeg"),
		},
	}
	chunks, err := c.Chunk(context.Background(), content)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for image, got %d", len(chunks))
	}
	if chunks[0].Blocks[0].Type != goagent.ContentImage {
		t.Errorf("block type = %v, want ContentImage", chunks[0].Blocks[0].Type)
	}
}

func TestBlockChunker_DocumentWithoutExtractorPassesThrough(t *testing.T) {
	c := vector.NewBlockChunker() // no PDFExtractor
	content := vector.ChunkContent{
		Blocks: []goagent.ContentBlock{
			goagent.DocumentBlock([]byte("%PDF"), "application/pdf", "doc.pdf"),
		},
	}
	chunks, err := c.Chunk(context.Background(), content)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Blocks[0].Type != goagent.ContentDocument {
		t.Errorf("block type = %v, want ContentDocument", chunks[0].Blocks[0].Type)
	}
}


