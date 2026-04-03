package goagent

import (
	"context"
	"time"
)

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
//	        OnToolCall: func(_ context.Context, name string, args map[string]any) {
//	            fmt.Printf("tool: %s\n", name)
//	        },
//	    }),
//	)
type Hooks struct {
	// OnRunStart is called at the beginning of each Run/RunBlocks call,
	// before loading memory or building messages. It may return an enriched
	// ctx (e.g. containing an OTel span) that the loop will use for all
	// subsequent hook calls and operations. If it returns nil, the loop
	// uses the original ctx unchanged.
	//
	// For callers that only need to observe the event without enriching ctx:
	//   OnRunStart: func(ctx context.Context) context.Context { return ctx }
	OnRunStart func(ctx context.Context) context.Context

	// OnRunEnd is called at the end of each Run/RunBlocks call, just
	// before returning to the caller. It is always called — on success,
	// provider failure, MaxIterationsError, and context cancellation.
	//
	// ctx is the same context returned by OnRunStart (or the original ctx
	// if OnRunStart was not set), allowing span finalisation and cleanup.
	//
	// result contains accumulated metrics: total duration, iterations,
	// total token usage, total tool calls, and total tool execution time.
	// result.Err is nil when the Run succeeds.
	OnRunEnd func(ctx context.Context, result RunResult)

	// OnProviderRequest is called before each Provider.Complete call,
	// once per iteration of the ReAct loop.
	// iteration is 0-indexed. model is the identifier sent to the provider.
	// messageCount is the number of messages in the request.
	OnProviderRequest func(ctx context.Context, iteration int, model string, messageCount int)

	// OnProviderResponse is called after each Provider.Complete call,
	// on both success and provider error.
	// iteration is 0-indexed. event contains the call duration, token
	// usage, stop reason, and error (if any).
	//
	// On provider error, event.Err carries the underlying error (before
	// wrapping as *ProviderError) and Usage/StopReason are zero values.
	OnProviderResponse func(ctx context.Context, iteration int, event ProviderEvent)

	// OnIterationStart is called at the start of each ReAct loop
	// iteration, before calling the provider.
	// iteration is 0-indexed: the first iteration is 0.
	OnIterationStart func(ctx context.Context, iteration int)

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
	OnThinking func(ctx context.Context, text string)

	// OnToolCall is called when the model requests a tool invocation,
	// before the dispatcher executes it.
	// Called once per tool call in the model's response. If the model
	// requests N tools in parallel, it is called N times before dispatch.
	OnToolCall func(ctx context.Context, name string, args map[string]any)

	// OnToolResult is called after a tool finishes executing.
	// content is the result that will be sent back to the model.
	// duration is how long the execution took.
	// err is nil on success, or the error if the tool failed.
	//
	// Called even when the tool fails — err contains the error.
	// Called once per tool call, after all parallel calls complete.
	OnToolResult func(ctx context.Context, name string, content []ContentBlock, duration time.Duration, err error)

	// OnCircuitOpen is called when a tool's circuit breaker transitions to the
	// open state and rejects a call. toolName is the name of the disabled tool
	// and openUntil is the earliest time the circuit may close again.
	OnCircuitOpen func(ctx context.Context, toolName string, openUntil time.Time)

	// OnResponse is called when the model produces the final response,
	// just before Run/RunBlocks returns to the caller.
	// text is the extracted text response (without thinking blocks).
	// iterations is the total number of iterations the loop used (1-indexed).
	//
	// Also called when the loop is exhausted (MaxIterationsError) —
	// text may be "" if the last iteration ended with a tool use.
	OnResponse func(ctx context.Context, text string, iterations int)

	// OnShortTermLoad is called after the agent loads conversation history
	// from short-term memory at the start of each Run, on both success
	// and error.
	// results is the number of messages loaded (0 if err != nil).
	// duration is how long the operation took.
	// err is nil on success.
	//
	// Only called when a ShortTermMemory is configured.
	OnShortTermLoad func(ctx context.Context, results int, duration time.Duration, err error)

	// OnShortTermAppend is called after the agent persists the turn to
	// short-term memory at the end of each Run, on both success and error.
	// msgs is the number of messages that were stored.
	// duration is how long the operation took.
	// err is nil on success.
	//
	// Only called when a ShortTermMemory is configured.
	OnShortTermAppend func(ctx context.Context, msgs int, duration time.Duration, err error)

	// OnLongTermRetrieve is called after the agent queries long-term
	// memory at the start of each Run, on both success and error.
	// results is the number of messages retrieved (0 if err != nil).
	// duration is how long the retrieval operation took.
	// err is nil on success.
	//
	// Only called when a LongTermMemory is configured.
	OnLongTermRetrieve func(ctx context.Context, results int, duration time.Duration, err error)

	// OnLongTermStore is called after the agent persists a turn to
	// long-term memory at the end of each Run, on both success and error.
	// msgs is the number of messages that were stored.
	// duration is how long the storage operation took.
	// err is nil on success.
	//
	// Only called when a LongTermMemory is configured and the
	// WritePolicy decided to persist the turn. Not called when the
	// policy discards the turn.
	OnLongTermStore func(ctx context.Context, msgs int, duration time.Duration, err error)
}

// MergeHooks combines multiple Hooks structs into one. For each hook field,
// the merged hook calls every non-nil callback in order. Fields where no
// input hook has a callback remain nil, preserving the zero-value semantics
// of the Hooks struct.
//
// OnRunStart is special: each hook's return value is passed as the input ctx
// to the next hook in the chain, so multiple hooks can each enrich the context
// (e.g. adding an OTel span, a request ID, and a logger simultaneously).
//
// This enables composing independent hook sets (e.g. OTel tracing + custom
// logging) without manual wiring.
//
//	agent, _ := goagent.New(
//	    goagent.WithHooks(goagent.MergeHooks(otelHooks, loggingHooks, metricsHooks)),
//	)
func MergeHooks(hooks ...Hooks) Hooks {
	if len(hooks) == 0 {
		return Hooks{}
	}
	if len(hooks) == 1 {
		return hooks[0]
	}

	var merged Hooks

	if anyHas(hooks, func(h *Hooks) bool { return h.OnRunStart != nil }) {
		merged.OnRunStart = func(ctx context.Context) context.Context {
			for i := range hooks {
				if fn := hooks[i].OnRunStart; fn != nil {
					if enriched := fn(ctx); enriched != nil {
						ctx = enriched
					}
				}
			}
			return ctx
		}
	}

	if anyHas(hooks, func(h *Hooks) bool { return h.OnRunEnd != nil }) {
		merged.OnRunEnd = func(ctx context.Context, result RunResult) {
			for i := range hooks {
				if fn := hooks[i].OnRunEnd; fn != nil {
					fn(ctx, result)
				}
			}
		}
	}

	if anyHas(hooks, func(h *Hooks) bool { return h.OnProviderRequest != nil }) {
		merged.OnProviderRequest = func(ctx context.Context, iteration int, model string, messageCount int) {
			for i := range hooks {
				if fn := hooks[i].OnProviderRequest; fn != nil {
					fn(ctx, iteration, model, messageCount)
				}
			}
		}
	}

	if anyHas(hooks, func(h *Hooks) bool { return h.OnProviderResponse != nil }) {
		merged.OnProviderResponse = func(ctx context.Context, iteration int, event ProviderEvent) {
			for i := range hooks {
				if fn := hooks[i].OnProviderResponse; fn != nil {
					fn(ctx, iteration, event)
				}
			}
		}
	}

	if anyHas(hooks, func(h *Hooks) bool { return h.OnIterationStart != nil }) {
		merged.OnIterationStart = func(ctx context.Context, iteration int) {
			for i := range hooks {
				if fn := hooks[i].OnIterationStart; fn != nil {
					fn(ctx, iteration)
				}
			}
		}
	}

	if anyHas(hooks, func(h *Hooks) bool { return h.OnThinking != nil }) {
		merged.OnThinking = func(ctx context.Context, text string) {
			for i := range hooks {
				if fn := hooks[i].OnThinking; fn != nil {
					fn(ctx, text)
				}
			}
		}
	}

	if anyHas(hooks, func(h *Hooks) bool { return h.OnToolCall != nil }) {
		merged.OnToolCall = func(ctx context.Context, name string, args map[string]any) {
			for i := range hooks {
				if fn := hooks[i].OnToolCall; fn != nil {
					fn(ctx, name, args)
				}
			}
		}
	}

	if anyHas(hooks, func(h *Hooks) bool { return h.OnToolResult != nil }) {
		merged.OnToolResult = func(ctx context.Context, name string, content []ContentBlock, duration time.Duration, err error) {
			for i := range hooks {
				if fn := hooks[i].OnToolResult; fn != nil {
					fn(ctx, name, content, duration, err)
				}
			}
		}
	}

	if anyHas(hooks, func(h *Hooks) bool { return h.OnCircuitOpen != nil }) {
		merged.OnCircuitOpen = func(ctx context.Context, toolName string, openUntil time.Time) {
			for i := range hooks {
				if fn := hooks[i].OnCircuitOpen; fn != nil {
					fn(ctx, toolName, openUntil)
				}
			}
		}
	}

	if anyHas(hooks, func(h *Hooks) bool { return h.OnResponse != nil }) {
		merged.OnResponse = func(ctx context.Context, text string, iterations int) {
			for i := range hooks {
				if fn := hooks[i].OnResponse; fn != nil {
					fn(ctx, text, iterations)
				}
			}
		}
	}

	if anyHas(hooks, func(h *Hooks) bool { return h.OnShortTermLoad != nil }) {
		merged.OnShortTermLoad = func(ctx context.Context, results int, duration time.Duration, err error) {
			for i := range hooks {
				if fn := hooks[i].OnShortTermLoad; fn != nil {
					fn(ctx, results, duration, err)
				}
			}
		}
	}

	if anyHas(hooks, func(h *Hooks) bool { return h.OnShortTermAppend != nil }) {
		merged.OnShortTermAppend = func(ctx context.Context, msgs int, duration time.Duration, err error) {
			for i := range hooks {
				if fn := hooks[i].OnShortTermAppend; fn != nil {
					fn(ctx, msgs, duration, err)
				}
			}
		}
	}

	if anyHas(hooks, func(h *Hooks) bool { return h.OnLongTermRetrieve != nil }) {
		merged.OnLongTermRetrieve = func(ctx context.Context, results int, duration time.Duration, err error) {
			for i := range hooks {
				if fn := hooks[i].OnLongTermRetrieve; fn != nil {
					fn(ctx, results, duration, err)
				}
			}
		}
	}

	if anyHas(hooks, func(h *Hooks) bool { return h.OnLongTermStore != nil }) {
		merged.OnLongTermStore = func(ctx context.Context, msgs int, duration time.Duration, err error) {
			for i := range hooks {
				if fn := hooks[i].OnLongTermStore; fn != nil {
					fn(ctx, msgs, duration, err)
				}
			}
		}
	}

	return merged
}

// anyHas reports whether at least one hook in the slice satisfies the predicate.
func anyHas(hooks []Hooks, check func(*Hooks) bool) bool {
	for i := range hooks {
		if check(&hooks[i]) {
			return true
		}
	}
	return false
}
