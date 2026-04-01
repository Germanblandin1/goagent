package goagent

import (
	"context"
	"io"
	"log/slog"
	"time"
)

// MCPConnectorFn is a function that establishes an MCP connection, discovers
// tools, and returns them along with a Closer for lifecycle management.
// It is called by New for each WithMCP* option applied to the agent.
type MCPConnectorFn func(ctx context.Context, logger *slog.Logger) ([]Tool, io.Closer, error)

// options holds the resolved configuration for an Agent.
type options struct {
	name           string
	model          string
	maxIterations  int
	tools          []Tool
	systemPrompt   string
	provider       Provider
	shortTerm      ShortTermMemory
	longTerm       LongTermMemory
	writePolicy    WritePolicy
	longTermTopK   int
	traceTools     bool
	logger         *slog.Logger
	thinking       *ThinkingConfig
	effort         string
	hooks          Hooks
	runResult      *RunResult
	mcpConnectors  []MCPConnectorFn
	mcpClosers     []io.Closer
	toolTimeout    time.Duration
	cbMaxFailures  int
	cbResetTimeout time.Duration
	dispatchMWs    []DispatchMiddleware
}

// Option is a functional option for configuring an Agent.
type Option func(*options)

// WithName assigns a name to the agent. When a LongTermMemory is configured,
// the name is used as the session namespace: all vectors stored and retrieved
// during Run are scoped to this name, so two agents sharing the same
// LongTermMemory backend but different names cannot see each other's entries.
// If not set, no session filtering is applied.
func WithName(name string) Option {
	return func(o *options) { o.name = name }
}

// WithModel sets the model identifier forwarded to the provider. Required.
// The value is provider-specific (e.g. "qwen3" for Ollama, "claude-sonnet-4-6"
// for Anthropic). If not set, the provider may return an error.
func WithModel(model string) Option {
	return func(o *options) { o.model = model }
}

// WithTool registers a tool the agent may invoke during the ReAct loop.
func WithTool(t Tool) Option {
	return func(o *options) { o.tools = append(o.tools, t) }
}

// WithMaxIterations limits how many reasoning iterations the agent may perform.
// Defaults to 10.
func WithMaxIterations(n int) Option {
	return func(o *options) { o.maxIterations = n }
}

// WithSystemPrompt sets a system-level instruction sent to the provider on
// every Run call.
func WithSystemPrompt(prompt string) Option {
	return func(o *options) { o.systemPrompt = prompt }
}

// WithProvider sets the LLM backend used by the agent.
func WithProvider(p Provider) Option {
	return func(o *options) { o.provider = p }
}

// WithShortTermMemory configures the agent to persist and replay conversation
// history across Run calls using the provided ShortTermMemory.
// The default is stateless — each Run starts with no history.
//
// Note: sharing a ShortTermMemory across concurrent Run calls produces
// undefined message ordering. Sequential use (one Run at a time) is safe.
func WithShortTermMemory(m ShortTermMemory) Option {
	return func(o *options) { o.shortTerm = m }
}

// WithLongTermMemory configures the agent to retrieve semantically relevant
// context from past sessions before each Run, and to store the completed
// turn after each Run (subject to the configured WritePolicy).
// The default is no long-term memory.
//
// Note: concurrent Run calls on the same Agent will issue concurrent Retrieve
// and Store calls to this backend. Whether that is safe depends on the
// LongTermMemory implementation supplied by the caller.
func WithLongTermMemory(m LongTermMemory) Option {
	return func(o *options) { o.longTerm = m }
}

// WithWritePolicy sets the function that decides whether a completed turn
// (prompt + final response) is stored in long-term memory.
// Default: StoreAlways. Only effective when WithLongTermMemory is configured.
func WithWritePolicy(p WritePolicy) Option {
	return func(o *options) { o.writePolicy = p }
}

// WithLongTermTopK sets how many messages the long-term memory retrieves per
// Run. Default: 3.
func WithLongTermTopK(k int) Option {
	return func(o *options) { o.longTermTopK = k }
}

// WithShortTermTraceTools controls whether the full ReAct trace (tool calls
// and their results) is included when persisting to short-term memory.
//
//   - true (default): the complete trace is stored — user message, all
//     intermediate assistant turns, tool results, and final assistant answer.
//     The next Run will see the full reasoning history.
//   - false: only the user message and the final assistant answer are stored,
//     discarding intermediate tool call steps.
func WithShortTermTraceTools(include bool) Option {
	return func(o *options) { o.traceTools = include }
}

// WithLogger sets the structured logger for the agent's operational output.
//
// The agent logs at three levels:
//   - Info: run lifecycle (start, end, cancellation)
//   - Warn: recoverable failures (provider errors, memory write errors, circuit breaker open)
//   - Debug: per-iteration detail (provider calls, tool dispatch)
//
// Default: slog.Default(). The logger is never nil internally — the default is
// always applied. To suppress all log output, pass a logger with a discard handler:
//
//	goagent.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
func WithLogger(l *slog.Logger) Option {
	return func(o *options) { o.logger = l }
}

// WithThinking enables extended thinking with a fixed token budget.
// The model uses up to budgetTokens tokens for internal reasoning before
// responding or invoking a tool.
//
// Minimum: 1024 tokens. Recommended ranges:
//   - Simple tasks: 4 000–8 000
//   - Complex tasks (math, code): 10 000–16 000
//   - Deep reasoning: 16 000–32 000
//
// For budgets above 32 000 tokens, consider WithAdaptiveThinking instead.
// Supported models: claude-sonnet-4-6, claude-opus-4-6, claude-sonnet-3-7,
// claude-opus-4, claude-opus-4-5.
func WithThinking(budgetTokens int) Option {
	return func(o *options) {
		o.thinking = &ThinkingConfig{Enabled: true, BudgetTokens: budgetTokens}
	}
}

// WithAdaptiveThinking enables thinking in adaptive mode: the model decides
// how much to reason based on the complexity of each prompt.
// Recommended for claude-opus-4-6 and claude-sonnet-4-6.
//
// On models that do not support adaptive mode, the provider may fall back to
// a manual budget.
func WithAdaptiveThinking() Option {
	return func(o *options) {
		o.thinking = &ThinkingConfig{Enabled: true, BudgetTokens: 0}
	}
}

// WithEffort controls the overall effort the model puts into its response,
// affecting text quality, tool call accuracy, and reasoning depth.
//
// Valid values:
//   - "high":   maximum effort — equivalent to the model's default behaviour.
//   - "medium": balanced quality and cost — suitable for most tasks.
//   - "low":    faster and cheaper responses — best for simple classification
//     or extraction tasks.
//
// Effort and thinking are orthogonal and can be combined freely.
// Supported models: claude-opus-4-6, claude-sonnet-4-6, claude-opus-4-5.
// Models that do not support effort silently ignore this setting.
func WithEffort(level string) Option {
	return func(o *options) { o.effort = level }
}

// WithRunResult configures a destination pointer that the agent writes after
// each Run/RunBlocks call completes. The pointed-to RunResult is overwritten
// on every call, so the caller should read it before starting the next Run.
//
// This is a synchronous, non-hook alternative to Hooks.OnRunEnd for callers
// that prefer inspecting metrics after Run returns rather than inside a
// callback.
//
// Note: sharing the same pointer across concurrent Run calls is a data race.
// Use one pointer per goroutine or use Hooks.OnRunEnd with a mutex instead.
func WithRunResult(dst *RunResult) Option {
	return func(o *options) { o.runResult = dst }
}

// WithHooks registers observability callbacks for the ReAct loop.
// All fields of Hooks are optional — only non-nil hooks are invoked.
//
// Example:
//
//	agent := goagent.New(
//	    goagent.WithProvider(provider),
//	    goagent.WithHooks(goagent.Hooks{
//	        OnToolCall: func(name string, args map[string]any) {
//	            fmt.Printf("calling tool: %s\n", name)
//	        },
//	    }),
//	)
func WithHooks(h Hooks) Option {
	return func(o *options) { o.hooks = h }
}

// WithToolTimeout sets an independent deadline for each individual tool call,
// separate from the parent context. If a tool does not complete within d,
// its context is cancelled and the failure is recorded (including by the
// circuit breaker if configured). Zero disables per-tool timeouts.
func WithToolTimeout(d time.Duration) Option {
	return func(o *options) { o.toolTimeout = d }
}

// WithCircuitBreaker enables per-tool circuit breaking. After maxFailures
// consecutive failures, the tool is disabled for resetTimeout. Disabled tools
// return CircuitOpenError immediately without calling Execute.
// The OnCircuitOpen hook (if set) is called each time a call is rejected.
// maxFailures must be > 0; resetTimeout must be > 0.
func WithCircuitBreaker(maxFailures int, resetTimeout time.Duration) Option {
	return func(o *options) {
		o.cbMaxFailures = maxFailures
		o.cbResetTimeout = resetTimeout
	}
}

// WithDispatchMiddleware appends a custom DispatchMiddleware to the chain.
// Custom middlewares run after the built-in ones (logging → timeout →
// circuit breaker → custom → Execute).
// Multiple calls append in order: first call = outermost custom middleware.
func WithDispatchMiddleware(mw DispatchMiddleware) Option {
	return func(o *options) { o.dispatchMWs = append(o.dispatchMWs, mw) }
}

// WithMCPConnector registers an MCP connector that is called during New to
// establish a connection, discover tools, and obtain a Closer for lifecycle
// management. The connection is established once during New; if it fails, New
// returns an error and closes any already-opened connections.
//
// Callers typically use the higher-level helpers in the mcp sub-package
// (mcp.WithStdio, mcp.WithSSE) rather than calling this directly.
func WithMCPConnector(fn MCPConnectorFn) Option {
	return func(o *options) {
		o.mcpConnectors = append(o.mcpConnectors, fn)
	}
}

// defaults returns the baseline options before any Option is applied.
func defaults() *options {
	return &options{
		model:         "",
		maxIterations: 10,
		longTermTopK:  3,
		traceTools:    true,
		logger:        slog.Default(),
	}
}
