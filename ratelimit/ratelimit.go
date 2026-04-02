package ratelimit

import (
	"context"
	"fmt"

	"golang.org/x/time/rate"

	"github.com/Germanblandin1/goagent"
)

// Middleware returns a [goagent.DispatchMiddleware] that enforces a shared
// token-bucket rate limit across all tool calls. It blocks until a token is
// available or ctx is cancelled/expired.
//
// Parameters:
//   - rps: sustained requests per second (the refill rate). Must be > 0.
//   - burst: maximum number of calls allowed in a single burst. Must be ≥ 1.
//
// Middleware returns an error if rps ≤ 0 or burst < 1. Use [MustMiddleware]
// when invalid parameters should panic (e.g. in top-level var blocks).
//
// Example:
//
//	mw, err := ratelimit.Middleware(5, 10)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	agent, _ := goagent.New(
//	    goagent.WithDispatchMiddleware(mw),
//	)
func Middleware(rps float64, burst int) (goagent.DispatchMiddleware, error) {
	if err := validateParams(rps, burst); err != nil {
		return nil, err
	}

	limiter := rate.NewLimiter(rate.Limit(rps), burst)

	return func(next goagent.DispatchFunc) goagent.DispatchFunc {
		return func(ctx context.Context, name string, args map[string]any) ([]goagent.ContentBlock, error) {
			if err := limiter.Wait(ctx); err != nil {
				return nil, fmt.Errorf("ratelimit: %w", err)
			}
			return next(ctx, name, args)
		}
	}, nil
}

// MustMiddleware is like [Middleware] but panics on invalid parameters.
// Intended for use in top-level var blocks or init-time setup where an
// error return is impractical.
//
// Example:
//
//	agent, _ := goagent.New(
//	    goagent.WithDispatchMiddleware(ratelimit.MustMiddleware(5, 10)),
//	)
func MustMiddleware(rps float64, burst int) goagent.DispatchMiddleware {
	mw, err := Middleware(rps, burst)
	if err != nil {
		panic(err)
	}
	return mw
}

// PerTool returns a [goagent.DispatchMiddleware] that maintains an independent
// rate limiter for each tool, keyed by name. Limiters are created lazily on
// first call and never removed. In practice, the number of entries is bounded
// by the number of tools registered on the agent.
//
// Use this when different tools hit different external APIs and you want to
// limit each independently rather than sharing a global budget.
//
// PerTool returns an error if rps ≤ 0 or burst < 1. Use [MustPerTool]
// when invalid parameters should panic.
//
// Example:
//
//	mw, err := ratelimit.PerTool(2, 5)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	agent, _ := goagent.New(
//	    goagent.WithDispatchMiddleware(mw),
//	)
func PerTool(rps float64, burst int) (goagent.DispatchMiddleware, error) {
	if err := validateParams(rps, burst); err != nil {
		return nil, err
	}

	limiters := newLimiterMap(rate.Limit(rps), burst)

	return func(next goagent.DispatchFunc) goagent.DispatchFunc {
		return func(ctx context.Context, name string, args map[string]any) ([]goagent.ContentBlock, error) {
			if err := limiters.get(name).Wait(ctx); err != nil {
				return nil, fmt.Errorf("ratelimit: tool %s: %w", name, err)
			}
			return next(ctx, name, args)
		}
	}, nil
}

// MustPerTool is like [PerTool] but panics on invalid parameters.
// Intended for use in top-level var blocks or init-time setup where an
// error return is impractical.
//
// Example:
//
//	agent, _ := goagent.New(
//	    goagent.WithDispatchMiddleware(ratelimit.MustPerTool(2, 5)),
//	)
func MustPerTool(rps float64, burst int) goagent.DispatchMiddleware {
	mw, err := PerTool(rps, burst)
	if err != nil {
		panic(err)
	}
	return mw
}

// validateParams checks that rps and burst are valid.
func validateParams(rps float64, burst int) error {
	if rps <= 0 {
		return fmt.Errorf("ratelimit: rps must be > 0, got %v", rps)
	}
	if burst < 1 {
		return fmt.Errorf("ratelimit: burst must be >= 1, got %d", burst)
	}
	return nil
}
