# goagent — Specifications for AI Agents

Prescriptive reference document. Intended for an AI agent that needs to write correct code for this project without reading the full source.

**Complements [`ARCHITECTURE.md`](ARCHITECTURE.md)** — that document explains *how it works internally*. This one explains *what rules to follow when contributing*.

---

## 0. Before Making Any Change

This section is mandatory. Read it before touching any code.

### 0.1 When to ask first

**Always ask the user before proceeding** when a proposed change involves any of the following:

| Trigger | Examples |
|---------|---------|
| Violates a rule in this document | Adding global state, using `panic`, ignoring `ctx` |
| Modifies a public interface signature | Adding/removing methods, changing parameter types or return types |
| Renames or removes exported symbols | Renaming `WithModel` → `WithModelID`, deleting `ToolBlocksFunc` |
| Adds a new dependency | Any new entry in `go.mod` |
| Changes a default behavior | Flipping a default from `true` to `false`, changing iteration budget |
| Requires significant cross-package refactoring | Changes that touch ≥ 3 packages or affect the ReAct loop |
| Is architecturally ambiguous | Multiple valid approaches with real trade-offs |

**Never assume** that a behavior is desired just because it seems reasonable. When in doubt, ask.

### 0.2 Required format when asking

When a change requires user approval, present it in this structure:

```
**Problem**
One paragraph describing what the current code does, what is missing or broken,
and why a change is needed.

**Proposal A — [short name]**
Description of the approach.
✅ Pros: ...
❌ Cons / trade-offs: ...

**Proposal B — [short name]**
Description of the approach.
✅ Pros: ...
❌ Cons / trade-offs: ...

**Recommendation:** Proposal X, because [one-sentence rationale].
```

Provide at least 2 proposals. If only one approach exists, say so explicitly and explain why.

### 0.3 Changes that are safe to make without asking

The following changes are low-risk and do not require prior approval:

- Adding a new private (unexported) helper function
- Adding a new test or example
- Fixing a typo in a comment or doc string
- Implementing a new `Tool`, `Provider`, or `VectorStore` that follows existing patterns exactly
- Bug fixes that do not change the public API

---

## 1. Non-negotiable Rules

These rules apply to **all** code in the project. Violating them is an error even if the code compiles.

### 1.1 Function signatures

1. **`context.Context` is always the first parameter** in every blocking operation (I/O, LLM calls, memory access, tool execution). No exceptions.
2. **`error` is always the last return value** when a function can fail.
3. **Variadic option parameters (`...Option`) always go last:** `func New(required T, opts ...Option)`.

### 1.2 Interfaces

4. **Interfaces have ≤ 3 methods.** If you need more, split into smaller composable interfaces.
5. **Interfaces are defined where they are consumed**, not where they are implemented. The `goagent` package defines `Provider`, `Tool`, `Embedder` — the `providers/` packages implement them.
6. **Only export interfaces that callers need to implement.** Purely internal interfaces go in `internal/`.
7. **Optional extensions are separate interfaces**, not additions to the main interface. The caller type-asserts at runtime. Example: `BulkVectorStore` extends `VectorStore`; `BatchEmbedder` extends `Embedder`.

### 1.3 Configuration

8. **Functional options** is the only configuration pattern. Constructors accept `...Option`; options are `func(*options)`.
9. **The zero-value of configuration structs must be functional** or equivalent to the documented default. Example: `Hooks{}` is a valid no-op.
10. **Default values are documented in the GoDoc of the option**, not in the name or type.

### 1.4 Errors

11. **All package errors are typed** — sentinel (`var ErrX = errors.New(...)`) or struct (`type XError struct{...}`). Never a bare `fmt.Errorf("...")` in library code.
12. **Wrappable errors implement `Unwrap() error`** for compatibility with `errors.Is` / `errors.As`.
13. **Errors are wrapped with context using `%w`:** `fmt.Errorf("operation: %w", err)`.
14. **Never use `panic`** in library code. If a function cannot continue, return an `error`.

### 1.5 State and concurrency

15. **Zero global mutable state.** No package-level `var` modified at runtime. No `init()`.
16. **`Agent` is immutable after `New()`**. All fields are written once; `Run` is safe for concurrent calls.
17. **Goroutines must have documented lifecycles.** If you launch a goroutine, document when it exits and how it is cancelled.
18. **Respect `ctx.Done()`** in loops and long-running operations. Cancellation must propagate.

### 1.6 Testing

19. **Race detector on all tests:** `go test -race ./...`. Every commit must pass.
20. **Table-driven tests** as the primary pattern. Each case has a descriptive name.
21. **Black-box preferred:** `package foo_test` unless internal access is required.
22. **No external mocking frameworks.** Implement interfaces directly in `internal/testutil/`.
23. **Minimum coverage:** ≥ 80 % in core packages, ≥ 70 % in sub-packages.

### 1.7 Documentation

24. **Every exported symbol has GoDoc.** The comment starts with the symbol name.
25. **No comments that repeat the code.** Comments explain the *why* or the *behavioral contract*, not the *what*.
26. **`Example*` functions** for every constructor and main exported function — they appear on pkg.go.dev.

### 1.8 Imports

27. **Three groups, separated by blank lines:** stdlib / external dependencies / internal project packages.

---

## 2. Interfaces — complete registry

### 2.1 Core interfaces (package `goagent`)

#### `Provider`
```go
type Provider interface {
    Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}
```
LLM backend abstraction. `req.Model` is never empty — the agent sets it before calling.

#### `StreamingProvider` _(optional — type assertion)_
```go
type StreamingProvider interface {
    CompleteStream(ctx context.Context, req CompletionRequest) (Stream, error)
}
```
The agent detects support at runtime: `if sp, ok := provider.(StreamingProvider); ok { ... }`. Not implementing it is not an error; the agent falls back to `Provider.Complete`.

#### `Stream`
```go
type Stream interface {
    Next(ctx context.Context) bool
    Event() StreamEvent
    Err() error
    Close() error
}
```
Event iterator for streaming. Usage pattern:
```go
for stream.Next(ctx) {
    ev := stream.Event()
}
if err := stream.Err(); err != nil { ... }
```
`Close()` must always be called (defer). Safe to call multiple times.

#### `Tool`
```go
type Tool interface {
    Definition() ToolDefinition
    Execute(ctx context.Context, args map[string]any) ([]ContentBlock, error)
}
```
`Definition()` takes no ctx — it is a constant. `Execute` receives `args` already deserialized from the model's JSON. Errors from `Execute` are reported to the model as text; they do not abort the loop or other tools.

#### `ShortTermMemory`
```go
type ShortTermMemory interface {
    Messages(ctx context.Context) ([]Message, error)
    Append(ctx context.Context, msgs ...Message) error
}
```
Conversation history within a session. Filtering (FixedWindow, TokenWindow) happens in `Messages`, never in `Append`.

#### `LongTermMemory`
```go
type LongTermMemory interface {
    Store(ctx context.Context, msgs ...Message) error
    Retrieve(ctx context.Context, query []ContentBlock, topK int, opts ...SearchOption) ([]ScoredMessage, error)
}
```
Semantic retrieval across sessions. `opts` are forwarded to the underlying `VectorStore.Search` call.

#### `VectorStore`
```go
type VectorStore interface {
    Upsert(ctx context.Context, id string, vector []float32, msg Message) error
    Search(ctx context.Context, vector []float32, topK int, opts ...SearchOption) ([]ScoredMessage, error)
    Delete(ctx context.Context, id string) error
}
```
`Delete` is a no-op if the ID does not exist. `Search` returns scores in [0.0, 1.0] for normalized vectors with cosine similarity.

#### `Embedder`
```go
type Embedder interface {
    Embed(ctx context.Context, content []ContentBlock) ([]float32, error)
}
```
Receives the full `[]ContentBlock` for multimodal support. Returns `vector.ErrNoEmbeddeableContent` when there are no embeddable blocks.

#### `TransientError` _(error classification — type assertion)_
```go
type TransientError interface {
    IsTransient() bool
}
```
Providers can implement this on their error types so that `RetryProvider` knows whether to retry.

### 2.2 Optional interfaces (extension by type assertion)

```go
// BulkVectorStore — batch operations; cheaper than N individual Upserts
type BulkVectorStore interface {
    VectorStore
    BulkUpsert(ctx context.Context, entries []UpsertEntry) error
    BulkDelete(ctx context.Context, ids []string) error
}

// BatchEmbedder — N embeddings in a single HTTP round-trip
type BatchEmbedder interface {
    Embedder
    BatchEmbed(ctx context.Context, inputs [][]ContentBlock) ([][]float32, error)
}

// CountableStore — count without a vector query (health checks, monitoring)
type CountableStore interface {
    Count(ctx context.Context, opts ...SearchOption) (int64, error)
}
```

**Rule:** code that uses these always falls back gracefully when the assertion fails:
```go
if bulk, ok := store.(BulkVectorStore); ok {
    return bulk.BulkUpsert(ctx, entries)
}
// fallback: loop of individual Upsert calls
```

### 2.3 Sub-package interfaces

#### `memory/storage.Storage`
```go
type Storage interface {
    Load(ctx context.Context) ([]Message, error)
    Save(ctx context.Context, msgs []Message) error
    Append(ctx context.Context, msgs ...Message) error
}
```

#### `memory/policy.Policy`
```go
type Policy interface {
    Apply(ctx context.Context, msgs []Message) []Message
}
```
Reads the history, returns the subset the provider will see. Never modifies at write time.

#### `memory/vector.Chunker`
```go
type Chunker interface {
    Chunk(ctx context.Context, content ChunkContent) ([]ChunkResult, error)
}
```

#### `memory/vector.SizeEstimator`
```go
type SizeEstimator interface {
    Estimate(text string) int
}
```

#### `memory/vector.PDFExtractor`
```go
type PDFExtractor interface {
    ExtractPages(ctx context.Context, data []byte) ([]PDFPage, error)
}
```

#### `orchestration.Executor`
```go
type Executor interface {
    RunWithContext(ctx context.Context, sc *StageContext) error
}
```
Every orchestration primitive implements `Executor`. This enables composition without type assertions.

#### `orchestration.NodeFunc` _(function type, not interface)_
```go
type NodeFunc func(ctx context.Context, sc *StageContext) (next string, err error)
```
Returning `""` as `next` terminates the graph.

---

## 3. Configuration options

### 3.1 Agent options (`goagent.Option`)

| Option | Value type | Default | Description |
|--------|-----------|---------|-------------|
| `WithProvider(p)` | `Provider` | — | **Required.** LLM backend |
| `WithModel(m)` | `string` | `""` | **Required.** Model ID sent in `CompletionRequest` |
| `WithTool(t)` | `Tool` | — | Register a tool (repeatable) |
| `WithSystemPrompt(s)` | `string` | `""` | System instruction for every run |
| `WithMaxIterations(n)` | `int` | `10` | ReAct loop iteration budget |
| `WithName(name)` | `string` | `""` | Agent identity; session namespace for LongTermMemory |
| `WithLogger(l)` | `*slog.Logger` | `slog.Default()` | Structured logger |
| `WithShortTermMemory(m)` | `ShortTermMemory` | `nil` | Conversation history |
| `WithLongTermMemory(m)` | `LongTermMemory` | `nil` | Semantic retrieval across sessions |
| `WithWritePolicy(p)` | `WritePolicy` | `StoreAlways` | What to persist to LongTermMemory |
| `WithLongTermTopK(k)` | `int` | `3` | Messages to retrieve from LongTermMemory per run |
| `WithShortTermTraceTools(b)` | `bool` | `true` | Include full tool trace in short-term history |
| `WithThinking(budget)` | `int` | — | Extended thinking, fixed token budget |
| `WithAdaptiveThinking()` | — | — | Extended thinking, model-chosen budget |
| `WithEffort(level)` | `string` | `""` | `"high"` / `"medium"` / `"low"` / `""` (model default) |
| `WithToolTimeout(d)` | `time.Duration` | `0` (off) | Per-tool deadline; cancels the tool's ctx after `d` |
| `WithCircuitBreaker(n, d)` | `int, time.Duration` | — | Open circuit after `n` consecutive failures; reset after `d` |
| `WithDispatchMiddleware(mw)` | `DispatchMiddleware` | — | Custom middleware in the dispatch chain (repeatable) |
| `WithHooks(h)` | `Hooks` | `Hooks{}` | Observability callbacks |
| `WithRunResult(dst)` | `*RunResult` | `nil` | Synchronous metrics destination after each run |
| `WithMCPConnector(fn)` | `MCPConnectorFn` | — | Low-level MCP connection function |

### 3.2 Search options (`SearchOption`)

| Option | Description |
|--------|-------------|
| `WithScoreThreshold(min float64)` | Discard results with score < min |
| `WithFilter(f map[string]any)` | Filter by metadata (AND semantics) |
| `WithTokenBudget(budget int, est func)` | Cap total token cost of `Retrieve` results |

### 3.3 Streaming options (`StreamOption`)

| Option | Default | Description |
|--------|---------|-------------|
| `WithShowThinkingText(show bool)` | `true` | Forward reasoning tokens to the stream handler |

### 3.4 Reusable function types

```go
// WritePolicy decides what to persist in LongTermMemory after each turn.
// nil → discard; non-nil slice → store exactly those messages.
type WritePolicy func(prompt, response Message) []Message

var StoreAlways WritePolicy       // always persists [prompt, response]
func MinLength(n int) WritePolicy // only persists if combined text length > n chars

// DispatchFunc is the base signature of the tool dispatch middleware chain.
type DispatchFunc func(ctx context.Context, name string, args map[string]any) ([]ContentBlock, error)

// DispatchMiddleware wraps DispatchFunc.
type DispatchMiddleware func(next DispatchFunc) DispatchFunc

// StreamHandler is the per-event callback for RunStream.
type StreamHandler func(event StreamEvent) error

// MCPConnectorFn is the signature of a low-level MCP connection function.
type MCPConnectorFn func(ctx context.Context, logger *slog.Logger) ([]Tool, io.Closer, error)
```

---

## 4. Error types

### 4.1 Sentinel errors

```go
var ErrToolNotFound       = errors.New("tool not found")
var ErrUnsupportedContent = errors.New("unsupported content type")
var ErrInvalidMediaType   = errors.New("invalid media type")
```

Usage: `errors.Is(err, goagent.ErrToolNotFound)`.

### 4.2 Typed errors (struct)

| Type | Key fields | Wrappable | When |
|------|-----------|-----------|------|
| `MaxIterationsError` | `Iterations int`, `LastThought string` | No | Loop exhausted its budget |
| `ToolExecutionError` | `ToolName string`, `Args map[string]any`, `Cause error` | Yes (`Unwrap`) | A tool failed; wraps `CircuitOpenError` when the circuit is open |
| `CircuitOpenError` | `Tool string`, `OpenUntil time.Time` | No | Tool call rejected by circuit breaker |
| `ToolPanicError` | `ToolName string`, `Value any`, `Stack []byte` | No | Tool panicked (recovered) |
| `ProviderError` | `Provider string`, `Cause error` | Yes (`Unwrap`) | Provider returned an error |
| `UnsupportedContentError` | `ContentType`, `Provider string`, `Reason string` | Yes (`Unwrap`) | Content type not supported by the provider |

Usage:
```go
var toolErr *goagent.ToolExecutionError
if errors.As(err, &toolErr) {
    fmt.Println(toolErr.ToolName)
}
```

### 4.3 Sub-package errors

| Package | Error | When |
|---------|-------|------|
| `mcp` | `*MCPConnectionError` | MCP handshake failed |
| `mcp` | `*MCPDiscoveryError` | `tools/list` call failed |
| `memory` | `ErrMissingVectorStore` | `NewLongTerm` called without a store |
| `memory` | `ErrMissingEmbedder` | `NewLongTerm` called without an embedder |
| `memory/vector` | `ErrNoEmbeddeableContent` | `Embed` received no text blocks |
| `orchestration` | `PanicError` | A `ParallelGroup` stage panicked |
| `orchestration` | `MaxRetriesError` | `RetryMiddleware` exhausted all attempts |

---

## 5. Code patterns (recipes)

### 5.1 Implementing a `Tool`

```go
// Option A: ToolFunc for tools that return plain text
calc := goagent.ToolFunc("calculator", "Evaluates arithmetic expressions",
    goagent.SchemaFrom(struct {
        Op string  `json:"op"  jsonschema_enum:"add,sub,mul,div"`
        A  float64 `json:"a"`
        B  float64 `json:"b"`
    }{}),
    func(ctx context.Context, args map[string]any) (string, error) {
        // args are already deserialized from the model's JSON
        return compute(args), nil
    },
)

// Option B: ToolBlocksFunc for tools that return ContentBlocks (multimodal)
img := goagent.ToolBlocksFunc("screenshot", "Captures the screen",
    goagent.SchemaFrom(struct{}{}),
    func(ctx context.Context, args map[string]any) ([]goagent.ContentBlock, error) {
        data, err := captureScreen()
        if err != nil {
            return nil, err
        }
        return []goagent.ContentBlock{goagent.ImageBlock(data, "image/png")}, nil
    },
)

// Option C: struct that directly implements Tool
type MyTool struct{ client *http.Client }

func (t *MyTool) Definition() goagent.ToolDefinition {
    return goagent.ToolDefinition{
        Name:        "my_tool",
        Description: "Description for the model.",
        Parameters:  goagent.SchemaFrom(myParams{}),
    }
}

func (t *MyTool) Execute(ctx context.Context, args map[string]any) ([]goagent.ContentBlock, error) {
    // respect ctx.Done()
    result, err := t.client.Get(ctx, args["url"].(string))
    if err != nil {
        return nil, err  // the agent reports the error to the model as text
    }
    return []goagent.ContentBlock{goagent.TextBlock(result)}, nil
}
```

### 5.2 Implementing a `Provider`

```go
type MyProvider struct {
    client *mySDK.Client
}

func (p *MyProvider) Complete(ctx context.Context, req goagent.CompletionRequest) (goagent.CompletionResponse, error) {
    // req.Model is never empty
    // req.Tools is nil when no tools are registered
    // req.Thinking.Enabled indicates whether to use extended thinking
    resp, err := p.client.Chat(ctx, toSDKRequest(req))
    if err != nil {
        return goagent.CompletionResponse{}, &goagent.ProviderError{
            Provider: "myprovider",
            Cause:    err,
        }
    }
    return fromSDKResponse(resp), nil
}

// StreamingProvider is optional — not implementing it is valid
func (p *MyProvider) CompleteStream(ctx context.Context, req goagent.CompletionRequest) (goagent.Stream, error) {
    stream, err := p.client.ChatStream(ctx, toSDKRequest(req))
    if err != nil {
        return nil, &goagent.ProviderError{Provider: "myprovider", Cause: err}
    }
    return &myStream{inner: stream}, nil
}
```

### 5.3 Implementing a `VectorStore`

```go
type MyStore struct {
    mu   sync.RWMutex
    data map[string]entry
}

func (s *MyStore) Upsert(ctx context.Context, id string, vector []float32, msg goagent.Message) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.data[id] = entry{vector: vector, msg: msg}
    return nil
}

func (s *MyStore) Search(ctx context.Context, vector []float32, topK int, opts ...goagent.SearchOption) ([]goagent.ScoredMessage, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    // ... cosine similarity, apply ScoreThreshold and Filter from opts, limit to topK
    return results, nil
}

func (s *MyStore) Delete(ctx context.Context, id string) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    delete(s.data, id) // no-op if id does not exist
    return nil
}

// Optional extension: BulkVectorStore
func (s *MyStore) BulkUpsert(ctx context.Context, entries []goagent.UpsertEntry) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    for _, e := range entries {
        s.data[e.ID] = entry{vector: e.Vector, msg: e.Message}
    }
    return nil
}
```

### 5.4 Adding a `DispatchMiddleware`

```go
// Chain order (outermost → innermost): logging → timeout → circuit breaker → custom → Execute
func metricsMiddleware(next goagent.DispatchFunc) goagent.DispatchFunc {
    return func(ctx context.Context, name string, args map[string]any) ([]goagent.ContentBlock, error) {
        start := time.Now()
        result, err := next(ctx, name, args)
        recordToolMetric(name, time.Since(start), err)
        return result, err
    }
}

agent, _ := goagent.New(
    goagent.WithProvider(provider),
    goagent.WithDispatchMiddleware(metricsMiddleware),
)
```

### 5.5 Implementing a `WritePolicy`

```go
// Only persist turns that mention a specific keyword
func onlySubstantive(prompt, response goagent.Message) []goagent.Message {
    if len(goagent.TextFrom(prompt.Content)) < 50 {
        return nil // discard short exchanges
    }
    return []goagent.Message{prompt, response}
}

agent, _ := goagent.New(
    goagent.WithProvider(provider),
    goagent.WithLongTermMemory(ltm),
    goagent.WithWritePolicy(onlySubstantive),
)
```

### 5.6 Extending with an optional interface

Standard pattern to avoid breaking existing implementations:

```go
// At the call site:
if bulk, ok := store.(goagent.BulkVectorStore); ok {
    if err := bulk.BulkUpsert(ctx, entries); err != nil {
        return err
    }
} else {
    for _, e := range entries {
        if err := store.Upsert(ctx, e.ID, e.Vector, e.Message); err != nil {
            return err
        }
    }
}
```

### 5.7 Implementing a `NodeFunc` in orchestration

```go
graph, err := orchestration.NewGraph(
    orchestration.WithStart("generate"),
    orchestration.WithNode("generate", func(ctx context.Context, sc *orchestration.StageContext) (string, error) {
        output, err := coderAgent.Run(ctx, sc.Goal)
        if err != nil {
            return "", err
        }
        sc.SetOutput("code", output)
        return "review", nil         // name of the next node
    }, orchestration.WithToNodes("review")),
    orchestration.WithNode("review", func(ctx context.Context, sc *orchestration.StageContext) (string, error) {
        // ...
        if approved {
            return "", nil           // "" terminates the graph
        }
        return "generate", nil
    }, orchestration.WithToNodes("generate", "")),
)
```

---

## 6. Anti-patterns

### ❌ Do not do this

```go
// 1. Global mutable state
var globalProvider Provider  // ❌

// 2. panic in library code
func mustLoad(path string) []byte {
    data, err := os.ReadFile(path)
    if err != nil {
        panic(err)  // ❌ return error instead
    }
    return data
}

// 3. Interface with too many methods
type Memory interface {
    Load(ctx) error
    Save(ctx) error
    Append(ctx) error
    Clear(ctx) error
    Export(ctx) error  // ❌ split into smaller interfaces
}

// 4. Context omitted from blocking operations
func (p *MyProvider) Complete(req CompletionRequest) (CompletionResponse, error) {  // ❌ missing ctx
    return callHTTP(req)
}

// 5. Untyped error in library code
return fmt.Errorf("tool failed: %s", name)  // ❌ use ToolExecutionError

// 6. Mutating args before passing them downstream
func (mw myMiddleware) Execute(ctx context.Context, args map[string]any) ([]ContentBlock, error) {
    args["injected"] = "value"  // ❌ args belong to the model; do not mutate
    return mw.next.Execute(ctx, args)
}

// 7. Constructor without functional options
func NewTool(name, desc string, timeout int) *MyTool {  // ❌
    return &MyTool{name: name, desc: desc, timeout: timeout}
}
// ✅ correct:
func NewTool(name, desc string, opts ...ToolOption) *MyTool { ... }

// 8. Using RoleDocument in messages sent to a provider
messages = append(messages, Message{Role: RoleDocument, ...})  // ❌ RoleDocument lives only in VectorStore
provider.Complete(ctx, CompletionRequest{Messages: messages})   // provider returns an error

// 9. Sharing *RunResult across concurrent runs
var result goagent.RunResult
go agent.Run(ctx1, "query1")  // ❌ data race
go agent.Run(ctx2, "query2")
// WithRunResult points to the same *result

// 10. Silencing memory errors
_ = mem.Append(ctx, msgs...)  // ❌ log or propagate the error
```

---

## Appendix — Key data types

```go
// Message in the conversation history
type Message struct {
    Role       Role           // RoleUser, RoleAssistant, RoleTool, RoleSystem, RoleDocument
    Content    []ContentBlock // text, image, or document
    ToolCalls  []ToolCall     // only in assistant messages with tool_use
    ToolCallID string         // only in RoleTool messages — links result to request
    Metadata   map[string]any // optional; used by RAG chunks (source, chunk_index)
}

// Content block (union type)
// Constructors: TextBlock(s), ImageBlock(data, mimeType), DocumentBlock(data, mimeType)
type ContentBlock struct { /* opaque */ }

// Similarity-scored message
type ScoredMessage struct {
    Message Message
    Score   float64  // [0.0, 1.0] for cosine similarity with normalized vectors
}

// Tool definition (sent to the model)
type ToolDefinition struct {
    Name        string
    Description string
    Parameters  map[string]any  // valid JSON Schema
}

// Metrics for a complete run
type RunResult struct {
    Duration   time.Duration
    Iterations int
    TotalUsage Usage
    ToolCalls  int
    ToolTime   time.Duration
    Err        error
}

// Valid roles
const (
    RoleUser      Role = "user"
    RoleAssistant Role = "assistant"
    RoleTool      Role = "tool"
    RoleSystem    Role = "system"
    RoleDocument  Role = "document"  // VectorStore only — never send to a provider
)
```

---

> **Related documents:**
> - [`ARCHITECTURE.md`](ARCHITECTURE.md) — how the ReAct loop, dispatcher, memory, and orchestration work internally
> - [`CLAUDE.md`](CLAUDE.md) — Claude Code-specific instructions for this repo
> - [`README.md`](README.md) — usage guide for framework consumers
