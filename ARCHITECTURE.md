# goagent Architecture

Minimalist, idiomatic Go framework for building AI agents with a ReAct (Reason + Act) loop.

## Package layout

```
goagent/                      Root package — Agent, ReAct loop, core interfaces
├── mcp/                      MCP client (stdio + SSE), MCP server, adapter and router
├── memory/                   Memory implementations
│   ├── storage/              Message persistence (InMemory, etc.)
│   └── policy/               Read-time message filtering (FixedWindow, etc.)
├── providers/
│   ├── anthropic/            Provider for Claude (Anthropic API)
│   └── ollama/               Provider for local models (OpenAI-compatible API)
├── examples/
│   ├── calculator/           Example: agent with a calculator tool
│   ├── chatbot/              Example: multi-turn conversation with memory
│   ├── chatbot-persistent/   Example: file-backed persistence
│   └── chatbot-mcp-fs/       Example: chatbot with filesystem access via MCP stdio
└── internal/
    └── testutil/             Provider, Tool and Memory mocks for tests
```

## Core interfaces

The design is built on small interfaces (≤3 methods) that compose with each other:

```go
// Provider — LLM backend (Anthropic, Ollama, etc.)
type Provider interface {
    Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}

// Tool — a tool the model can invoke
type Tool interface {
    Definition() ToolDefinition
    Execute(ctx context.Context, args map[string]any) ([]ContentBlock, error)
}

// ShortTermMemory — conversation history across Run calls
type ShortTermMemory interface {
    Messages(ctx context.Context) ([]Message, error)
    Append(ctx context.Context, msgs ...Message) error
}

// LongTermMemory — semantic retrieval across sessions
type LongTermMemory interface {
    Store(ctx context.Context, msgs ...Message) error
    Retrieve(ctx context.Context, query []ContentBlock, topK int) ([]Message, error)
}
```

## Agent and the Functional Options pattern

`Agent` is immutable after construction. All configuration is injected via functional options.

**`New` returns `(*Agent, error)`** — if MCP connectors are configured, `New` establishes the connections during construction and returns an error if any fail. Always call `defer agent.Close()` to release MCP resources.

```go
agent, err := goagent.New(
    goagent.WithProvider(provider),                // required
    goagent.WithModel("claude-sonnet-4-6"),
    goagent.WithTool(myTool),
    goagent.WithSystemPrompt("You are an assistant..."),
    goagent.WithMaxIterations(10),                 // default: 10
    goagent.WithShortTermMemory(mem),              // optional
    goagent.WithLongTermMemory(ltm),               // optional
    goagent.WithThinking(8000),                    // optional: extended thinking
    goagent.WithEffort("medium"),                  // optional: effort level
    goagent.WithHooks(goagent.Hooks{...}),         // optional: observability
    mcp.WithStdio("npx", "-y", "@mcp/server-fs"),  // optional: MCP tools
)
if err != nil {
    log.Fatal(err)
}
defer agent.Close()
```

The Agent holds no mutable state — all fields are written once by `New` and never modified again. This makes `Run` safe for concurrent use as long as the injected implementations (Provider, Memory) are also safe.

---

## The ReAct Loop — core of the framework

The ReAct loop (Reason + Act) is implemented in `agent.go:129-209` inside the private `run()` method. Both `Run(ctx, prompt)` and `RunBlocks(ctx, blocks...)` delegate to this method.

### Full flow

```
┌──────────────────────────────────────────────────────┐
│  1. BUILD MESSAGES  (buildMessages)                  │
│     ├─ Retrieve context from LongTermMemory          │
│     ├─ Load history from ShortTermMemory             │
│     └─ Append current user message                   │
│     result: [long-term...] + [short-term...] + [user]│
└──────────────────────────────────────────────────────┘
                        │
                        ▼
┌──────────────────────────────────────────────────────┐
│  2. CREATE DISPATCHER                                │
│     Maps name → Tool for O(1) lookup                 │
└──────────────────────────────────────────────────────┘
                        │
                        ▼
┌──────────────────────────────────────────────────────┐
│  3. LOOP  for i := 0; i < maxIterations; i++         │
│                                                      │
│  ┌────────────────────────────────────────────────┐  │
│  │ a. Check ctx.Done() (cancellation)             │  │
│  └────────────────────────────────────────────────┘  │
│                      │                               │
│                      ▼                               │
│  ┌────────────────────────────────────────────────┐  │
│  │ b. provider.Complete(ctx, request)             │  │
│  │    Sends: model, systemPrompt, messages, tools │  │
│  │    Receives: Message + StopReason + Usage      │  │
│  └────────────────────────────────────────────────┘  │
│                      │                               │
│                      ▼                               │
│  ┌────────────────────────────────────────────────┐  │
│  │ c. Append response to messages                 │  │
│  │    Extract text: lastContent                   │  │
│  └────────────────────────────────────────────────┘  │
│                      │                               │
│               ┌──────┴──────┐                        │
│               │             │                        │
│           ToolUse?     No ToolUse                    │
│               │             │                        │
│               ▼             ▼                        │
│  ┌──────────────────┐  ┌─────────────────────┐      │
│  │ d. DISPATCH       │  │ FINAL RESPONSE      │      │
│  │ Run tools         │  │ persistTurn()       │      │
│  │ in parallel       │  │ return lastContent  │──────┼──► END
│  │ (fan-out/fan-in)  │  └─────────────────────┘      │
│  └──────────────────┘                                │
│           │                                          │
│           ▼                                          │
│  ┌──────────────────────────────────────────────┐    │
│  │ e. Append tool results as messages           │    │
│  │    with Role: RoleTool                       │    │
│  └──────────────────────────────────────────────┘    │
│           │                                          │
│           └───────── next iteration ─────────────────┘
│                                                      │
│  If iterations are exhausted:                        │
│    persistTurn() + return MaxIterationsError         │
└──────────────────────────────────────────────────────┘
```

### Step by step with the code

**1. Entry and validation** (`agent.go:129-132`)

```go
func (a *Agent) run(ctx context.Context, content []ContentBlock) (string, error) {
    if a.opts.provider == nil {
        return "", errors.New("goagent: no provider configured; use WithProvider")
    }
```

If there is no Provider, it fails immediately. The Provider is the only required component.

**2. Building messages** (`agent.go:134-137`, `buildMessages` at `agent.go:217-242`)

`buildMessages` assembles the full context the model will see:

1. If `LongTermMemory` is configured, it calls `Retrieve(ctx, content, topK)` to fetch semantically relevant messages from past sessions.
2. If `ShortTermMemory` is configured, it calls `Messages(ctx)` to load recent history (already filtered by the configured Policy).
3. Appends the current user message at the end.

It also returns `historyLen` — the number of messages that came from memory. This is used at the end to determine which messages are new (the delta for this turn).

**3. The loop** (`agent.go:149-201`)

Each iteration follows the Reason-Act pattern:

- **Reason**: The model receives all accumulated messages (history + prior tool results) and decides what to do.
- **Act**: If the model requests tool use (`StopReason == StopReasonToolUse`), the dispatcher runs them in parallel and the results are appended as `RoleTool` messages.

The exit condition is:

```go
if resp.StopReason != StopReasonToolUse || len(resp.Message.ToolCalls) == 0 {
    // The model produced a final response
    a.persistTurn(ctx, messages, historyLen, lastContent)
    return lastContent, nil
}
```

The loop exits when the model decides it has enough information to answer (emits `StopReasonEndTurn`) or when `StopReasonMaxTokens` is reached.

**4. Parallel tool dispatch** (`dispatcher.go:30-44`)

```go
func (d *dispatcher) dispatch(ctx context.Context, calls []ToolCall) []ToolResult {
    results := make([]ToolResult, len(calls))
    var wg sync.WaitGroup
    for i, call := range calls {
        wg.Add(1)
        go func(idx int, tc ToolCall) {
            defer wg.Done()
            results[idx] = d.execute(ctx, tc)
        }(i, call)
    }
    wg.Wait()
    return results
}
```

Each tool runs in its own goroutine. Since each goroutine writes to its own index in the `results` slice, no mutex is needed. If a tool fails or does not exist, the error is captured in the `ToolResult` and reported to the model as text — it does not abort the other tools or the loop.

**5. Tool results → messages** (`agent.go:188-200`)

Results are converted into messages with `Role: RoleTool`:

```go
for _, r := range results {
    var toolContent []ContentBlock
    if r.Err != nil {
        toolContent = []ContentBlock{TextBlock(fmt.Sprintf("Error: %s", r.Err.Error()))}
    } else {
        toolContent = r.Content
    }
    messages = append(messages, Message{
        Role:       RoleTool,
        Content:    toolContent,
        ToolCallID: r.ToolCallID,
    })
}
```

Each result is linked to the original `ToolCallID` so the model knows which tool produced which result. Errors are sent as text so the model can reason about them and decide whether to retry or offer an alternative response.

---

## Memory persistence — persistTurn

After producing a response (or exhausting iterations), `persistTurn` (`agent.go:251-283`) saves the turn to the configured memories:

### ShortTermMemory

Two modes controlled by `WithShortTermTraceTools`:

- **`traceTools: true`** (default): Saves the full trace — user message + all model calls + all tool results. The model sees exactly what happened in previous turns.
- **`traceTools: false`**: Saves only the user message and the final response. More compact, but the model loses visibility of the intermediate reasoning.

### LongTermMemory

A `WritePolicy` decides what to store:

- **`StoreAlways`** (default): Always saves `[prompt, final_response]`.
- **`MinLength(n)`**: Only saves if the combined text exceeds `n` characters.
- **Custom policy**: Can transform, condense, or discard the turn.

Memory errors are logged as warnings but are **never** propagated to the caller — the response has already been produced correctly.

---

## Message types

```go
type Message struct {
    Role       Role           // user, assistant, tool, system
    Content    []ContentBlock // text, image or document
    ToolCalls  []ToolCall     // only in assistant messages (tool requests)
    ToolCallID string         // only in tool messages (links result to request)
}
```

Multimodal support is first-class: `ContentBlock` can be text (`ContentText`), image (`ContentImage` — JPEG, PNG, GIF, WebP) or document (`ContentDocument` — PDF, text/plain).

## Tools

Two helpers for creating tools without defining a struct:

- **`ToolFunc`**: For tools that return plain text (`string`).
- **`ToolBlocksFunc`**: For tools that return multimodal content (`[]ContentBlock`).

```go
calc := goagent.ToolFunc("calculator", "Evaluates expressions", schema,
    func(ctx context.Context, args map[string]any) (string, error) {
        // ...
        return "42", nil
    },
)
```

## Typed errors

All errors are typed and support `errors.Is` / `errors.As`:

| Error | When |
|-------|------|
| `*ProviderError` | The provider returned an error |
| `*MaxIterationsError` | The iteration budget was exhausted |
| `*ToolExecutionError` | A tool failed (does not abort the loop) |
| `*UnsupportedContentError` | The provider does not support a content type |
| `ErrToolNotFound` | A tool was requested that does not exist |
| `ErrInvalidMediaType` | Invalid MIME type in a ContentBlock |

## Providers

Both implement the `Provider` interface and are configured with their own functional options:

- **`providers/anthropic`**: Uses the official Anthropic SDK. Supports text, images and documents.
- **`providers/ollama`**: Uses the OpenAI-compatible API. Supports text and images (model-dependent). Does not support documents.

---

## Extended Thinking and Effort

### Thinking

Controls the model's internal reasoning before responding or invoking tools.

```go
// Fixed budget: the model uses up to N tokens to reason
goagent.WithThinking(16000)

// Adaptive: the model decides how much to reason based on prompt complexity
goagent.WithAdaptiveThinking()
```

`ThinkingConfig` is the internal type propagated to the provider in each `CompletionRequest`:

```go
type ThinkingConfig struct {
    Enabled      bool
    BudgetTokens int  // 0 = adaptive
}
```

When the model produces a thinking block, the `OnThinking` hook fires before the final text reaches the caller. Thinking blocks are **not included** in the text returned by `Run`.

Supported models: `claude-sonnet-4-6`, `claude-opus-4-6`, `claude-sonnet-3-7`, `claude-opus-4`, `claude-opus-4-5`.

### Effort

Controls the overall effort the model puts into its response, affecting quality, tool call accuracy, and reasoning depth.

```go
goagent.WithEffort("high")    // maximum effort (equivalent to the model's default behaviour)
goagent.WithEffort("medium")  // balanced quality and cost — recommended for most tasks
goagent.WithEffort("low")     // faster and cheaper — best for simple classification
```

Thinking and effort are **orthogonal** and can be combined freely. On models that do not support effort, the option is silently ignored.

---

## Observability — Hooks

`Hooks` lets you observe ReAct loop events without modifying its behaviour. All fields are optional and injected with `WithHooks`:

```go
agent, _ := goagent.New(
    goagent.WithProvider(provider),
    goagent.WithHooks(goagent.Hooks{
        OnIterationStart: func(iteration int) {
            fmt.Printf("iteration %d\n", iteration)
        },
        OnThinking: func(text string) {
            fmt.Printf("thinking: %s\n", text)
        },
        OnToolCall: func(name string, args map[string]any) {
            fmt.Printf("→ %s(%v)\n", name, args)
        },
        OnToolResult: func(name string, content []ContentBlock, d time.Duration, err error) {
            fmt.Printf("← %s in %s\n", name, d)
        },
        OnResponse: func(text string, iterations int) {
            fmt.Printf("final response (%d iterations)\n", iterations)
        },
    }),
)
```

| Hook | When it fires |
|------|--------------|
| `OnIterationStart(iteration int)` | At the start of each iteration, before calling the provider |
| `OnThinking(text string)` | Once per thinking block in the model's response |
| `OnToolCall(name, args)` | When the model requests a tool, before dispatch |
| `OnToolResult(name, content, duration, err)` | After each tool execution (even on failure) |
| `OnResponse(text, iterations)` | Just before `Run` returns, including on MaxIterationsError |

Hooks are invoked **synchronously** inside the loop. For heavy work (sending to external services, distributed logging), the hook should spawn a goroutine internally to avoid blocking the loop.

---

## MCP — Model Context Protocol

The `mcp/` package integrates the MCP protocol both as a consumer (client) and as a server. It allows the agent to use tools exposed by any external MCP server.

### As a client (consuming MCP tools)

The idiomatic way is to use `mcp.WithStdio` or `mcp.WithSSE` directly as a `goagent.Option`:

```go
import "github.com/Germanblandin1/goagent/mcp"

agent, err := goagent.New(
    goagent.WithProvider(provider),
    // Launch an npx process and connect via stdio
    mcp.WithStdio("npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp"),
    // Connect to a remote MCP server via SSE
    mcp.WithSSE("http://localhost:8080/sse"),
)
defer agent.Close() // closes subprocesses and HTTP connections
```

`New` performs the MCP handshake and discovers tools during construction. On failure it closes all already-opened connections and returns an error.

#### Client lifecycle

```
New()
  └─ WithMCPConnector → NewStdioClient / NewSSEClient
       ├─ initializeWithRetry (handshake, up to 3 attempts with exponential backoff)
       ├─ discoverTools (tools/list → adapts each Tool to goagent.Tool)
       └─ registers io.Closer for shutdown

agent.Close()
  └─ closes all registered io.Closers (terminates subprocesses, closes HTTP)
```

#### Supported transports

| Transport | Constructor | When to use |
|-----------|-------------|-------------|
| `stdio` | `mcp.NewStdioClient(ctx, logger, cmd, args...)` | Local server launched as a subprocess |
| `SSE` | `mcp.NewSSEClient(ctx, logger, url)` | Remote HTTP+SSE server already running |

#### Adapter and Router

- **Adapter** (`mcp/adapter.go`): converts an `mcp.Tool` (MCP protocol) into a `goagent.Tool` (framework interface). Created internally during `discoverTools`.
- **Router** (`mcp/router.go`): routes tool calls to the correct MCP server when multiple connectors are configured.

### As a server (exposing tools via MCP)

```go
s := mcp.NewServer("weather-service", "1.0.0")

s.MustAddTool("get_weather",
    "Returns current weather for a city",
    struct {
        City string `json:"city"`
    }{},
    func(ctx context.Context, args map[string]any) (string, error) {
        city, _ := args["city"].(string)
        return fetchWeather(city)
    },
)

// Local mode (subprocess via stdin/stdout)
s.ServeStdio()

// Remote mode (HTTP + SSE)
s.ServeSSE(":8080")
```

`AddTool` returns an error if the schema cannot be serialised to JSON. `MustAddTool` panics — appropriate for static construction where an invalid schema is a programming error.

### Typed MCP errors

| Error | When |
|-------|------|
| `*MCPConnectionError` | MCP handshake with the server failed |
| `*MCPDiscoveryError` | `tools/list` call failed |

---

## End-to-end flow example

```
User: "What is (123 × 456) + 789?"

Iteration 1:
  → provider.Complete([user: "What is...?"])
  ← assistant: ToolCall{calculator, {op:"mul", a:123, b:456}}
  → dispatcher runs calculator → "56088"
  ← tool: "56088"

Iteration 2:
  → provider.Complete([user, assistant, tool: "56088"])
  ← assistant: ToolCall{calculator, {op:"add", a:56088, b:789}}
  → dispatcher runs calculator → "56877"
  ← tool: "56877"

Iteration 3:
  → provider.Complete([user, assistant, tool, assistant, tool: "56877"])
  ← assistant: "The result is 56,877" (StopReason: EndTurn)
  → persistTurn() → save to memory if configured
  → return "The result is 56,877"
```
