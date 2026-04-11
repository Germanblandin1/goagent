// Command rag_batch_index is an interactive RAG chatbot that indexes Markdown
// documents from a local directory and answers questions about them. It shows
// the BatchEmbedder optimization in action: because OllamaEmbedder implements
// goagent.BatchEmbedder, rag.Pipeline.Index embeds all chunks of a document
// in a single batch of concurrent HTTP calls to Ollama — replacing K serial
// round trips with ~max(embed_latency) per document.
//
// The conversation is multi-turn: the agent remembers previous exchanges
// within a session. Type "exit" or "quit" to leave.
//
// Prerequisites:
//
//	ollama pull nomic-embed-text   # embedding model
//	ollama pull llama3.2           # chat model (or any tool-capable model)
//	docker run -d --name qdrant-goagent -p 6333:6333 -p 6334:6334 qdrant/qdrant:latest
//
// Usage:
//
//	go run ./examples/rag_batch_index
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector"
	goqdrant "github.com/Germanblandin1/goagent/memory/vector/qdrant"
	"github.com/Germanblandin1/goagent/providers/ollama"
	"github.com/Germanblandin1/goagent/rag"
	qdrantclient "github.com/qdrant/go-client/qdrant"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 1. Shared HTTP client — one connection pool for provider and embedder.
	client := ollama.NewClient()

	// 2. Embedder — OllamaEmbedder implements goagent.BatchEmbedder, so
	//    Pipeline.Index will call BatchEmbed instead of Embed per chunk.
	embedder := ollama.NewEmbedderWithClient(client,
		ollama.WithEmbedModel("nomic-embed-text"),
	)
	chunker := vector.NewRecursiveChunker(
		vector.WithRCMaxSize(400),
		vector.WithRCOverlap(40),
	)

	// 3. Qdrant vector store — gRPC port 6334.
	//    Run: docker run -d --name qdrant-goagent -p 6333:6333 -p 6334:6334 qdrant/qdrant:latest
	qclient, err := qdrantclient.NewClient(&qdrantclient.Config{
		Host: "localhost",
		Port: 6334,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer qclient.Close()

	const collectionName = "rag_batch_index"
	if err := goqdrant.CreateCollection(ctx, qclient, goqdrant.CollectionConfig{
		CollectionName: collectionName,
		Dims:           768, // nomic-embed-text
	}); err != nil {
		log.Fatal(err)
	}
	store, err := goqdrant.New(qclient, goqdrant.Config{CollectionName: collectionName})
	if err != nil {
		log.Fatal(err)
	}

	// 4. Pipeline — same API as rag_docs, but indexOne now takes the
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

	// 5. Index markdown docs from documentacion/ directory (if it exists).
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

	// 6. Tool
	searchTool := rag.NewTool(pipeline,
		rag.WithToolName("search_docs"),
		rag.WithToolDescription(
			"Search goagent's internal documentation and design documents. "+
				"Use this when asked about how goagent works, its architecture, "+
				"its API design, or implementation details.",
		),
		rag.WithTopK(3),
	)

	// 7. Agent — shares the same OllamaClient as the embedder.
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

	scanner := bufio.NewScanner(os.Stdin)

	fmt.Printf("RAG chatbot ready (%d docs indexed). Type your question, or \"exit\" to quit.\n\n", len(docs))

	for {
		fmt.Print("You: ")

		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "exit" || input == "quit" {
			break
		}

		reply, err := agent.Run(ctx, input)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			log.Printf("error: %v\n", err)
			continue
		}

		fmt.Printf("Bot: %s\n\n", reply)
	}

	fmt.Println("Goodbye!")
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
