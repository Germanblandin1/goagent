# goagent

> ⚠️ **Work in Progress** — API may change without notice. Not production-ready yet.

A minimal, Go-idiomatic framework for building AI agents with a [ReAct](https://arxiv.org/abs/2210.03629) loop and pluggable model providers.

![Go version](https://img.shields.io/badge/go-1.23%2B-blue)
![License](https://img.shields.io/badge/license-Apache%202.0-blue)

## Install

```bash
git clone https://github.com/Germanblandin1/goagent.git
```

> `go get` will be available once the first version is tagged.

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
├── providers/
│   ├── anthropic/    Anthropic Messages API (Claude)
│   ├── ollama/       Local Ollama via OpenAI-compatible API (+ embedder)
│   └── voyage/       Voyage AI embedder
├── examples/
│   ├── calculator/           Tool use with arithmetic
│   ├── chatbot/              Multi-turn conversation
│   ├── chatbot-persistent/   Persistent memory across sessions
│   └── chatbot-mcp-fs/       Filesystem access via MCP stdio
└── internal/testutil/        Shared mocks
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

Implement the `Tool` interface or use the `ToolFunc` helper:

```go
echo := goagent.ToolFunc("echo", "Returns the input text.",
    map[string]any{
        "type": "object",
        "properties": map[string]any{"text": map[string]any{"type": "string"}},
        "required": []string{"text"},
    },
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
s.MustAddTool("echo", "Returns the input unchanged", nil,
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

```go
goagent.WithHooks(goagent.Hooks{
    OnRunStart:          func()                                                               { /* ... */ },
    OnRunEnd:            func(result goagent.RunResult)                                       { /* ... */ },
    OnProviderRequest:   func(iteration int, model string, messageCount int)                  { /* ... */ },
    OnProviderResponse:  func(iteration int, event goagent.ProviderEvent)                     { /* ... */ },
    OnIterationStart:    func(i int)                                                          { /* ... */ },
    OnThinking:          func(text string)                                                    { /* ... */ },
    OnToolCall:          func(name string, args map[string]any)                               { /* ... */ },
    OnToolResult:        func(name string, _ []goagent.ContentBlock, d time.Duration, err error) { /* ... */ },
    OnCircuitOpen:       func(toolName string, openUntil time.Time)                           { /* ... */ },
    OnResponse:          func(text string, iterations int)                                    { /* ... */ },
    OnShortTermLoad:     func(results int, d time.Duration, err error)                        { /* ... */ },
    OnShortTermAppend:   func(msgs int, d time.Duration, err error)                           { /* ... */ },
    OnLongTermRetrieve:  func(results int, d time.Duration, err error)                        { /* ... */ },
    OnLongTermStore:     func(msgs int, d time.Duration, err error)                           { /* ... */ },
})
```

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
| Anthropic | `providers/anthropic` | Reads `ANTHROPIC_API_KEY`; supports text, images (5 MB), PDFs (32 MB) |
| Ollama | `providers/ollama` | Default `http://localhost:11434/v1`; supports text and images; includes `NewEmbedder` |
| Voyage AI | `providers/voyage` | Reads `VOYAGE_API_KEY`; embedder only (e.g. `"voyage-3"`) |

## License

Apache 2.0 — see [LICENSE](./LICENSE).
