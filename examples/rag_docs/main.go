// Command rag_docs demonstrates a RAG pipeline that indexes local markdown
// files and makes them searchable via an agent tool, using a local Ollama
// instance for both the chat model and the embedding model.
//
// Prerequisites:
//
//	ollama pull nomic-embed-text   # embedding model
//	ollama pull llama3.2           # chat model (or any tool-capable model)
//
// Usage:
//
//	go run ./examples/rag_docs
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector"
	"github.com/Germanblandin1/goagent/providers/ollama"
	"github.com/Germanblandin1/goagent/rag"
)

func main() {
	ctx := context.Background()

	// 1. Shared HTTP client — one connection pool for provider and embedder.
	client := ollama.NewClient()

	// 2. Components
	embedder := ollama.NewEmbedderWithClient(client,
		ollama.WithEmbedModel("nomic-embed-text"),
	)
	chunker := vector.NewTextChunker(
		vector.WithMaxSize(400),
		vector.WithOverlap(40),
	)
	store := vector.NewInMemoryStore()

	// 3. Pipeline with observer — logging inline
	pipeline, err := rag.NewPipeline(chunker, embedder, store,
		rag.WithSearchObserver(func(
			_ context.Context,
			query string,
			results []rag.SearchResult,
			dur time.Duration,
			err error,
		) {
			if err != nil {
				slog.Error("rag search failed", "query", query, "err", err)
				return
			}
			topScore := 0.0
			if len(results) > 0 {
				topScore = results[0].Score
			}
			slog.Debug("rag search",
				"query", query,
				"results", len(results),
				"top_score", topScore,
				"dur", dur,
			)
			if topScore < 0.5 {
				slog.Warn("low quality retrieval", "query", query, "top_score", topScore)
			}
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	// 4. Index markdown docs from documentacion/ directory (if it exists)
	docs, err := loadMarkdownDocs("documentacion/")
	if err != nil {
		log.Fatal(err)
	}
	if len(docs) == 0 {
		// Create a small in-memory doc for demo purposes
		docs = []rag.Document{
			{
				Source: "goagent-overview.md",
				Content: []goagent.ContentBlock{goagent.TextBlock(
					"goagent is a Go-idiomatic framework for building AI agents with Claude. " +
						"Use WithTool to register tools. Use WithLongTermMemory for persistent context. " +
						"The ReAct loop handles tool calls automatically.",
				)},
			},
		}
	}
	if err := pipeline.Index(ctx, docs...); err != nil {
		log.Fatal(err)
	}
	slog.Info("indexed documents", "count", len(docs))

	// 5. Tool
	searchTool := rag.NewTool(pipeline,
		rag.WithToolName("search_docs"),
		rag.WithToolDescription(
			"Search goagent's internal documentation and design documents. "+
				"Use this when asked about how goagent works, its architecture, "+
				"its API design, or implementation details.",
		),
		rag.WithTopK(3),
	)

	// 6. Agent — shares the same OllamaClient as the embedder
	provider := ollama.NewWithClient(client,
		ollama.WithModel("qwen3.5:cloud"),
	)
	agent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithTool(searchTool),
		goagent.WithSystemPrompt(
			"You are a helpful assistant for the goagent library. "+
				"Use search_docs to answer questions about the library's design and API.",
		),
	)
	if err != nil {
		log.Fatal(err)
	}

	resp, err := agent.Run(ctx, "¿Cómo implemento una tool personalizada en goagent?")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(resp)
}

// loadMarkdownDocs reads all .md files from dir and returns them as Documents.
// Returns nil, nil when the directory does not exist.
func loadMarkdownDocs(dir string) ([]rag.Document, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}
	var docs []rag.Document
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		docs = append(docs, rag.Document{
			Source:  path,
			Content: []goagent.ContentBlock{goagent.TextBlock(string(data))},
		})
	}
	return docs, nil
}
