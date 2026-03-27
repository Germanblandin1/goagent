# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

