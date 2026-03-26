# goagent

> ⚠️ **Work in Progress** — This library is under active development and the API
> may change without notice. Feedback welcome, but please don't depend on this
> in production yet.


A minimal, Go-idiomatic framework for building AI agents with a [ReAct](https://arxiv.org/abs/2210.03629) loop and pluggable model providers.

![Go version](https://img.shields.io/badge/go-1.23%2B-blue)
![License](https://img.shields.io/badge/license-Apache%202.0-blue)

## Install
```bash
git clone https://github.com/Germanblandin1/goagent.git
```

> A proper `go get` will be available once the first version is tagged.

## Quickstart

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/Germanblandin1/goagent"
    "github.com/Germanblandin1/goagent/providers/ollama"
)

func main() {
    agent := goagent.New(
        goagent.WithProvider(ollama.New()),
        goagent.WithModel("qwen3"),
    )

    answer, err := agent.Run(context.Background(), "What is the capital of France?")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(answer)
}
```

## Architecture

```
goagent/                      Core — Agent, ReAct loop, interfaces
├── memory/                   Short-term and long-term memory
│   ├── storage/              Message persistence (InMemory, etc.)
│   └── policy/               Message filtering (FixedWindow, TokenWindow, etc.)
├── providers/
│   ├── anthropic/            Anthropic Messages API (Claude)
│   └── ollama/               Local Ollama via OpenAI-compatible API
├── examples/
│   ├── calculator/           Tool use with basic arithmetic
│   ├── chatbot/              Multi-turn conversation with short-term memory
│   └── chatbot-persistent/   Persistent memory across sessions
└── internal/
    └── testutil/             Shared mocks for Provider, Tool, Memory
```

### Component diagram

```
                         ┌─────────────────────────────┐
   prompt ──────────────▶│           Agent              │
   answer ◀──────────────│        (ReAct loop)          │
                         └──────┬──────────┬────────────┘
                                │          │
                  ┌─────────────▼──┐  ┌────▼─────────────────────────┐
                  │    Provider    │  │            Hooks              │
                  │────────────────│  │───────────────────────────────│
                  │ anthropic      │  │ OnIterationStart  OnThinking  │
                  │ ollama         │  │ OnToolCall        OnResponse  │
                  └────────────────┘  │ OnToolResult                 │
                                      └──────────────────────────────┘
          ┌───────────────────────────────────────────────────────┐
          │                     Memory (optional)                 │
          │  ShortTerm (storage + policy)   LongTerm (vector)    │
          └───────────────────────────────────────────────────────┘
                  ┌──────────────────────────────────┐
                  │      Tools (zero or more)         │
                  │  ToolFunc  ToolBlocksFunc  custom │
                  └──────────────────────────────────┘
```

## Concepts

### ReAct loop

`Agent.Run` alternates between calling the model and dispatching tool calls until the model produces a final answer, the context is cancelled, or the iteration budget is exhausted.

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

A `Tool` is anything that implements two methods:

```go
type Tool interface {
    Definition() ToolDefinition
    Execute(ctx context.Context, args map[string]any) ([]ContentBlock, error)
}
```

For simple tools use `ToolFunc` (returns text) or `ToolBlocksFunc` (returns multimodal content):

```go
echo := goagent.ToolFunc(
    "echo",
    "Returns the input text unchanged.",
    map[string]any{
        "type": "object",
        "properties": map[string]any{
            "text": map[string]any{"type": "string"},
        },
        "required": []string{"text"},
    },
    func(_ context.Context, args map[string]any) (string, error) {
        return args["text"].(string), nil
    },
)

agent := goagent.New(
    goagent.WithProvider(ollama.New()),
    goagent.WithModel("qwen3"),
    goagent.WithTool(echo),
)
```

When the model requests multiple tools in a single turn, they are dispatched concurrently.

### Providers

A `Provider` wraps a model backend:

```go
type Provider interface {
    Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}
```

| Provider | Package | Backend |
|---|---|---|
| Anthropic | `providers/anthropic` | Anthropic Messages API (Claude) |
| Ollama | `providers/ollama` | Local Ollama via OpenAI-compatible API |

Implement the `Provider` interface to connect any other backend.

### Memory

The framework provides two levels of memory:

**ShortTermMemory** — conversation history between `Run` calls within the same session:

```go
type ShortTermMemory interface {
    Messages(ctx context.Context) ([]Message, error)
    Append(ctx context.Context, msgs ...Message) error
}
```

**LongTermMemory** — semantic retrieval across sessions (requires a vector store and embedder):

```go
type LongTermMemory interface {
    Store(ctx context.Context, msgs ...Message) error
    Retrieve(ctx context.Context, query []ContentBlock, topK int) ([]Message, error)
}
```

Ready-to-use implementations live in the `memory/` package:

```go
import (
    "github.com/Germanblandin1/goagent/memory"
    "github.com/Germanblandin1/goagent/memory/policy"
    "github.com/Germanblandin1/goagent/memory/storage"
)

shortMem := memory.NewShortTerm(
    memory.WithStorage(storage.NewInMemory()),
    memory.WithPolicy(policy.NewFixedWindow(20)),
)

agent := goagent.New(
    goagent.WithProvider(provider),
    goagent.WithModel("qwen3"),
    goagent.WithShortTermMemory(shortMem),
)
```

Available policies: `NewNoOp()` (pass-through), `NewFixedWindow(n)` (keep last n messages), `NewTokenWindow(maxTokens)` (fit within token budget).

### Multimodal content

Messages support text, images, and documents via `ContentBlock`:

```go
goagent.TextBlock("hello")
goagent.ImageBlock(pngBytes, "image/png")       // JPEG, PNG, GIF, WebP
goagent.DocumentBlock(pdfBytes, "application/pdf", "report.pdf")  // PDF, text/plain
```

Use `RunBlocks` for multimodal input:

```go
answer, err := agent.RunBlocks(ctx,
    goagent.TextBlock("Describe this image"),
    goagent.ImageBlock(imgData, "image/png"),
)
```

### Extended thinking

Extended thinking lets supported models reason internally before responding. Thinking blocks are visible via the `OnThinking` hook and are preserved during the loop (required by the API) but stripped before writing to memory.

```go
// Fixed token budget — recommended for most tasks
agent := goagent.New(
    goagent.WithProvider(anthropic.New()),
    goagent.WithModel("claude-sonnet-4-6"),
    goagent.WithThinking(10_000),  // 4 000–32 000 tokens depending on complexity
)

// Adaptive mode — model decides how much to reason
agent := goagent.New(
    goagent.WithProvider(anthropic.New()),
    goagent.WithModel("claude-sonnet-4-6"),
    goagent.WithAdaptiveThinking(),
)
```

Ollama captures reasoning from the `reasoning` field or `<think>…</think>` tags automatically; no configuration needed.

### Effort

Effort controls the model's overall computational investment, affecting text quality, tool-call accuracy, and reasoning depth. It is orthogonal to thinking — both can be combined freely.

```go
agent := goagent.New(
    goagent.WithProvider(anthropic.New()),
    goagent.WithModel("claude-sonnet-4-6"),
    goagent.WithEffort("medium"),  // "high" | "medium" | "low" | "" (model default)
)
```

### Observability hooks

Hooks provide synchronous callbacks for observing ReAct loop events without modifying agent behaviour. The zero-value `Hooks{}` is a complete no-op — only set what you need.

```go
agent := goagent.New(
    goagent.WithProvider(ollama.New()),
    goagent.WithModel("qwen3"),
    goagent.WithHooks(goagent.Hooks{
        OnIterationStart: func(iteration int) {
            fmt.Printf("── iteration %d ──\n", iteration+1)
        },
        OnThinking: func(text string) {
            fmt.Printf("\033[90m💭 %s\033[0m\n", text)  // grey
        },
        OnToolCall: func(name string, args map[string]any) {
            fmt.Printf("→ %s(%v)\n", name, args)
        },
        OnToolResult: func(name string, _ []goagent.ContentBlock, d time.Duration, err error) {
            fmt.Printf("← %s in %s (err=%v)\n", name, d, err)
        },
        OnResponse: func(text string, iterations int) {
            fmt.Printf("done in %d iteration(s)\n", iterations)
        },
    }),
)
```

| Hook | When fired |
|---|---|
| `OnIterationStart` | Before each call to the model (0-indexed) |
| `OnThinking` | Once per thinking block in the model response |
| `OnToolCall` | Before each tool is dispatched |
| `OnToolResult` | After each tool completes, with duration and optional error |
| `OnResponse` | Before `Run` returns, even on `MaxIterationsError` (iterations is 1-indexed) |

## Configuration options

| Option | Default | Description |
|---|---|---|
| `WithProvider(p)` | — | **Required.** The model backend |
| `WithModel(m)` | — | **Required.** Model identifier (e.g. `"qwen3"`, `"claude-sonnet-4-6"`) |
| `WithTool(t)` | — | Register a tool (repeatable) |
| `WithSystemPrompt(s)` | — | System-level instruction for every run |
| `WithMaxIterations(n)` | `10` | Maximum ReAct iterations before giving up |
| `WithThinking(budgetTokens)` | — | Enable extended thinking with a fixed token budget (4 000–32 000) |
| `WithAdaptiveThinking()` | — | Enable extended thinking in adaptive mode (model chooses budget) |
| `WithEffort(level)` | `""` | Output-effort level: `"high"`, `"medium"`, `"low"`, or `""` (model default) |
| `WithHooks(h)` | — | Observability callbacks for ReAct loop events |
| `WithShortTermMemory(m)` | — | Conversation history between runs |
| `WithLongTermMemory(m)` | — | Semantic retrieval across sessions |
| `WithWritePolicy(p)` | `StoreAlways` | Controls what gets persisted to long-term memory |
| `WithLongTermTopK(k)` | `3` | Number of messages to retrieve from long-term memory |
| `WithShortTermTraceTools(b)` | `true` | Include tool call traces in short-term history |
| `WithLogger(l)` | `slog.Default()` | Structured logger for debug output |

## Error handling

All errors are typed and support `errors.Is` / `errors.As`:

```go
answer, err := agent.Run(ctx, prompt)

var maxErr *goagent.MaxIterationsError
var provErr *goagent.ProviderError

switch {
case errors.As(err, &maxErr):
    fmt.Printf("gave up after %d iterations\n", maxErr.Iterations)
case errors.As(err, &provErr):
    fmt.Printf("provider error: %v\n", provErr.Cause)
case errors.Is(err, context.DeadlineExceeded):
    fmt.Println("timed out")
}
```

| Error | When |
|---|---|
| `*ProviderError` | The provider returned an error |
| `*MaxIterationsError` | Iteration budget exhausted |
| `*ToolExecutionError` | A tool failed (does not abort the loop) |
| `*UnsupportedContentError` | The provider does not support a content type |
| `ErrToolNotFound` | Requested tool does not exist |
| `ErrInvalidMediaType` | Invalid MIME type in a ContentBlock |

## Examples

### Calculator ([`examples/calculator`](./examples/calculator))

Demonstrates tool use with basic arithmetic. Requires [Ollama](https://ollama.com) running locally.

```bash
go run ./examples/calculator
```

### Chatbot ([`examples/chatbot`](./examples/chatbot))

Interactive multi-turn conversation with short-term memory and a fixed window policy.

```bash
go run ./examples/chatbot
```

### Chatbot Persistent ([`examples/chatbot-persistent`](./examples/chatbot-persistent))

Multi-turn conversation with both short-term and long-term memory. Uses a file-backed store and a judge policy to decide what to persist.

```bash
go run ./examples/chatbot-persistent
```

## Providers

### Ollama

Connects to a locally running Ollama instance via its OpenAI-compatible endpoint (`http://localhost:11434/v1`).

```go
// Default — connects to localhost:11434
provider := ollama.New()

// Custom URL
provider := ollama.New(ollama.WithBaseURL("http://my-host:11434/v1"))
```

Supports text and images (with vision models like llava). No document support.

### Anthropic

Connects to the Anthropic Messages API. Reads `ANTHROPIC_API_KEY` from environment by default.

```go
provider := anthropic.New()

// Or with explicit configuration
provider := anthropic.New(
    anthropic.WithAPIKey("sk-..."),
    anthropic.WithMaxTokens(8192),
)
```

Supports text, images (JPEG, PNG, GIF, WebP — 5 MB limit), and documents (PDF, text/plain — 32 MB limit).

## License

Apache 2.0 — see [LICENSE](./LICENSE).
