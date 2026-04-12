package goagent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"
)

var dispBenchLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// nopTool is a minimal Tool implementation for benchmarks. It returns a fixed
// text block immediately with no I/O, isolating dispatch overhead from tool work.
type nopTool struct{ name string }

func (t *nopTool) Definition() ToolDefinition {
	return ToolDefinition{Name: t.name, Description: "bench"}
}

func (t *nopTool) Execute(_ context.Context, _ map[string]any) ([]ContentBlock, error) {
	return []ContentBlock{TextBlock("ok")}, nil
}

// BenchmarkChain_4Middlewares measures the cost of constructing the middleware
// chain per tool call. chain() is called inside execute() on every dispatch, so
// this allocation is paid once per tool invocation regardless of concurrency.
func BenchmarkChain_4Middlewares(b *testing.B) {
	base := DispatchFunc(func(_ context.Context, _ string, _ map[string]any) ([]ContentBlock, error) {
		return nil, nil
	})
	mws := []DispatchMiddleware{
		loggingMiddleware(dispBenchLogger),
		timeoutMiddleware(30 * time.Second),
		circuitBreakerMiddleware(5, 30*time.Second, dispBenchLogger, nil),
		panicRecoveryMiddleware(),
	}
	for b.Loop() {
		_ = chain(base, mws...)
	}
}

// BenchmarkDispatcher_Execute_NoMiddleware measures a single tool execution
// with no middleware — the minimum dispatch cost (map lookup + Execute call).
func BenchmarkDispatcher_Execute_NoMiddleware(b *testing.B) {
	d := newDispatcher([]Tool{&nopTool{"t"}}, dispBenchLogger, nil)
	tc := ToolCall{ID: "id1", Name: "t", Arguments: map[string]any{}}
	ctx := context.Background()
	for b.Loop() {
		_ = d.execute(ctx, tc)
	}
}

// BenchmarkDispatcher_Execute_FullChain measures a single tool execution with
// all four built-in middlewares active (logging, timeout, circuit breaker,
// panic recovery). Compare against NoMiddleware to isolate middleware overhead.
func BenchmarkDispatcher_Execute_FullChain(b *testing.B) {
	mws := []DispatchMiddleware{
		loggingMiddleware(dispBenchLogger),
		timeoutMiddleware(30 * time.Second),
		circuitBreakerMiddleware(5, 30*time.Second, dispBenchLogger, nil),
		panicRecoveryMiddleware(),
	}
	d := newDispatcher([]Tool{&nopTool{"t"}}, dispBenchLogger, mws)
	tc := ToolCall{ID: "id1", Name: "t", Arguments: map[string]any{}}
	ctx := context.Background()
	for b.Loop() {
		_ = d.execute(ctx, tc)
	}
}

// BenchmarkDispatcher_Dispatch_5 measures parallel dispatch of 5 tool calls.
// Provides a focused signal on fan-out/fan-in overhead vs the full agent loop
// measured in agent_bench_test.go.
func BenchmarkDispatcher_Dispatch_5(b *testing.B) {
	benchmarkDispatch(b, 5)
}

// BenchmarkDispatcher_Dispatch_20 measures parallel dispatch at higher
// concurrency to reveal WaitGroup and mutex scaling behaviour.
func BenchmarkDispatcher_Dispatch_20(b *testing.B) {
	benchmarkDispatch(b, 20)
}

func benchmarkDispatch(b *testing.B, n int) {
	b.Helper()
	tools := make([]Tool, n)
	calls := make([]ToolCall, n)
	for i := range n {
		name := fmt.Sprintf("tool%d", i)
		tools[i] = &nopTool{name}
		calls[i] = ToolCall{ID: name + "-id", Name: name, Arguments: map[string]any{}}
	}
	d := newDispatcher(tools, dispBenchLogger, nil)
	ctx := context.Background()
	rctx := runContexts{io: ctx, hook: ctx}
	for b.Loop() {
		_ = d.dispatch(rctx, calls)
	}
}

// BenchmarkCircuitBreaker_Allow measures lock contention on the happy path —
// when the circuit is closed and every call passes through. This is the common
// production case and is paid on every tool dispatch when circuit breaking is
// active.
func BenchmarkCircuitBreaker_Allow(b *testing.B) {
	cb := &circuitBreaker{maxFailures: 5, resetTimeout: 30 * time.Second}
	for b.Loop() {
		_ = cb.allow()
	}
}
