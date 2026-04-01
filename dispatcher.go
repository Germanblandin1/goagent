package goagent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// DispatchFunc is the function signature for tool dispatch.
// It is used as the base of the middleware chain.
type DispatchFunc func(ctx context.Context, name string, args map[string]any) ([]ContentBlock, error)

// DispatchMiddleware wraps a DispatchFunc to add cross-cutting behavior
// (logging, timeouts, circuit breaking, metrics, etc.).
// Middlewares are applied outermost-first: the first middleware in the slice
// is the first to execute and the last to return.
type DispatchMiddleware func(next DispatchFunc) DispatchFunc

// chain builds a DispatchFunc by wrapping base with each middleware.
// Middlewares are applied right-to-left so that middlewares[0] is outermost.
func chain(base DispatchFunc, middlewares ...DispatchMiddleware) DispatchFunc {
	for i := len(middlewares) - 1; i >= 0; i-- {
		base = middlewares[i](base)
	}
	return base
}

// cbState is the state of a per-tool circuit breaker.
type cbState int

const (
	cbClosed   cbState = iota
	cbOpen
	cbHalfOpen
)

// circuitBreaker tracks failure state for a single tool.
type circuitBreaker struct {
	mu           sync.Mutex
	state        cbState
	failures     int
	openUntil    time.Time
	maxFailures  int
	resetTimeout time.Duration
}

// allow reports whether the circuit breaker permits a call.
// It transitions cbOpen → cbHalfOpen when the reset window has elapsed,
// allowing one probe call through.
func (cb *circuitBreaker) allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case cbClosed:
		return true
	case cbOpen:
		if time.Now().After(cb.openUntil) {
			cb.state = cbHalfOpen
			return true
		}
		return false
	default: // cbHalfOpen
		return false
	}
}

// recordSuccess resets the circuit breaker to cbClosed.
func (cb *circuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = 0
	cb.state = cbClosed
}

// recordFailure increments the failure count and opens the circuit when the
// threshold is reached, or immediately if the breaker is in half-open state.
func (cb *circuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures++
	if cb.state == cbHalfOpen || cb.failures >= cb.maxFailures {
		cb.state = cbOpen
		cb.openUntil = time.Now().Add(cb.resetTimeout)
	}
}

// circuitBreakerMiddleware returns a DispatchMiddleware that enforces per-tool
// circuit breaking. After maxFailures consecutive failures the tool is
// disabled for resetTimeout. The onOpen callback (may be nil) is invoked each
// time a call is rejected while the circuit is open.
func circuitBreakerMiddleware(maxFailures int, resetTimeout time.Duration, logger *slog.Logger, onOpen func(string, time.Time)) DispatchMiddleware {
	var mapMu sync.Mutex
	breakers := make(map[string]*circuitBreaker)

	getBreaker := func(name string) *circuitBreaker {
		mapMu.Lock()
		defer mapMu.Unlock()
		if b, ok := breakers[name]; ok {
			return b
		}
		b := &circuitBreaker{maxFailures: maxFailures, resetTimeout: resetTimeout}
		breakers[name] = b
		return b
	}

	return func(next DispatchFunc) DispatchFunc {
		return func(ctx context.Context, name string, args map[string]any) ([]ContentBlock, error) {
			cb := getBreaker(name)
			if !cb.allow() {
				cb.mu.Lock()
				openUntil := cb.openUntil
				cb.mu.Unlock()
				logger.WarnContext(ctx, "circuit breaker open", "tool", name, "open_until", openUntil)
				if onOpen != nil {
					onOpen(name, openUntil)
				}
				return nil, &CircuitOpenError{Tool: name, OpenUntil: openUntil}
			}

			result, err := next(ctx, name, args)
			if err != nil {
				cb.recordFailure()
				return nil, err
			}
			cb.recordSuccess()
			return result, nil
		}
	}
}

// timeoutMiddleware returns a DispatchMiddleware that cancels the tool context
// after d. If d is ≤ 0 the middleware is a no-op pass-through.
func timeoutMiddleware(d time.Duration) DispatchMiddleware {
	return func(next DispatchFunc) DispatchFunc {
		return func(ctx context.Context, name string, args map[string]any) ([]ContentBlock, error) {
			if d <= 0 {
				return next(ctx, name, args)
			}
			ctx, cancel := context.WithTimeout(ctx, d)
			defer cancel()
			return next(ctx, name, args)
		}
	}
}

// loggingMiddleware returns a DispatchMiddleware that logs each tool dispatch
// at Debug level with the tool name, duration, and error (if any).
func loggingMiddleware(logger *slog.Logger) DispatchMiddleware {
	return func(next DispatchFunc) DispatchFunc {
		return func(ctx context.Context, name string, args map[string]any) ([]ContentBlock, error) {
			start := time.Now()
			result, err := next(ctx, name, args)
			elapsed := time.Since(start)
			logger.DebugContext(ctx, "tool dispatch", "tool", name, "duration", elapsed, "error", err)
			return result, err
		}
	}
}

// panicRecoveryMiddleware returns a DispatchMiddleware that recovers from
// panics in tool execution and converts them to *ToolPanicError. This is
// always the innermost middleware in the chain so that upstream middlewares
// (circuit breaker, timeout, logging) observe the recovered error.
func panicRecoveryMiddleware() DispatchMiddleware {
	return func(next DispatchFunc) DispatchFunc {
		return func(ctx context.Context, name string, args map[string]any) (result []ContentBlock, err error) {
			defer func() {
				if r := recover(); r != nil {
					err = newToolPanicError(name, r)
				}
			}()
			return next(ctx, name, args)
		}
	}
}

// dispatcher executes tool calls, running them in parallel via goroutines.
// Each dispatcher is owned by a single Agent and is safe for concurrent use.
type dispatcher struct {
	tools       map[string]Tool
	logger      *slog.Logger
	middlewares []DispatchMiddleware
}

// newDispatcher builds a dispatcher indexed by tool name.
func newDispatcher(tools []Tool, logger *slog.Logger, mws []DispatchMiddleware) *dispatcher {
	m := make(map[string]Tool, len(tools))
	for _, t := range tools {
		m[t.Definition().Name] = t
	}
	return &dispatcher{tools: m, logger: logger, middlewares: mws}
}

// dispatch executes all tool calls in parallel.
// Each goroutine writes to its own index in results, so no mutex is needed.
// A missing or failing tool is recorded as an error result and does not abort
// the remaining calls.
func (d *dispatcher) dispatch(ctx context.Context, calls []ToolCall) []ToolResult {
	results := make([]ToolResult, len(calls))

	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Add(1)
		go func(idx int, tc ToolCall) {
			defer wg.Done()
			results[idx] = d.execute(ctx, tc)
		}(i, call)
	}
	wg.Wait()

	return results
}

// execute runs a single tool call through the middleware chain and returns its
// result. Tool-not-found is handled before the chain and does not consume a
// circuit-breaker slot.
func (d *dispatcher) execute(ctx context.Context, tc ToolCall) ToolResult {
	t, ok := d.tools[tc.Name]
	if !ok {
		d.logger.Debug("tool not found", "tool", tc.Name)
		return ToolResult{
			ToolCallID: tc.ID,
			Name:       tc.Name,
			Err:        fmt.Errorf("%w: %s", ErrToolNotFound, tc.Name),
		}
	}

	base := DispatchFunc(func(ctx context.Context, _ string, args map[string]any) ([]ContentBlock, error) {
		return t.Execute(ctx, args)
	})

	fn := chain(base, d.middlewares...)

	start := time.Now()
	content, err := fn(ctx, tc.Name, tc.Arguments)
	duration := time.Since(start)

	if err != nil {
		return ToolResult{
			ToolCallID: tc.ID,
			Name:       tc.Name,
			Err:        &ToolExecutionError{ToolName: tc.Name, Args: tc.Arguments, Cause: err},
			Duration:   duration,
		}
	}

	return ToolResult{
		ToolCallID: tc.ID,
		Name:       tc.Name,
		Content:    content,
		Duration:   duration,
	}
}
