package main

// goagent documentation excerpts used as the RAG corpus.
// Sources match the RelevantSources in evalDataset.

const providerDoc = `# Provider

Provider is the LLM backend abstraction in goagent.

## Interface

	type Provider interface {
	    Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
	}

Provider is defined in the goagent package and implemented in providers/.
The Anthropic provider (providers/anthropic) calls the Anthropic Messages API.
The Ollama provider (providers/ollama) calls a local OpenAI-compatible API.

## CompletionRequest

	type CompletionRequest struct {
	    Model        string          // required: model identifier
	    SystemPrompt string          // system-level instruction
	    Messages     []Message       // conversation history
	    Tools        []ToolDefinition // tools the model may call
	    Thinking     *ThinkingConfig  // extended thinking (optional)
	    Effort       string           // "high"/"medium"/"low" (optional)
	}

req.Model is never empty when called by the agent — the agent sets it before calling.

## CompletionResponse

	type CompletionResponse struct {
	    Message    Message    // model's reply (RoleAssistant)
	    StopReason StopReason // why the model stopped
	    Usage      Usage      // token counts
	}

## Optional: StreamingProvider

	type StreamingProvider interface {
	    CompleteStream(ctx context.Context, req CompletionRequest) (Stream, error)
	}

The agent detects StreamingProvider at runtime. Not implementing it is valid.

## Error wrapping

Provider implementations must wrap errors as *ProviderError:

	return goagent.CompletionResponse{}, &goagent.ProviderError{
	    Provider: "myprovider",
	    Cause:    err,
	}
`

const architectureDoc = `# goagent Architecture

goagent is a minimalist Go framework for building AI agents using the ReAct loop.

## Package layout

	goagent/          — Agent, ReAct loop, core interfaces
	providers/
	  anthropic/      — Anthropic provider (Claude)
	  ollama/         — Ollama provider + embedder
	memory/           — BufferMemory, SlidingWindowMemory, VectorMemory
	  vector/         — VectorStore implementations (InMemory, pgvector, qdrant, sqlitevec)
	rag/              — RAG pipeline: chunking, embedding, retrieval, reranking
	orchestration/    — Pipeline, Graph, ParallelGroup, Supervisor
	otel/             — OpenTelemetry integration
	examples/         — Runnable examples

## ReAct loop

The Agent iterates:
1. Assemble prompt (system + memory + history + user turn)
2. Call provider.Complete
3. If model returns tool calls: execute tools in parallel, feed results back, loop
4. If model returns a final answer: return it to the caller

WithMaxIterations(n) caps the loop (default 10). When the budget is exhausted,
the agent returns the last response with no error — it is a safety bound, not a failure.

## Memory vs RAG

ShortTermMemory holds the active conversation window within a single session.
LongTermMemory recalls previous conversations via semantic search across sessions.
RAG (rag package) indexes arbitrary documents — not conversation history.
All three can be combined: ShortTerm for current turn, LongTerm for past sessions,
RAG for the knowledge base.

## Hooks

Hooks is a struct with optional function fields (OnStart, OnTurn, OnToolCall, etc.).
The agent calls each non-nil hook at the corresponding lifecycle event.
Adding new hook fields never breaks existing callers — existing Hooks structs simply
do not set the new field, and the agent skips nil hooks.

## Orchestration

orchestration.NewPipeline chains stages sequentially. Each stage wraps an Executor.
orchestration.NewGraph enables conditional branching based on node output.
StageContext carries outputs between stages; use SetOutput/RequireOutput to share data.
`

const toolsDoc = `# Tools

Tools allow the agent to call external functions during the ReAct loop.

## Tool interface

	type Tool interface {
	    Definition() ToolDefinition
	    Execute(ctx context.Context, args map[string]any) ([]ContentBlock, error)
	}

Definition() takes no ctx — it is a constant descriptor.
Execute receives args already deserialized from the model's JSON.

## Tool error handling

Errors from Execute are reported to the model as text — they do not abort the loop
or affect other tools. The model decides whether to retry or give up.

Internally, the agent wraps tool errors in *ToolExecutionError:

	type ToolExecutionError struct {
	    ToolName string
	    Args     map[string]any
	    Cause    error
	}

The error is forwarded to the model as a tool result, not returned to the Run caller.
The Run caller receives a ToolExecutionError only if the agent decides to stop on it.

## Constructors

Three patterns:

	// ToolFunc: returns plain text
	calc := goagent.ToolFunc("calculator", "Evaluates arithmetic",
	    goagent.SchemaFrom(struct{ Expression string }{}),
	    func(ctx context.Context, args map[string]any) (string, error) { ... },
	)

	// ToolBlocksFunc: returns ContentBlocks (multimodal)
	img := goagent.ToolBlocksFunc("screenshot", "Captures screen",
	    goagent.SchemaFrom(struct{}{}),
	    func(ctx context.Context, args map[string]any) ([]goagent.ContentBlock, error) { ... },
	)

	// Struct implementing Tool directly
	type MyTool struct{ client *http.Client }
	func (t *MyTool) Definition() goagent.ToolDefinition { ... }
	func (t *MyTool) Execute(ctx context.Context, args map[string]any) ([]goagent.ContentBlock, error) { ... }

## Circuit breaker and timeout

WithToolTimeout(d) cancels the tool's ctx after duration d.
WithCircuitBreaker(n, d) opens the circuit after n consecutive failures.
A CircuitOpenError wraps a ToolExecutionError when the circuit is open.
`

const errorsDoc = `# Error handling

goagent uses typed errors throughout the library.

## Sentinel errors

	var ErrToolNotFound       = errors.New("tool not found")
	var ErrUnsupportedContent = errors.New("unsupported content type")
	var ErrInvalidMediaType   = errors.New("invalid media type")

Usage: errors.Is(err, goagent.ErrToolNotFound)

## Typed errors

ToolExecutionError wraps a failed tool call. It is passed to the model as a result,
not returned to the Run caller — unless the agent decides to stop.

	type ToolExecutionError struct {
	    ToolName string
	    Args     map[string]any
	    Cause    error          // implements Unwrap
	}

ProviderError wraps an LLM backend error:

	type ProviderError struct {
	    Provider string
	    Cause    error
	}

MaxIterationsError is returned when the loop exhausts its budget:

	type MaxIterationsError struct {
	    Iterations  int
	    LastThought string
	}

## Wrapping convention

All errors are wrapped with context using %w:

	return fmt.Errorf("operation: %w", err)

Library code never uses bare fmt.Errorf("...") without a sentinel or typed wrapper.
`

const ragDoc = `# RAG in goagent

RAG (Retrieval-Augmented Generation) is implemented in the rag package.

## Pipeline

	pipeline, err := rag.NewPipeline(chunker, embedder, store)

Three components:
- vector.Chunker: splits documents into overlapping text segments
- goagent.Embedder: converts text blocks to float32 vectors
- goagent.VectorStore: stores and retrieves vectors by cosine similarity

Methods:
- Index(ctx, docs...): chunk → embed → upsert. Idempotent: re-indexing by the same Source replaces existing chunks.
- Search(ctx, query, topK): embed query → vector search → return []SearchResult

## SearchResult

	type SearchResult struct {
	    Message     goagent.Message  // content blocks
	    Score       float64          // cosine similarity [0,1]
	    RerankScore float64          // reranker score (0 if not reranked)
	    Source      string           // origin document
	}

## Reranking (over-fetch pattern)

	rag.WithReranker(rag.NewLLMReranker(provider, model), rerankN)

Search fetches rerankN candidates from the store, then the reranker selects topK.
LLMReranker scores each (query, passage) pair in parallel with the configured provider.

## Agentic RAG patterns

Pattern 1 — Tool: rag.NewTool(pipeline) wraps the pipeline as a goagent.Tool.
Pattern 2 — Standalone: call pipeline.Search directly in application code.
Pattern 3 — Multi-agent: Planner decomposes query, parallel Go goroutines search,
  Synthesizer merges results.

## Evaluation metrics (rag package)

PrecisionAtK: fraction of top-K retrieved that are relevant.
RecallAtK: fraction of all relevant docs retrieved within top-K.
ReciprocalRank: 1/position of the first relevant result.
MRR: mean ReciprocalRank across multiple cases.
`

const memoryDoc = `# Memory in goagent

goagent supports two memory types plus RAG for documents.

## ShortTermMemory

Holds conversation history within a single session.

	type ShortTermMemory interface {
	    Messages(ctx context.Context) ([]Message, error)
	    Append(ctx context.Context, msgs ...Message) error
	}

Built-in: BufferMemory (unlimited), SlidingWindowMemory (last N messages).
Configure with goagent.WithShortTermMemory(mem).

Filtering policies live in memory/policy:
- FixedWindow: keeps the last N messages
- TokenWindow: keeps messages within a token budget

Policies are applied in Messages() — never in Append().

## LongTermMemory

Semantic retrieval across sessions using a vector store and an embedder.

	type LongTermMemory interface {
	    Store(ctx context.Context, msgs ...Message) error
	    Retrieve(ctx context.Context, query []ContentBlock, topK int, opts ...SearchOption) ([]ScoredMessage, error)
	}

Configure with goagent.WithLongTermMemory(ltm).
Use goagent.WithLongTermTopK(k) to control how many past messages are retrieved per run.
Use goagent.WithWritePolicy(p) to control what gets persisted (StoreAlways, MinLength).

## Memory vs RAG

ShortTermMemory holds the active conversation window.
LongTermMemory recalls previous conversations.
RAG (rag package) indexes arbitrary document corpora — not conversation history.
All three can be combined.
`

const hooksDoc = `# Hooks

Hooks provide observability callbacks for the agent lifecycle.

## Hooks struct

	type Hooks struct {
	    OnStart    func(ctx context.Context, input string)
	    OnTurn     func(ctx context.Context, turn int, response CompletionResponse)
	    OnToolCall func(ctx context.Context, name string, args map[string]any)
	    OnToolResult func(ctx context.Context, name string, result []ContentBlock, err error)
	    OnEnd      func(ctx context.Context, response string, err error)
	}

All fields are optional. The agent calls each non-nil hook at the corresponding
lifecycle event. A Hooks{} zero value is valid and is a no-op.

## Adding new hooks

Adding new fields to the Hooks struct never breaks existing callers. Callers that
set only some fields are not affected when new fields are added. The agent checks
each field for nil before calling it.

## Configuration

	agent, _ := goagent.New(
	    goagent.WithProvider(provider),
	    goagent.WithHooks(goagent.Hooks{
	        OnToolCall: func(ctx context.Context, name string, args map[string]any) {
	            slog.Info("tool called", "name", name)
	        },
	    }),
	)

## Integration with OpenTelemetry

The otel/ package provides hook implementations that emit spans and RED metrics.
Use otel.WithHooks(tracer) to add tracing without modifying application code.
`

const agentDoc = `# Agent

Agent is the core type in goagent. It runs the ReAct loop.

## Constructor

	agent, err := goagent.New(
	    goagent.WithProvider(provider),   // required
	    goagent.WithModel("model-id"),    // required
	    goagent.WithSystemPrompt("..."),  // optional
	    goagent.WithTool(myTool),         // optional, repeatable
	    goagent.WithMaxIterations(10),    // default: 10
	)

## Run

	response, err := agent.Run(ctx, "user prompt")

Run executes the ReAct loop synchronously and returns the model's final answer.

## WithMaxIterations

WithMaxIterations sets the maximum number of ReAct loop iterations.
Default: 10.

When the iteration budget is exhausted, the agent returns the last response
without error — it is a safety bound, not a failure. The caller can detect this
by checking for MaxIterationsError in the error:

	_, err := agent.Run(ctx, prompt)
	var maxErr *goagent.MaxIterationsError
	if errors.As(err, &maxErr) {
	    fmt.Printf("stopped after %d iterations\n", maxErr.Iterations)
	}

## Concurrency

Agent is immutable after New(). All fields are written once.
Run is safe for concurrent calls — each call manages its own state.

## RunStream

	err := agent.RunStream(ctx, "user prompt", handler, opts...)

RunStream uses the provider's streaming interface when available. Falls back to
Complete when the provider does not implement StreamingProvider.
`
