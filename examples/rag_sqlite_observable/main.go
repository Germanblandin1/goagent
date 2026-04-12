// Command rag_sqlite_observable is an interactive RAG chatbot that indexes
// Markdown documents and demonstrates full observability across every layer of
// the stack:
//
//   - VectorStoreObserver — raw store timing: BulkUpsert latency, Search
//     results/topK ratio, 0-results detection, per-operation error logging.
//   - RAG IndexObserver — per-document stats: chunks produced, embedded, skipped.
//   - RAG SearchObserver — per-query stats: query string, result count, top score.
//   - Agent Hooks — agent lifecycle: tool calls, tool results, run duration.
//
// All four layers emit structured slog records. Running with -debug shows every
// event; without it only Info/Warn/Error are shown.
//
// The vector store is sqlite-vec, an embedded SQLite extension — no external
// server required. Data is persisted to ./rag_observable.db, so re-runs skip
// re-indexing when the file already exists and docs haven't changed.
//
// Prerequisites:
//
//	ollama pull nomic-embed-text   # embedding model (768-dim)
//	ollama pull llama3.2           # chat model (or any tool-capable model)
//
// Usage:
//
//	go run ./examples/rag_sqlite_observable           # Info-level logs
//	go run ./examples/rag_sqlite_observable -debug    # Debug-level logs (full observability)
package main

import (
	"bufio"
	"context"
	"flag"
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
	"github.com/Germanblandin1/goagent/memory/vector/sqlitevec"
	"github.com/Germanblandin1/goagent/providers/ollama"
	"github.com/Germanblandin1/goagent/rag"
)

const (
	// nomic-embed-text produces 768-dimensional vectors.
	embedDims = 768
	dbFile    = "rag_observable.db"
	tableName = "docs"
)

func main() {
	debug := flag.Bool("debug", false, "enable debug-level logs (shows all observability events)")
	flag.Parse()

	// ── Logging ──────────────────────────────────────────────────────────────
	logLevel := slog.LevelInfo
	if *debug {
		logLevel = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	})))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// ── 1. SQLite-vec store ───────────────────────────────────────────────────
	// Open (or create) a persistent SQLite database. Re-runs reuse indexed data.
	db, err := sqlitevec.Open(dbFile)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	migCfg := sqlitevec.MigrateConfig{
		TableName: tableName,
		Dims:      embedDims,
	}
	if err := sqlitevec.Migrate(ctx, db, migCfg); err != nil {
		log.Fatal(err)
	}

	rawStore, err := sqlitevec.New(db, sqlitevec.TableConfig{
		Table:          tableName,
		IDColumn:       "id",
		VectorColumn:   "embedding",
		TextColumn:     "content",
		MetadataColumn: "metadata",
	})
	if err != nil {
		log.Fatal(err)
	}

	// ── 2. VectorStoreObserver (Layer 1 of observability) ────────────────────
	// Fires after every raw store call: Upsert, Search, Delete, BulkUpsert, BulkDelete.
	// This is the lowest-level view: raw latency, 0-results detection, per-entry sizing.
	storeObs := goagent.VectorStoreObserver{
		OnBulkUpsert: func(_ context.Context, count int, dur time.Duration, err error) {
			if err != nil {
				slog.Error("vector bulk upsert failed", "count", count, "dur", dur, "err", err)
				return
			}
			perDoc := dur / time.Duration(max(count, 1))
			slog.Debug("vector bulk upsert",
				"count", count,
				"total_dur", dur,
				"per_doc", perDoc,
			)
		},
		OnSearch: func(_ context.Context, topK int, results int, dur time.Duration, err error) {
			if err != nil {
				slog.Error("vector search failed", "topK", topK, "dur", dur, "err", err)
				return
			}
			// KEY SIGNAL: 0 results means the store may be empty or the score
			// threshold is too aggressive — this is invisible without a hook.
			if results == 0 {
				slog.Warn("vector search returned 0 results",
					"topK", topK,
					"dur", dur,
				)
				return
			}
			slog.Debug("vector search",
				"topK", topK,
				"results", results,
				"dur", dur,
			)
		},
		OnUpsert: func(_ context.Context, id string, dur time.Duration, err error) {
			if err != nil {
				slog.Error("vector upsert failed", "id", id, "dur", dur, "err", err)
			} else {
				slog.Debug("vector upsert", "id", id, "dur", dur)
			}
		},
		OnDelete: func(_ context.Context, id string, dur time.Duration, err error) {
			if err != nil {
				slog.Error("vector delete failed", "id", id, "dur", dur, "err", err)
			}
		},
	}

	// Wrap the raw store — transparent to the RAG pipeline.
	// Because sqlitevec.Store implements BulkVectorStore, the wrapper also
	// implements it and OnBulkUpsert fires during pipeline.Index.
	store := goagent.NewObservableStore(rawStore, storeObs)

	// ── 3. Ollama components ──────────────────────────────────────────────────
	client := ollama.NewClient()
	embedder := ollama.NewEmbedderWithClient(client,
		ollama.WithEmbedModel("nomic-embed-text"),
	)
	chunker := vector.NewRecursiveChunker(
		vector.WithRCMaxSize(400),
		vector.WithRCOverlap(40),
	)

	// ── 4. RAG pipeline (Layers 2 and 3 of observability) ────────────────────
	pipeline, err := rag.NewPipeline(chunker, embedder, store,
		// IndexObserver: fires once per document after it is indexed.
		// Shows chunk breakdown (how many were produced, embedded, skipped).
		rag.WithIndexObserver(func(
			_ context.Context,
			source string,
			chunked, embedded, skipped int,
			dur time.Duration,
			err error,
		) {
			if err != nil {
				slog.Error("rag index failed",
					"source", source,
					"err", err,
				)
				return
			}
			slog.Debug("rag indexed",
				"source", source,
				"chunks", chunked,
				"embedded", embedded,
				"skipped", skipped,
				"dur", dur,
			)
		}),

		// SearchObserver: fires after each tool invocation (high-level view).
		// Complements the VectorStoreObserver: this one has the query string
		// and the full SearchResult slice; the store observer has raw latency.
		rag.WithSearchObserver(func(
			_ context.Context,
			query string,
			results []rag.SearchResult,
			dur time.Duration,
			err error,
		) {
			if err != nil {
				slog.Error("rag search failed",
					"query", query,
					"err", err,
				)
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
			// Low score signals a mismatch between query and indexed content.
			if topScore > 0 && topScore < 0.3 {
				slog.Warn("low quality retrieval — results may be irrelevant",
					"query", query,
					"top_score", topScore,
				)
			}
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	// ── 5. Index documents ────────────────────────────────────────────────────
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
						"The ReAct loop handles tool calls automatically. " +
						"VectorStore stores embeddings; BulkVectorStore enables efficient batch writes.",
				)},
			},
		}
	}

	indexStart := time.Now()
	if err := pipeline.Index(ctx, docs...); err != nil {
		log.Fatal(err)
	}
	slog.Info("indexing complete",
		"docs", len(docs),
		"db", dbFile,
		"dur", time.Since(indexStart),
	)

	// ── 6. Agent with hooks (Layer 4 of observability) ───────────────────────
	// Hooks fire at the agent loop level: tool invocations and overall run stats.
	hooks := goagent.Hooks{
		OnToolCall: func(_ context.Context, name string, args map[string]any) {
			slog.Debug("agent tool call", "tool", name, "args", args)
		},
		OnToolResult: func(_ context.Context, name string, _ []goagent.ContentBlock, dur time.Duration, err error) {
			if err != nil {
				slog.Error("agent tool failed", "tool", name, "dur", dur, "err", err)
				return
			}
			slog.Debug("agent tool result", "tool", name, "dur", dur)
		},
		OnRunEnd: func(_ context.Context, result goagent.RunResult) {
			if result.Err != nil {
				slog.Error("agent run failed", "err", result.Err, "dur", result.Duration)
				return
			}
			slog.Info("agent run complete",
				"iterations", result.Iterations,
				"tool_calls", result.ToolCalls,
				"input_tokens", result.TotalUsage.InputTokens,
				"output_tokens", result.TotalUsage.OutputTokens,
				"dur", result.Duration,
			)
		},
	}

	searchTool := rag.NewTool(pipeline,
		rag.WithToolName("search_docs"),
		rag.WithToolDescription(
			"Search the documentation. Use this to answer questions about goagent's "+
				"design, architecture, API, and implementation details.",
		),
		rag.WithTopK(4),
	)

	provider := ollama.NewWithClient(client)
	agent, err := goagent.New(
		goagent.WithModel("llama3.2"),
		goagent.WithProvider(provider),
		goagent.WithTool(searchTool),
		goagent.WithHooks(hooks),
		goagent.WithMaxIterations(20),
		goagent.WithSystemPrompt(
			"You are a helpful assistant for the goagent library. "+
				"Use search_docs to answer questions about the library.",
		),
	)
	if err != nil {
		log.Fatal(err)
	}

	// ── 7. Interactive loop ───────────────────────────────────────────────────
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Printf(
		"\nRAG chatbot ready (%d docs indexed, db: %s).\nUse -debug to see all observability events.\nType \"exit\" to quit.\n\n",
		len(docs), dbFile,
	)

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
			slog.Error("run failed", "err", err)
			continue
		}
		fmt.Printf("\nBot: %s\n\n", reply)
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
