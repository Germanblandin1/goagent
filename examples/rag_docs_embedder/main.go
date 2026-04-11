// Command rag_docs_embedder is the same RAG demo as rag_docs but stores
// documents via LongTermMemory instead of rag.Pipeline. Because
// OllamaEmbedder implements goagent.BatchEmbedder, LongTermMemory.Store
// embeds all chunks in a single batch of concurrent HTTP calls to Ollama —
// replacing N×K serial round trips with ~max(embed_latency).
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
	"github.com/Germanblandin1/goagent/memory"
	"github.com/Germanblandin1/goagent/memory/vector"
	"github.com/Germanblandin1/goagent/providers/ollama"
)

func main() {
	ctx := context.Background()

	// 1. Shared HTTP client — one connection pool for provider and embedder.
	client := ollama.NewClient()

	// 2. Embedder — OllamaEmbedder implements goagent.BatchEmbedder, so
	//    LongTermMemory.Store will call BatchEmbed instead of individual Embed.
	embedder := ollama.NewEmbedderWithClient(client,
		ollama.WithEmbedModel("nomic-embed-text"),
	)

	// 3. Long-term memory backed by an in-memory vector store.
	//    Store() detects BatchEmbedder at runtime via type assertion and uses
	//    the fast path automatically — no extra configuration needed.
	store := vector.NewInMemoryStore()
	ltm, err := memory.NewLongTerm(
		memory.WithVectorStore(store),
		memory.WithEmbedder(embedder),
		memory.WithChunker(vector.NewTextChunker(
			vector.WithMaxSize(400),
			vector.WithOverlap(40),
		)),
	)
	if err != nil {
		log.Fatal(err)
	}

	// 4. Load markdown docs (falls back to a built-in snippet when absent).
	docs, err := loadMarkdownDocs("documentacion/")
	if err != nil {
		log.Fatal(err)
	}
	if len(docs) == 0 {
		docs = []goagent.Message{
			goagent.UserMessage(
				"goagent is a Go-idiomatic framework for building AI agents with Claude. " +
					"Use WithTool to register tools. Use WithLongTermMemory for persistent context. " +
					"The ReAct loop handles tool calls automatically.",
			),
		}
	}

	// Store triggers the BatchEmbedder path: all chunks across all messages
	// are collected, embedded in a single concurrent batch, and BulkUpserted.
	start := time.Now()
	if err := ltm.Store(ctx, docs...); err != nil {
		log.Fatal(err)
	}
	slog.Info("indexed via BatchEmbedder", "docs", len(docs), "dur", time.Since(start))

	// 5. Search tool backed by LongTermMemory.Retrieve.
	searchTool := goagent.ToolFunc(
		"search_docs",
		"Search goagent's documentation and design documents. "+
			"Use this when asked about how goagent works, its architecture, "+
			"its API design, or implementation details.",
		goagent.SchemaFrom(struct {
			Query string `json:"query" jsonschema_description:"The search query."`
		}{}),
		func(ctx context.Context, args map[string]any) (string, error) {
			query, _ := args["query"].(string)
			results, err := ltm.Retrieve(ctx,
				[]goagent.ContentBlock{goagent.TextBlock(query)},
				3,
			)
			if err != nil {
				return "", fmt.Errorf("search failed: %w", err)
			}
			if len(results) == 0 {
				return "No relevant documents found.", nil
			}
			var sb strings.Builder
			for i, r := range results {
				fmt.Fprintf(&sb, "[%d] (score=%.2f) %s\n",
					i+1, r.Score, r.Message.TextContent())
			}
			return sb.String(), nil
		},
	)

	// 6. Agent — shares the same OllamaClient as the embedder.
	provider := ollama.NewWithClient(client)
	agent, err := goagent.New(
		goagent.WithModel("qwen3.5:cloud"),
		goagent.WithProvider(provider),
		goagent.WithTool(searchTool),
		goagent.WithMaxIterations(40),
		goagent.WithSystemPrompt(
			"You are a helpful assistant for the goagent library. " +
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

// loadMarkdownDocs reads all .md files from dir and returns them as Messages.
// Returns nil, nil when the directory does not exist.
func loadMarkdownDocs(dir string) ([]goagent.Message, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}
	var msgs []goagent.Message
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		msgs = append(msgs, goagent.UserMessage(string(data)))
	}
	return msgs, nil
}
