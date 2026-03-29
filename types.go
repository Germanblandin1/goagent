package goagent

import (
	"context"
	"time"
)

// Role identifies who authored a message in a conversation.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"

	// RoleSystem is provided for completeness and for callers that build
	// Provider implementations or raw message slices. When using Agent,
	// the system prompt is set via WithSystemPrompt and is forwarded to the
	// provider through CompletionRequest.SystemPrompt — never as a Message
	// with this role. Callers do not need to construct RoleSystem messages
	// directly.
	RoleSystem Role = "system"
)

// Message is a single turn in a conversation.
//
// Content is a slice of ContentBlock that can hold text, images, documents,
// or any combination. For simple text messages, use the helpers TextMessage()
// or UserMessage(). To extract concatenated text from all blocks, use
// TextContent().
type Message struct {
	Role    Role
	Content []ContentBlock

	// ToolCalls is non-empty when the model requests one or more tool
	// invocations. Only set on assistant messages (Role == RoleAssistant).
	ToolCalls []ToolCall

	// ToolCallID is the ID of the ToolCall this message is a result for.
	// Must be set — and must exactly match ToolCall.ID — when Role == RoleTool.
	// The Agent sets this automatically when building tool result messages;
	// Provider implementations must populate ToolCall.ID for the correlation
	// to work correctly.
	ToolCallID string
}

// ToolCall represents a request from the model to invoke a tool.
type ToolCall struct {
	// ID is the opaque identifier assigned by the model to this tool call.
	// It must be echoed back in Message.ToolCallID of the corresponding
	// tool result message so the model can correlate the result with the
	// request. Provider implementations must populate this field; leaving
	// it empty will cause the next completion to fail on APIs that enforce
	// the tool call / tool result pairing (e.g. Anthropic, OpenAI).
	ID string

	// Name is the tool name the model wants to invoke, matching the
	// Name field of a registered ToolDefinition.
	Name string

	// Arguments contains the arguments the model supplied for this call,
	// decoded from the provider's JSON payload. Keys match the parameter
	// names defined in ToolDefinition.Parameters.
	Arguments map[string]any
}

// StopReason indicates why the model stopped generating.
type StopReason int

const (
	StopReasonEndTurn   StopReason = iota // model produced a final answer
	StopReasonMaxTokens                   // token limit reached
	StopReasonToolUse                     // model wants to call tools
	StopReasonError                       // provider-level error
)

// String returns a human-readable representation of the stop reason.
func (s StopReason) String() string {
	switch s {
	case StopReasonEndTurn:
		return "end_turn"
	case StopReasonMaxTokens:
		return "max_tokens"
	case StopReasonToolUse:
		return "tool_use"
	case StopReasonError:
		return "error"
	default:
		return "unknown"
	}
}

// ThinkingConfig configures the model's extended thinking mode.
//
// Manual mode: Enabled true, BudgetTokens > 0.
// The model uses up to BudgetTokens tokens for internal reasoning before
// responding or invoking a tool. Minimum: 1024 tokens.
//
// Adaptive mode: Enabled true, BudgetTokens 0.
// The model decides how much to reason based on prompt complexity.
// Recommended for Opus 4.6 and Sonnet 4.6.
//
// Disabled: Enabled false (or nil pointer in CompletionRequest).
type ThinkingConfig struct {
	Enabled      bool
	BudgetTokens int
}

// CompletionRequest is the input to a provider's Complete call.
type CompletionRequest struct {
	// Model is the exact model identifier forwarded to the provider
	// (e.g. "llama3", "qwen3"). Interpretation is provider-specific;
	// the framework passes it through without validation.
	Model string

	// SystemPrompt is the system-level instruction for the model.
	// Providers must forward it using their native mechanism — for
	// OpenAI-compatible APIs this means prepending a system message;
	// for Anthropic it maps to the top-level "system" field.
	// Empty string means no system prompt.
	SystemPrompt string

	// Messages is the conversation history to send, in chronological order.
	// The slice is never nil when sent by Agent, but Provider implementations
	// must handle a nil or empty slice without panicking.
	Messages []Message

	// Tools is the list of tools the model may call during this completion.
	// Nil or empty means no tool use is available for this request.
	// Providers must not error when this field is nil.
	Tools []ToolDefinition

	// Thinking configures extended thinking for this request.
	// nil means thinking is disabled (default behaviour).
	// Providers that do not support thinking must ignore this field.
	Thinking *ThinkingConfig

	// Effort controls the overall effort the model puts into its response,
	// affecting text, tool calls, and thinking (when enabled).
	// Valid values: "high", "medium", "low". Empty string means the model's
	// default (equivalent to "high"). Thinking and Effort are orthogonal —
	// each can be set independently.
	// Providers that do not support effort must ignore this field.
	Effort string
}

// CompletionResponse is the output from a provider's Complete call.
type CompletionResponse struct {
	// Message is the model's reply. Role is always RoleAssistant.
	// Content may be empty if the model produced only tool calls.
	// ToolCalls is non-empty when StopReason is StopReasonToolUse.
	Message Message

	// StopReason indicates why the model stopped generating.
	StopReason StopReason

	// Usage reports token consumption for this completion.
	// Providers should populate this when the API returns it;
	// zero values are valid if the backend does not expose token counts.
	Usage Usage
}

// Usage reports token consumption for a completion.
type Usage struct {
	// InputTokens is the number of tokens in the prompt (including history).
	InputTokens int

	// OutputTokens is the number of tokens in the model's response.
	OutputTokens int

	// CacheCreationInputTokens is the number of tokens written to the prompt
	// cache during this request. Only populated by providers that support
	// prompt caching (e.g. Anthropic); zero otherwise.
	CacheCreationInputTokens int

	// CacheReadInputTokens is the number of tokens read from the prompt
	// cache during this request. Only populated by providers that support
	// prompt caching (e.g. Anthropic); zero otherwise.
	CacheReadInputTokens int
}

// add accumulates the token counts from other into u.
func (u *Usage) add(other Usage) {
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
	u.CacheCreationInputTokens += other.CacheCreationInputTokens
	u.CacheReadInputTokens += other.CacheReadInputTokens
}

// ProviderEvent captures metrics from a single provider call within the
// ReAct loop. It is passed to Hooks.OnProviderResponse after each
// Provider.Complete invocation, whether it succeeds or fails.
type ProviderEvent struct {
	// Duration is the wall-clock time of the Provider.Complete call.
	Duration time.Duration

	// Usage reports token consumption for this completion.
	// Zero value if the call failed or the provider does not report usage.
	Usage Usage

	// StopReason indicates why the model stopped generating.
	// Only meaningful when Err is nil.
	StopReason StopReason

	// ToolCalls is the number of tool calls the model requested in this
	// completion. Zero when the model produced a final answer or an error.
	ToolCalls int

	// Err is nil on success. On provider failure it carries the underlying
	// error (before wrapping as *ProviderError).
	Err error
}

// RunResult aggregates metrics for an entire Run/RunBlocks call.
// It is passed to Hooks.OnRunEnd and optionally written to a caller-supplied
// pointer via WithRunResult.
type RunResult struct {
	// Duration is the wall-clock time of the entire Run, from entry to return.
	Duration time.Duration

	// Iterations is the number of ReAct iterations executed (1-indexed).
	// On success this is the iteration that produced the final answer.
	// On MaxIterationsError this equals the configured maximum.
	Iterations int

	// TotalUsage is the sum of token counts across all provider calls.
	TotalUsage Usage

	// ToolCalls is the total number of tool invocations across all iterations.
	ToolCalls int

	// ToolTime is the total wall-clock time spent executing tools.
	// Parallel tool calls within the same iteration overlap, so this may
	// exceed the actual elapsed time.
	ToolTime time.Duration

	// Err is nil when Run succeeds. Otherwise it carries the same error
	// that Run returns (*ProviderError, *MaxIterationsError, context error, etc.).
	Err error
}

// Provider is the interface that wraps a language model backend.
// Callers supply a Provider to Agent via WithProvider.
type Provider interface {
	Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}
