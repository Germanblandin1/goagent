package goagent_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/internal/testutil"
)

// --- RetryProvider tests ---

// failingProvider fails the first failFor calls, then delegates to inner.
type failingProvider struct {
	mu      sync.Mutex
	inner   *testutil.MockProvider
	failFor int
	calls   int
	err     error
}

func (p *failingProvider) Complete(ctx context.Context, req goagent.CompletionRequest) (goagent.CompletionResponse, error) {
	p.mu.Lock()
	n := p.calls
	p.calls++
	p.mu.Unlock()
	if n < p.failFor {
		return goagent.CompletionResponse{}, p.err
	}
	return p.inner.Complete(ctx, req)
}

func (p *failingProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func TestRetryProvider_SucceedsAfterTransientFailures(t *testing.T) {
	t.Parallel()

	mp := testutil.NewMockProvider(endTurnResp("hello"))
	ep := &failingProvider{inner: mp, failFor: 2, err: errors.New("503 service unavailable")}

	provider := goagent.RetryProvider(ep, goagent.RetryPolicy{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
	})

	resp, err := provider.Complete(context.Background(), goagent.CompletionRequest{})
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if resp.Message.TextContent() != "hello" {
		t.Errorf("response = %q, want %q", resp.Message.TextContent(), "hello")
	}
	if ep.callCount() != 3 {
		t.Errorf("call count = %d, want 3", ep.callCount())
	}
}

func TestRetryProvider_ExhaustsAttempts(t *testing.T) {
	t.Parallel()

	permErr := errors.New("always fails")
	ep := &failingProvider{inner: testutil.NewMockProvider(), failFor: 100, err: permErr}

	provider := goagent.RetryProvider(ep, goagent.RetryPolicy{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
	})

	_, err := provider.Complete(context.Background(), goagent.CompletionRequest{})
	if err == nil {
		t.Fatal("expected error after exhausting attempts")
	}
	if err.Error() != permErr.Error() {
		t.Errorf("error = %q, want %q", err.Error(), permErr.Error())
	}
	if ep.callCount() != 3 {
		t.Errorf("call count = %d, want 3", ep.callCount())
	}
}

func TestRetryProvider_RetryableStopsEarly(t *testing.T) {
	t.Parallel()

	nonRetryable := errors.New("400 bad request")
	ep := &failingProvider{inner: testutil.NewMockProvider(), failFor: 100, err: nonRetryable}

	provider := goagent.RetryProvider(ep, goagent.RetryPolicy{
		MaxAttempts:  5,
		InitialDelay: time.Millisecond,
		Retryable:    func(error) bool { return false },
	})

	_, err := provider.Complete(context.Background(), goagent.CompletionRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	if ep.callCount() != 1 {
		t.Errorf("call count = %d, want 1 (should stop on first non-retryable error)", ep.callCount())
	}
}

func TestRetryProvider_RespectsContextCancellation(t *testing.T) {
	t.Parallel()

	ep := &failingProvider{
		inner:   testutil.NewMockProvider(),
		failFor: 100,
		err:     errors.New("fail"),
	}

	provider := goagent.RetryProvider(ep, goagent.RetryPolicy{
		MaxAttempts:  10,
		InitialDelay: 5 * time.Second, // long delay
	})

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after first attempt's delay starts.
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := provider.Complete(ctx, goagent.CompletionRequest{})
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
	if elapsed > time.Second {
		t.Errorf("elapsed = %v, should have been cancelled quickly", elapsed)
	}
}

func TestRetryProvider_RetryAfterOverridesBackoff(t *testing.T) {
	t.Parallel()

	// Provider always fails with a "rate limited" error.
	rateLimitErr := errors.New("429 rate limited")
	ep := &failingProvider{
		inner:   testutil.NewMockProvider(endTurnResp("ok")),
		failFor: 1,
		err:     rateLimitErr,
	}

	var retryAfterCalled bool
	provider := goagent.RetryProvider(ep, goagent.RetryPolicy{
		MaxAttempts:  2,
		InitialDelay: 5 * time.Second, // very long default
		RetryAfter: func(err error) time.Duration {
			retryAfterCalled = true
			if err.Error() == "429 rate limited" {
				return time.Millisecond // server says retry quickly
			}
			return 0
		},
	})

	start := time.Now()
	resp, err := provider.Complete(context.Background(), goagent.CompletionRequest{})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if resp.Message.TextContent() != "ok" {
		t.Errorf("response = %q, want %q", resp.Message.TextContent(), "ok")
	}
	if !retryAfterCalled {
		t.Error("RetryAfter was not called")
	}
	// Should have used the 1ms delay, not the 5s default.
	if elapsed > time.Second {
		t.Errorf("elapsed = %v, RetryAfter should have overridden the 5s backoff", elapsed)
	}
}

func TestRetryProvider_MaxAttemptsOne_NoRetry(t *testing.T) {
	t.Parallel()

	ep := &failingProvider{
		inner:   testutil.NewMockProvider(),
		failFor: 100,
		err:     errors.New("fail"),
	}

	provider := goagent.RetryProvider(ep, goagent.RetryPolicy{
		MaxAttempts: 1,
	})

	_, err := provider.Complete(context.Background(), goagent.CompletionRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	if ep.callCount() != 1 {
		t.Errorf("call count = %d, want 1", ep.callCount())
	}
}

func TestRetryProvider_NoRetryOnSuccess(t *testing.T) {
	t.Parallel()

	mp := testutil.NewMockProvider(endTurnResp("ok"))
	ep := &failingProvider{inner: mp, failFor: 0, err: nil}

	provider := goagent.RetryProvider(ep, goagent.RetryPolicy{
		MaxAttempts:  5,
		InitialDelay: time.Millisecond,
	})

	resp, err := provider.Complete(context.Background(), goagent.CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.TextContent() != "ok" {
		t.Errorf("response = %q, want %q", resp.Message.TextContent(), "ok")
	}
	if ep.callCount() != 1 {
		t.Errorf("call count = %d, want 1 (no retry on success)", ep.callCount())
	}
}

// --- RetryTool tests ---

func TestRetryTool_SucceedsAfterTransientFailures(t *testing.T) {
	t.Parallel()

	tool := &countingTool{failFor: 2, result: "ok"}
	retried := goagent.RetryTool(tool, goagent.RetryPolicy{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
	})

	mp := testutil.NewMockProvider(
		toolUseResp("id1", "counting", map[string]any{}),
		endTurnResp("done"),
	)

	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithTool(retried),
	)
	if err != nil {
		t.Fatal(err)
	}

	result, rerr := a.Run(context.Background(), "go")
	if rerr != nil {
		t.Fatalf("unexpected error: %v", rerr)
	}
	if result != "done" {
		t.Errorf("result = %q, want %q", result, "done")
	}
	if tool.callCount() != 3 {
		t.Errorf("tool calls = %d, want 3", tool.callCount())
	}
}

func TestRetryTool_ExhaustsAttempts(t *testing.T) {
	t.Parallel()

	tool := &countingTool{failFor: 100, result: "never"}
	retried := goagent.RetryTool(tool, goagent.RetryPolicy{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
	})

	mp := testutil.NewMockProvider(
		toolUseResp("id1", "counting", map[string]any{}),
		endTurnResp("handled"),
	)

	var toolErr error
	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithTool(retried),
		goagent.WithHooks(goagent.Hooks{
			OnToolResult: func(_ string, _ []goagent.ContentBlock, _ time.Duration, err error) {
				toolErr = err
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	result, rerr := a.Run(context.Background(), "go")
	if rerr != nil {
		t.Fatalf("unexpected agent error: %v", rerr)
	}
	if result != "handled" {
		t.Errorf("result = %q, want %q", result, "handled")
	}
	if toolErr == nil {
		t.Error("expected tool error after exhausting retries")
	}
	if tool.callCount() != 3 {
		t.Errorf("tool calls = %d, want 3", tool.callCount())
	}
}

func TestRetryTool_RetryAfterOverridesBackoff(t *testing.T) {
	t.Parallel()

	tool := &countingTool{failFor: 1, result: "ok"}
	var retryAfterCalled bool

	retried := goagent.RetryTool(tool, goagent.RetryPolicy{
		MaxAttempts:  2,
		InitialDelay: 5 * time.Second, // very long default
		RetryAfter: func(err error) time.Duration {
			retryAfterCalled = true
			return time.Millisecond // server says retry quickly
		},
	})

	mp := testutil.NewMockProvider(
		toolUseResp("id1", "counting", map[string]any{}),
		endTurnResp("done"),
	)

	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithTool(retried),
	)
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	result, rerr := a.Run(context.Background(), "go")
	elapsed := time.Since(start)

	if rerr != nil {
		t.Fatalf("unexpected error: %v", rerr)
	}
	if result != "done" {
		t.Errorf("result = %q, want %q", result, "done")
	}
	if !retryAfterCalled {
		t.Error("RetryAfter was not called")
	}
	if elapsed > time.Second {
		t.Errorf("elapsed = %v, RetryAfter should have overridden the 5s backoff", elapsed)
	}
}

func TestRetryTool_RespectsContextCancellation(t *testing.T) {
	t.Parallel()

	tool := &countingTool{failFor: 100, result: "never"}
	retried := goagent.RetryTool(tool, goagent.RetryPolicy{
		MaxAttempts:  100,
		InitialDelay: 5 * time.Second,
	})

	mp := testutil.NewMockProvider(
		toolUseResp("id1", "counting", map[string]any{}),
		endTurnResp("done"),
	)

	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithTool(retried),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, rerr := a.Run(ctx, "go")
	if rerr == nil {
		t.Fatal("expected error from context cancellation")
	}
}

func TestRetryTool_PreservesDefinition(t *testing.T) {
	t.Parallel()

	tool := &countingTool{failFor: 0, result: "ok"}
	retried := goagent.RetryTool(tool, goagent.RetryPolicy{MaxAttempts: 3})

	def := retried.Definition()
	if def.Name != "counting" {
		t.Errorf("Definition().Name = %q, want %q", def.Name, "counting")
	}
}

func TestRetryTool_MaxAttemptsOne_ReturnsOriginal(t *testing.T) {
	t.Parallel()

	tool := &countingTool{failFor: 0, result: "ok"}
	retried := goagent.RetryTool(tool, goagent.RetryPolicy{MaxAttempts: 1})

	// With MaxAttempts=1, RetryTool should return the original tool unwrapped.
	result, err := retried.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("result count = %d, want 1", len(result))
	}
	if result[0].Text != "ok" {
		t.Errorf("result text = %q, want %q", result[0].Text, "ok")
	}
	if tool.callCount() != 1 {
		t.Errorf("call count = %d, want 1", tool.callCount())
	}
}
