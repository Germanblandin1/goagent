package rag_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector"
	"github.com/Germanblandin1/goagent/rag"
)

func newIndexedPipeline(t *testing.T, docs ...rag.Document) *rag.Pipeline {
	t.Helper()
	p, err := rag.NewPipeline(
		vector.NewNoOpChunker(),
		&stubEmbedder{vec: []float32{1, 0}},
		vector.NewInMemoryStore(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) > 0 {
		if err := p.Index(context.Background(), docs...); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestRagTool_ExecuteReturnsText(t *testing.T) {
	t.Parallel()

	doc := rag.Document{Source: "guide.md", Content: []goagent.ContentBlock{goagent.TextBlock("Use WithTool to register tools.")}}
	tool := rag.NewTool(newIndexedPipeline(t, doc),
		rag.WithToolName("search"),
		rag.WithToolDescription("Search docs."),
	)

	blocks, err := tool.Execute(context.Background(), map[string]any{"query": "tools"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(blocks) == 0 {
		t.Fatal("expected at least one ContentBlock")
	}
	text := blocks[0].Text
	if !strings.Contains(text, "Relevant information") {
		t.Errorf("unexpected output: %q", text)
	}
}

func TestRagTool_ExecuteEmptyQuery(t *testing.T) {
	t.Parallel()

	tool := rag.NewTool(newIndexedPipeline(t))

	_, err := tool.Execute(context.Background(), map[string]any{"query": "   "})
	if err == nil {
		t.Fatal("expected error for empty query")
	}
	if !strings.Contains(err.Error(), "query") {
		t.Errorf("error message should mention 'query': %v", err)
	}
}

func TestRagTool_ExecuteNoResults(t *testing.T) {
	t.Parallel()

	// Empty pipeline — nothing indexed.
	tool := rag.NewTool(newIndexedPipeline(t))

	blocks, err := tool.Execute(context.Background(), map[string]any{"query": "anything"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(blocks) == 0 {
		t.Fatal("expected fallback ContentBlock")
	}
	if !strings.Contains(blocks[0].Text, "No relevant information") {
		t.Errorf("expected no-results message, got: %q", blocks[0].Text)
	}
}

func TestRagTool_MultimodalFormat(t *testing.T) {
	t.Parallel()

	doc := rag.Document{
		Source:  "img.md",
		Content: []goagent.ContentBlock{goagent.TextBlock("image content")},
	}
	tool := rag.NewTool(
		newIndexedPipeline(t, doc),
		rag.WithFormatter(rag.MultimodalFormat),
	)

	blocks, err := tool.Execute(context.Background(), map[string]any{"query": "image"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(blocks) == 0 {
		t.Fatal("expected blocks from MultimodalFormat")
	}
	// MultimodalFormat returns blocks directly, not wrapped in a header
	if strings.Contains(blocks[0].Text, "Relevant information found") {
		t.Error("MultimodalFormat should not add the header text")
	}
}

func TestRagTool_Definition(t *testing.T) {
	t.Parallel()

	tool := rag.NewTool(newIndexedPipeline(t),
		rag.WithToolName("find_docs"),
		rag.WithToolDescription("Find relevant documentation."),
	)

	def := tool.Definition()
	if def.Name != "find_docs" {
		t.Errorf("Name = %q, want find_docs", def.Name)
	}
	if def.Description != "Find relevant documentation." {
		t.Errorf("Description = %q, want 'Find relevant documentation.'", def.Description)
	}
	if def.Parameters == nil {
		t.Error("Parameters schema should not be nil")
	}
}

func TestNewTool_PanicsOnNilPipeline(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil pipeline")
		}
	}()
	rag.NewTool(nil)
}
