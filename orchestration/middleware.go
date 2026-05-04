package orchestration

import (
	"context"
	"fmt"
	"time"
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

// RetryMiddleware retries the NodeFunc up to maxRetries times on error.
// If all attempts fail, a MaxRetriesError wrapping the last error is returned.
// The next node string from the last attempt is returned on success.
//
// Note: retries re-execute the entire NodeFunc including any writes to
// StageContext. NodeFuncs used with retry should be idempotent or handle
// repeated writes gracefully (e.g. SetOutput overwrites — safe).
//
// Example:
//
//	orchestration.WithNode("fetch", fetchFunc,
//	    orchestration.WithNodeMiddleware(orchestration.RetryMiddleware(3)),
//	)
func RetryMiddleware(maxRetries int) NodeMiddleware {
	return func(next NodeFunc) NodeFunc {
		return func(ctx context.Context, sc *StageContext) (string, error) {
			var lastErr error
			for range maxRetries {
				n, err := next(ctx, sc)
				if err == nil {
					return n, nil
				}
				lastErr = err
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				default:
				}
			}
			return "", &MaxRetriesError{Retries: maxRetries, Err: lastErr}
		}
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
				err = fmt.Errorf("node panic: %v", r)
			}
		}()
		return next(ctx, sc)
	}
}
