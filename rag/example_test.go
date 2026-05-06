package rag_test

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/internal/testutil"
	"github.com/Germanblandin1/goagent/memory/vector"
	"github.com/Germanblandin1/goagent/rag"
)

// Example demonstrates the full RAG pipeline: index documents, then search.
// Uses an in-memory store and a deterministic mock embedder — no API key required.
func Example() {
	ctx := context.Background()
	embedder := &testutil.MockEmbedder{}
	store := vector.NewInMemoryStore()

	pipeline, err := rag.NewPipeline(vector.NewTextChunker(), embedder, store)
	if err != nil {
		log.Fatal(err)
	}

	docs := []rag.Document{
		{
			Source:  "go-spec.md",
			Content: []goagent.ContentBlock{goagent.TextBlock("Go is a statically typed, compiled language.")},
		},
		{
			Source:  "go-tour.md",
			Content: []goagent.ContentBlock{goagent.TextBlock("The Go programming language is open source.")},
		},
	}
	if err := pipeline.Index(ctx, docs...); err != nil {
		log.Fatal(err)
	}

	results, err := pipeline.Search(ctx, "Go programming language", 2)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(results))
	// Output: 2
}

// ExampleNewPipeline shows constructing a Pipeline with a chunker, an embedder,
// a vector store, and an optional search observer for logging and tracing.
// No Output: because the components shown here depend on external services in
// production; this example verifies the API compiles correctly.
func ExampleNewPipeline() {
	embedder := &testutil.MockEmbedder{}
	store := vector.NewInMemoryStore()

	p, err := rag.NewPipeline(
		vector.NewTextChunker(vector.WithMaxSize(200)),
		embedder,
		store,
		rag.WithSearchObserver(func(_ context.Context, _ string, _ []rag.SearchResult, _ time.Duration, _ error) {
		}),
	)
	if err != nil {
		log.Fatal(err)
	}
	_ = p
}

// ExamplePipeline_Index shows indexing a document and verifying it is searchable.
func ExamplePipeline_Index() {
	ctx := context.Background()

	p, err := rag.NewPipeline(
		vector.NewNoOpChunker(),
		&testutil.MockEmbedder{},
		vector.NewInMemoryStore(),
	)
	if err != nil {
		log.Fatal(err)
	}

	err = p.Index(ctx, rag.Document{
		Source:  "readme.md",
		Content: []goagent.ContentBlock{goagent.TextBlock("goagent is a Go framework for AI agents.")},
	})
	if err != nil {
		log.Fatal(err)
	}

	results, err := p.Search(ctx, "AI framework", 1)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(results))
	fmt.Println(results[0].Source)
	// Output:
	// 1
	// readme.md
}

// ExamplePipeline_Search shows how a SearchObserver receives the query for every
// Search call, enabling logging or tracing without modifying the pipeline.
func ExamplePipeline_Search() {
	ctx := context.Background()
	var searched []string

	p, err := rag.NewPipeline(
		vector.NewNoOpChunker(),
		&testutil.MockEmbedder{},
		vector.NewInMemoryStore(),
		rag.WithSearchObserver(func(_ context.Context, query string, _ []rag.SearchResult, _ time.Duration, _ error) {
			searched = append(searched, query)
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	_ = p.Index(ctx, rag.Document{
		Source:  "doc.md",
		Content: []goagent.ContentBlock{goagent.TextBlock("Go concurrency via goroutines and channels.")},
	})

	_, _ = p.Search(ctx, "concurrency", 3)
	fmt.Println(searched[0])
	// Output: concurrency
}

// ExampleNewTool shows wrapping a Pipeline as a goagent.Tool so an agent can
// invoke RAG searches during its ReAct loop.
func ExampleNewTool() {
	ctx := context.Background()

	p, err := rag.NewPipeline(
		vector.NewNoOpChunker(),
		&testutil.MockEmbedder{},
		vector.NewInMemoryStore(),
	)
	if err != nil {
		log.Fatal(err)
	}
	_ = p.Index(ctx, rag.Document{
		Source:  "manual.md",
		Content: []goagent.ContentBlock{goagent.TextBlock("How to configure goagent.")},
	})

	tool := rag.NewTool(p,
		rag.WithToolName("search_docs"),
		rag.WithToolDescription("Search the goagent documentation."),
		rag.WithTopK(3),
	)
	def := tool.Definition()
	fmt.Println(def.Name)
	fmt.Println(def.Description)
	// Output:
	// search_docs
	// Search the goagent documentation.
}

// ExampleMultimodalFormat shows how MultimodalFormat returns ContentBlocks directly
// instead of serialising them as text. Useful for image corpora with vision-capable providers.
func ExampleMultimodalFormat() {
	results := []rag.SearchResult{
		{
			Message: goagent.Message{
				Role:    goagent.RoleDocument,
				Content: []goagent.ContentBlock{goagent.TextBlock("Go interfaces are powerful.")},
			},
			Score:  0.92,
			Source: "interfaces.md",
		},
	}
	blocks := rag.MultimodalFormat(results)
	fmt.Println(len(blocks))
	fmt.Println(blocks[0].Text)
	// Output:
	// 1
	// Go interfaces are powerful.
}
