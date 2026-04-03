package otel

import (
	"context"
	"time"

	goagent "github.com/Germanblandin1/goagent"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// NewHooks returns a [goagent.Hooks] struct with all callbacks wired to emit
// OpenTelemetry spans and record RED metrics.
//
// Spans are nested correctly: OnRunStart opens the root span and returns an
// enriched ctx; all inner hooks (provider, tool, memory) create child spans
// using that ctx. OnRunEnd closes the root span.
//
// The returned Hooks are safe for concurrent use — multiple Run calls on the
// same agent produce independent trace trees with no shared mutable state.
// Metric instruments (Counter, Histogram) are thread-safe by OTel SDK design.
func NewHooks(tracer trace.Tracer, meter metric.Meter) (goagent.Hooks, error) {
	inst, err := newInstruments(meter)
	if err != nil {
		return goagent.Hooks{}, err
	}

	return goagent.Hooks{
		// OnRunStart opens the root span. The enriched ctx is propagated to all
		// subsequent hook calls via the goagent loop, enabling proper nesting.
		OnRunStart: func(ctx context.Context) context.Context {
			ctx, _ = tracer.Start(ctx, "goagent.run")
			return ctx
		},

		// OnRunEnd finalises the root span with run-level attributes and metrics.
		OnRunEnd: func(ctx context.Context, result goagent.RunResult) {
			span := trace.SpanFromContext(ctx)
			span.SetAttributes(
				attribute.Int("iterations", result.Iterations),
				attribute.Int("tool_calls", result.ToolCalls),
				attribute.Int("input_tokens", result.TotalUsage.InputTokens),
				attribute.Int("output_tokens", result.TotalUsage.OutputTokens),
			)
			if result.Err != nil {
				span.RecordError(result.Err)
				span.SetStatus(codes.Error, result.Err.Error())
				inst.runErrors.Add(ctx, 1)
			} else {
				span.SetStatus(codes.Ok, "")
			}
			span.End()
			inst.runDuration.Record(ctx, result.Duration.Seconds())
		},

		// OnProviderRequest decorates the root span with the model name on the
		// first iteration — the model is constant across iterations.
		OnProviderRequest: func(ctx context.Context, iter int, model string, messageCount int) {
			if iter == 0 {
				span := trace.SpanFromContext(ctx)
				span.SetAttributes(
					attribute.String("model", model),
					attribute.Int("initial_message_count", messageCount),
				)
			}
		},

		// OnProviderResponse creates a child span for each LLM call using a
		// retroactive start timestamp so the span reflects the actual call window.
		OnProviderResponse: func(ctx context.Context, iter int, event goagent.ProviderEvent) {
			start := time.Now().Add(-event.Duration)
			_, span := tracer.Start(ctx, "goagent.provider.complete",
				trace.WithTimestamp(start),
			)
			span.SetAttributes(
				attribute.Int("iteration", iter),
				attribute.Int("input_tokens", event.Usage.InputTokens),
				attribute.Int("output_tokens", event.Usage.OutputTokens),
				attribute.String("stop_reason", event.StopReason.String()),
			)
			if event.Err != nil {
				span.RecordError(event.Err)
				span.SetStatus(codes.Error, event.Err.Error())
			} else {
				span.SetStatus(codes.Ok, "")
			}
			span.End()
			inst.providerDuration.Record(ctx, event.Duration.Seconds())
			inst.providerTokensIn.Add(ctx, int64(event.Usage.InputTokens))
			inst.providerTokensOut.Add(ctx, int64(event.Usage.OutputTokens))
		},

		// OnToolResult creates a child span for each tool execution.
		OnToolResult: func(ctx context.Context, name string, _ []goagent.ContentBlock, dur time.Duration, err error) {
			start := time.Now().Add(-dur)
			_, span := tracer.Start(ctx, "goagent.tool."+name,
				trace.WithTimestamp(start),
			)
			span.SetAttributes(attribute.String("tool.name", name))
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				inst.toolErrors.Add(ctx, 1, metric.WithAttributes(
					attribute.String("tool.name", name),
				))
			} else {
				span.SetStatus(codes.Ok, "")
			}
			span.End()
			inst.toolDuration.Record(ctx, dur.Seconds(), metric.WithAttributes(
				attribute.String("tool.name", name),
			))
		},

		// Memory hooks — each creates a child span with a retroactive timestamp.

		OnShortTermLoad: func(ctx context.Context, results int, dur time.Duration, err error) {
			start := time.Now().Add(-dur)
			_, span := tracer.Start(ctx, "goagent.memory.short_term.load",
				trace.WithTimestamp(start),
			)
			span.SetAttributes(attribute.Int("message_count", results))
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			} else {
				span.SetStatus(codes.Ok, "")
			}
			span.End()
			inst.memoryLoadDuration.Record(ctx, dur.Seconds())
		},

		OnShortTermAppend: func(ctx context.Context, msgs int, dur time.Duration, err error) {
			start := time.Now().Add(-dur)
			_, span := tracer.Start(ctx, "goagent.memory.short_term.append",
				trace.WithTimestamp(start),
			)
			span.SetAttributes(attribute.Int("message_count", msgs))
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			} else {
				span.SetStatus(codes.Ok, "")
			}
			span.End()
			inst.memoryAppendDuration.Record(ctx, dur.Seconds())
		},

		OnLongTermRetrieve: func(ctx context.Context, results int, dur time.Duration, err error) {
			start := time.Now().Add(-dur)
			_, span := tracer.Start(ctx, "goagent.memory.long_term.retrieve",
				trace.WithTimestamp(start),
			)
			span.SetAttributes(attribute.Int("message_count", results))
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			} else {
				span.SetStatus(codes.Ok, "")
			}
			span.End()
			inst.memoryLoadDuration.Record(ctx, dur.Seconds())
		},

		OnLongTermStore: func(ctx context.Context, msgs int, dur time.Duration, err error) {
			start := time.Now().Add(-dur)
			_, span := tracer.Start(ctx, "goagent.memory.long_term.store",
				trace.WithTimestamp(start),
			)
			span.SetAttributes(attribute.Int("message_count", msgs))
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			} else {
				span.SetStatus(codes.Ok, "")
			}
			span.End()
			inst.memoryAppendDuration.Record(ctx, dur.Seconds())
		},

		// The following hooks do not emit spans or metrics because their data
		// is fully covered by other hooks:
		//   OnIterationStart  — iteration number is already in OnProviderResponse
		//   OnThinking        — reasoning text is not a metric
		//   OnToolCall        — name and args are in OnToolResult
		//   OnResponse        — final text is implicit from a successful OnRunEnd
		//   OnCircuitOpen     — future: could be a counter; left as extension point
	}, nil
}
