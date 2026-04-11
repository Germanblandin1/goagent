// Command rag_docs_embedder is the same RAG demo as rag_docs but shows the
// BatchEmbedder optimization in action. Because OllamaEmbedder implements
// goagent.BatchEmbedder, rag.Pipeline.Index now embeds all chunks of a
// document in a single batch of concurrent HTTP calls to Ollama — replacing
// K serial round trips with ~max(embed_latency) per document.
//
// Prerequisites:
//
//	ollama pull nomic-embed-text   # embedding model
//	ollama pull llama3.2           # chat model (or any tool-capable model)
//
// Usage:
//
//	go run ./examples/rag_docs_embedder
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

	// 2. Embedder — OllamaEmbedder implements goagent.BatchEmbedder, so
	//    Pipeline.Index will call BatchEmbed instead of Embed per chunk.
	embedder := ollama.NewEmbedderWithClient(client,
		ollama.WithEmbedModel("nomic-embed-text"),
	)
	chunker := vector.NewTextChunker(
		vector.WithMaxSize(400),
		vector.WithOverlap(40),
	)
	store := vector.NewInMemoryStore()

	// 3. Pipeline — same API as rag_docs, but indexOne now takes the
	//    BatchEmbedder fast path: one BatchEmbed call per document.
	pipeline, err := rag.NewPipeline(chunker, embedder, store,
		rag.WithIndexObserver(func(
			_ context.Context,
			source string,
			chunked, embedded, skipped int,
			dur time.Duration,
			err error,
		) {
			if err != nil {
				slog.Error("rag index failed", "source", source, "err", err)
				return
			}
			slog.Debug("rag indexed (batch embed)",
				"source", source,
				"chunks", chunked,
				"embedded", embedded,
				"skipped", skipped,
				"dur", dur,
			)
		}),
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

	// 4. Index markdown docs from documentacion/ directory (if it exists).
	docs, err := loadMarkdownDocs("documentacion/")
	if err != nil {
		log.Fatal(err)
	}
	if len(docs) == 0 {
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
	start := time.Now()
	if err := pipeline.Index(ctx, docs...); err != nil {
		log.Fatal(err)
	}
	slog.Info("indexed documents", "count", len(docs), "dur", time.Since(start))

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

	// 6. Agent — shares the same OllamaClient as the embedder.
	provider := ollama.NewWithClient(client)
	agent, err := goagent.New(
		goagent.WithModel("qwen3.5:cloud"),
		goagent.WithProvider(provider),
		goagent.WithTool(searchTool),
		goagent.WithMaxIterations(40),
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
