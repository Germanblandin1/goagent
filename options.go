package goagent

import "log/slog"

// options holds the resolved configuration for an Agent.
type options struct {
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
}

// Option is a functional option for configuring an Agent.
type Option func(*options)

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

// WithLogger sets the structured logger used for debug output.
func WithLogger(l *slog.Logger) Option {
	return func(o *options) { o.logger = l }
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
