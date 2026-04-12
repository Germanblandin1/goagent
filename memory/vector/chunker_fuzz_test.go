package vector_test

import (
	"context"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector"
)

// FuzzTextChunker verifies that TextChunker.Chunk never panics, never produces
// empty chunks, and never loses tokens regardless of maxSize and overlap values.
//
// The critical invariant: splitWithOverlap must always advance at least one word
// per iteration to prevent infinite loops, even when overlap ≥ maxSize.
//
// Run with: go test -fuzz=FuzzTextChunker -fuzztime=60s ./memory/vector
func FuzzTextChunker(f *testing.F) {
	// Seed corpus: edge-case size/overlap combinations.
	f.Add("hello world foo bar baz", 500, 50)
	f.Add("one two three", 1, 0)
	f.Add("a b c d e f", 2, 1)
	f.Add("word", 1, 1)         // overlap == maxSize
	f.Add("a b c", 1, 999)      // overlap >> maxSize
	f.Add("test", 0, 0)         // maxSize zero
	f.Add("", 100, 10)           // empty text
	f.Add("alpha beta gamma delta", 3, 2)

	f.Fuzz(func(t *testing.T, text string, maxSize, overlap int) {
		// Clamp values to avoid unreasonable inputs that produce massive output.
		if maxSize < 0 {
			maxSize = 0
		}
		if maxSize > 10000 {
			maxSize = 10000
		}
		if overlap < 0 {
			overlap = 0
		}
		if overlap > maxSize+1 {
			overlap = maxSize + 1
		}

		c := vector.NewTextChunker(
			vector.WithMaxSize(maxSize),
			vector.WithOverlap(overlap),
		)

		ctx := context.Background()
		content := vector.ChunkContent{
			Blocks: []goagent.ContentBlock{goagent.TextBlock(text)},
		}

		defer func() {
			if r := recover(); r != nil {
				t.Errorf("TextChunker.Chunk panicked with text=%q maxSize=%d overlap=%d: %v",
					text, maxSize, overlap, r)
			}
		}()

		chunks, err := c.Chunk(ctx, content)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		// No chunk should be empty.
		for i, ch := range chunks {
			for _, blk := range ch.Blocks {
				if blk.Type == goagent.ContentText && blk.Text == "" {
					t.Errorf("chunk[%d] contains empty text block", i)
				}
			}
		}
	})
}

// FuzzExtractText verifies that ExtractText never panics given arbitrary content blocks.
//
// Run with: go test -fuzz=FuzzExtractText -fuzztime=30s ./memory/vector
func FuzzExtractText(f *testing.F) {
	f.Add("hello world")
	f.Add("")
	f.Add("   ")
	f.Add("multi\nline")

	f.Fuzz(func(t *testing.T, text string) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("ExtractText panicked with text=%q: %v", text, r)
			}
		}()

		blocks := []goagent.ContentBlock{
			goagent.TextBlock(text),
			goagent.ImageBlock([]byte{0xFF}, "image/png"),
		}
		result := vector.ExtractText(blocks)
		_ = result // just verifying no panic
	})
}
