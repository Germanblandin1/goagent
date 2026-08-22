package goagent

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"time"
)

// RetryPolicy configures retry behaviour for RetryProvider and RetryMiddleware.
//
// Zero-value fields use sensible defaults: 3 attempts, 200ms initial delay,
// 10s max delay, 2x multiplier, retry all errors.
type RetryPolicy struct {
	// MaxAttempts is the total number of attempts including the initial call.
	// Values ≤ 1 disable retries (the call is made once). Default: 3.
	MaxAttempts int

	// InitialDelay is the base wait time before the first retry.
	// Actual delay includes jitter (±25%). Default: 200ms.
	InitialDelay time.Duration

	// MaxDelay caps the computed delay for any single retry.
	// Default: 10s.
	MaxDelay time.Duration

	// Multiplier scales the delay on each successive retry.
	// Default: 2.0.
	Multiplier float64

	// Retryable decides whether an error should be retried.
	// When nil every non-nil error is retried.
	// Returning false stops the retry loop immediately.
	Retryable func(error) bool

	// RetryAfter extracts a server-suggested wait time from the error.
	// When non-nil and the returned duration is > 0, that duration is used
	// instead of the computed exponential delay for that attempt.
	// Typical use: parse a Retry-After header from a 429/503 response.
	// When nil or when it returns ≤ 0, the normal backoff applies.
	RetryAfter func(error) time.Duration
}

// defaults returns a copy of p with zero fields replaced by defaults.
func (p RetryPolicy) defaults() RetryPolicy {
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

// retryProvider wraps a Provider with retry logic.
type retryProvider struct {
	inner  Provider
	policy RetryPolicy
}

// RetryProvider wraps inner with exponential-backoff retry logic.
// On each failed Complete call, the policy decides whether to retry,
// how long to wait, and how many total attempts to make.
//
// When the error carries a server-suggested delay (e.g. Retry-After header)
// and RetryPolicy.RetryAfter is set, that delay takes precedence over the
// computed backoff for that attempt.
//
// Context cancellation is respected between retries: if ctx is cancelled
// while waiting, the wait returns immediately with ctx.Err().
//
// Optional interfaces are preserved: when inner also implements
// StreamingProvider, the returned wrapper implements it too, so wrapping a
// streaming provider with RetryProvider does not disable streaming. See
// retryStreamProvider for the streaming retry semantics.
func RetryProvider(inner Provider, policy RetryPolicy) Provider {
	p := policy.defaults()
	if p.MaxAttempts <= 1 {
		return inner
	}
	rp := &retryProvider{inner: inner, policy: p}
	if _, ok := inner.(StreamingProvider); ok {
		return &retryStreamProvider{retryProvider: rp}
	}
	return rp
}

func (r *retryProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	var lastResp CompletionResponse
	var lastErr error

	for attempt := range r.policy.MaxAttempts {
		lastResp, lastErr = r.inner.Complete(ctx, req)
		if lastErr == nil {
			return lastResp, nil
		}

		if !shouldRetry(lastErr, r.policy.Retryable) {
			return lastResp, lastErr
		}

		if attempt == r.policy.MaxAttempts-1 {
			break
		}

		delay := backoffDelay(attempt, lastErr, r.policy)
		if err := sleepCtx(ctx, delay); err != nil {
			return lastResp, err
		}
	}

	return lastResp, lastErr
}

// retryStreamProvider decorates retryProvider so that RetryProvider preserves
// the optional StreamingProvider capability of its inner provider. It embeds
// *retryProvider to promote Complete unchanged and adds CompleteStream.
//
// The constructor guarantees the inner provider implements StreamingProvider,
// so this type is only ever created around a streaming inner.
type retryStreamProvider struct {
	*retryProvider
}

// CompleteStream retries only the establishment of the stream — the
// CompleteStream call that opens it. Once a stream is returned successfully,
// errors surfaced later via Stream.Next are NOT retried: replaying them would
// duplicate tokens already delivered to the handler. It reuses the same
// shouldRetry, backoffDelay and sleepCtx helpers as Complete.
func (r *retryStreamProvider) CompleteStream(ctx context.Context, req CompletionRequest) (Stream, error) {
	sp := r.inner.(StreamingProvider) // guaranteed by RetryProvider constructor

	var lastErr error
	for attempt := range r.policy.MaxAttempts {
		stream, err := sp.CompleteStream(ctx, req)
		if err == nil {
			return stream, nil
		}
		lastErr = err

		if !shouldRetry(err, r.policy.Retryable) {
			return nil, err
		}

		if attempt == r.policy.MaxAttempts-1 {
			break
		}

		delay := backoffDelay(attempt, err, r.policy)
		if werr := sleepCtx(ctx, delay); werr != nil {
			return nil, werr
		}
	}

	return nil, lastErr
}

// retryTool wraps a Tool with retry logic.
type retryTool struct {
	inner  Tool
	policy RetryPolicy
}

// RetryTool wraps inner with exponential-backoff retry logic.
// On each failed Execute call, the policy decides whether to retry,
// how long to wait, and how many total attempts to make.
//
// The caller decides which tools get retry behaviour:
//
//	apiTool := goagent.RetryTool(myAPITool, goagent.RetryPolicy{
//	    MaxAttempts:  3,
//	    InitialDelay: 100 * time.Millisecond,
//	})
//
//	agent, _ := goagent.New(
//	    goagent.WithTool(apiTool),          // retried
//	    goagent.WithTool(localTool),         // not retried
//	)
func RetryTool(inner Tool, policy RetryPolicy) Tool {
	p := policy.defaults()
	if p.MaxAttempts <= 1 {
		return inner
	}
	return &retryTool{inner: inner, policy: p}
}

func (r *retryTool) Definition() ToolDefinition {
	return r.inner.Definition()
}

func (r *retryTool) Execute(ctx context.Context, args map[string]any) ([]ContentBlock, error) {
	var lastResult []ContentBlock
	var lastErr error

	for attempt := range r.policy.MaxAttempts {
		lastResult, lastErr = r.inner.Execute(ctx, args)
		if lastErr == nil {
			return lastResult, nil
		}

		if r.policy.Retryable != nil && !r.policy.Retryable(lastErr) {
			return lastResult, lastErr
		}

		if attempt == r.policy.MaxAttempts-1 {
			break
		}

		delay := backoffDelay(attempt, lastErr, r.policy)
		if err := sleepCtx(ctx, delay); err != nil {
			return lastResult, err
		}
	}

	return lastResult, lastErr
}

// backoffDelay computes the delay for the given attempt.
// If the policy has a RetryAfter function and it returns a positive duration,
// that duration is used directly. Otherwise exponential backoff with ±25%
// jitter is applied, capped at MaxDelay.
func backoffDelay(attempt int, err error, p RetryPolicy) time.Duration {
	// Prefer server-suggested delay when available.
	if p.RetryAfter != nil {
		if d := p.RetryAfter(err); d > 0 {
			return d
		}
	}

	// Exponential backoff: InitialDelay * Multiplier^attempt.
	delay := float64(p.InitialDelay) * math.Pow(p.Multiplier, float64(attempt))

	// Cap at MaxDelay.
	if delay > float64(p.MaxDelay) {
		delay = float64(p.MaxDelay)
	}

	// Add ±25% jitter to avoid thundering herd.
	jitter := delay * 0.25 * (2*rand.Float64() - 1) // range: [-25%, +25%]
	delay += jitter

	return time.Duration(delay)
}

// shouldRetry reports whether err warrants another attempt.
// Priority: explicit Retryable hook > TransientError interface > default (retry).
// Unknown errors (those that implement neither) are retried to preserve
// backward-compatible behaviour.
func shouldRetry(err error, retryable func(error) bool) bool {
	if retryable != nil {
		return retryable(err)
	}
	var te TransientError
	if errors.As(err, &te) {
		return te.IsTransient()
	}
	return true
}

// sleepCtx blocks for d or until ctx is cancelled, whichever comes first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
