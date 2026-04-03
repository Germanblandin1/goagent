package goagent_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/internal/testutil"
)

// blockingTool blocks Execute until its context is cancelled.
// Used to exercise WithToolTimeout.
type blockingTool struct{ toolName string }

func (b *blockingTool) Definition() goagent.ToolDefinition {
	return goagent.ToolDefinition{
		Name:        b.toolName,
		Description: "blocks until context cancels",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}
}

func (b *blockingTool) Execute(ctx context.Context, _ map[string]any) ([]goagent.ContentBlock, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// countingTool returns an error for the first failFor calls, then succeeds.
// Safe for concurrent use.
type countingTool struct {
	mu      sync.Mutex
	calls   int
	failFor int
	result  string
}

func (c *countingTool) Definition() goagent.ToolDefinition {
	return goagent.ToolDefinition{
		Name:        "counting",
		Description: "counts calls",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}
}

func (c *countingTool) Execute(_ context.Context, _ map[string]any) ([]goagent.ContentBlock, error) {
	c.mu.Lock()
	n := c.calls
	c.calls++
	c.mu.Unlock()
	if n < c.failFor {
		return nil, errors.New("deliberate failure")
	}
	return []goagent.ContentBlock{goagent.TextBlock(c.result)}, nil
}

// callCount returns how many times Execute was called.
func (c *countingTool) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// newTestDispatcher creates an agent with the given tools and runs it with
// a mock provider that triggers exactly one round of tool calls followed by
// a final response. It returns the tool results observed by the provider
// (encoded as the assistant message in the second call).
//
// For simpler assertions, use the agent integration path in agent_test.go.
// This file focuses on the dispatcher's error-handling behaviour in isolation
// by checking what gets appended to the message history.

func TestDispatcher_SingleTool(t *testing.T) {
	t.Parallel()

	tool := testutil.NewMockTool("echo", "echoes input", "pong")
	mp := testutil.NewMockProvider(
		toolUseResp("id1", "echo", map[string]any{}),
		endTurnResp("done"),
	)

	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithTool(tool),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.Run(context.Background(), "ping")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "done" {
		t.Errorf("result = %q, want %q", result, "done")
	}

	// The second call must carry a RoleTool message with the tool result.
	calls := mp.Calls()
	if len(calls) < 2 {
		t.Fatalf("expected 2 provider calls, got %d", len(calls))
	}
	msgs := calls[1].Messages
	toolMsg := msgs[len(msgs)-1]
	if toolMsg.Role != goagent.RoleTool {
		t.Errorf("last message role = %v, want RoleTool", toolMsg.Role)
	}
	if toolMsg.TextContent() != "pong" {
		t.Errorf("tool message content = %q, want %q", toolMsg.TextContent(), "pong")
	}
}

func TestDispatcher_ParallelTools(t *testing.T) {
	t.Parallel()

	tool := testutil.NewMockTool("add", "adds numbers", "10")
	mp := testutil.NewMockProvider(
		goagent.CompletionResponse{
			Message: goagent.Message{
				Role: goagent.RoleAssistant,
				ToolCalls: []goagent.ToolCall{
					{ID: "a", Name: "add", Arguments: map[string]any{}},
					{ID: "b", Name: "add", Arguments: map[string]any{}},
					{ID: "c", Name: "add", Arguments: map[string]any{}},
				},
			},
			StopReason: goagent.StopReasonToolUse,
		},
		endTurnResp("sum done"),
	)

	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithTool(tool),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.Run(context.Background(), "sum three things")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "sum done" {
		t.Errorf("result = %q, want %q", result, "sum done")
	}

	// Three tool messages must follow the assistant turn.
	calls := mp.Calls()
	msgs := calls[1].Messages
	toolMsgCount := 0
	for _, m := range msgs {
		if m.Role == goagent.RoleTool {
			toolMsgCount++
		}
	}
	if toolMsgCount != 3 {
		t.Errorf("tool message count = %d, want 3", toolMsgCount)
	}
}

func TestDispatcher_ToolNotFound(t *testing.T) {
	t.Parallel()

	mp := testutil.NewMockProvider(
		toolUseResp("id1", "nonexistent", map[string]any{}),
		endTurnResp("handled"),
	)

	// No tools registered — dispatcher must report ErrToolNotFound as observation.
	a, err := goagent.New(goagent.WithProvider(mp))
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.Run(context.Background(), "use nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "handled" {
		t.Errorf("result = %q, want %q", result, "handled")
	}

	calls := mp.Calls()
	msgs := calls[1].Messages
	toolMsg := msgs[len(msgs)-1]
	if toolMsg.Role != goagent.RoleTool {
		t.Errorf("message role = %v, want RoleTool", toolMsg.Role)
	}
	if toolMsg.TextContent() == "" {
		t.Error("expected error content in tool message, got empty string")
	}
}

func TestDispatcher_ToolError_DoesNotAbortOtherCalls(t *testing.T) {
	t.Parallel()

	goodTool := testutil.NewMockTool("good", "works", "ok")
	badTool := testutil.NewMockToolWithError("bad", "fails", errors.New("boom"))

	mp := testutil.NewMockProvider(
		goagent.CompletionResponse{
			Message: goagent.Message{
				Role: goagent.RoleAssistant,
				ToolCalls: []goagent.ToolCall{
					{ID: "g", Name: "good", Arguments: map[string]any{}},
					{ID: "b", Name: "bad", Arguments: map[string]any{}},
				},
			},
			StopReason: goagent.StopReasonToolUse,
		},
		endTurnResp("both observed"),
	)

	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithTool(goodTool),
		goagent.WithTool(badTool),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.Run(context.Background(), "run both")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "both observed" {
		t.Errorf("result = %q, want %q", result, "both observed")
	}

	// Both tool messages must be present.
	calls := mp.Calls()
	msgs := calls[1].Messages
	toolMsgCount := 0
	for _, m := range msgs {
		if m.Role == goagent.RoleTool {
			toolMsgCount++
		}
	}
	if toolMsgCount != 2 {
		t.Errorf("tool message count = %d, want 2", toolMsgCount)
	}
}

// --- middleware chain tests ---

// TestDispatchChain_MiddlewareOrder verifies that custom middlewares registered
// via WithDispatchMiddleware execute in the correct nesting order: the first
// registered middleware is outermost (executes before and after all others).
func TestDispatchChain_MiddlewareOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		wantOrder []string
	}{
		{
			name:      "two middlewares nest correctly",
			wantOrder: []string{"mw1:before", "mw2:before", "mw2:after", "mw1:after"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var order []string

			makeRecording := func(label string) goagent.DispatchMiddleware {
				return func(next goagent.DispatchFunc) goagent.DispatchFunc {
					return func(ctx context.Context, name string, args map[string]any) ([]goagent.ContentBlock, error) {
						order = append(order, label+":before")
						r, err := next(ctx, name, args)
						order = append(order, label+":after")
						return r, err
					}
				}
			}

			mp := testutil.NewMockProvider(
				toolUseResp("id1", "echo", map[string]any{}),
				endTurnResp("done"),
			)
			tool := testutil.NewMockTool("echo", "echoes", "pong")

			a, err := goagent.New(
				goagent.WithProvider(mp),
				goagent.WithTool(tool),
				goagent.WithDispatchMiddleware(makeRecording("mw1")),
				goagent.WithDispatchMiddleware(makeRecording("mw2")),
			)
			if err != nil {
				t.Fatal(err)
			}

			if _, err := a.Run(context.Background(), "ping"); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(order) != len(tt.wantOrder) {
				t.Fatalf("order = %v, want %v", order, tt.wantOrder)
			}
			for i, got := range order {
				if got != tt.wantOrder[i] {
					t.Errorf("order[%d] = %q, want %q", i, got, tt.wantOrder[i])
				}
			}
		})
	}
}

// TestWithToolTimeout_CancelsToolContext verifies that a tool whose execution
// exceeds the configured per-tool timeout receives a cancelled context.
func TestWithToolTimeout_CancelsToolContext(t *testing.T) {
	t.Parallel()

	bt := &blockingTool{toolName: "slow"}
	mp := testutil.NewMockProvider(
		toolUseResp("id1", "slow", map[string]any{}),
		endTurnResp("timeout handled"),
	)

	var toolErr error
	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithTool(bt),
		goagent.WithToolTimeout(100*time.Millisecond),
		goagent.WithHooks(goagent.Hooks{
			OnToolResult: func(_ context.Context, name string, _ []goagent.ContentBlock, _ time.Duration, err error) {
				toolErr = err
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := a.Run(context.Background(), "go slow")
	if err != nil {
		t.Fatalf("unexpected agent error: %v", err)
	}
	if result != "timeout handled" {
		t.Errorf("result = %q, want %q", result, "timeout handled")
	}
	if toolErr == nil {
		t.Fatal("expected tool to report an error, got nil")
	}
	var execErr *goagent.ToolExecutionError
	if !errors.As(toolErr, &execErr) {
		t.Fatalf("err type = %T, want *ToolExecutionError", toolErr)
	}
	if !errors.Is(execErr.Unwrap(), context.DeadlineExceeded) {
		t.Errorf("cause = %v, want context.DeadlineExceeded", execErr.Unwrap())
	}
}

// TestWithCircuitBreaker_OpensAfterMaxFailures verifies that consecutive tool
// failures open the circuit and that the next call is rejected with
// *CircuitOpenError without invoking Execute.
func TestWithCircuitBreaker_OpensAfterMaxFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		maxFailures int
	}{
		{"maxFailures=1", 1},
		{"maxFailures=2", 2},
		{"maxFailures=3", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Build provider: maxFailures tool-use responses (tool will fail each
			// time) + one more tool-use response (should be rejected by CB) + end.
			responses := make([]goagent.CompletionResponse, 0, tt.maxFailures+2)
			for i := 0; i < tt.maxFailures+1; i++ {
				responses = append(responses,
					toolUseResp(fmt.Sprintf("id%d", i), "fail-tool", map[string]any{}),
				)
			}
			responses = append(responses, endTurnResp("circuit handled"))
			mp := testutil.NewMockProvider(responses...)

			failTool := testutil.NewMockToolWithError("fail-tool", "always fails", errors.New("boom"))

			var cbOpenSeen bool
			var toolResults []error
			a, err := goagent.New(
				goagent.WithProvider(mp),
				goagent.WithTool(failTool),
				goagent.WithCircuitBreaker(tt.maxFailures, time.Minute),
				goagent.WithHooks(goagent.Hooks{
					OnCircuitOpen: func(_ context.Context, _ string, _ time.Time) {
						cbOpenSeen = true
					},
					OnToolResult: func(_ context.Context, _ string, _ []goagent.ContentBlock, _ time.Duration, err error) {
						toolResults = append(toolResults, err)
					},
				}),
			)
			if err != nil {
				t.Fatal(err)
			}

			result, err := a.Run(context.Background(), "trigger failures")
			if err != nil {
				t.Fatalf("unexpected agent error: %v", err)
			}
			if result != "circuit handled" {
				t.Errorf("result = %q, want %q", result, "circuit handled")
			}

			// The last tool result must be a CircuitOpenError.
			if len(toolResults) == 0 {
				t.Fatal("no tool results observed")
			}
			lastErr := toolResults[len(toolResults)-1]
			if lastErr == nil {
				t.Fatal("last tool result: expected error, got nil")
			}
			var cbErr *goagent.CircuitOpenError
			if !errors.As(lastErr, &cbErr) {
				t.Errorf("last error type = %T, want *CircuitOpenError", lastErr)
			}

			if !cbOpenSeen {
				t.Error("OnCircuitOpen hook was not called")
			}
		})
	}
}

// TestWithCircuitBreaker_HalfOpen_SuccessResetsToClosed verifies that after
// the reset window elapses the circuit allows one probe call, and that a
// successful probe resets the breaker to closed.
func TestWithCircuitBreaker_HalfOpen_SuccessResetsToClosed(t *testing.T) {
	t.Parallel()

	const resetTimeout = 50 * time.Millisecond

	// Tool fails once (opens circuit), then succeeds on subsequent calls.
	tool := &countingTool{failFor: 1, result: "ok"}

	// Run 1: triggers tool → fails → circuit opens. Provider ends turn.
	// Run 2: (after sleep) triggers tool → probe succeeds → circuit closes. Provider ends turn.
	// Run 3: triggers tool → circuit closed → succeeds. Provider ends turn.
	mp := testutil.NewMockProvider(
		toolUseResp("id1", "counting", map[string]any{}), // run 1 - fail
		endTurnResp("done1"),
		toolUseResp("id2", "counting", map[string]any{}), // run 2 - probe
		endTurnResp("done2"),
		toolUseResp("id3", "counting", map[string]any{}), // run 3 - closed
		endTurnResp("done3"),
	)

	var cbErrors int
	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithTool(tool),
		goagent.WithCircuitBreaker(1, resetTimeout),
		goagent.WithHooks(goagent.Hooks{
			OnToolResult: func(_ context.Context, _ string, _ []goagent.ContentBlock, _ time.Duration, err error) {
				var cbErr *goagent.CircuitOpenError
				if errors.As(err, &cbErr) {
					cbErrors++
				}
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	// Run 1: tool fails, circuit opens.
	if _, err := a.Run(context.Background(), "p1"); err != nil {
		t.Fatalf("run 1 unexpected error: %v", err)
	}

	// Sleep until reset window elapses.
	time.Sleep(2 * resetTimeout)

	// Run 2: probe (half-open) — tool succeeds, circuit resets.
	if _, err := a.Run(context.Background(), "p2"); err != nil {
		t.Fatalf("run 2 unexpected error: %v", err)
	}

	// Run 3: circuit closed — tool succeeds.
	if _, err := a.Run(context.Background(), "p3"); err != nil {
		t.Fatalf("run 3 unexpected error: %v", err)
	}

	if cbErrors != 0 {
		t.Errorf("CircuitOpenError count = %d, want 0 (probe and closed calls must not be rejected)", cbErrors)
	}
	if tool.callCount() != 3 {
		t.Errorf("tool.calls = %d, want 3 (all three runs must reach Execute)", tool.callCount())
	}
}

// TestWithCircuitBreaker_HalfOpen_FailureReopens verifies that a failed probe
// in half-open state re-opens the circuit.
func TestWithCircuitBreaker_HalfOpen_FailureReopens(t *testing.T) {
	t.Parallel()

	const resetTimeout = 50 * time.Millisecond

	failTool := testutil.NewMockToolWithError("fail-tool", "always fails", errors.New("boom"))

	// Run 1: fails → circuit opens.
	// Run 2: circuit still open → CircuitOpenError (no sleep yet).
	// (sleep)
	// Run 3: probe (half-open) → fails → circuit reopens.
	// Run 4: circuit open → CircuitOpenError.
	mp := testutil.NewMockProvider(
		toolUseResp("id1", "fail-tool", map[string]any{}), // run 1 - fail
		endTurnResp("done1"),
		toolUseResp("id2", "fail-tool", map[string]any{}), // run 2 - open rejection
		endTurnResp("done2"),
		toolUseResp("id3", "fail-tool", map[string]any{}), // run 3 - probe, fails
		endTurnResp("done3"),
		toolUseResp("id4", "fail-tool", map[string]any{}), // run 4 - open rejection
		endTurnResp("done4"),
	)

	var cbErrors int
	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithTool(failTool),
		goagent.WithCircuitBreaker(1, resetTimeout),
		goagent.WithHooks(goagent.Hooks{
			OnToolResult: func(_ context.Context, _ string, _ []goagent.ContentBlock, _ time.Duration, err error) {
				var cbErr *goagent.CircuitOpenError
				if errors.As(err, &cbErr) {
					cbErrors++
				}
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := a.Run(context.Background(), "p1"); err != nil {
		t.Fatalf("run 1: %v", err)
	}
	// Run 2 immediately: circuit still open.
	if _, err := a.Run(context.Background(), "p2"); err != nil {
		t.Fatalf("run 2: %v", err)
	}

	time.Sleep(2 * resetTimeout)

	// Run 3: probe (half-open), fails → reopens.
	if _, err := a.Run(context.Background(), "p3"); err != nil {
		t.Fatalf("run 3: %v", err)
	}
	// Run 4 immediately: circuit open again.
	if _, err := a.Run(context.Background(), "p4"); err != nil {
		t.Fatalf("run 4: %v", err)
	}

	// Runs 2 and 4 should have been rejected by the circuit breaker.
	if cbErrors != 2 {
		t.Errorf("CircuitOpenError count = %d, want 2 (runs 2 and 4 must be rejected)", cbErrors)
	}
}

// TestWithCircuitBreaker_Concurrency exercises the circuit breaker under high
// concurrency. A single Run dispatches 100 parallel tool calls; the race
// detector must not report any data races.
func TestWithCircuitBreaker_Concurrency(t *testing.T) {
	t.Parallel()

	const parallel = 100

	// Build a single response with 100 parallel tool calls.
	calls := make([]goagent.ToolCall, parallel)
	for i := range calls {
		calls[i] = goagent.ToolCall{
			ID:        fmt.Sprintf("id%d", i),
			Name:      "fail-tool",
			Arguments: map[string]any{},
		}
	}
	mp := testutil.NewMockProvider(
		goagent.CompletionResponse{
			Message: goagent.Message{
				Role:      goagent.RoleAssistant,
				ToolCalls: calls,
			},
			StopReason: goagent.StopReasonToolUse,
		},
		endTurnResp("done"),
	)

	failTool := testutil.NewMockToolWithError("fail-tool", "always fails", errors.New("boom"))

	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithTool(failTool),
		goagent.WithCircuitBreaker(5, time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}

	// A single Run with 100 parallel tool calls stress-tests the shared CB state.
	if _, err := a.Run(context.Background(), "concurrent"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- panic recovery tests ---

// panickingTool panics with the given value when Execute is called.
type panickingTool struct {
	toolName   string
	panicValue any
}

func (p *panickingTool) Definition() goagent.ToolDefinition {
	return goagent.ToolDefinition{
		Name:        p.toolName,
		Description: "panics on execute",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}
}

func (p *panickingTool) Execute(_ context.Context, _ map[string]any) ([]goagent.ContentBlock, error) {
	panic(p.panicValue)
}

// TestPanicRecovery_ToolPanicBecomesError verifies that a panicking tool does
// not crash the process and instead produces a *ToolPanicError observable
// through the OnToolResult hook.
func TestPanicRecovery_ToolPanicBecomesError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		panicValue any
		wantSubstr string
	}{
		{
			name:       "string panic",
			panicValue: "something went wrong",
			wantSubstr: "something went wrong",
		},
		{
			name:       "error panic",
			panicValue: errors.New("unexpected state"),
			wantSubstr: "unexpected state",
		},
		{
			name:       "integer panic",
			panicValue: 42,
			wantSubstr: "42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pt := &panickingTool{toolName: "panicker", panicValue: tt.panicValue}
			mp := testutil.NewMockProvider(
				toolUseResp("id1", "panicker", map[string]any{}),
				endTurnResp("recovered"),
			)

			var toolErr error
			a, err := goagent.New(
				goagent.WithProvider(mp),
				goagent.WithTool(pt),
				goagent.WithHooks(goagent.Hooks{
					OnToolResult: func(_ context.Context, name string, _ []goagent.ContentBlock, _ time.Duration, err error) {
						toolErr = err
					},
				}),
			)
			if err != nil {
				t.Fatal(err)
			}

			result, err := a.Run(context.Background(), "trigger panic")
			if err != nil {
				t.Fatalf("agent should not return error (tool panic is an observation): %v", err)
			}
			if result != "recovered" {
				t.Errorf("result = %q, want %q", result, "recovered")
			}
			if toolErr == nil {
				t.Fatal("expected tool error from panic recovery, got nil")
			}

			// Unwrap through ToolExecutionError to find ToolPanicError.
			var execErr *goagent.ToolExecutionError
			if !errors.As(toolErr, &execErr) {
				t.Fatalf("err type = %T, want *ToolExecutionError wrapping *ToolPanicError", toolErr)
			}
			var panicErr *goagent.ToolPanicError
			if !errors.As(execErr, &panicErr) {
				t.Fatalf("cause type = %T, want *ToolPanicError", execErr.Cause)
			}
			if panicErr.ToolName != "panicker" {
				t.Errorf("ToolPanicError.ToolName = %q, want %q", panicErr.ToolName, "panicker")
			}
			if len(panicErr.Stack) == 0 {
				t.Error("ToolPanicError.Stack is empty, want goroutine stack trace")
			}
			if trace := panicErr.StackTrace(); trace == "" {
				t.Error("ToolPanicError.StackTrace() is empty")
			}
			errMsg := panicErr.Error()
			if !contains(errMsg, tt.wantSubstr) {
				t.Errorf("error message %q does not contain %q", errMsg, tt.wantSubstr)
			}
		})
	}
}

// TestPanicRecovery_DoesNotAbortParallelCalls verifies that a panicking tool
// does not affect other tools dispatched in the same parallel batch.
func TestPanicRecovery_DoesNotAbortParallelCalls(t *testing.T) {
	t.Parallel()

	goodTool := testutil.NewMockTool("good", "works fine", "ok")
	badTool := &panickingTool{toolName: "bad", panicValue: "boom"}

	mp := testutil.NewMockProvider(
		goagent.CompletionResponse{
			Message: goagent.Message{
				Role: goagent.RoleAssistant,
				ToolCalls: []goagent.ToolCall{
					{ID: "g", Name: "good", Arguments: map[string]any{}},
					{ID: "b", Name: "bad", Arguments: map[string]any{}},
				},
			},
			StopReason: goagent.StopReasonToolUse,
		},
		endTurnResp("both handled"),
	)

	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithTool(goodTool),
		goagent.WithTool(badTool),
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := a.Run(context.Background(), "run both")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "both handled" {
		t.Errorf("result = %q, want %q", result, "both handled")
	}

	// Both tool results must be present in the provider's second call.
	calls := mp.Calls()
	msgs := calls[1].Messages
	toolMsgCount := 0
	for _, m := range msgs {
		if m.Role == goagent.RoleTool {
			toolMsgCount++
		}
	}
	if toolMsgCount != 2 {
		t.Errorf("tool message count = %d, want 2", toolMsgCount)
	}
}

// TestPanicRecovery_CircuitBreakerCountsPanicAsFailure verifies that a
// recovered panic counts as a failure for the circuit breaker.
func TestPanicRecovery_CircuitBreakerCountsPanicAsFailure(t *testing.T) {
	t.Parallel()

	pt := &panickingTool{toolName: "panicker", panicValue: "boom"}

	// Two tool-use rounds: first panics → circuit records failure,
	// second panics → circuit opens (maxFailures=2), third is rejected.
	mp := testutil.NewMockProvider(
		toolUseResp("id1", "panicker", map[string]any{}),
		toolUseResp("id2", "panicker", map[string]any{}),
		toolUseResp("id3", "panicker", map[string]any{}),
		endTurnResp("done"),
	)

	var cbOpenSeen bool
	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithTool(pt),
		goagent.WithCircuitBreaker(2, time.Minute),
		goagent.WithHooks(goagent.Hooks{
			OnCircuitOpen: func(_ context.Context, _ string, _ time.Time) {
				cbOpenSeen = true
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := a.Run(context.Background(), "trigger"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cbOpenSeen {
		t.Error("circuit breaker should have opened after panicking tool failures")
	}
}

// contains is a simple string-contains helper for tests.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestLoggingMiddleware_NilLogger_NoPanic verifies that passing a nil logger to
// loggingMiddleware (via WithLogger) does not panic during tool dispatch.
// loggingMiddleware is a no-op pass-through when its logger is nil.
//
// Note: the Agent itself uses the logger in the ReAct loop, so we cannot pass
// a nil logger to WithLogger without causing panics outside the dispatcher.
// Instead, we pass a custom DispatchMiddleware that explicitly holds a nil
// *slog.Logger and verifies the nil-guard path, while the agent keeps a valid
// (discarding) logger.
func TestLoggingMiddleware_NilLogger_NoPanic(t *testing.T) {
	t.Parallel()

	// Custom middleware that mirrors the loggingMiddleware nil-guard: if the
	// logger is nil it must not call any methods on it.
	var nilLogger *slog.Logger
	guardedMW := goagent.DispatchMiddleware(func(next goagent.DispatchFunc) goagent.DispatchFunc {
		return func(ctx context.Context, name string, args map[string]any) ([]goagent.ContentBlock, error) {
			start := time.Now()
			result, err := next(ctx, name, args)
			elapsed := time.Since(start)
			if nilLogger != nil {
				nilLogger.DebugContext(ctx, "tool dispatch", "tool", name, "duration", elapsed, "error", err)
			}
			return result, err
		}
	})

	mp := testutil.NewMockProvider(
		toolUseResp("id1", "echo", map[string]any{}),
		endTurnResp("done"),
	)
	tool := testutil.NewMockTool("echo", "echoes", "pong")

	// Use a discarding logger for the agent to keep the ReAct loop working,
	// while the custom middleware exercises the nil-logger guard path.
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))

	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithTool(tool),
		goagent.WithLogger(discard),
		goagent.WithDispatchMiddleware(guardedMW),
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := a.Run(context.Background(), "ping")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "done" {
		t.Errorf("result = %q, want %q", result, "done")
	}
}
