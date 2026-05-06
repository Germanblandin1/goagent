# goagent

> ⚠️ **Work in Progress** — API may change without notice. Not production-ready yet.

A minimal, Go-idiomatic framework for building AI agents with a [ReAct](https://arxiv.org/abs/2210.03629) loop and pluggable model providers.

![Go version](https://img.shields.io/badge/go-1.26%2B-blue)
![License](https://img.shields.io/badge/license-Apache%202.0-blue)

## Install

```bash
go get github.com/Germanblandin1/goagent
```

## Sub-modules

Each sub-module is versioned and installed independently.

| Module | Description |
|--------|-------------|
| [orchestration](orchestration/) | Multi-agent coordination — Pipeline, Graph, ParallelGroup, Supervisor |
| [providers/anthropic](providers/anthropic/README.md) | Anthropic Messages API provider (Claude) |
| [providers/ollama](providers/ollama/README.md) | Local Ollama provider + embedder |
| [providers/voyage](providers/voyage/README.md) | Voyage AI embedder |
| [mcp](mcp/README.md) | MCP client + server integration |
| [rag](rag/README.md) | RAG pipeline — chunking, embedding, retrieval |
| [otel](otel/README.md) | OpenTelemetry spans and metrics |
| [ratelimit](ratelimit/README.md) | Token-bucket rate limiters for tool dispatch |
| [memory/vector/pgvector](memory/vector/pgvector/README.md) | Persistent VectorStore — PostgreSQL + pgvector |
| [memory/vector/qdrant](memory/vector/qdrant/README.md) | Persistent VectorStore — Qdrant |
| [memory/vector/sqlitevec](memory/vector/sqlitevec/README.md) | Persistent VectorStore — SQLite + sqlite-vec (CGO) |
| [memory/vector/tiktoken](memory/vector/tiktoken/README.md) | Exact token-count SizeEstimator via tiktoken |

## Quickstart

```go
agent, err := goagent.New(
    goagent.WithProvider(ollama.New()),
    goagent.WithModel("qwen3"),
)
if err != nil {
    log.Fatal(err)
}

answer, err := agent.Run(context.Background(), "What is the capital of France?")
```

## Package layout

```
goagent/              Core — Agent, ReAct loop, interfaces
├── mcp/              MCP client + server (stdio and SSE transports)
├── memory/           Short-term and long-term memory
│   ├── storage/      InMemory storage
│   ├── policy/       FixedWindow, TokenWindow, NoOp
│   └── vector/       VectorStore, chunkers, similarity, size estimators
│       ├── pgvector/ Persistent VectorStore — PostgreSQL + pgvector (HNSW)
│       └── sqlitevec/ Persistent VectorStore — SQLite + sqlite-vec (CGO)
├── orchestration/    Multi-agent coordination — Pipeline, Graph, ParallelGroup, Supervisor
├── providers/
│   ├── anthropic/    Anthropic Messages API (Claude)
│   ├── ollama/       Local Ollama via OpenAI-compatible API (+ embedder)
│   └── voyage/       Voyage AI embedder
├── rag/              RAG pipeline — Pipeline, NewTool, observers, formatters
├── examples/
│   ├── calculator/              Tool use with arithmetic
│   ├── chatbot/                 Multi-turn conversation
│   ├── chatbot-persistent/      Persistent memory across sessions
│   ├── chatbot-mcp-fs/          Filesystem access via MCP stdio
│   ├── graph-conditional-parallel/ Graph with in-node conditional parallelism
│   ├── graph-loop-judge/        Judge-loop pattern with a Graph
│   ├── graph-nested/            Nested Pipeline inside a Graph node
│   ├── multi-agent/             Supervisor coordinating worker agents
│   ├── rag_batch_index/         Interactive RAG chatbot — BatchEmbedder + Qdrant
│   └── streaming/               Real-time token streaming — text and tool-call paths
└── internal/testutil/           Shared mocks
```

## Core concepts

### ReAct loop

`Agent.Run` alternates between calling the model and executing tool calls until the model produces a final answer, the context is cancelled, or the iteration budget is exhausted. All tool calls within a single turn are dispatched concurrently.

```
                    ┌──────────────────────┐
                    │        prompt        │
                    └──────────┬───────────┘
                               │
                    ┌──────────▼───────────┐
             ┌─────▶│        model         │◀── OnIterationStart
             │      └──────────┬───────────┘
             │                 │
             │      ┌──────────▼───────────────────────┐
             │      │    response has tool calls?       │
             │      └──────────┬────────────┬───────────┘
             │                yes           no
             │      ┌──────────▼──────┐  ┌──▼─────────────┐
             │      │ dispatch tools  │  │     answer      │
             │      │  (concurrent)   │  │    (return)     │
             │      │  OnToolCall     │  └────────────────-┘
             │      │  OnToolResult   │       OnResponse
             │      └──────────┬──────┘
             │                 │
             └─────────────────┘  (next iteration)
```

### Tools

Implement the `Tool` interface or use the `ToolFunc` helper. Use `SchemaFrom` to derive the JSON Schema from a struct instead of building the map by hand:

```go
echo := goagent.ToolFunc("echo", "Returns the input text.",
    goagent.SchemaFrom(struct {
        Text string `json:"text" jsonschema_description:"Text to echo back."`
    }{}),
    func(_ context.Context, args map[string]any) (string, error) {
        return args["text"].(string), nil
    },
)

agent, _ := goagent.New(
    goagent.WithProvider(ollama.New()),
    goagent.WithModel("qwen3"),
    goagent.WithTool(echo),
)
```

`SchemaFrom` supports four struct tags:

| Tag | Effect |
|---|---|
| `json:"name"` | Property name; `"-"` skips the field |
| `json:"name,omitempty"` | Optional field (omitted from `"required"`) |
| `jsonschema_description:"text"` | Adds `"description"` to the property |
| `jsonschema_enum:"a,b,c"` | Adds `"enum"` with the comma-separated values |

### MCP (Model Context Protocol)

Connect external tool servers over stdio or SSE. The agent discovers all tools automatically at startup.

```go
// Spawn a local MCP server as a subprocess
agent, err := goagent.New(
    goagent.WithProvider(ollama.New()),
    goagent.WithModel("qwen3"),
    mcp.WithStdio("./my-mcp-server", "--flag"),
)
if err != nil {
    log.Fatal(err) // connection or discovery error
}
defer agent.Close()

// Connect to a running HTTP+SSE server
agent, err := goagent.New(
    goagent.WithProvider(anthropic.New()),
    mcp.WithSSE("http://localhost:8080/sse"),
)
```

Multiple servers can be attached in a single `New` call. Build your own MCP server with `mcp.NewServer`:

```go
s := mcp.NewServer("my-tools", "1.0.0")
s.MustAddTool("echo", "Returns the input unchanged",
    goagent.SchemaFrom(struct {
        Text string `json:"text" jsonschema_description:"Text to echo back."`
    }{}),
    func(_ context.Context, args map[string]any) (string, error) {
        return args["text"].(string), nil
    },
)
log.Fatal(s.ServeStdio())
```

### Memory

```go
mem := memory.NewShortTerm(
    memory.WithStorage(storage.NewInMemory()),
    memory.WithPolicy(policy.NewFixedWindow(20)),
)

agent, _ := goagent.New(
    goagent.WithProvider(ollama.New()),
    goagent.WithShortTermMemory(mem),
)
```

Available policies: `NewNoOp()`, `NewFixedWindow(n)` (last *n* groups), `NewTokenWindow(maxTokens)` (most recent groups within a token budget). Both window policies treat an `assistant+tool_use` message and all its `tool_result` replies as a single atomic group, so the tool-call invariant is preserved at every window boundary regardless of where the cut falls.

Long-term memory enables semantic retrieval across sessions. It requires a `VectorStore` and an `Embedder`; both are provided by the framework:

```go
store := vector.NewInMemoryStore()

embedder := ollama.NewEmbedder(
    ollama.WithEmbedModel("nomic-embed-text"),
)
// or: voyage.NewEmbedder(voyage.WithEmbedModel("voyage-3"))

ltm, err := memory.NewLongTerm(
    memory.WithVectorStore(store),
    memory.WithEmbedder(embedder),
)

agent, _ := goagent.New(
    goagent.WithName("my-agent"),         // session namespace
    goagent.WithProvider(ollama.New()),
    goagent.WithShortTermMemory(mem),
    goagent.WithLongTermMemory(ltm),
)
```

For long documents, plug in a chunker before embedding:

```go
ltm, err := memory.NewLongTerm(
    memory.WithVectorStore(store),
    memory.WithEmbedder(embedder),
    memory.WithChunker(vector.NewTextChunker(
        vector.WithMaxSize(500),
        vector.WithOverlap(50),
    )),
)
```

Available chunkers:

| Chunker | When to use |
|---------|-------------|
| `NewNoOpChunker()` | Conversational messages (default) |
| `NewTextChunker(...)` | Long text blocks; word-boundary splits with configurable max size and overlap |
| `NewSentenceChunker(...)` | Narrative or prose text; overlap counted in complete sentences |
| `NewRecursiveChunker(...)` | Markdown and structured docs; respects `\n\n` → `\n` → sentence → word hierarchy |
| `NewBlockChunker(...)` | Mixed multimodal content; text chunked, images pass through, PDFs split by page |
| `NewPageChunker(...)` | PDF-only per-page chunking |

### Persistent vector stores

`vector.NewInMemoryStore()` is the default and is suitable for development and single-process use. For production deployments that require durability or shared access across processes, two persistent backends are available as separate sub-modules.

#### PostgreSQL + pgvector

```bash
go get github.com/Germanblandin1/goagent/memory/vector/pgvector
```

```go
import (
    "database/sql"
    _ "github.com/jackc/pgx/v5/stdlib"
    "github.com/Germanblandin1/goagent/memory/vector/pgvector"
)

db, _ := sql.Open("pgx", "postgres://user:pass@localhost/mydb")

// Optional: create the extension, table, and HNSW index automatically.
pgvector.Migrate(ctx, db, pgvector.MigrateConfig{
    TableName: "goagent_embeddings",
    Dims:      1024, // must match your embedding model
})

store, err := pgvector.New(db, pgvector.TableConfig{
    Table:          "goagent_embeddings",
    IDColumn:       "id",
    VectorColumn:   "embedding",
    TextColumn:     "content",
    MetadataColumn: "metadata", // optional JSONB column
})
```

`TableConfig` describes the caller's existing table — the package imposes no schema. Metadata filtering is supported natively via `goagent.WithFilter`, which translates to a JSONB containment query (`metadata @> filter::jsonb`) applied server-side before similarity ranking. For large tables, add a GIN index: `CREATE INDEX ON goagent_embeddings USING gin(metadata jsonb_path_ops)`.

Three distance functions are available via `WithDistanceFunc`:

| Constant | Operator | Score | Recommended when |
|----------|----------|-------|-----------------|
| `pgvector.Cosine` (default) | `<=>` | `1 − distance` ∈ [0, 1] | Most text embedding models |
| `pgvector.L2` | `<->` | `1 / (1 + distance)` ∈ (0, 1] | Non-normalised vectors |
| `pgvector.InnerProduct` | `<#>` | `−distance` | Normalised vectors, speed-sensitive |

The `DistanceFunc` passed to `New` and the HNSW operator class used by `Migrate` must match — a mismatch causes pgvector to fall back to a sequential scan.

`pgvector.New` accepts a `Querier` — the minimal interface satisfied by both `*sql.DB` and `*sql.Tx` — so queries can run inside a caller-managed transaction.

#### Qdrant

```bash
go get github.com/Germanblandin1/goagent/memory/vector/qdrant
```

```go
import (
    "github.com/qdrant/go-client/qdrant"
    goagent_qdrant "github.com/Germanblandin1/goagent/memory/vector/qdrant"
)

client, _ := qdrant.NewClient(&qdrant.Config{Host: "localhost", Port: 6334})

// Optional: create the collection automatically.
goagent_qdrant.CreateCollection(ctx, client, goagent_qdrant.CollectionConfig{
    CollectionName: "goagent_embeddings",
    Dims:           1536,
})

store, err := goagent_qdrant.New(client, goagent_qdrant.Config{
    CollectionName: "goagent_embeddings",
})
```

Metadata filtering via `goagent.WithFilter` is translated to Qdrant `Must` conditions and evaluated server-side — Qdrant never scores points that do not pass the filter. `goagent.WithScoreThreshold` maps to Qdrant's native `score_threshold` field, also server-side.

#### SQLite + sqlite-vec

```bash
go get github.com/Germanblandin1/goagent/memory/vector/sqlitevec
```

> **Build requirement:** this package requires CGO. Run once from the repository root:
> ```bash
> go env -w CGO_CFLAGS="-I$(pwd)/memory/vector/sqlitevec/csrc -I$(go env GOMODCACHE)/github.com/mattn/go-sqlite3@v1.14.40"
> ```

```go
import "github.com/Germanblandin1/goagent/memory/vector/sqlitevec"

// Open registers the sqlite-vec extension and opens the database.
db, _ := sqlitevec.Open("/path/to/mydb.sqlite")

// Optional: create the data table and the vec0 virtual table.
sqlitevec.Migrate(ctx, db, sqlitevec.MigrateConfig{
    TableName: "goagent_embeddings",
    Dims:      768, // must match your embedding model
})

store, err := sqlitevec.New(db, sqlitevec.TableConfig{
    Table:          "goagent_embeddings",
    IDColumn:       "id",
    VectorColumn:   "embedding",
    TextColumn:     "content",
    MetadataColumn: "metadata", // optional TEXT/JSON column
})
```

The schema uses two tables: a regular data table (`Table`) and a `vec0` virtual table (`Table+"_vec"`) that provides indexed KNN search. `Migrate` creates both.

Two distance metrics are available via `WithDistanceMetric`:

| Constant | Algorithm | Score | Notes |
|----------|-----------|-------|-------|
| `sqlitevec.L2` (default) | Euclidean KNN via MATCH | `1 / (1 + distance)` ∈ (0, 1] | Index-accelerated |
| `sqlitevec.Cosine` | `vec_distance_cosine` full scan | `1 − distance` ∈ [0, 1] | Full scan — use for small datasets or normalise vectors and use L2 |

When not using `Open`, call `sqlitevec.Register()` before `sql.Open` to load the extension.

#### Optional store capabilities

All three persistent backends (and `InMemoryStore`) implement optional interfaces beyond `VectorStore`. Check for support with a type assertion — stores that do not implement the interface simply fail the assertion cleanly.

**`CountableStore`** — count entries without a vector query:

```go
if cs, ok := store.(goagent.CountableStore); ok {
    // Total entries in the store.
    n, err := cs.Count(ctx)

    // Entries whose metadata matches the filter.
    n, err = cs.Count(ctx, goagent.WithFilter(map[string]any{"env": "prod"}))
}
```

`WithFilter` applies the same metadata matching as `Search`. `WithScoreThreshold` is silently ignored — there is no query vector to score against.

Useful for health checks, monitoring store growth, or debugging index state without querying the underlying database directly.

**`VectorStoreObserver`** — observe every store operation with elapsed duration and error:

```go
observed := goagent.NewObservableStore(store,
    goagent.VectorStoreObserver{
        AfterUpsert: func(ctx context.Context, d time.Duration, err error) { /* ... */ },
        AfterSearch: func(ctx context.Context, results int, d time.Duration, err error) { /* ... */ },
        AfterDelete: func(ctx context.Context, d time.Duration, err error) { /* ... */ },
        AfterCount:  func(ctx context.Context, n int, d time.Duration, err error) { /* ... */ },
    },
)
```

`NewObservableStore` wraps any `VectorStore` (including `BulkVectorStore`) and forwards all calls transparently. Compose multiple observer sets with `MergeVectorStoreObservers`. For OpenTelemetry instrumentation, use `otel.NewVectorStoreObserver` (see [OpenTelemetry](#opentelemetry) section).

#### Token budget on retrieval

Cap the token cost of long-term memory retrieval with `WithTokenBudget`:

```go
results, err := ltm.Retrieve(ctx, query,
    goagent.WithTokenBudget(2000, func(ctx context.Context, text string) int {
        return len(strings.Fields(text)) // rough word-count estimate
    }),
)
```

`WithTokenBudget` walks the results (score-descending) and stops before the first result that would push the running total over the budget. The estimator is a plain `func(ctx, text) int` — plug in a tiktoken counter, a word counter, or a character counter.

### RAG (Retrieval-Augmented Generation)

The `rag` sub-module provides a standalone pipeline for indexing documents and exposing them as an agent tool. It is decoupled from long-term memory — use it when you want to index a corpus offline and let the agent search it on demand.

```bash
go get github.com/Germanblandin1/goagent/rag
```

```go
import "github.com/Germanblandin1/goagent/rag"

// 1. Build the pipeline
pipeline, err := rag.NewPipeline(
    vector.NewRecursiveChunker(vector.WithRCMaxSize(400), vector.WithRCOverlap(40)),
    ollama.NewEmbedder(ollama.WithEmbedModel("nomic-embed-text")),
    vector.NewInMemoryStore(),
)

// 2. Index documents
docs := []rag.Document{
    {Source: "readme.md", Content: []goagent.ContentBlock{goagent.TextBlock(text)}},
}
if err := pipeline.Index(ctx, docs...); err != nil { log.Fatal(err) }

// 3. Wrap as a tool and give it to the agent
searchTool := rag.NewTool(pipeline,
    rag.WithToolName("search_docs"),
    rag.WithToolDescription("Search the project documentation."),
    rag.WithTopK(3),
)

agent, _ := goagent.New(
    goagent.WithProvider(ollama.New()),
    goagent.WithModel("llama3.2"),
    goagent.WithTool(searchTool),
)
```

Observe indexing and retrieval with the built-in callbacks:

```go
pipeline, _ := rag.NewPipeline(chunker, embedder, store,
    rag.WithIndexObserver(func(ctx context.Context, source string,
        chunked, embedded, skipped int, dur time.Duration, err error) {
        slog.Info("indexed", "source", source, "chunks", embedded, "skipped", skipped)
    }),
    rag.WithSearchObserver(func(ctx context.Context, query string,
        results []rag.SearchResult, dur time.Duration, err error) {
        if len(results) > 0 && results[0].Score < 0.5 {
            slog.Warn("low quality retrieval", "query", query, "score", results[0].Score)
        }
    }),
)
```

Both observers receive the caller's `ctx`, so active OTel spans are accessible via `trace.SpanFromContext(ctx)` without the `rag` package importing the OTel SDK.

#### BatchEmbedder fast path

When the configured embedder implements `goagent.BatchEmbedder`, `Pipeline.Index` embeds all chunks of a document in a single `BatchEmbed` call instead of K serial round-trips. `OllamaEmbedder` implements this interface out of the box:

```go
embedder := ollama.NewEmbedder(ollama.WithEmbedModel("nomic-embed-text"))
// OllamaEmbedder implements goagent.BatchEmbedder — Pipeline picks the fast path automatically.

pipeline, _ := rag.NewPipeline(
    vector.NewRecursiveChunker(vector.WithRCMaxSize(400)),
    embedder,
    store,
)
```

The same optimization applies to `LongTermMemory.Store()` — when `BatchEmbedder` is available, all chunks from a turn are embedded in one call.

### Multi-Agent Orchestration

The `orchestration` sub-module coordinates multiple agents in structured workflows.

```bash
go get github.com/Germanblandin1/goagent/orchestration
```

```go
import "github.com/Germanblandin1/goagent/orchestration"
```

**Choose the right primitive:**

| Primitive | When to use |
|-----------|-------------|
| `Pipeline` | Linear, deterministic flow — stages run in order |
| `Graph` | Dynamic routing — nodes return the next node name; supports branching and loops |
| `ParallelGroup` | All stages run concurrently |
| `Supervisor` | LLM decides which workers to call and in what order |

#### Pipeline — sequential stages

```go
pipeline := orchestration.NewPipeline(
    orchestration.WithStages(
        orchestration.Stage("research", orchestration.AgentStage(
            researcherAgent,
            orchestration.GoalOnly,
        )),
        orchestration.Stage("code", orchestration.AgentStage(
            coderAgent,
            orchestration.OutputOf("research"), // reads previous stage output
        )),
    ),
)

sc, err := pipeline.Run(ctx, "implement a REST API endpoint")
if err != nil {
    log.Fatal(err)
}
fmt.Println(sc.Outputs()) // map["research": "...", "code": "..."]
```

`AgentStage` uses the `Stage` name as the output key automatically. `OutputOf("research")` reads the previous stage's output to feed it forward. Use `GoalOnly` for the first stage.

#### Graph — dynamic routing with branching and loops

```go
graph, err := orchestration.NewGraph(
    orchestration.WithStart("generate"),
    orchestration.WithMaxIterations(10),
    orchestration.WithNode("generate", func(ctx context.Context, sc *orchestration.StageContext) (string, error) {
        output, err := coderAgent.Run(ctx, sc.Goal)
        if err != nil {
            return "", err
        }
        sc.SetOutput("code", output)
        return "review", nil
    }, orchestration.WithToNodes("review")),
    orchestration.WithNode("review", func(ctx context.Context, sc *orchestration.StageContext) (string, error) {
        code, _ := sc.RequireOutput("code")
        verdict, err := reviewerAgent.Run(ctx, "Review this code:\n"+code)
        if err != nil {
            return "", err
        }
        if strings.Contains(verdict, "APPROVED") {
            return "", nil // end graph
        }
        sc.SetArtifact("feedback", verdict)
        return "generate", nil // loop back
    }, orchestration.WithToNodes("generate", "")),
)
if err != nil {
    log.Fatal(err)
}

sc, err := graph.Run(ctx, "implement a REST API endpoint")
```

Returning `""` from a `NodeFunc` terminates the graph. `WithToNodes` declares edges for `Graph.Mermaid()` and is validated at construction time. `WithMaxCycles(n)` limits per-node executions.

#### ParallelGroup — concurrent stages

```go
group := orchestration.NewParallelGroup(
    orchestration.WithParallelStages(
        orchestration.Stage("tests",  testerAdapter),
        orchestration.Stage("docs",   docsAdapter),
        orchestration.Stage("review", reviewAdapter),
    ),
    orchestration.WithMaxConcurrency(2), // at most 2 stages at a time
    orchestration.WithStrictKeys(),      // detect key collisions in dev
)

sc, err := group.Run(ctx, "finalise the REST API")
```

All stages share the same `StageContext` — writes are mutex-safe. `PanicError` is returned when a stage panics (wraps the recovered value). All stage errors are collected and joined before returning.

#### Supervisor — emergent LLM delegation

```go
supervisor, err := orchestration.NewSupervisor(
    "result",  // output key
    nil,       // PromptBuilder — nil uses GoalOnly
    []orchestration.Worker{
        {
            Name:             "researcher",
            Description:      "Researches technical topics and APIs in depth.",
            InputDescription: "The topic to research. Include technology name and version.",
            Agent:            researcherAgent,
        },
        {
            Name:             "coder",
            Description:      "Writes idiomatic Go code.",
            InputDescription: "Goal and any research context. Include design constraints.",
            Agent:            coderAgent,
        },
    },
    goagent.WithProvider(provider),
    goagent.WithModel("claude-sonnet-4-6"),
    goagent.WithSystemPrompt("You are a software coordinator. Use your tools to complete the task."),
)
if err != nil {
    log.Fatal(err)
}
defer supervisor.(*orchestration.AgentAdapter) // supervisor implements Executor

sc := orchestration.NewStageContext("implement a REST API endpoint")
if err := supervisor.RunWithContext(ctx, sc); err != nil {
    log.Fatal(err)
}
fmt.Println(sc.Outputs()["result"])
```

The supervisor LLM decides which workers to invoke, in what order, and whether to call multiple workers in the same turn (parallel dispatch via the ReAct tool fan-out). `InputDescription` is the most important field — the more specific, the better the supervisor's routing decisions.

#### Node middleware (Graph only)

```go
orchestration.WithNode("call_api", apiFunc,
    orchestration.WithNodeMiddleware(orchestration.TimeoutMiddleware(30*time.Second)),
    orchestration.WithNodeMiddleware(orchestration.RetryMiddleware(goagent.RetryPolicy{
        MaxAttempts: 5,
        Retryable:   func(err error) bool { return isRateLimitErr(err) },
    })),
)
// execution order: TimeoutMiddleware → RetryMiddleware → apiFunc
```

| Middleware | Purpose |
|-----------|---------|
| `RetryMiddleware(policy)` | Exponential backoff with jitter; `RetryAfter` support for HTTP 429 |
| `TimeoutMiddleware(d)` | Per-invocation `context.WithTimeout` |
| `RecoverMiddleware` | Converts panics to errors with stack trace |

#### Observability hooks

```go
hooks := orchestration.OrchestrationHooks{
    OnStageStart: func(ctx context.Context, name string) context.Context {
        ctx, _ = tracer.Start(ctx, "stage."+name)
        return ctx
    },
    OnStageEnd: func(ctx context.Context, name string, dur time.Duration, err error) {
        trace.SpanFromContext(ctx).End()
    },
    OnNodeEnter: func(ctx context.Context, name string) context.Context {
        ctx, _ = tracer.Start(ctx, "node."+name)
        return ctx
    },
    OnNodeExit: func(ctx context.Context, name, next string, dur time.Duration, err error) {
        trace.SpanFromContext(ctx).End()
    },
}

pipeline := orchestration.NewPipeline(
    orchestration.WithStages(/* … */),
    orchestration.WithPipelineHooks(hooks),
)
```

On*Start hooks return a `context.Context` that is forwarded to the executor or node, so OTel spans nest correctly without importing OTel in the orchestration package. Compose multiple hook sets with `MergeOrchestrationHooks`.

### Extended thinking & effort

```go
// Extended thinking — fixed budget
goagent.WithThinking(10_000)

// Extended thinking — adaptive (model decides)
goagent.WithAdaptiveThinking()

// Effort level
goagent.WithEffort("medium")  // "high" | "medium" | "low" | "" (model default)
```

Ollama captures reasoning from the `reasoning` field or `<think>…</think>` tags automatically.

### Dispatch resilience

Per-tool timeouts and circuit breaking are configured at agent construction time and apply to every tool call in every `Run`.

```go
agent, _ := goagent.New(
    goagent.WithProvider(provider),
    goagent.WithTool(myTool),

    // Cancel a tool's context if it takes longer than 5 s.
    goagent.WithToolTimeout(5*time.Second),

    // Open the circuit after 3 consecutive failures; reset after 30 s.
    goagent.WithCircuitBreaker(3, 30*time.Second),
)
```

Circuit-breaker state persists across `Run` calls on the same agent. Use `OnCircuitOpen` to observe rejections:

```go
goagent.WithHooks(goagent.Hooks{
    OnCircuitOpen: func(toolName string, openUntil time.Time) {
        log.Printf("tool %s disabled until %s", toolName, openUntil.Format(time.RFC3339))
    },
})
```

For custom cross-cutting logic (metrics, retry, rate-limiting), implement `DispatchMiddleware`:

```go
func metricsMiddleware(next goagent.DispatchFunc) goagent.DispatchFunc {
    return func(ctx context.Context, name string, args map[string]any) ([]goagent.ContentBlock, error) {
        start := time.Now()
        result, err := next(ctx, name, args)
        recordMetric(name, time.Since(start), err)
        return result, err
    }
}

agent, _ := goagent.New(
    goagent.WithProvider(provider),
    goagent.WithTool(myTool),
    goagent.WithDispatchMiddleware(metricsMiddleware),
)
```

The full chain order (outermost first): `logging → timeout → circuit breaker → custom → Execute`.

### Observability hooks

All callbacks receive `ctx context.Context` as the first argument. `OnRunStart` returns a `context.Context` — use it to embed values (e.g. a trace span) that will be forwarded to every subsequent hook in the same run.

```go
goagent.WithHooks(goagent.Hooks{
    OnRunStart:          func(ctx context.Context) context.Context                                              { return ctx },
    OnRunEnd:            func(ctx context.Context, result goagent.RunResult)                                    { /* ... */ },
    OnProviderRequest:   func(ctx context.Context, iteration int, model string, messageCount int)               { /* ... */ },
    OnProviderResponse:  func(ctx context.Context, iteration int, event goagent.ProviderEvent)                  { /* ... */ },
    OnIterationStart:    func(ctx context.Context, i int)                                                       { /* ... */ },
    OnThinking:          func(ctx context.Context, text string)                                                 { /* ... */ },
    OnThinkingText:      func(ctx context.Context, token string)                                                { /* ... */ },
    OnToolCall:          func(ctx context.Context, name string, args map[string]any)                            { /* ... */ },
    OnToolResult:        func(ctx context.Context, name string, _ []goagent.ContentBlock, d time.Duration, err error) { /* ... */ },
    OnCircuitOpen:       func(ctx context.Context, toolName string, openUntil time.Time)                        { /* ... */ },
    OnResponse:          func(ctx context.Context, text string, iterations int)                                 { /* ... */ },
    OnShortTermLoad:     func(ctx context.Context, results int, d time.Duration, err error)                     { /* ... */ },
    OnShortTermAppend:   func(ctx context.Context, msgs int, d time.Duration, err error)                        { /* ... */ },
    OnLongTermRetrieve:  func(ctx context.Context, results []goagent.ScoredMessage, d time.Duration, err error) { /* ... */ },
    OnLongTermStore:     func(ctx context.Context, msgs int, d time.Duration, err error)                        { /* ... */ },
})
```

Multiple independent hook sets can be composed with `MergeHooks`. Each set's `OnRunStart` return value is chained — the enriched context from one hook is passed to the next, so span hierarchies nest correctly:

```go
goagent.WithHooks(goagent.MergeHooks(metricsHooks, loggingHooks))
```

### OpenTelemetry

The `otel` sub-module translates hook events into OpenTelemetry spans and RED metrics (Rate, Errors, Duration) with no manual instrumentation required.

```bash
go get github.com/Germanblandin1/goagent/otel
```

```go
import (
    "go.opentelemetry.io/otel/metric"
    "go.opentelemetry.io/otel/trace"
    agentotel "github.com/Germanblandin1/goagent/otel"
)

hooks, err := agentotel.NewHooks(tracer, meter)
if err != nil {
    log.Fatal(err)
}

agent, err := goagent.New(
    goagent.WithProvider(provider),
    goagent.WithModel("claude-sonnet-4-6"),
    goagent.WithHooks(hooks),
)
```

If the caller context already carries an active span (e.g. from an HTTP handler), the agent's spans are automatically nested under it:

```go
ctx, span := tracer.Start(r.Context(), "handle_request")
defer span.End()
result, err := agent.Run(ctx, prompt)
```

**Span hierarchy** emitted per `Run` call:

```
goagent.run
  ├── goagent.provider.complete   (one per LLM call)
  ├── goagent.tool.<name>         (one per tool execution)
  ├── goagent.memory.short_term.load
  ├── goagent.memory.short_term.append
  ├── goagent.memory.long_term.retrieve
  └── goagent.memory.long_term.store
```

**RED metrics** recorded:

| Metric | Instrument | Unit | Useful for |
|---|---|---|---|
| `goagent.run.duration` | Histogram | s | p50/p99 latency per run |
| `goagent.run.errors` | Counter | {error} | Run error rate |
| `goagent.provider.duration` | Histogram | s | LLM call latency per iteration |
| `goagent.provider.tokens.input` | Counter | {token} | Input token spend |
| `goagent.provider.tokens.output` | Counter | {token} | Output token spend |
| `goagent.tool.duration` | Histogram | s | Tool latency by `tool.name` |
| `goagent.tool.errors` | Counter | {error} | Tool error rate by `tool.name` |
| `goagent.memory.load.duration` | Histogram | s | Memory read latency |
| `goagent.memory.append.duration` | Histogram | s | Memory write latency |

`tool.duration` and `tool.errors` carry the `tool.name` attribute, so you can break down latency and error rates per tool in Grafana or any OTel-compatible backend.

To instrument a `VectorStore` independently of the agent, use `otel.NewVectorStoreObserver`:

```go
observer, err := agentotel.NewVectorStoreObserver(tracer, meter)
if err != nil {
    log.Fatal(err)
}

store = goagent.NewObservableStore(store, observer)
```

This records spans and RED metrics for every `Upsert`, `Search`, `Delete`, `Count`, `BulkUpsert`, and `BulkDelete` call. Additional metrics recorded:

| Metric | Instrument | Unit | Useful for |
|---|---|---|---|
| `goagent.vector.upsert.duration` | Histogram | s | Write latency per backend |
| `goagent.vector.search.duration` | Histogram | s | Query latency per backend |
| `goagent.vector.search.results` | Histogram | {result} | Result set size distribution |
| `goagent.vector.delete.duration` | Histogram | s | Delete latency |
| `goagent.vector.bulk_upsert.duration` | Histogram | s | Bulk write latency |
| `goagent.vector.bulk_upsert.batch_size` | Histogram | {entry} | Entries per bulk call |
| `goagent.vector.errors` | Counter | {error} | Error rate by `operation` |

### Streaming

Stream tokens in real time instead of waiting for the complete response. Both Anthropic and Ollama providers support streaming.

```go
agent, _ := goagent.New(
    goagent.WithProvider(ollama.New()),
    goagent.WithModel("qwen3"),
)

_, err := agent.RunStream(ctx, "Write a short poem about Go.",
    goagent.TextHandler(os.Stdout), // prints each token as it arrives
)
```

`RunStream` returns the final accumulated text alongside any error. For multimodal input use `RunStreamBlocks`.

The full `StreamHandler` signature — `func(StreamEvent) error` — lets you handle each event type individually:

```go
handler := func(e goagent.StreamEvent) error {
    switch e.Type {
    case goagent.StreamEventText:
        fmt.Print(e.Text)
    case goagent.StreamEventToolStart:
        fmt.Printf("\n[calling %s]\n", e.ToolName)
    case goagent.StreamEventDone:
        fmt.Printf("\ntokens: %d in / %d out\n", e.Usage.InputTokens, e.Usage.OutputTokens)
    }
    return nil
}

_, err := agent.RunStream(ctx, prompt, handler)
```

Tool calls are still dispatched and their results fed back to the model automatically — streaming and the ReAct loop compose transparently.

When extended thinking is enabled and the model reasons before a tool call, those reasoning tokens are suppressed by default. Pass `WithShowThinkingText` to forward them to the handler:

```go
_, err := agent.RunStream(ctx, prompt,
    goagent.TextHandler(os.Stdout),
    goagent.WithShowThinkingText(true),
)
```

`Hooks.OnThinkingText` fires per reasoning token when `WithShowThinkingText(true)` is active, complementing `OnThinking` (which fires once with the full block after it completes).

### Multimodal input

```go
answer, err := agent.RunBlocks(ctx,
    goagent.TextBlock("Describe this image"),
    goagent.ImageBlock(imgData, "image/png"),
)
```

## Configuration reference

| Option | Default | Description |
|---|---|---|
| `WithProvider(p)` | — | **Required.** Model backend |
| `WithModel(m)` | — | **Required.** Model identifier |
| `WithTool(t)` | — | Register a tool (repeatable) |
| `WithSystemPrompt(s)` | — | System instruction for every run |
| `WithMaxIterations(n)` | `10` | Max ReAct iterations |
| `WithThinking(budget)` | — | Extended thinking, fixed token budget |
| `WithAdaptiveThinking()` | — | Extended thinking, model-chosen budget |
| `WithEffort(level)` | `""` | `"high"`, `"medium"`, `"low"`, or `""` |
| `WithToolTimeout(d)` | `0` (off) | Per-tool deadline; cancels the tool's context after `d` |
| `WithCircuitBreaker(n, d)` | — | Open circuit after `n` consecutive failures; reset after `d` |
| `WithDispatchMiddleware(mw)` | — | Append a custom `DispatchMiddleware` to the chain (repeatable) |
| `WithHooks(h)` | — | Observability callbacks |
| `WithRunResult(dst)` | — | Write `RunResult` metrics to `*dst` after each `Run` (synchronous alternative to `OnRunEnd`) |
| `WithName(name)` | — | Agent identity / session namespace for long-term memory |
| `WithShortTermMemory(m)` | — | Conversation history within a session |
| `WithLongTermMemory(m)` | — | Semantic retrieval across sessions |
| `WithWritePolicy(p)` | `StoreAlways` | What to persist to long-term memory |
| `WithLongTermTopK(k)` | `3` | Messages to retrieve from long-term memory |
| `WithShortTermTraceTools(b)` | `true` | Include tool traces in short-term history |
| `WithLogger(l)` | `slog.Default()` | Structured logger |

## Error handling

```go
var maxErr *goagent.MaxIterationsError
var provErr *goagent.ProviderError

switch {
case errors.As(err, &maxErr):
    fmt.Printf("gave up after %d iterations\n", maxErr.Iterations)
case errors.As(err, &provErr):
    fmt.Printf("provider error: %v\n", provErr.Cause)
}
```

| Error | When |
|---|---|
| `*ProviderError` | Provider returned an error |
| `*MaxIterationsError` | Iteration budget exhausted |
| `*ToolExecutionError` | A tool failed (wraps `*CircuitOpenError` when circuit is open) |
| `*CircuitOpenError` | Tool call rejected because the circuit breaker is open |
| `*mcp.MCPConnectionError` | MCP server unreachable at startup |
| `*mcp.MCPDiscoveryError` | MCP tool listing failed |
| `ErrToolNotFound` | Requested tool does not exist |

## Providers

| Provider | Package | Notes |
|---|---|---|
| Anthropic | `providers/anthropic` | Reads `ANTHROPIC_API_KEY`; supports text, images (5 MB), PDFs (32 MB); streaming via SSE |
| Ollama | `providers/ollama` | Default `http://localhost:11434/v1`; supports text and images; streaming via NDJSON; includes `NewEmbedder` |
| Voyage AI | `providers/voyage` | Reads `VOYAGE_API_KEY`; embedder only (e.g. `"voyage-3"`) |

## License

Apache 2.0 — see [LICENSE](./LICENSE).
