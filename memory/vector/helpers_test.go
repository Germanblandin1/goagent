package vector_test

import (
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector"
)

// ── ExtractText ──────────────────────────────────────────────────────────────

func TestExtractText_OnlyTextBlocks(t *testing.T) {
	t.Parallel()

	blocks := []goagent.ContentBlock{
		goagent.TextBlock("hello"),
		goagent.TextBlock("world"),
	}
	got := vector.ExtractText(blocks)
	want := "hello world"
	if got != want {
		t.Errorf("ExtractText = %q, want %q", got, want)
	}
}

func TestExtractText_SkipsNonTextBlocks(t *testing.T) {
	t.Parallel()

	blocks := []goagent.ContentBlock{
		goagent.TextBlock("text"),
		goagent.ImageBlock([]byte{0xFF}, "image/png"),
	}
	got := vector.ExtractText(blocks)
	if got != "text" {
		t.Errorf("ExtractText = %q, want %q", got, "text")
	}
}

func TestExtractText_Empty(t *testing.T) {
	t.Parallel()

	if got := vector.ExtractText(nil); got != "" {
		t.Errorf("ExtractText(nil) = %q, want %q", got, "")
	}
	if got := vector.ExtractText([]goagent.ContentBlock{}); got != "" {
		t.Errorf("ExtractText([]) = %q, want %q", got, "")
	}
}

func TestExtractText_WhitespaceOnlySkipped(t *testing.T) {
	t.Parallel()

	blocks := []goagent.ContentBlock{
		goagent.TextBlock("   "),
		goagent.TextBlock("real"),
	}
	got := vector.ExtractText(blocks)
	if got != "real" {
		t.Errorf("ExtractText = %q, want %q", got, "real")
	}
}

// ── ChunkToMessage ───────────────────────────────────────────────────────────

func TestChunkToMessage_PreservesRoleAndToolCallID(t *testing.T) {
	t.Parallel()

	orig := goagent.Message{
		Role:       goagent.RoleUser,
		ToolCallID: "tc1",
		Content:    []goagent.ContentBlock{goagent.TextBlock("original")},
	}
	chunk := vector.ChunkResult{
		Blocks:   []goagent.ContentBlock{goagent.TextBlock("chunk text")},
		Metadata: map[string]any{"chunk_index": 0},
	}

	got := vector.ChunkToMessage(orig, chunk)

	if got.Role != orig.Role {
		t.Errorf("Role = %v, want %v", got.Role, orig.Role)
	}
	if got.ToolCallID != orig.ToolCallID {
		t.Errorf("ToolCallID = %q, want %q", got.ToolCallID, orig.ToolCallID)
	}
	if len(got.Content) != 1 || got.Content[0].Text != "chunk text" {
		t.Errorf("Content = %v, want chunk text", got.Content)
	}
	// Metadata is NOT copied to the message — it lives in ChunkResult only.
	if len(got.Metadata) != 0 {
		t.Errorf("Metadata should be empty, got %v", got.Metadata)
	}
}

func TestChunkToMessage_ReplacesContent(t *testing.T) {
	t.Parallel()

	orig := goagent.Message{
		Role:    goagent.RoleAssistant,
		Content: []goagent.ContentBlock{goagent.TextBlock("old content")},
	}
	newBlocks := []goagent.ContentBlock{
		goagent.TextBlock("part1"),
		goagent.TextBlock("part2"),
	}
	chunk := vector.ChunkResult{Blocks: newBlocks}

	got := vector.ChunkToMessage(orig, chunk)

	if len(got.Content) != len(newBlocks) {
		t.Errorf("len(Content) = %d, want %d", len(got.Content), len(newBlocks))
	}
}
