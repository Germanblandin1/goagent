package orchestration

import (
	"context"
	"time"
)

// OrchestrationHooks defines callbacks for the lifecycle of orchestration
// primitives: Pipeline, ParallelGroup, and Graph.
//
// All fields are optional — zero value is safe and produces no-op behavior.
// Use MergeOrchestrationHooks to compose multiple hook sets.
//
// The On*Start hooks return context.Context. The returned ctx is passed to
// the executor of that stage or node, allowing callers to inject a parent
// span for OTel tracing. If the hook returns nil, the original ctx is used.
//
// Example — connecting OTel without importing OTel in orchestration/:
//
//	hooks := orchestration.OrchestrationHooks{
//	    OnStageStart: func(ctx context.Context, name string) context.Context {
//	        ctx, span := tracer.Start(ctx, "orchestration.stage."+name)
//	        _ = span // stored in ctx, retrieved in OnStageEnd
//	        return ctx
//	    },
//	    OnStageEnd: func(ctx context.Context, name string, dur time.Duration, err error) {
//	        span := trace.SpanFromContext(ctx)
//	        if err != nil { span.RecordError(err) }
//	        span.End()
//	    },
//	}
type OrchestrationHooks struct {
	// OnPipelineStart is called at the beginning of Pipeline.Run or
	// Pipeline.RunWithContext, before any stage executes.
	// The returned ctx is used for all subsequent stage executions.
	// Return nil to use the original ctx unchanged.
	OnPipelineStart func(ctx context.Context, goal string) context.Context

	// OnPipelineEnd is called when the pipeline finishes, whether by
	// success, error, or context cancellation. Always called if
	// OnPipelineStart was called.
	OnPipelineEnd func(ctx context.Context, sc *StageContext, err error)

	// OnStageStart is called before each stage executor runs.
	// name is the stage name passed to Stage().
	// The returned ctx is passed to the executor — use it to inject a
	// parent span so agent spans nest correctly under the stage span.
	// Return nil to use the pipeline ctx unchanged.
	OnStageStart func(ctx context.Context, name string) context.Context

	// OnStageEnd is called after each stage executor finishes.
	// ctx is the same ctx returned by OnStageStart for this stage.
	// err is nil on success, non-nil on failure or cancellation.
	OnStageEnd func(ctx context.Context, name string, dur time.Duration, err error)

	// OnGraphStart is called at the beginning of Graph.Run or
	// Graph.RunWithContext, before any node executes.
	// The returned ctx is used for all subsequent node executions.
	// Return nil to use the original ctx unchanged.
	OnGraphStart func(ctx context.Context, goal string) context.Context

	// OnGraphEnd is called when the graph finishes, whether by success,
	// error, context cancellation, or max iterations exceeded.
	// Always called if OnGraphStart was called.
	OnGraphEnd func(ctx context.Context, sc *StageContext, err error)

	// OnNodeEnter is called before each node's NodeFunc executes.
	// name is the node name registered with WithNode.
	// The returned ctx is passed to the NodeFunc — use it to inject a
	// parent span so agent spans inside the node nest correctly.
	// Return nil to use the graph ctx unchanged.
	OnNodeEnter func(ctx context.Context, name string) context.Context

	// OnNodeExit is called after each node's NodeFunc finishes.
	// ctx is the same ctx returned by OnNodeEnter for this node.
	// next is the name of the next node ("" means the graph ended).
	// err is nil on success, non-nil on failure.
	OnNodeExit func(ctx context.Context, name string, next string, dur time.Duration, err error)
}

// MergeOrchestrationHooks composes multiple OrchestrationHooks into one.
// Hooks are called in order. For On*Start hooks, the ctx returned by the
// first hook is passed as input to the second — allowing span hierarchies
// to be built across multiple hook sets. If a hook returns nil, the current
// ctx is passed unchanged to the next hook.
func MergeOrchestrationHooks(hooks ...OrchestrationHooks) OrchestrationHooks {
	return OrchestrationHooks{
		OnPipelineStart: func(ctx context.Context, goal string) context.Context {
			for _, h := range hooks {
				if fn := h.OnPipelineStart; fn != nil {
					if enriched := fn(ctx, goal); enriched != nil {
						ctx = enriched
					}
				}
			}
			return ctx
		},
		OnPipelineEnd: func(ctx context.Context, sc *StageContext, err error) {
			for _, h := range hooks {
				if fn := h.OnPipelineEnd; fn != nil {
					fn(ctx, sc, err)
				}
			}
		},
		OnStageStart: func(ctx context.Context, name string) context.Context {
			for _, h := range hooks {
				if fn := h.OnStageStart; fn != nil {
					if enriched := fn(ctx, name); enriched != nil {
						ctx = enriched
					}
				}
			}
			return ctx
		},
		OnStageEnd: func(ctx context.Context, name string, dur time.Duration, err error) {
			for _, h := range hooks {
				if fn := h.OnStageEnd; fn != nil {
					fn(ctx, name, dur, err)
				}
			}
		},
		OnGraphStart: func(ctx context.Context, goal string) context.Context {
			for _, h := range hooks {
				if fn := h.OnGraphStart; fn != nil {
					if enriched := fn(ctx, goal); enriched != nil {
						ctx = enriched
					}
				}
			}
			return ctx
		},
		OnGraphEnd: func(ctx context.Context, sc *StageContext, err error) {
			for _, h := range hooks {
				if fn := h.OnGraphEnd; fn != nil {
					fn(ctx, sc, err)
				}
			}
		},
		OnNodeEnter: func(ctx context.Context, name string) context.Context {
			for _, h := range hooks {
				if fn := h.OnNodeEnter; fn != nil {
					if enriched := fn(ctx, name); enriched != nil {
						ctx = enriched
					}
				}
			}
			return ctx
		},
		OnNodeExit: func(ctx context.Context, name string, next string, dur time.Duration, err error) {
			for _, h := range hooks {
				if fn := h.OnNodeExit; fn != nil {
					fn(ctx, name, next, dur, err)
				}
			}
		},
	}
}

// invokeStart calls a start hook and returns the enriched ctx.
// If fn is nil or returns nil, the original ctx is returned unchanged.
func invokeStart[T any](fn func(context.Context, T) context.Context, ctx context.Context, arg T) context.Context {
	if fn == nil {
		return ctx
	}
	if enriched := fn(ctx, arg); enriched != nil {
		return enriched
	}
	return ctx
}
