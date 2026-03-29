package goagent

import "time"

// Hooks allows observing events in the ReAct loop without modifying its
// behaviour. All fields are optional — a nil hook is silently skipped.
//
// Hooks are invoked synchronously within the loop. If a hook needs to
// perform heavy work (e.g. sending to an external service), it should
// spawn a goroutine internally to avoid blocking the loop.
//
// The zero value of Hooks is functional and invokes no callbacks.
//
// Example:
//
//	agent := goagent.New(
//	    goagent.WithProvider(provider),
//	    goagent.WithHooks(goagent.Hooks{
//	        OnToolCall: func(name string, args map[string]any) {
//	            fmt.Printf("tool: %s\n", name)
//	        },
//	    }),
//	)
type Hooks struct {
	// OnRunStart is called at the beginning of each Run/RunBlocks call,
	// before loading memory or building messages. Use it to initialise
	// external metrics or start a tracing span for the entire Run.
	OnRunStart func()

	// OnRunEnd is called at the end of each Run/RunBlocks call, just
	// before returning to the caller. It is always called — on success,
	// provider failure, MaxIterationsError, and context cancellation.
	//
	// result contains accumulated metrics: total duration, iterations,
	// total token usage, total tool calls, and total tool execution time.
	// result.Err is nil when the Run succeeds.
	OnRunEnd func(result RunResult)

	// OnProviderRequest is called before each Provider.Complete call,
	// once per iteration of the ReAct loop.
	// iteration is 0-indexed. model is the identifier sent to the provider.
	// messageCount is the number of messages in the request.
	OnProviderRequest func(iteration int, model string, messageCount int)

	// OnProviderResponse is called after each Provider.Complete call,
	// on both success and provider error.
	// iteration is 0-indexed. event contains the call duration, token
	// usage, stop reason, and error (if any).
	//
	// On provider error, event.Err carries the underlying error (before
	// wrapping as *ProviderError) and Usage/StopReason are zero values.
	OnProviderResponse func(iteration int, event ProviderEvent)

	// OnIterationStart is called at the start of each ReAct loop
	// iteration, before calling the provider.
	// iteration is 0-indexed: the first iteration is 0.
	OnIterationStart func(iteration int)

	// OnThinking is called when the model produces a thinking block.
	// text is the reasoning content — it may be a summary on Claude 4+
	// or the full reasoning on local models and Claude Sonnet 3.7.
	//
	// Called once per thinking block in the model's response. If the
	// response has multiple thinking blocks (interleaved thinking),
	// it is called once for each, in order.
	//
	// Only called when thinking is enabled (WithThinking,
	// WithAdaptiveThinking) or when a local model produces thinking.
	OnThinking func(text string)

	// OnToolCall is called when the model requests a tool invocation,
	// before the dispatcher executes it.
	// Called once per tool call in the model's response. If the model
	// requests N tools in parallel, it is called N times before dispatch.
	OnToolCall func(name string, args map[string]any)

	// OnToolResult is called after a tool finishes executing.
	// content is the result that will be sent back to the model.
	// duration is how long the execution took.
	// err is nil on success, or the error if the tool failed.
	//
	// Called even when the tool fails — err contains the error.
	// Called once per tool call, after all parallel calls complete.
	OnToolResult func(name string, content []ContentBlock, duration time.Duration, err error)

	// OnResponse is called when the model produces the final response,
	// just before Run/RunBlocks returns to the caller.
	// text is the extracted text response (without thinking blocks).
	// iterations is the total number of iterations the loop used (1-indexed).
	//
	// Also called when the loop is exhausted (MaxIterationsError) —
	// text may be "" if the last iteration ended with a tool use.
	OnResponse func(text string, iterations int)

	// OnShortTermLoad is called after the agent loads conversation history
	// from short-term memory at the start of each Run, on both success
	// and error.
	// results is the number of messages loaded (0 if err != nil).
	// duration is how long the operation took.
	// err is nil on success.
	//
	// Only called when a ShortTermMemory is configured.
	OnShortTermLoad func(results int, duration time.Duration, err error)

	// OnShortTermAppend is called after the agent persists the turn to
	// short-term memory at the end of each Run, on both success and error.
	// msgs is the number of messages that were stored.
	// duration is how long the operation took.
	// err is nil on success.
	//
	// Only called when a ShortTermMemory is configured.
	OnShortTermAppend func(msgs int, duration time.Duration, err error)

	// OnLongTermRetrieve is called after the agent queries long-term
	// memory at the start of each Run, on both success and error.
	// results is the number of messages retrieved (0 if err != nil).
	// duration is how long the retrieval operation took.
	// err is nil on success.
	//
	// Only called when a LongTermMemory is configured.
	OnLongTermRetrieve func(results int, duration time.Duration, err error)

	// OnLongTermStore is called after the agent persists a turn to
	// long-term memory at the end of each Run, on both success and error.
	// msgs is the number of messages that were stored.
	// duration is how long the storage operation took.
	// err is nil on success.
	//
	// Only called when a LongTermMemory is configured and the
	// WritePolicy decided to persist the turn. Not called when the
	// policy discards the turn.
	OnLongTermStore func(msgs int, duration time.Duration, err error)
}
