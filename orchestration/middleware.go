package orchestration

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"runtime/debug"
	"time"

	"github.com/Germanblandin1/goagent"
)

// NodeMiddleware wraps a NodeFunc to add cross-cutting behavior per node.
// It receives the next NodeFunc and returns a new NodeFunc that wraps it.
//
// Middlewares are applied in reverse registration order — the first registered
// middleware is innermost (closest to the NodeFunc). This matches standard Go
// HTTP middleware conventions.
//
// Example:
//
//	orchestration.WithNode("call_api", apiFunc,
//	    orchestration.WithNodeMiddleware(orchestration.RetryMiddleware(3)),
//	    orchestration.WithNodeMiddleware(orchestration.TimeoutMiddleware(30*time.Second)),
//	)
//
// Execution order: TimeoutMiddleware → RetryMiddleware → apiFunc
type NodeMiddleware func(next NodeFunc) NodeFunc

// WithNodeMiddleware wraps the node's NodeFunc with the given middleware.
// Multiple calls stack — see NodeMiddleware for ordering semantics.
// Implements NodeOption so it integrates with WithNode directly.
func WithNodeMiddleware(mw NodeMiddleware) NodeOption {
	return func(e *nodeEntry) {
		e.fn = mw(e.fn)
	}
}

// MaxRetriesError is returned by RetryMiddleware when all retry attempts fail.
type MaxRetriesError struct {
	Retries int
	Err     error
}

func (e *MaxRetriesError) Error() string {
	return fmt.Sprintf("after %d retries: %v", e.Retries, e.Err)
}

// Unwrap allows errors.Is and errors.As to inspect the underlying error.
func (e *MaxRetriesError) Unwrap() error { return e.Err }

// RetryMiddleware retries the NodeFunc on error using exponential backoff with
// jitter. The policy mirrors goagent.RetryPolicy — MaxAttempts, InitialDelay,
// MaxDelay, Multiplier, Retryable, and RetryAfter are all respected.
//
// Zero-value policy fields use the same defaults as RetryPolicy: 3 attempts,
// 200ms initial delay, 10s max delay, 2× multiplier.
//
// Note: retries re-execute the entire NodeFunc including any writes to
// StageContext. NodeFuncs used with retry should be idempotent or handle
// repeated writes gracefully (e.g. SetOutput overwrites — safe).
//
// Example:
//
//	orchestration.WithNode("call_llm", llmFunc,
//	    orchestration.WithNodeMiddleware(orchestration.RetryMiddleware(goagent.RetryPolicy{
//	        MaxAttempts:  5,
//	        InitialDelay: 200 * time.Millisecond,
//	        Retryable:    func(err error) bool { return isRateLimit(err) },
//	        RetryAfter:   func(err error) time.Duration { return parseRetryAfter(err) },
//	    })),
//	)
func RetryMiddleware(policy goagent.RetryPolicy) NodeMiddleware {
	p := applyRetryDefaults(policy)
	return func(next NodeFunc) NodeFunc {
		return func(ctx context.Context, sc *StageContext) (string, error) {
			var lastErr error
			for attempt := range p.MaxAttempts {
				n, err := next(ctx, sc)
				if err == nil {
					return n, nil
				}
				lastErr = err

				if p.Retryable != nil && !p.Retryable(lastErr) {
					return "", lastErr
				}
				var te goagent.TransientError
				if errors.As(lastErr, &te) && !te.IsTransient() {
					return "", lastErr
				}

				if attempt == p.MaxAttempts-1 {
					break
				}

				delay := nodeBackoffDelay(attempt, lastErr, p)
				if err := nodeSleepCtx(ctx, delay); err != nil {
					return "", err
				}
			}
			return "", &MaxRetriesError{Retries: p.MaxAttempts, Err: lastErr}
		}
	}
}

// applyRetryDefaults returns a copy of p with zero fields replaced by defaults.
func applyRetryDefaults(p goagent.RetryPolicy) goagent.RetryPolicy {
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = 3
	}
	if p.InitialDelay <= 0 {
		p.InitialDelay = 200 * time.Millisecond
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = 10 * time.Second
	}
	if p.Multiplier <= 0 {
		p.Multiplier = 2.0
	}
	return p
}

// nodeBackoffDelay computes the sleep duration for a retry attempt.
// It honours RetryAfter (server-suggested delay) and falls back to
// exponential backoff with ±25% jitter capped at MaxDelay.
func nodeBackoffDelay(attempt int, err error, p goagent.RetryPolicy) time.Duration {
	if p.RetryAfter != nil {
		if d := p.RetryAfter(err); d > 0 {
			return d
		}
	}
	delay := float64(p.InitialDelay) * math.Pow(p.Multiplier, float64(attempt))
	if delay > float64(p.MaxDelay) {
		delay = float64(p.MaxDelay)
	}
	jitter := delay * 0.25 * (2*rand.Float64() - 1)
	delay += jitter
	return time.Duration(delay)
}

// nodeSleepCtx blocks for d or until ctx is cancelled, whichever comes first.
func nodeSleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// TimeoutMiddleware adds a per-node timeout. If the NodeFunc does not complete
// within d, the context is cancelled and ctx.Err() is returned.
//
// Example:
//
//	orchestration.WithNode("slow_op", slowFunc,
//	    orchestration.WithNodeMiddleware(orchestration.TimeoutMiddleware(10*time.Second)),
//	)
func TimeoutMiddleware(d time.Duration) NodeMiddleware {
	return func(next NodeFunc) NodeFunc {
		return func(ctx context.Context, sc *StageContext) (string, error) {
			ctx, cancel := context.WithTimeout(ctx, d)
			defer cancel()
			return next(ctx, sc)
		}
	}
}

// RecoverMiddleware catches panics from the NodeFunc and converts them to
// errors, preventing a panicking node from crashing the entire graph.
//
// Example:
//
//	orchestration.WithNode("risky", riskyFunc,
//	    orchestration.WithNodeMiddleware(orchestration.RecoverMiddleware),
//	)
func RecoverMiddleware(next NodeFunc) NodeFunc {
	return func(ctx context.Context, sc *StageContext) (nextNode string, err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("node panic: %v\n%s", r, debug.Stack())
			}
		}()
		return next(ctx, sc)
	}
}
