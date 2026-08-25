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

// --- RetryProvider streaming capability tests ---

// flakyStreamProvider fails CompleteStream the first failFor times, then returns
// a stream of the configured events. Implements goagent.StreamingProvider.
type flakyStreamProvider struct {
	mu      sync.Mutex
	events  []goagent.StreamEvent
	failFor int
	calls   int
	err     error
}

func (p *flakyStreamProvider) Complete(context.Context, goagent.CompletionRequest) (goagent.CompletionResponse, error) {
	return goagent.CompletionResponse{}, errors.New("flakyStreamProvider: Complete not supported")
}

func (p *flakyStreamProvider) CompleteStream(_ context.Context, _ goagent.CompletionRequest) (goagent.Stream, error) {
	p.mu.Lock()
	n := p.calls
	p.calls++
	p.mu.Unlock()
	if n < p.failFor {
		return nil, p.err
	}
	return &testutil.MockStream{Events: p.events}, nil
}

func (p *flakyStreamProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// catalogModels is the fixed model list returned by the catalog mocks below.
var catalogModels = []goagent.ModelInfo{{Name: "model-a"}, {Name: "model-b"}}

// streamCatalogProvider implements Provider, StreamingProvider and ModelCatalog
// at once, mirroring providers like Ollama. It is used to verify RetryProvider
// preserves both optional capabilities simultaneously.
type streamCatalogProvider struct {
	*testutil.MockStreamingProvider
}

func (p *streamCatalogProvider) Models(context.Context) ([]goagent.ModelInfo, error) {
	return catalogModels, nil
}

func (p *streamCatalogProvider) ModelInfo(_ context.Context, model string) (goagent.ModelInfo, error) {
	return goagent.ModelInfo{Name: model}, nil
}

// catalogOnlyProvider implements Provider and ModelCatalog but NOT
// StreamingProvider, exercising the catalog-only wrapper branch.
type catalogOnlyProvider struct {
	*testutil.MockProvider
}

func (p *catalogOnlyProvider) Models(context.Context) ([]goagent.ModelInfo, error) {
	return catalogModels, nil
}

func (p *catalogOnlyProvider) ModelInfo(_ context.Context, model string) (goagent.ModelInfo, error) {
	return goagent.ModelInfo{Name: model}, nil
}

// TestRetryProvider_PreservesStreamAndCatalog is the regression test for the bug
// where wrapping a provider that implements both StreamingProvider and
// ModelCatalog dropped the catalog capability: the returned wrapper must satisfy
// both interfaces at once, and Models must delegate to the inner provider.
func TestRetryProvider_PreservesStreamAndCatalog(t *testing.T) {
	t.Parallel()

	inner := &streamCatalogProvider{MockStreamingProvider: testutil.NewMockStreamingProvider(nil)}
	provider := goagent.RetryProvider(inner, goagent.RetryPolicy{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
	})

	if _, ok := provider.(goagent.StreamingProvider); !ok {
		t.Error("wrapper must implement StreamingProvider")
	}
	cat, ok := provider.(goagent.ModelCatalog)
	if !ok {
		t.Fatal("wrapper must implement ModelCatalog")
	}

	models, err := cat.Models(context.Background())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) != len(catalogModels) || models[0].Name != "model-a" {
		t.Errorf("Models = %v, want delegation to inner (%v)", models, catalogModels)
	}
}

// TestRetryProvider_PreservesCatalogWithoutStreaming covers the catalog-only
// branch: an inner that implements ModelCatalog but not StreamingProvider must
// keep the catalog and must NOT gain a fabricated streaming capability.
func TestRetryProvider_PreservesCatalogWithoutStreaming(t *testing.T) {
	t.Parallel()

	inner := &catalogOnlyProvider{MockProvider: testutil.NewMockProvider(endTurnResp("ok"))}
	provider := goagent.RetryProvider(inner, goagent.RetryPolicy{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
	})

	if _, ok := provider.(goagent.StreamingProvider); ok {
		t.Error("catalog-only inner must not gain StreamingProvider")
	}
	cat, ok := provider.(goagent.ModelCatalog)
	if !ok {
		t.Fatal("wrapper must implement ModelCatalog")
	}

	models, err := cat.Models(context.Background())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) != len(catalogModels) {
		t.Errorf("Models len = %d, want %d", len(models), len(catalogModels))
	}
}

// TestRetryProvider_PreservesStreamingCapability is the regression test for the
// bug where wrapping a StreamingProvider disabled streaming: the returned
// wrapper must still satisfy StreamingProvider so Agent.RunStream detects it.
func TestRetryProvider_PreservesStreamingCapability(t *testing.T) {
	t.Parallel()

	inner := testutil.NewMockStreamingProvider(nil)
	provider := goagent.RetryProvider(inner, goagent.RetryPolicy{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
	})

	if _, ok := provider.(goagent.StreamingProvider); !ok {
		t.Fatal("RetryProvider over a streaming inner must implement StreamingProvider")
	}
}

// TestRetryProvider_NonStreamingInner_NoStreamingCapability guards the fallback:
// a non-streaming inner must not gain a fabricated CompleteStream, or RunStream
// would try to stream a provider that cannot.
func TestRetryProvider_NonStreamingInner_NoStreamingCapability(t *testing.T) {
	t.Parallel()

	inner := testutil.NewMockProvider(endTurnResp("ok"))
	provider := goagent.RetryProvider(inner, goagent.RetryPolicy{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
	})

	if _, ok := provider.(goagent.StreamingProvider); ok {
		t.Fatal("RetryProvider over a non-streaming inner must not implement StreamingProvider")
	}
}

// TestRetryProvider_MaxAttemptsOne_PreservesStreaming ensures the MaxAttempts<=1
// short-circuit returns the inner untouched, keeping its StreamingProvider.
func TestRetryProvider_MaxAttemptsOne_PreservesStreaming(t *testing.T) {
	t.Parallel()

	inner := testutil.NewMockStreamingProvider(nil)
	provider := goagent.RetryProvider(inner, goagent.RetryPolicy{MaxAttempts: 1})

	if _, ok := provider.(goagent.StreamingProvider); !ok {
		t.Fatal("MaxAttempts<=1 must return the inner unchanged, preserving StreamingProvider")
	}
}

// TestRetryProvider_CompleteStream_RetriesEstablishment verifies retry applies to
// opening the stream: transient failures are retried until a stream is returned.
func TestRetryProvider_CompleteStream_RetriesEstablishment(t *testing.T) {
	t.Parallel()

	events := []goagent.StreamEvent{
		{Type: goagent.StreamEventText, Text: "hi"},
		{Type: goagent.StreamEventDone, StopReason: goagent.StopReasonEndTurn},
	}
	fp := &flakyStreamProvider{events: events, failFor: 2, err: &transientErr{"503 service unavailable"}}

	provider := goagent.RetryProvider(fp, goagent.RetryPolicy{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
	})
	sp, ok := provider.(goagent.StreamingProvider)
	if !ok {
		t.Fatal("expected StreamingProvider")
	}

	stream, err := sp.CompleteStream(context.Background(), goagent.CompletionRequest{})
	if err != nil {
		t.Fatalf("expected a stream after retries, got: %v", err)
	}
	defer stream.Close()

	if fp.callCount() != 3 {
		t.Errorf("CompleteStream calls = %d, want 3", fp.callCount())
	}
}

// TestRetryProvider_CompleteStream_PermanentErrorNoRetry verifies a permanent
// error (IsTransient()=false) opening the stream is not retried.
func TestRetryProvider_CompleteStream_PermanentErrorNoRetry(t *testing.T) {
	t.Parallel()

	fp := &flakyStreamProvider{failFor: 100, err: &permanentErr{"400 bad request"}}

	provider := goagent.RetryProvider(fp, goagent.RetryPolicy{
		MaxAttempts:  5,
		InitialDelay: time.Millisecond,
	})
	sp := provider.(goagent.StreamingProvider)

	_, err := sp.CompleteStream(context.Background(), goagent.CompletionRequest{})
	if err == nil {
		t.Fatal("expected error for permanent failure")
	}
	if fp.callCount() != 1 {
		t.Errorf("CompleteStream calls = %d, want 1 (permanent error must not be retried)", fp.callCount())
	}
}

// TestRetryProvider_RunStream_FiresStreamAndThinkingHooks is the end-to-end
// acceptance test reproducing the original bug: Agent.RunStream over a
// RetryProvider-wrapped streaming provider must still stream — firing
// OnStreamToken and OnThinkingText — instead of falling back to Complete.
func TestRetryProvider_RunStream_FiresStreamAndThinkingHooks(t *testing.T) {
	t.Parallel()

	events := []goagent.StreamEvent{
		{Type: goagent.StreamEventThinking, Text: "reasoning"},
		{Type: goagent.StreamEventText, Text: "answer"},
		{Type: goagent.StreamEventDone, StopReason: goagent.StopReasonEndTurn},
	}
	inner := testutil.NewMockStreamingProvider(events)
	provider := goagent.RetryProvider(inner, goagent.RetryPolicy{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
	})

	var thinkingTokens, streamTokens []string
	a, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithModel("test-model"),
		goagent.WithHooks(goagent.Hooks{
			OnThinkingText: func(_ context.Context, tok string) { thinkingTokens = append(thinkingTokens, tok) },
			OnStreamToken:  func(_ context.Context, tok string) { streamTokens = append(streamTokens, tok) },
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	var handlerCalled bool
	handler := func(goagent.StreamEvent) error {
		handlerCalled = true
		return nil
	}

	result, err := a.RunStream(context.Background(), "hi", handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "answer" {
		t.Errorf("result = %q, want %q", result, "answer")
	}
	if !handlerCalled {
		t.Error("stream handler was never called — RunStream fell back to Complete")
	}
	if len(thinkingTokens) != 1 || thinkingTokens[0] != "reasoning" {
		t.Errorf("OnThinkingText tokens = %v, want [reasoning]", thinkingTokens)
	}
	if len(streamTokens) != 1 || streamTokens[0] != "answer" {
		t.Errorf("OnStreamToken tokens = %v, want [answer]", streamTokens)
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
			OnToolResult: func(_ context.Context, _ string, _ []goagent.ContentBlock, _ time.Duration, err error) {
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

// --- TransientError interface ---

type permanentErr struct{ msg string }

func (e *permanentErr) Error() string     { return e.msg }
func (e *permanentErr) IsTransient() bool { return false }

type transientErr struct{ msg string }

func (e *transientErr) Error() string     { return e.msg }
func (e *transientErr) IsTransient() bool { return true }

func TestRetryProvider_PermanentTransientError_StopsAfterOneAttempt(t *testing.T) {
	t.Parallel()

	ep := &failingProvider{
		inner:   testutil.NewMockProvider(),
		failFor: 100,
		err:     &permanentErr{"validation failed"},
	}

	provider := goagent.RetryProvider(ep, goagent.RetryPolicy{
		MaxAttempts:  5,
		InitialDelay: time.Millisecond,
	})

	_, err := provider.Complete(context.Background(), goagent.CompletionRequest{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if ep.callCount() != 1 {
		t.Errorf("call count = %d, want 1 (permanent error must not be retried)", ep.callCount())
	}
}

func TestRetryProvider_TransientTransientError_Retries(t *testing.T) {
	t.Parallel()

	mp := testutil.NewMockProvider(endTurnResp("ok"))
	ep := &failingProvider{
		inner:   mp,
		failFor: 2,
		err:     &transientErr{"network hiccup"},
	}

	provider := goagent.RetryProvider(ep, goagent.RetryPolicy{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
	})

	resp, err := provider.Complete(context.Background(), goagent.CompletionRequest{})
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if resp.Message.TextContent() != "ok" {
		t.Errorf("response = %q, want %q", resp.Message.TextContent(), "ok")
	}
	if ep.callCount() != 3 {
		t.Errorf("call count = %d, want 3", ep.callCount())
	}
}

func TestRetryProvider_RetryableTakesPrecedenceOverTransientError(t *testing.T) {
	t.Parallel()

	// permanentErr claims IsTransient()=false, but Retryable overrides to true.
	ep := &failingProvider{
		inner:   testutil.NewMockProvider(endTurnResp("ok")),
		failFor: 1,
		err:     &permanentErr{"should be overridden"},
	}

	provider := goagent.RetryProvider(ep, goagent.RetryPolicy{
		MaxAttempts:  2,
		InitialDelay: time.Millisecond,
		Retryable:    func(error) bool { return true }, // explicit override
	})

	_, err := provider.Complete(context.Background(), goagent.CompletionRequest{})
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if ep.callCount() != 2 {
		t.Errorf("call count = %d, want 2 (Retryable must take precedence)", ep.callCount())
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
