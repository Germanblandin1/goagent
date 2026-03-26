package goagent_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/internal/testutil"
)

// thinkingResp builds a response with thinking blocks followed by a final answer.
func thinkingResp(thinkingText, answerText string) goagent.CompletionResponse {
	return goagent.CompletionResponse{
		Message: goagent.Message{
			Role: goagent.RoleAssistant,
			Content: []goagent.ContentBlock{
				goagent.ThinkingBlock(thinkingText, "sig"),
				goagent.TextBlock(answerText),
			},
		},
		StopReason: goagent.StopReasonEndTurn,
	}
}

func TestHooks_OnIterationStart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		responses     []goagent.CompletionResponse
		tool          goagent.Tool
		wantCallCount int
	}{
		{
			name:          "one iteration — no tools",
			responses:     []goagent.CompletionResponse{endTurnResp("done")},
			wantCallCount: 1,
		},
		{
			name: "two iterations — one tool call",
			responses: []goagent.CompletionResponse{
				toolUseResp("t1", "calc", map[string]any{}),
				endTurnResp("done"),
			},
			tool:          testutil.NewMockTool("calc", "arithmetic", "42"),
			wantCallCount: 2,
		},
		{
			name: "three iterations — two tool calls",
			responses: []goagent.CompletionResponse{
				toolUseResp("t1", "calc", map[string]any{}),
				toolUseResp("t2", "calc", map[string]any{}),
				endTurnResp("done"),
			},
			tool:          testutil.NewMockTool("calc", "arithmetic", "42"),
			wantCallCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var callCount int
			var iterations []int

			opts := []goagent.Option{
				goagent.WithProvider(testutil.NewMockProvider(tt.responses...)),
				goagent.WithHooks(goagent.Hooks{
					OnIterationStart: func(i int) {
						callCount++
						iterations = append(iterations, i)
					},
				}),
			}
			if tt.tool != nil {
				opts = append(opts, goagent.WithTool(tt.tool))
			}

			agent, err := goagent.New(opts...)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = agent.Run(context.Background(), "test")

			if callCount != tt.wantCallCount {
				t.Errorf("OnIterationStart called %d times, want %d", callCount, tt.wantCallCount)
			}
			for i, got := range iterations {
				if got != i {
					t.Errorf("iterations[%d] = %d, want %d (0-indexed)", i, got, i)
				}
			}
		})
	}
}

func TestHooks_OnThinking(t *testing.T) {
	t.Parallel()

	var gotTexts []string

	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(
			thinkingResp("step one", "final answer"),
		)),
		goagent.WithHooks(goagent.Hooks{
			OnThinking: func(text string) {
				gotTexts = append(gotTexts, text)
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = agent.Run(context.Background(), "think")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(gotTexts) != 1 {
		t.Fatalf("OnThinking called %d times, want 1", len(gotTexts))
	}
	if gotTexts[0] != "step one" {
		t.Errorf("OnThinking text = %q, want %q", gotTexts[0], "step one")
	}
}

func TestHooks_OnThinking_MultipleBlocks(t *testing.T) {
	t.Parallel()

	var gotTexts []string

	// Response with two thinking blocks.
	resp := goagent.CompletionResponse{
		Message: goagent.Message{
			Role: goagent.RoleAssistant,
			Content: []goagent.ContentBlock{
				goagent.ThinkingBlock("thought one", "sig1"),
				goagent.ThinkingBlock("thought two", "sig2"),
				goagent.TextBlock("answer"),
			},
		},
		StopReason: goagent.StopReasonEndTurn,
	}

	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(resp)),
		goagent.WithHooks(goagent.Hooks{
			OnThinking: func(text string) {
				gotTexts = append(gotTexts, text)
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = agent.Run(context.Background(), "think")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"thought one", "thought two"}
	if len(gotTexts) != len(want) {
		t.Fatalf("OnThinking called %d times, want %d", len(gotTexts), len(want))
	}
	for i, w := range want {
		if gotTexts[i] != w {
			t.Errorf("gotTexts[%d] = %q, want %q", i, gotTexts[i], w)
		}
	}
}

func TestHooks_OnThinking_NoThinking(t *testing.T) {
	t.Parallel()

	called := false

	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(endTurnResp("answer"))),
		goagent.WithHooks(goagent.Hooks{
			OnThinking: func(text string) { called = true },
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = agent.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if called {
		t.Error("OnThinking was called, but response had no thinking blocks")
	}
}

func TestHooks_OnToolCall(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		responses []goagent.CompletionResponse
		args      map[string]any
		wantCalls []string
	}{
		{
			name: "single tool call",
			responses: []goagent.CompletionResponse{
				toolUseResp("t1", "calc", map[string]any{"x": float64(1)}),
				endTurnResp("done"),
			},
			wantCalls: []string{"calc"},
		},
		{
			name: "parallel tool calls",
			responses: []goagent.CompletionResponse{
				{
					Message: goagent.Message{
						Role: goagent.RoleAssistant,
						ToolCalls: []goagent.ToolCall{
							{ID: "t1", Name: "calc", Arguments: map[string]any{}},
							{ID: "t2", Name: "search", Arguments: map[string]any{}},
						},
					},
					StopReason: goagent.StopReasonToolUse,
				},
				endTurnResp("done"),
			},
			wantCalls: []string{"calc", "search"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotNames []string

			agent, err := goagent.New(
				goagent.WithProvider(testutil.NewMockProvider(tt.responses...)),
				goagent.WithTool(testutil.NewMockTool("calc", "arithmetic", "42")),
				goagent.WithTool(testutil.NewMockTool("search", "web search", "result")),
				goagent.WithHooks(goagent.Hooks{
					OnToolCall: func(name string, args map[string]any) {
						gotNames = append(gotNames, name)
					},
				}),
			)
			if err != nil {
				t.Fatal(err)
			}

			_, err = agent.Run(context.Background(), "test")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(gotNames) != len(tt.wantCalls) {
				t.Fatalf("OnToolCall called %d times, want %d: got %v", len(gotNames), len(tt.wantCalls), gotNames)
			}
			for i, want := range tt.wantCalls {
				if gotNames[i] != want {
					t.Errorf("gotNames[%d] = %q, want %q", i, gotNames[i], want)
				}
			}
		})
	}
}

func TestHooks_OnToolResult_Success(t *testing.T) {
	t.Parallel()

	type result struct {
		name     string
		duration time.Duration
		err      error
	}
	var got []result

	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(
			toolUseResp("t1", "calc", map[string]any{}),
			endTurnResp("done"),
		)),
		goagent.WithTool(testutil.NewMockTool("calc", "arithmetic", "42")),
		goagent.WithHooks(goagent.Hooks{
			OnToolResult: func(name string, _ []goagent.ContentBlock, dur time.Duration, err error) {
				got = append(got, result{name: name, duration: dur, err: err})
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = agent.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("OnToolResult called %d times, want 1", len(got))
	}
	if got[0].name != "calc" {
		t.Errorf("name = %q, want %q", got[0].name, "calc")
	}
	if got[0].err != nil {
		t.Errorf("err = %v, want nil", got[0].err)
	}
	// Duration may be 0 on very fast machines, but must be >= 0.
	if got[0].duration < 0 {
		t.Errorf("duration = %v, want >= 0", got[0].duration)
	}
}

func TestHooks_OnToolResult_Error(t *testing.T) {
	t.Parallel()

	toolErr := errors.New("tool failed")
	var gotErr error

	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(
			toolUseResp("t1", "bad", map[string]any{}),
			endTurnResp("done"),
		)),
		goagent.WithTool(testutil.NewMockToolWithError("bad", "always fails", toolErr)),
		goagent.WithHooks(goagent.Hooks{
			OnToolResult: func(_ string, _ []goagent.ContentBlock, _ time.Duration, err error) {
				gotErr = err
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = agent.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotErr == nil {
		t.Fatal("OnToolResult: err is nil, want non-nil")
	}
	if !errors.Is(gotErr, toolErr) {
		t.Errorf("err = %v, want to wrap %v", gotErr, toolErr)
	}
}

func TestHooks_OnResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		responses      []goagent.CompletionResponse
		tool           goagent.Tool
		wantText       string
		wantIterations int
	}{
		{
			name:           "one iteration — no tools",
			responses:      []goagent.CompletionResponse{endTurnResp("hello")},
			wantText:       "hello",
			wantIterations: 1,
		},
		{
			name: "two iterations — one tool call",
			responses: []goagent.CompletionResponse{
				toolUseResp("t1", "calc", map[string]any{}),
				endTurnResp("result is 42"),
			},
			tool:           testutil.NewMockTool("calc", "arithmetic", "42"),
			wantText:       "result is 42",
			wantIterations: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotText string
			var gotIter int
			callCount := 0

			opts := []goagent.Option{
				goagent.WithProvider(testutil.NewMockProvider(tt.responses...)),
				goagent.WithHooks(goagent.Hooks{
					OnResponse: func(text string, iters int) {
						callCount++
						gotText = text
						gotIter = iters
					},
				}),
			}
			if tt.tool != nil {
				opts = append(opts, goagent.WithTool(tt.tool))
			}

			agent, err := goagent.New(opts...)
			if err != nil {
				t.Fatal(err)
			}
			_, err = agent.Run(context.Background(), "test")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if callCount != 1 {
				t.Errorf("OnResponse called %d times, want 1", callCount)
			}
			if gotText != tt.wantText {
				t.Errorf("text = %q, want %q", gotText, tt.wantText)
			}
			if gotIter != tt.wantIterations {
				t.Errorf("iterations = %d, want %d", gotIter, tt.wantIterations)
			}
		})
	}
}

func TestHooks_OnResponse_MaxIterations(t *testing.T) {
	t.Parallel()

	calc := testutil.NewMockTool("calc", "arithmetic", "42")
	// Provide enough responses to exhaust the 2-iteration budget.
	provider := testutil.NewMockProvider(
		toolUseResp("t1", "calc", map[string]any{}),
		toolUseResp("t2", "calc", map[string]any{}),
		toolUseResp("t3", "calc", map[string]any{}), // never reached
	)

	callCount := 0
	var gotIter int

	agent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithTool(calc),
		goagent.WithMaxIterations(2),
		goagent.WithHooks(goagent.Hooks{
			OnResponse: func(_ string, iters int) {
				callCount++
				gotIter = iters
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = agent.Run(context.Background(), "test")
	if err == nil {
		t.Fatal("expected MaxIterationsError, got nil")
	}
	var maxErr *goagent.MaxIterationsError
	if !errors.As(err, &maxErr) {
		t.Fatalf("err type = %T, want *MaxIterationsError", err)
	}

	if callCount != 1 {
		t.Errorf("OnResponse called %d times, want 1", callCount)
	}
	if gotIter != 2 {
		t.Errorf("iterations = %d, want 2 (maxIterations)", gotIter)
	}
}

func TestHooks_ZeroValue(t *testing.T) {
	t.Parallel()

	// A Hooks{} zero value must not cause any panic and must not change behaviour.
	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(endTurnResp("hi"))),
		goagent.WithHooks(goagent.Hooks{}),
	)
	if err != nil {
		t.Fatal(err)
	}

	got, err := agent.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hi" {
		t.Errorf("result = %q, want %q", got, "hi")
	}
}

func TestHooks_PartialHooks(t *testing.T) {
	t.Parallel()

	// Only OnToolCall is set. The other hooks must not be invoked (no panic
	// from a nil function call) and normal execution must complete.
	toolCallCount := 0

	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(
			toolUseResp("t1", "calc", map[string]any{}),
			endTurnResp("done"),
		)),
		goagent.WithTool(testutil.NewMockTool("calc", "arithmetic", "42")),
		goagent.WithHooks(goagent.Hooks{
			OnToolCall: func(name string, _ map[string]any) {
				toolCallCount++
			},
			// All other hooks intentionally nil.
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = agent.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if toolCallCount != 1 {
		t.Errorf("OnToolCall called %d times, want 1", toolCallCount)
	}
}

func TestHooks_Race(t *testing.T) {
	t.Parallel()

	// This test verifies that hooks writing to a shared slice don't produce a
	// data race. The loop is sequential, so all hook invocations happen in the
	// same goroutine as Run — no synchronisation is required.
	var toolNames []string
	var iterStarts []int

	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(
			toolUseResp("t1", "calc", map[string]any{}),
			toolUseResp("t2", "calc", map[string]any{}),
			endTurnResp("done"),
		)),
		goagent.WithTool(testutil.NewMockTool("calc", "arithmetic", "42")),
		goagent.WithHooks(goagent.Hooks{
			OnIterationStart: func(i int) {
				iterStarts = append(iterStarts, i)
			},
			OnToolCall: func(name string, _ map[string]any) {
				toolNames = append(toolNames, name)
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = agent.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(toolNames) != 2 {
		t.Errorf("expected 2 tool calls, got %d", len(toolNames))
	}
	if len(iterStarts) != 3 {
		t.Errorf("expected 3 iterations, got %d", len(iterStarts))
	}
}
