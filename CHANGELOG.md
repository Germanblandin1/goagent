# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[0.1.1]: https://github.com/Germanblandin1/goagent/releases/tag/v0.1.1
[0.1.0]: https://github.com/Germanblandin1/goagent/releases/tag/v0.1.0
