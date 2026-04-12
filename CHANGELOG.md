# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.5.5] - 2026-04-12

### Added

**`VectorStoreObserver` decorator (`goagent`)**
- `NewObservableStore(store, observer)` — wraps any `VectorStore` (including `BulkVectorStore`) and fires optional callbacks after each operation (`AfterUpsert`, `AfterSearch`, `AfterDelete`, `AfterCount`, `AfterBulkUpsert`, `AfterBulkDelete`) with elapsed duration and error
- `VectorStoreObserver` — struct of optional callbacks; unset fields are no-ops
- `MergeVectorStoreObservers(a, b, ...)` — composes multiple observer sets in order; each callback fires sequentially

**`CountableStore` optional interface (`goagent`)**
- `CountableStore` — optional interface implemented by all four stores (`InMemoryStore`, `pgvector`, `qdrant`, `sqlitevec`); exposes `Count(ctx, opts ...SearchOption) (int64, error)` to return the number of stored entries without a vector query
- Supports `WithFilter` for metadata-scoped counts (same semantics as `Search`); `WithScoreThreshold` is silently ignored
- Useful for health checks, monitoring store growth, or debugging index state

**`WithTokenBudget` search option (`goagent`)**
- `WithTokenBudget(budget int, estimator func(ctx context.Context, text string) int) SearchOption` — caps the token cost of `LongTermMemory.Retrieve`; walks results in score-descending order and stops before the first result that would exceed the budget
- The estimator is a plain `func(ctx, text) int` — decoupled from any tokeniser package to avoid circular imports; plug in tiktoken, word count, or character count
- Applied after `store.Search` returns; the `VectorStore` interface and all backends are unchanged

**`WithFilter` on `InMemoryStore` (`goagent`)**
- `InMemoryStore.Search` now applies `WithFilter` using deep equality (`reflect.DeepEqual`), matching the semantics of `pgvector` and `sqlitevec`; eliminates a silent dev/prod divergence where filters were ignored in tests but active in production stores

**OTel vector store instrumentation (`goagent/otel`)**
- `otel.NewVectorStoreObserver(tracer, meter)` — returns a `VectorStoreObserver` that records retroactive spans and RED metrics for every `VectorStore` and `BulkVectorStore` operation; plug it into any store via `goagent.NewObservableStore`
- New metrics: `goagent.vector.upsert.duration`, `goagent.vector.search.duration`, `goagent.vector.search.results`, `goagent.vector.delete.duration`, `goagent.vector.bulk_upsert.duration`, `goagent.vector.bulk_upsert.batch_size`, `goagent.vector.errors` (by `operation`)
- Complements the existing `otel.NewHooks` agent instrumentation without requiring changes to the agent or store implementations

**Examples**
- `examples/rag_sqlite_observable` — interactive RAG chatbot wiring together `sqlite-vec` (in-process, no server), `RecursiveChunker`, `BatchEmbedder` fast path, `VectorStoreObserver` with structured logging, and `otel.NewVectorStoreObserver`; demonstrates full observability without Qdrant or PostgreSQL

**CI / tooling**
- `.github/workflows/ci.yml` — five-job pipeline (lint, security, test, coverage, integration) triggered on push to `main` and pull requests; jobs: `go vet` + `staticcheck`, `govulncheck`, race-detector tests with coverage artefacts, coverage threshold enforcement (≥80 % core / ≥70 % sub-packages), integration tests via `testcontainers-go` for `pgvector` and `qdrant`
- `.golangci.yml` — local lint configuration for `golangci-lint`

### Fixed

- **qdrant**: whole-number `float64` values in `WithFilter` are now stored as `IntegerValue` (matching the existing Qdrant payload type), so `MatchInteger` conditions fire correctly — previously filters on integer-valued float fields silently returned no results
- **examples**: `rag_sqlite_observable` uses the correct model `qwen3.5:cloud`, consistent with `rag_batch_index`

### Tests

- `testcontainers-go` wired into `pgvector` and `qdrant` integration tests; `sqlitevec` tests run fully in-memory — no external infrastructure needed to run the full test suite
- `BulkUpsert` / `BulkDelete` / filter / distance integration tests added to reach coverage targets across all three persistent backends
- Fuzz tests, benchmarks, and unit tests added to reach the ≥80 % coverage threshold in core packages
- Focused benchmarks for `Dispatcher`, vector store operations, all chunkers, and `TokenWindow`

---

## [0.5.4] - 2026-04-11

### Added

**BatchEmbedder interface (`goagent`)**
- `BatchEmbedder` — new optional interface extending `Embedder` with `BatchEmbed(ctx, [][]ContentBlock) ([][]float32, error)`; mirrors the `BulkVectorStore` pattern — callers type-assert at runtime, no breaking change for existing embedders
- A `nil` vector at index `i` in the returned slice signals "no embeddable content" for that input (equivalent to `ErrNoEmbeddeableContent` in the single-embed path)

**BulkVectorStore interface (`goagent`)**
- `BulkVectorStore` — new optional interface extending `VectorStore` with `BulkUpsert(ctx, []UpsertEntry) error` and `BulkDelete(ctx, []string) error`; stores that implement it receive all chunks in a single RPC call instead of N serial upserts
- `UpsertEntry` — struct grouping `ID`, `Vector`, and `Message` for batch operations
- All three persistent stores (`pgvector`, `qdrant`, `sqlitevec`) implement `BulkVectorStore`

**Parallel embedding in `LongTermMemory.Store()` (`goagent/memory`)**
- `BatchEmbedder` fast path: when the configured embedder implements `BatchEmbedder`, all chunks from all messages are collected and embedded in a single `BatchEmbed` call followed by a single `BulkUpsert` — reduces N×K round-trips to 1 for embedders with native batch support
- Parallel fallback: when `BatchEmbedder` is not available, chunks within each message are embedded concurrently with `sync.WaitGroup` (fan-out / fan-in), reducing per-message latency from K×embed_latency to ~max(embed_latency)
- `BulkVectorStore` fast path: when the store implements `BulkVectorStore`, all entries are written in a single call; the serial `Upsert` loop is the fallback

**Parallel embedding in `rag.Pipeline.Index()` (`goagent/rag`)**
- Same two-path strategy as `LongTermMemory.Store()`: `BatchEmbedder` fast path (one `BatchEmbed` per document) and concurrent parallel path (one goroutine per chunk via `sync.WaitGroup`)
- `IndexObserver` counters (`chunked`, `embedded`, `skipped`) are maintained accurately in both paths

**`BatchEmbedder` on `OllamaEmbedder` (`goagent/providers/ollama`)**
- `OllamaEmbedder` now implements `goagent.BatchEmbedder`; `BatchEmbed` fans out one goroutine per input, each calling `Embed` concurrently; a `nil` result at index `i` signals no-text input (not an error)
- Compile-time assertions: `var _ goagent.Embedder = (*OllamaEmbedder)(nil)` and `var _ goagent.BatchEmbedder = (*OllamaEmbedder)(nil)`

**`BulkUpsert` / `BulkDelete` on Qdrant store (`goagent/memory/vector/qdrant`)**
- `Store.BulkUpsert(ctx, entries)` — all points submitted in a single `UpsertPoints` gRPC call
- `Store.BulkDelete(ctx, ids)` — all point IDs removed in a single `DeletePoints` gRPC call
- Compile-time assertion `var _ goagent.BulkVectorStore = (*Store)(nil)` added
- `go-client` dependency upgraded from `v1.13.0` to `v1.17.1` to match Qdrant server `v1.17.x` and eliminate the minor-version compatibility warning

**Examples**
- `examples/rag_batch_index` — new interactive RAG chatbot (replaces `rag_docs`); uses `OllamaEmbedder` (BatchEmbedder fast path), `RecursiveChunker`, Qdrant as the vector store, and a `bufio.Scanner` chat loop with `signal.NotifyContext` for clean Ctrl-C handling; indexes Markdown files from `documentacion/` on startup; vectors persist in Qdrant between runs

---

## [0.5.3] - 2026-04-09

### Added

**Search options (`goagent`)**
- `SearchOptions` — struct holding optional search parameters: `ScoreThreshold *float64` and `Filter map[string]any`
- `SearchOption` — functional option type `func(*SearchOptions)`, consistent with the rest of the library's configuration pattern
- `WithScoreThreshold(min float64) SearchOption` — discards results whose score falls below `min`; applied after the store returns results
- `WithFilter(f map[string]any) SearchOption` — restricts results to entries whose metadata contains all key-value pairs in `f` (AND semantics); filter strategy varies by backend (see below)

**VectorStore interface changes (`goagent`)**
- `Search` now accepts variadic `opts ...SearchOption`; callers that pass no options retain identical behaviour
- `Delete(ctx context.Context, id string) error` — removes a single entry by ID; no-op when the ID does not exist; all three persistent stores implement this

**LongTermMemory interface changes (`goagent`)**
- `Retrieve` now accepts variadic `opts ...SearchOption`; options are forwarded to the underlying `VectorStore.Search` call unchanged; `fileLongTermMemory` (chatbot-persistent example) updated to match

**pgvector metadata filtering (`goagent/memory/vector/pgvector`)**
- `WithFilter` is applied server-side using PostgreSQL's JSONB containment operator (`metadata @> $filter::jsonb`); only rows whose metadata JSONB contains all specified key-value pairs are candidates for similarity ranking; the filter SQL is pre-built at `New()` time (zero allocation per query)
- For large tables, a GIN index on `metadata` makes the JSONB filter index-assisted: `CREATE INDEX ON goagent_embeddings USING gin(metadata jsonb_path_ops)`
- Requires `MetadataColumn` to be set in `TableConfig`

**Qdrant metadata filtering (`goagent/memory/vector/qdrant`)**
- `WithFilter` is translated to Qdrant `Must` conditions on `"metadata.<key>"` payload fields; filtering happens server-side before distance scoring — Qdrant never computes similarity for points that do not match the filter
- Supported value types: `string` → `MatchKeyword`, `bool` → `MatchBoolean`, `int64` / whole-number `float64` → `MatchInteger`; unrecognised types are silently skipped
- `WithScoreThreshold` sets `QueryPoints.ScoreThreshold` — evaluated server-side after filtering and before results are sent over the wire
- Compile-time interface check `var _ goagent.VectorStore = (*Store)(nil)` added

**SQLite/sqlite-vec metadata filtering (`goagent/memory/vector/sqlitevec`)**
- `WithFilter` is applied in Go after sqlite-vec returns the topK nearest neighbours; all key-value pairs must match (`reflect.DeepEqual`); appropriate for sqlitevec's typical scale (< 100k entries) where post-filter overhead is negligible
- `WithScoreThreshold` is also applied post-query in Go; both options can be combined
- Requires `MetadataColumn` to be set in `TableConfig`

**Examples and documentation**
- `ExampleStore_Search_withFilter` and `ExampleStore_Search_withScoreThresholdAndFilter` added to pgvector, qdrant, and sqlitevec packages, each demonstrating a realistic multi-tenant isolation scenario
- `doc.go` updated for qdrant and sqlitevec to describe filter/threshold behaviour, supported value types, and scale context

### Fixed

- `fileLongTermMemory.Retrieve` in `examples/chatbot-persistent` updated to match the new `LongTermMemory` interface signature (`_ ...goagent.SearchOption` added)

---

## [0.5.2] - 2026-04-06

### Added

**PostgreSQL vector store (`goagent/memory/vector/pgvector`)**
- New sub-module `github.com/Germanblandin1/goagent/memory/vector/pgvector` implementing `goagent.VectorStore` over PostgreSQL with the pgvector extension
- `TableConfig` — caller-supplied schema descriptor; fields: `Table`, `IDColumn`, `VectorColumn`, `TextColumn`, `MetadataColumn` (optional JSONB); no default values, the caller must be explicit
- `New(db Querier, cfg TableConfig, opts ...StoreOption) (*Store, error)` — constructs a store; validates all required fields and rejects identifiers with unsafe characters; accepts a `Querier` so queries can run inside a caller-managed `*sql.Tx` as well as a `*sql.DB`
- `Querier` interface — minimal interface satisfied by `*sql.DB`, `*sql.Tx`, and any pgx adapter; allows transactional upserts without coupling to a specific driver
- `Store.Upsert(ctx, id, vec, msg)` — idempotent INSERT … ON CONFLICT DO UPDATE; serialises text content and optional JSONB metadata; vector passed as a pgvector literal
- `Store.Search(ctx, vec, topK, opts ...SearchOption)` — ORDER BY distance LIMIT topK; returns `[]goagent.ScoredMessage` with `RoleDocument`; `SearchOption` is variadic for forward-compatible future options (score threshold, metadata filters)
- `Store.Delete(ctx, id)` — no-op when id does not exist
- `DistanceFunc` — distance operator + HNSW operator class pair; three built-ins: `Cosine` (`<=>`, `vector_cosine_ops`), `L2` (`<->`, `vector_l2_ops`), `InnerProduct` (`<#>`, `vector_ip_ops`); custom operators supported via `NewDistanceFunc(operator, opsClass)`; scores normalised so higher always means more similar across all operators
- `WithDistanceFunc(d DistanceFunc) StoreOption` — selects the distance operator; must match the HNSW index operator class used at migration time; default: `Cosine`
- `MigrateConfig` — configures `Migrate`; fields: `TableName` (default: `"goagent_embeddings"`), `Dims` (required), `DistanceFunc` (default: `Cosine`), `HNSWm` (default: 16), `HNSWefConstruction` (default: 64)
- `Migrate(ctx, db, cfg MigrateConfig) error` — idempotent; creates the `vector` extension, the embeddings table (`id TEXT PK`, `embedding vector(Dims)`, `content TEXT`, `metadata JSONB`, `created_at TIMESTAMPTZ`), and an HNSW index; safe to call on every application start

**SQLite vector store (`goagent/memory/vector/sqlitevec`)**
- New sub-module `github.com/Germanblandin1/goagent/memory/vector/sqlitevec` implementing `goagent.VectorStore` over SQLite with the sqlite-vec extension (CGO required)
- `TableConfig` — schema descriptor with the same fields as the pgvector counterpart; `MetadataColumn` is a `TEXT`/JSON column (not JSONB); schema-qualified names are not supported (SQLite has no schemas)
- `Open(dsn string) (*sql.DB, error)` — registers the sqlite-vec extension and opens the database; recommended entry point
- `Register()` — registers the extension for all subsequent `sql.Open` calls; idempotent; use when managing the `*sql.DB` externally
- `New(db *sql.DB, cfg TableConfig, opts ...StoreOption) (*Store, error)` — constructs a store; same validation as pgvector
- `Store.Upsert(ctx, id, vec, msg)` — runs in a transaction: upserts the data table row, retrieves its rowid, deletes the old vec0 entry, inserts the updated vector blob; `vec0` virtual tables do not support `INSERT OR REPLACE`
- `Store.Search(ctx, vec, topK)` — L2 uses the sqlite-vec KNN index (`MATCH … AND k = ?`, index-accelerated); Cosine uses `vec_distance_cosine` with a full scan; scores normalised so higher = more similar
- `Store.Delete(ctx, id)` — transactional; removes both the data row and the vec0 entry; no-op if id does not exist
- `DistanceMetric` — string enum: `L2` (default, index-accelerated Euclidean KNN) and `Cosine` (full-scan cosine via `vec_distance_cosine`); for large datasets with cosine similarity, normalise vectors before inserting and use `L2` (equivalent on unit vectors)
- `WithDistanceMetric(m DistanceMetric) StoreOption` — selects the distance metric; default: `L2`
- `MigrateConfig` — fields: `TableName` (default: `"goagent_embeddings"`), `Dims` (required), `Metric` (documentation only, no schema effect)
- `Migrate(ctx, db, cfg MigrateConfig) error` — idempotent; creates the data table (`id TEXT PK`, `content TEXT`, `metadata TEXT`, `created_at INTEGER`) and the `vec0` virtual table (`TableName+"_vec"` with `embedding float[Dims]`); safe to call on every application start

## [0.5.0] - 2026-04-05

### Added

**RAG sub-module (`goagent/rag`)**
- New sub-module `github.com/Germanblandin1/goagent/rag` providing a composable Retrieval-Augmented Generation pipeline
- `Document` — unit of input for `Pipeline.Index`; carries a `Source` identifier and `[]ContentBlock` content
- `SearchResult` — enriched retrieval result with `Message`, cosine `Score`, and `Source` (extracted from chunk metadata)
- `Pipeline` — combines a `Chunker`, `Embedder`, and `VectorStore` into two phases: **Index** (chunk → embed → upsert) and **Search** (embed query → vector lookup → `[]SearchResult`)
- `NewPipeline(chunker, embedder, store, opts...)` — constructor; returns an error if any required component is nil
- `Pipeline.Index(ctx, docs...)` — processes documents through the chunk → embed → upsert pipeline; chunk IDs are `"<source>:<index>"` (deterministic, idempotent); chunks with no embeddable content are silently skipped; the `IndexObserver` is notified per document
- `Pipeline.Search(ctx, query, topK)` — embeds the query and retrieves the top-k most similar chunks; the `SearchObserver` is notified after every call
- `SearchObserver` — callback type `func(ctx, query string, results []SearchResult, dur time.Duration, err error)` invoked after every `Search`; receives the caller's `ctx` so active OTel spans are accessible via `trace.SpanFromContext`
- `IndexObserver` — callback type `func(ctx, source string, chunked, embedded, skipped int, dur time.Duration, err error)` invoked per document after `Index`; reports chunk counts and timing; receives the caller's `ctx` for OTel access
- `WithSearchObserver(obs SearchObserver)` — `PipelineOption` to register a search callback
- `WithIndexObserver(obs IndexObserver)` — `PipelineOption` to register a per-document index callback
- `NewTool(pipeline, opts...)` — wraps a `Pipeline` as a `goagent.Tool`; the agent invokes it autonomously when it decides to search; panics on nil pipeline (programming error)
- `WithToolName(name)`, `WithToolDescription(desc)`, `WithTopK(k)`, `WithFormatter(f)` — `ToolOption`s for `NewTool`; defaults: `"search_knowledge_base"`, generic description, `topK=3`, `defaultFormat`
- `MultimodalFormat` — result formatter that returns raw `ContentBlock`s instead of serialising to text; use when the corpus contains images indexed with a multimodal embedder and the provider has vision

**Core types (`goagent`)**
- `ScoredMessage` — new struct pairing a `Message` with its cosine similarity `Score float64`; returned by `VectorStore.Search` and `LongTermMemory.Retrieve`
- `RoleDocument` — new role constant for RAG chunk messages stored in a `VectorStore`; these messages must never reach a provider; `buildMessages` returns an error if LTM retrieval surfaces a `RoleDocument` message (diagnostic for shared RAG/LTM store misconfiguration); both Anthropic and Ollama providers now return an explicit error on unrecognised roles instead of silently dropping/mapping them

**Chunkers (`goagent/memory/vector`)**
- `SentenceChunker` — splits text at sentence boundaries (punctuation + whitespace, or paragraph breaks); overlap is counted in complete sentences to preserve semantic coherence at chunk boundaries
- `NewSentenceChunker(opts...)` — constructor with `WithSCMaxSize(n)`, `WithSCOverlap(n)`, `WithSCEstimator(e)` options
- `RecursiveChunker` — splits text by respecting a separator hierarchy (`\n\n` → `\n` → sentences → words), falling back to finer boundaries only when necessary; ideal for Markdown and paragraph-structured documents; overlap is applied post-split via tail extraction
- `NewRecursiveChunker(opts...)` — constructor with `WithRCSeparators(seps)`, `WithRCMaxSize(n)`, `WithRCOverlap(n)`, `WithRCEstimator(e)` options

**Examples**
- `examples/rag_docs` — RAG pipeline over local Markdown files; uses Ollama for both the chat model (`llama3.2`) and the embedding model (`nomic-embed-text`); demonstrates a shared `OllamaClient`, `TextChunker`, `InMemoryStore`, `SearchObserver` with low-score warning, and `NewTool` wired into an agent

### Changed

**`VectorStore` interface (`goagent`)**
- `Search(ctx, vector, topK)` return type changed from `([]Message, error)` to `([]ScoredMessage, error)` — every store now provides similarity scores by default; the `ScoredStore` opt-in interface and the `vector.ScoredSearch` helper are removed
- `InMemoryStore.SearchScored` removed; `Search` now always returns `[]ScoredMessage`
- `OnLongTermRetrieve` hook: the `results int` argument is now `results []ScoredMessage` — callers can inspect individual scores and sources without a separate lookup

**Providers (`goagent/providers/anthropic`, `goagent/providers/ollama`)**
- `WithModel` option removed from both providers; the model is owned exclusively by the agent (`goagent.WithModel`) and forwarded to the provider through `CompletionRequest.Model`; providers now return an error if `req.Model` is empty

## [0.4.3] - 2026-04-04

### Added

**Schema helper (`goagent`)**
- `Schema` — type alias for `map[string]any`; interchangeable with `ToolDefinition.Parameters` and the `parameters` argument of `ToolFunc` / `ToolBlocksFunc`
- `SchemaFrom(v any) Schema` — derives a JSON Schema object from a struct value using reflection; eliminates the need to write nested `map[string]any` literals by hand
- Supported struct tags:
  - `json:"name"` — property name in the schema; `"-"` skips the field entirely
  - `json:"name,omitempty"` — marks the field as optional (not included in `"required"`)
  - `jsonschema_description:"text"` — adds a `"description"` key to the property
  - `jsonschema_enum:"a,b,c"` — adds an `"enum"` key with the comma-separated values
- Go → JSON Schema type mapping: `string` → `"string"`, integer kinds → `"integer"`, float kinds → `"number"`, `bool` → `"boolean"`, slice/array → `"array"`, anything else → `"string"` (conservative fallback)
- Pointer arguments are dereferenced before inspection; non-struct inputs (after dereferencing) return `{"type":"object"}` without panicking
- `"required"` key is omitted entirely when no required fields exist (an absent key is cleaner than an empty array)

### Changed

**Call sites migrated to `SchemaFrom`**
- `examples/calculator` — `operation` field now carries `jsonschema_enum:"add,sub,mul,div"`, restoring the enum constraint that was previously inexpressible without a manual map
- `examples/multimodal-chatbot` — both `NewLoadFileTool` (`path`) and `NewScanDirTool` (`path`, `recursive`) migrated; the intermediate `params` variable is removed in each constructor
- `examples/chatbot-mcp-fs` — `list_dir` (`path` optional) and `read_file` (`path` required) migrated; `SchemaFrom` is passed directly as the `schema any` argument of `MustAddTool` since it serialises to JSON via `json.Marshal` internally
- `providers/anthropic/example_test.go` — `ExampleNew_withTool` migrated
- `doc.go` — package-level quickstart example updated
- `tool_test.go` — dummy schemas replaced with `SchemaFrom(struct{}{})`

## [0.4.2] - 2026-04-03

### Added

**OpenTelemetry sub-module (`goagent/otel`)**
- New module `github.com/Germanblandin1/goagent/otel` (requires OTel v1.40.0, Go 1.24+)
- `NewHooks(tracer trace.Tracer, meter metric.Meter) (goagent.Hooks, error)` — returns a fully wired `Hooks` struct that emits OTel spans and RED metrics; safe for concurrent use; plug into `WithHooks` or compose via `MergeHooks`
- Span hierarchy per `Run` call: `goagent.run` (root) with child spans for each `goagent.provider.complete`, `goagent.tool.<name>`, `goagent.memory.short_term.load`, `goagent.memory.short_term.append`, `goagent.memory.long_term.retrieve`, `goagent.memory.long_term.store`
- Retroactive span timestamps via `trace.WithTimestamp` so provider and tool spans reflect actual wall-clock windows, not callback dispatch time
- If the caller context already carries an active span (e.g. from an HTTP handler), all `goagent.run` spans are automatically nested under it
- RED metrics: `goagent.run.duration` (s), `goagent.run.errors` ({error}), `goagent.provider.duration` (s), `goagent.provider.tokens.input` ({token}), `goagent.provider.tokens.output` ({token}), `goagent.tool.duration` (s), `goagent.tool.errors` ({error}), `goagent.memory.load.duration` (s), `goagent.memory.append.duration` (s)
- `tool.duration` and `tool.errors` carry a `tool.name` attribute for per-tool breakdowns in Grafana or any OTel-compatible backend

### Changed

**Observability hooks (`goagent`)**
- All 14 hook callbacks now receive `ctx context.Context` as their first argument — the context carries span and baggage values set by `OnRunStart`
- `OnRunStart` signature changed from `func()` to `func(ctx context.Context) context.Context` — returning an enriched context (e.g. with an embedded trace span) causes that context to be forwarded to every subsequent hook in the same run; returning `nil` is safe and preserves the original context
- `MergeHooks` chains `OnRunStart` callbacks: the returned context from each hook is passed as input to the next, so span hierarchies nest correctly across composed hook sets
- The context returned by `OnRunStart` is strictly scoped to hook callbacks; it is never used for I/O operations (provider, tools, memory) — this prevents a hook from cancelling the agent's flow by returning a context it controls

## [0.4.1] - 2026-03-29

### Changed

**Memory policies (`goagent/memory/policy`)**
- `NewFixedWindow(n)` and `NewTokenWindow(maxTokens)` now operate on **atomic groups** instead of individual messages. A group is an indivisible exchange: a plain message (user or assistant without tool calls), or an `assistant+tool_use` message together with all its subsequent `tool_result` messages. The tool-call invariant is now guaranteed structurally — a `tool_result` can never appear as the first message in the window, regardless of where the window boundary falls.
- For histories without tool calls the behaviour is identical to the previous release — each message forms its own group.
- `NewFixedWindow(n)` no longer panics for `n ≤ 0`; `Apply` returns `nil` instead. `NewTokenWindow` continues to panic on `maxTokens ≤ 0` because a zero-token budget is always a programming error.
- `NewTokenWindow` adds a defensive rule: if the most recent group alone exceeds the token budget, it is included anyway — sending some context is better than sending none.

## [0.4.0] - 2026-03-29

### Added

**Dispatch middleware chain (`goagent`)**
- `DispatchFunc` — function signature for the base of the dispatch chain: `func(ctx context.Context, name string, args map[string]any) ([]ContentBlock, error)`
- `DispatchMiddleware` — wrapper type for cross-cutting tool dispatch behaviour: `func(next DispatchFunc) DispatchFunc`
- `WithToolTimeout(d time.Duration)` — per-tool deadline independent of the parent context; cancels the tool's context after `d` and records the failure (including toward the circuit breaker if configured); zero disables the timeout
- `WithCircuitBreaker(maxFailures int, resetTimeout time.Duration)` — per-tool circuit breaking; after `maxFailures` consecutive failures the tool is disabled for `resetTimeout`; disabled tools return `*CircuitOpenError` immediately without calling `Execute`; the circuit transitions through `Closed → Open → HalfOpen → Closed` states; `maxFailures` and `resetTimeout` must both be > 0
- `WithDispatchMiddleware(mw DispatchMiddleware)` — appends a custom middleware to the chain; custom middlewares run after the built-ins (`logging → timeout → circuit breaker → custom → Execute`); multiple calls append in order (first call = outermost custom middleware)
- `CircuitOpenError` — new error type returned when a tool call is rejected because the circuit breaker is open; fields: `Tool string`, `OpenUntil time.Time`; error string formatted as RFC3339; supports `errors.As` via `*ToolExecutionError.Unwrap`
- `Hooks.OnCircuitOpen func(toolName string, openUntil time.Time)` — new hook fired each time a tool call is rejected because the circuit breaker is open; `openUntil` is the earliest time the circuit may close again

**Run and provider observability hooks (`goagent`)**
- `Hooks.OnRunStart func()` — fired at the beginning of each `Run`/`RunBlocks` call, before loading memory or building messages; useful for initialising external metrics or starting a tracing span
- `Hooks.OnRunEnd func(result RunResult)` — fired at the end of each `Run`/`RunBlocks` call, always (success, provider error, `MaxIterationsError`, context cancellation); `result.Err` is nil on success
- `Hooks.OnProviderRequest func(iteration int, model string, messageCount int)` — fired before each `Provider.Complete` call; `iteration` is 0-indexed
- `Hooks.OnProviderResponse func(iteration int, event ProviderEvent)` — fired after each `Provider.Complete` call on both success and error; `event.Err` carries the underlying error before wrapping as `*ProviderError`

**New types (`goagent`)**
- `RunResult` — aggregates metrics for an entire `Run`/`RunBlocks` call: `Duration`, `Iterations`, `TotalUsage Usage`, `ToolCalls int`, `ToolTime time.Duration`, `Err error`
- `ProviderEvent` — captures metrics from a single `Provider.Complete` invocation: `Duration`, `Usage`, `StopReason`, `ToolCalls int`, `Err error`

**New option (`goagent`)**
- `WithRunResult(dst *RunResult)` — synchronous alternative to `Hooks.OnRunEnd`; after each `Run`/`RunBlocks` call the agent writes the accumulated `RunResult` to `*dst`; the caller must read it before starting the next `Run` on the same pointer; sharing the pointer across concurrent `Run` calls is a data race

### Changed

**Core agent (`goagent`)**
- `Agent` now holds a `*dispatcher` built once in `New()` (previously rebuilt on every `Run()` call); stateful middlewares such as `circuitBreakerMiddleware` now preserve their state across multiple `Run()` calls on the same agent
- Built-in dispatch chain order (outermost first): `loggingMiddleware` → `timeoutMiddleware` → `circuitBreakerMiddleware` (only when `WithCircuitBreaker` is set) → caller middlewares → `Execute`; logging measures the total end-to-end duration including all middleware overhead
- `DispatchFunc` return type changed from `(string, error)` to `([]ContentBlock, error)` — callers implementing custom `DispatchMiddleware` must update their function signatures accordingly

## [0.3.1] - 2026-03-29

### Added

**Memory observability hooks (`goagent`)**
- `OnShortTermLoad(results int, duration time.Duration, err error)` — fired after `ShortTermMemory.Messages` at the start of each `Run`; reports the number of messages loaded, the operation duration, and any error
- `OnShortTermAppend(msgs int, duration time.Duration, err error)` — fired after `ShortTermMemory.Append` at the end of each `Run`; reports the number of messages persisted, the operation duration, and any error
- `OnLongTermRetrieve(results int, duration time.Duration, err error)` — fired after `LongTermMemory.Retrieve` at the start of each `Run`; reports the number of messages retrieved, the operation duration, and any error
- `OnLongTermStore(msgs int, duration time.Duration, err error)` — fired after `LongTermMemory.Store` at the end of each `Run`; not fired when the `WritePolicy` discards the turn
- All four hooks follow the same nil-safe contract as existing hooks: a nil field is silently skipped
- Store and Append errors remain non-fatal; hooks receive the error and `Run` continues normally

**Example: `multimodal-chatbot`**
- New interactive chatbot example in `examples/multimodal-chatbot` targeting Ollama (`qwen3.5:cloud`)
- `scan_dir` tool: lists supported images and documents in a directory; optional `recursive` flag to walk sub-directories
- `load_file` tool: loads images (`jpg`, `png`, `gif`, `webp`) as `ImageBlock`; extracts plain text from `pdf` and `txt` files via `ledongthuc/pdf` and returns a `TextBlock` (works around Ollama's lack of native document support)
- Short-term memory: `InMemory` storage + `FixedWindow(20)` policy
- Long-term memory: `InMemoryStore` vector store + `nomic-embed-text:latest` embedder, `topK=3`, `MinLength(10)` write policy
- All nine hooks wired with timestamped, color-coded stderr logging (`STM←`, `STM→`, `LTM←`, `LTM→`, `LOOP`, `TOOL→`, `TOOL←`, `THINKING`, `DONE`)

## [0.3.0] - 2026-03-28

### Added

**Vector utilities (`goagent/memory/vector`)**
- New sub-package `memory/vector` providing four orthogonal building blocks for semantic memory: embedding, chunking, similarity, and storage
- `NewInMemoryStore()` — thread-safe `VectorStore` backed by an in-process map; `Upsert` / `Search` (cosine similarity, O(n)); filters results by session ID when the context carries one
- `FallbackEmbedder` — wraps any `goagent.Embedder` and skips blocks whose `ContentType` the underlying embedder does not support; fires an optional `OnSkipped` callback; returns `ErrNoEmbeddeableContent` when no blocks survive filtering
- Chunkers: `NewNoOpChunker()` (identity, default for conversational messages), `NewTextChunker(...)` (word-boundary splits with configurable max size and overlap), `NewBlockChunker(...)` (per-block type chunking; images pass through unchanged; PDFs split by page when a `PDFExtractor` is provided), `NewPageChunker(...)` (PDF-only per-page chunking; other blocks pass through)
- Similarity functions: `CosineSimilarity` (for unit vectors), `CosineSimilarityRaw` (for unnormalised vectors), `Normalize`
- `SizeEstimator` interface + three implementations: `CharEstimator` (Unicode code points), `HeuristicTokenEstimator` (`len/4+4`, no dependencies, default for chunkers), `OllamaTokenEstimator` (exact count via `/api/tokenize`, falls back to heuristic on error)
- Helpers: `ExtractText(blocks)` concatenates all `ContentText` blocks; `ChunkToMessage` builds a `Message` from an original message and a `ChunkResult`

**Long-term memory implementation (`goagent/memory`)**
- `NewLongTerm(opts ...LongTermOption) (goagent.LongTermMemory, error)` — concrete implementation of `goagent.LongTermMemory`; requires a `VectorStore` and an `Embedder`; chunking is optional (defaults to `NoOpChunker`)
- `WithVectorStore(s goagent.VectorStore)` — required; the backing store for vectors and messages
- `WithEmbedder(e goagent.Embedder)` — required; converts content to float vectors
- `WithChunker(c vector.Chunker)` — optional; splits messages before embedding (default: `NoOpChunker`)
- `WithTopK(k int)` — default number of messages retrieved per query (default: 3)
- `WithWritePolicy(p goagent.WritePolicy)` — controls what gets stored; same `WritePolicy` type as the root package option (default: `StoreAlways`)
- Storage IDs include session namespace when the context carries a session ID (`"sessionID:sha256:chunkIdx"`), enabling multiple agents to share a single store without cross-contamination
- `ErrMissingVectorStore` and `ErrMissingEmbedder` — returned by `NewLongTerm` when required options are absent

**Voyage AI embedder (`goagent/providers/voyage`)**
- New sub-module `github.com/Germanblandin1/goagent/providers/voyage` with a production-ready `Embedder` for the Voyage AI API
- `NewEmbedder(opts ...EmbedderOption)` — reads `VOYAGE_API_KEY` from the environment; `NewEmbedderWithClient` allows sharing a client across instances
- `WithEmbedModel(model string)` — required (e.g. `"voyage-3"`)
- `WithInputType(t string)` — optional (`"document"` or `"query"`)
- `WithMaxChars(n int)` — optional, default 30 000 (~7 500 tokens); truncates at word boundary

**Ollama embedder (`goagent/providers/ollama`)**
- `NewEmbedder(opts ...EmbedderOption)` — new constructor inside the existing `providers/ollama` package; uses the Ollama `/api/embeddings` endpoint
- `NewEmbedderWithClient(client *OllamaClient, opts ...EmbedderOption)` — shared-client variant
- `WithEmbedModel(model string)` — required (e.g. `"nomic-embed-text"`)
- `WithMaxChars(n int)` — optional, default 30 000

**Agent session identity (`goagent`)**
- `WithName(name string) Option` — assigns a stable name to the agent; when `LongTermMemory` is configured the name is used as a session namespace so that multiple agents sharing the same `VectorStore` do not see each other's entries; names must not contain `":"`

## [0.2.0] - 2026-03-27

### Added

**MCP integration (`goagent/mcp`)**
- New sub-module `github.com/Germanblandin1/goagent/mcp` with full MCP (Model Context Protocol) client and server support
- `NewServer(name, version)` — builds an in-process MCP server; `AddTool` / `MustAddTool` register tools with optional JSON Schema
- `ServeStdio()` / `ServeSSE(addr)` — starts the server on stdio or HTTP+SSE transport
- `NewStdioClient(ctx, logger, cmd, args...)` — spawns a subprocess and connects over stdin/stdout
- `NewSSEClient(ctx, logger, url)` — connects to an already-running HTTP+SSE MCP server
- Both clients perform the MCP handshake with exponential-backoff retry (up to 3 attempts: 100 ms, 200 ms, 400 ms) and auto-discover all tools via `tools/list`
- `NewRouter(logger, clients...)` — aggregates tools from multiple clients; last-registered wins on name collision
- `Transport` type (`TransportStdio`, `TransportSSE`) identifies the connection mechanism of a client
- Typed errors: `MCPConnectionError` (handshake/dial failure), `MCPDiscoveryError` (tool listing failure), `MCPToolError` (tool execution business error); all implement `errors.Is`/`errors.As` via `Unwrap`

**Core agent (`goagent`)**
- `goagent.New` now returns `(*Agent, error)` — connection errors from MCP connectors surface at construction time
- `WithMCPConnector(fn MCPConnectorFn)` — low-level option to attach any `MCPClient` to the agent; used internally by `mcp.WithStdio` and `mcp.WithSSE`
- `Agent.Close() error` — gracefully shuts down all attached MCP clients; idempotent (safe to call multiple times)

**MCP agent options (`goagent/mcp`)**
- `WithStdio(cmd, args...)` — convenience option: spawns a stdio MCP server subprocess and connects the agent to it
- `WithSSE(url)` — convenience option: connects the agent to a running HTTP+SSE MCP server

**Module structure**
- `go.work` workspace now includes `./mcp`, `./providers/anthropic`, `./providers/ollama`, and `./examples` for unified local development

**Examples**
- `examples/chatbot-mcp-fs` — self-contained interactive chatbot with read-only filesystem access via MCP stdio; the same binary acts as both the chatbot agent and the MCP server (`--serve` flag); `safePath` sanitization prevents directory traversal and rejects absolute paths; supports `list_dir` and `read_file` tools with a 100 KB file size limit

### Changed

**Core agent (`goagent`)**
- `goagent.New` signature changed from `*Agent` to `(*Agent, error)` — callers must handle the error (typically with `log.Fatal`)

### Fixed

**MCP (`goagent/mcp`)**
- `AddTool` now uses `mcp.NewToolWithRawSchema` when a schema is provided, avoiding a conflict between `InputSchema` and `RawInputSchema` that caused silent serialisation errors
- `schemaToMap` returns `nil` for tools registered without a schema instead of an empty `{"type":"object","properties":{}}` object

## [0.1.2] - 2026-03-26

### Added

**Extended thinking (`goagent`)**
- `WithThinking(budgetTokens int)` — enables extended thinking with a fixed token budget; recommended range 4 000–32 000 tokens depending on task complexity
- `WithAdaptiveThinking()` — enables extended thinking in adaptive mode; the model decides how much to reason; preferred for budgets above 32 000 tokens
- `ThinkingConfig` struct in `CompletionRequest` carries the configuration to the provider
- `ContentThinking` content type and `ThinkingData` struct expose thinking blocks in responses; `ThinkingBlock(thinking, signature)` helper constructs one
- Thinking blocks are preserved during the ReAct loop (required by the Anthropic API for multi-turn interactions) and stripped before persisting to short-term memory

**Effort (`goagent`)**
- `WithEffort(level string)` — sets the model's output-effort level; accepted values: `"high"`, `"medium"`, `"low"`, or `""` (model default); affects text quality, tool-call accuracy, and reasoning depth; orthogonal to thinking

**Observability hooks (`goagent`)**
- `Hooks` struct with five optional, nil-safe callbacks: `OnIterationStart`, `OnThinking`, `OnToolCall`, `OnToolResult`, `OnResponse`
- `WithHooks(h Hooks)` agent option to register hooks; zero-value `Hooks{}` is a complete no-op
- `OnIterationStart(iteration int)` — fired at the start of each ReAct iteration (0-indexed)
- `OnThinking(text string)` — fired once per thinking block when the model produces extended thinking
- `OnToolCall(name string, args map[string]any)` — fired before each tool execution
- `OnToolResult(name string, content []ContentBlock, duration time.Duration, err error)` — fired after each tool execution with timing and optional error
- `OnResponse(text string, iterations int)` — fired before `Run` returns, including on `MaxIterationsError`; `iterations` is 1-indexed
- `ToolResult.Duration` field records tool execution time; zero only when the tool was not found

**Anthropic provider (`goagent/providers/anthropic`)**
- Extended thinking: translates `ThinkingConfig` to SDK params; parses `"thinking"` and `"redacted_thinking"` response blocks; echoes thinking blocks back in subsequent turns
- Effort: translates `CompletionRequest.Effort` to `OutputConfig` SDK param

**Ollama provider (`goagent/providers/ollama`)**
- Captures reasoning from the `reasoning` field in the Ollama HTTP response (newer Ollama builds)
- Falls back to parsing `<think>…</think>` tags from text content when the `reasoning` field is absent
- Thinking blocks are not echoed back to local models (not required by Ollama)

**Examples**
- `examples/chatbot`: uses `OnThinking` hook to display model reasoning in grey text in the terminal

## [0.1.1] - 2026-03-21

### Changed

**Ollama provider (`goagent/providers/ollama`)**
- `WithBaseURL`: doc now states the default URL (`http://localhost:11434/v1`) explicitly
- `Complete`: doc now covers timeout/cancellation (caller-controlled via `ctx`), lack of retry, and what a "connection refused" error means

**Memory policy (`goagent/memory/policy`)**
- `NewTokenWindow` now accepts `...TokenWindowOption`; the built-in `len/4+4` heuristic remains the default
- New `TokenizerFunc` type and `WithTokenizer(fn TokenizerFunc)` option to plug in an exact tokenizer (e.g. tiktoken, Anthropic token-counting endpoint); removes the "for production use, replace this" warning

**Memory package (`goagent/memory`)**
- Package overview now states explicitly that no `VectorStore` or `Embedder` implementation is provided; both must be supplied by the caller
- Removed stale "available in v0.4+" note from the long-term memory section

**Root package (`goagent`)**
- `VectorStore`: removed false reference to a `memory/vectorstore` sub-package that does not exist
- `RoleSystem`: doc clarifies that `Agent` never places this role in the message slice; system prompts travel via `CompletionRequest.SystemPrompt` through `WithSystemPrompt`
- `Agent`: new `# Concurrency` section documenting that the struct itself is safe, and that safety for memory backends depends on the caller-supplied implementations
- `CompletionRequest`, `CompletionResponse`, `Usage`: all fields now have doc comments covering nil/empty semantics and provider obligations
- `ToolCall.ID` and `Message.ToolCallID`: doc explains the correlation contract — `ID` must be echoed in `ToolCallID` so the model can pair tool results with requests
- `MaxIterationsError.Iterations` and `MaxIterationsError.LastThought`: both fields now documented; `LastThought` clarifies it is the model's last text content and may be empty if the final turn produced only tool calls

**Examples**
- `providers/ollama`: new `example_test.go` with four examples covering default construction, `WithBaseURL`, and the stateless concurrent pattern

## [0.1.0] - 2026-03-21

### Added

**Core agent (`goagent`)**
- `Agent` struct with stateless `Run(ctx, prompt)` ReAct loop
- Functional options: `WithProvider`, `WithModel`, `WithTool`, `WithMaxIterations`, `WithSystemPrompt`, `WithLogger`
- `Tool` interface (`Definition() ToolDefinition`, `Execute(ctx, args) (string, error)`)
- `ToolFunc` helper to create a `Tool` from a plain function without defining a struct
- `Provider` interface (`Complete(ctx, CompletionRequest) (CompletionResponse, error)`)
- Parallel tool dispatch: all tool calls in a single turn execute concurrently via fan-out/fan-in
- Typed errors: `MaxIterationsError`, `ToolExecutionError`, `ProviderError`, `ErrToolNotFound`
- `Role` and `StopReason` constants for message and completion modelling

**Memory system (`goagent/memory`)**
- `NewShortTerm` — session-scoped memory backed by pluggable `Storage` and read `Policy`
- `NewLongTerm` — cross-session semantic memory via `VectorStore` + `Embedder`
- Agent options: `WithShortTermMemory`, `WithLongTermMemory`, `WithWritePolicy`, `WithLongTermTopK`, `WithShortTermTraceTools`
- `WritePolicy` type + `StoreAlways` default and `MinLength(n)` built-in policy
- `ShortTermMemory`, `LongTermMemory`, `VectorStore`, `Embedder` interfaces exported from the root package

**Memory policies (`goagent/memory/policy`)**
- `NewNoOp` — passes all messages through unchanged (default)
- `NewFixedWindow(n)` — retains the most recent *n* messages, preserving tool-call invariant
- `NewTokenWindow(maxTokens)` — retains recent messages within a token budget using a `len/4+4` heuristic

**Memory storage (`goagent/memory/storage`)**
- `NewInMemory` — thread-safe, in-process `Storage` implementation

**Ollama provider (`goagent/providers/ollama`)**
- `New` constructor with `WithBaseURL` option (default: `http://localhost:11434/v1`)
- Implements `goagent.Provider` via Ollama's OpenAI-compatible API using `go-openai` SDK

**Examples & tests**
- `examples/calculator` — runnable agent that uses arithmetic tools to solve math expressions
- `Example*` functions in root, `memory`, `memory/policy`, and `memory/storage` packages for pkg.go.dev
- Mock implementations in `internal/testutil` (`MockProvider`, `MockTool`, `MockMemory`)
- Full test suite with race-detector coverage (`go test -race`)

