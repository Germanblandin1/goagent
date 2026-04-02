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

func TestHooks_OnShortTermLoad(t *testing.T) {
	t.Parallel()

	history := []goagent.Message{
		goagent.UserMessage("previous question"),
		goagent.AssistantMessage("previous answer"),
	}
	stm := testutil.NewMockMemoryWithHistory(history...)

	type call struct {
		results  int
		duration time.Duration
		err      error
	}
	var got []call

	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(endTurnResp("hi"))),
		goagent.WithShortTermMemory(stm),
		goagent.WithHooks(goagent.Hooks{
			OnShortTermLoad: func(results int, d time.Duration, err error) {
				got = append(got, call{results, d, err})
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = agent.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("OnShortTermLoad called %d times, want 1", len(got))
	}
	if got[0].results != len(history) {
		t.Errorf("results = %d, want %d", got[0].results, len(history))
	}
	if got[0].err != nil {
		t.Errorf("err = %v, want nil", got[0].err)
	}
	if got[0].duration < 0 {
		t.Errorf("duration = %v, want >= 0", got[0].duration)
	}
}

func TestHooks_OnShortTermLoad_Error(t *testing.T) {
	t.Parallel()

	loadErr := errors.New("storage unavailable")
	stm := testutil.NewMockMemoryWithErrors(nil, loadErr)

	var gotErr error

	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(endTurnResp("hi"))),
		goagent.WithShortTermMemory(stm),
		goagent.WithHooks(goagent.Hooks{
			OnShortTermLoad: func(_ int, _ time.Duration, err error) {
				gotErr = err
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, runErr := agent.Run(context.Background(), "hello")
	if runErr == nil {
		t.Fatal("expected error from Run, got nil")
	}

	if gotErr == nil {
		t.Fatal("OnShortTermLoad: err is nil, want non-nil")
	}
	if !errors.Is(gotErr, loadErr) {
		t.Errorf("err = %v, want to wrap %v", gotErr, loadErr)
	}
}

func TestHooks_OnShortTermAppend(t *testing.T) {
	t.Parallel()

	stm := testutil.NewMockMemory()

	type call struct {
		msgs     int
		duration time.Duration
		err      error
	}
	var got []call

	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(endTurnResp("reply"))),
		goagent.WithShortTermMemory(stm),
		goagent.WithHooks(goagent.Hooks{
			OnShortTermAppend: func(msgs int, d time.Duration, err error) {
				got = append(got, call{msgs, d, err})
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = agent.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("OnShortTermAppend called %d times, want 1", len(got))
	}
	// traceTools=true by default: stores the full trace (at least user+assistant).
	if got[0].msgs < 2 {
		t.Errorf("msgs = %d, want >= 2", got[0].msgs)
	}
	if got[0].err != nil {
		t.Errorf("err = %v, want nil", got[0].err)
	}
	if got[0].duration < 0 {
		t.Errorf("duration = %v, want >= 0", got[0].duration)
	}
}

func TestHooks_OnShortTermAppend_Error(t *testing.T) {
	t.Parallel()

	appendErr := errors.New("append failed")
	stm := testutil.NewMockMemoryWithErrors(appendErr, nil)

	var gotErr error

	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(endTurnResp("reply"))),
		goagent.WithShortTermMemory(stm),
		goagent.WithHooks(goagent.Hooks{
			OnShortTermAppend: func(_ int, _ time.Duration, err error) {
				gotErr = err
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	// Append errors are non-fatal — Run should still succeed.
	_, err = agent.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotErr == nil {
		t.Fatal("OnShortTermAppend: err is nil, want non-nil")
	}
	if !errors.Is(gotErr, appendErr) {
		t.Errorf("err = %v, want to wrap %v", gotErr, appendErr)
	}
}

func TestHooks_OnLongTermRetrieve(t *testing.T) {
	t.Parallel()

	retrieved := []goagent.Message{
		goagent.UserMessage("past question"),
		goagent.AssistantMessage("past answer"),
	}
	ltm := testutil.NewMockLongTermMemoryWithRetrieve(retrieved...)

	type call struct {
		results  int
		duration time.Duration
		err      error
	}
	var got []call

	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(endTurnResp("hi"))),
		goagent.WithLongTermMemory(ltm),
		goagent.WithHooks(goagent.Hooks{
			OnLongTermRetrieve: func(results int, d time.Duration, err error) {
				got = append(got, call{results, d, err})
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = agent.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("OnLongTermRetrieve called %d times, want 1", len(got))
	}
	if got[0].results != len(retrieved) {
		t.Errorf("results = %d, want %d", got[0].results, len(retrieved))
	}
	if got[0].err != nil {
		t.Errorf("err = %v, want nil", got[0].err)
	}
	if got[0].duration < 0 {
		t.Errorf("duration = %v, want >= 0", got[0].duration)
	}
}

func TestHooks_OnLongTermRetrieve_Error(t *testing.T) {
	t.Parallel()

	retrieveErr := errors.New("vector store unavailable")
	ltm := testutil.NewMockLongTermMemoryWithErrors(nil, retrieveErr)

	var gotErr error

	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(endTurnResp("hi"))),
		goagent.WithLongTermMemory(ltm),
		goagent.WithHooks(goagent.Hooks{
			OnLongTermRetrieve: func(_ int, _ time.Duration, err error) {
				gotErr = err
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, runErr := agent.Run(context.Background(), "hello")
	if runErr == nil {
		t.Fatal("expected error from Run, got nil")
	}

	if gotErr == nil {
		t.Fatal("OnLongTermRetrieve: err is nil, want non-nil")
	}
	if !errors.Is(gotErr, retrieveErr) {
		t.Errorf("err = %v, want to wrap %v", gotErr, retrieveErr)
	}
}

func TestHooks_OnLongTermStore(t *testing.T) {
	t.Parallel()

	ltm := testutil.NewMockLongTermMemory()

	type call struct {
		msgs     int
		duration time.Duration
		err      error
	}
	var got []call

	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(endTurnResp("reply"))),
		goagent.WithLongTermMemory(ltm),
		goagent.WithHooks(goagent.Hooks{
			OnLongTermStore: func(msgs int, d time.Duration, err error) {
				got = append(got, call{msgs, d, err})
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = agent.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("OnLongTermStore called %d times, want 1", len(got))
	}
	// StoreAlways stores [userMsg, assistantMsg] — 2 messages.
	if got[0].msgs != 2 {
		t.Errorf("msgs = %d, want 2", got[0].msgs)
	}
	if got[0].err != nil {
		t.Errorf("err = %v, want nil", got[0].err)
	}
	if got[0].duration < 0 {
		t.Errorf("duration = %v, want >= 0", got[0].duration)
	}
}

func TestHooks_OnLongTermStore_Error(t *testing.T) {
	t.Parallel()

	storeErr := errors.New("write failed")
	ltm := testutil.NewMockLongTermMemoryWithErrors(storeErr, nil)

	var gotErr error

	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(endTurnResp("reply"))),
		goagent.WithLongTermMemory(ltm),
		goagent.WithHooks(goagent.Hooks{
			OnLongTermStore: func(_ int, _ time.Duration, err error) {
				gotErr = err
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	// Store errors are non-fatal — Run should still succeed.
	_, err = agent.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotErr == nil {
		t.Fatal("OnLongTermStore: err is nil, want non-nil")
	}
	if !errors.Is(gotErr, storeErr) {
		t.Errorf("err = %v, want to wrap %v", gotErr, storeErr)
	}
}

func TestHooks_OnLongTermStore_PolicySkip(t *testing.T) {
	t.Parallel()

	ltm := testutil.NewMockLongTermMemory()
	called := false

	// MinLength(1000) will reject the short "reply" response.
	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(endTurnResp("hi"))),
		goagent.WithLongTermMemory(ltm),
		goagent.WithWritePolicy(goagent.MinLength(1000)),
		goagent.WithHooks(goagent.Hooks{
			OnLongTermStore: func(_ int, _ time.Duration, _ error) {
				called = true
			},
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
		t.Error("OnLongTermStore was called, but policy should have skipped the turn")
	}
}

// endTurnRespWithUsage builds a final-answer response with token usage.
func endTurnRespWithUsage(content string, input, output int) goagent.CompletionResponse {
	resp := endTurnResp(content)
	resp.Usage = goagent.Usage{InputTokens: input, OutputTokens: output}
	return resp
}

// toolUseRespWithUsage builds a tool-use response with token usage.
func toolUseRespWithUsage(toolID, toolName string, args map[string]any, input, output int) goagent.CompletionResponse {
	resp := toolUseResp(toolID, toolName, args)
	resp.Usage = goagent.Usage{InputTokens: input, OutputTokens: output}
	return resp
}

// errorProvider is a Provider that always returns an error.
type errorProvider struct{ err error }

func (p *errorProvider) Complete(context.Context, goagent.CompletionRequest) (goagent.CompletionResponse, error) {
	return goagent.CompletionResponse{}, p.err
}

func TestHooks_OnRunStart(t *testing.T) {
	t.Parallel()

	called := false
	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(endTurnResp("hi"))),
		goagent.WithHooks(goagent.Hooks{
			OnRunStart: func() { called = true },
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = agent.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !called {
		t.Error("OnRunStart was not called")
	}
}

func TestHooks_OnRunEnd_Success(t *testing.T) {
	t.Parallel()

	var got goagent.RunResult

	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(
			endTurnRespWithUsage("done", 100, 50),
		)),
		goagent.WithHooks(goagent.Hooks{
			OnRunEnd: func(r goagent.RunResult) { got = r },
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = agent.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Iterations != 1 {
		t.Errorf("iterations = %d, want 1", got.Iterations)
	}
	if got.TotalUsage.InputTokens != 100 {
		t.Errorf("input tokens = %d, want 100", got.TotalUsage.InputTokens)
	}
	if got.TotalUsage.OutputTokens != 50 {
		t.Errorf("output tokens = %d, want 50", got.TotalUsage.OutputTokens)
	}
	if got.ToolCalls != 0 {
		t.Errorf("tool calls = %d, want 0", got.ToolCalls)
	}
	if got.Duration < 0 {
		t.Errorf("duration = %v, want >= 0", got.Duration)
	}
	if got.Err != nil {
		t.Errorf("err = %v, want nil", got.Err)
	}
}

func TestHooks_OnRunEnd_AccumulatesAcrossIterations(t *testing.T) {
	t.Parallel()

	var got goagent.RunResult

	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(
			toolUseRespWithUsage("t1", "calc", map[string]any{}, 100, 30),
			toolUseRespWithUsage("t2", "calc", map[string]any{}, 150, 40),
			endTurnRespWithUsage("done", 200, 50),
		)),
		goagent.WithTool(testutil.NewMockTool("calc", "arithmetic", "42")),
		goagent.WithHooks(goagent.Hooks{
			OnRunEnd: func(r goagent.RunResult) { got = r },
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = agent.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Iterations != 3 {
		t.Errorf("iterations = %d, want 3", got.Iterations)
	}
	wantInput := 100 + 150 + 200
	if got.TotalUsage.InputTokens != wantInput {
		t.Errorf("input tokens = %d, want %d", got.TotalUsage.InputTokens, wantInput)
	}
	wantOutput := 30 + 40 + 50
	if got.TotalUsage.OutputTokens != wantOutput {
		t.Errorf("output tokens = %d, want %d", got.TotalUsage.OutputTokens, wantOutput)
	}
	if got.ToolCalls != 2 {
		t.Errorf("tool calls = %d, want 2", got.ToolCalls)
	}
	if got.ToolTime < 0 {
		t.Errorf("tool time = %v, want >= 0", got.ToolTime)
	}
	if got.Err != nil {
		t.Errorf("err = %v, want nil", got.Err)
	}
}

func TestHooks_OnRunEnd_ProviderError(t *testing.T) {
	t.Parallel()

	provErr := errors.New("api down")
	var got goagent.RunResult

	agent, err := goagent.New(
		goagent.WithProvider(&errorProvider{err: provErr}),
		goagent.WithHooks(goagent.Hooks{
			OnRunEnd: func(r goagent.RunResult) { got = r },
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, runErr := agent.Run(context.Background(), "hello")
	if runErr == nil {
		t.Fatal("expected error from Run, got nil")
	}

	if got.Err == nil {
		t.Fatal("OnRunEnd: err is nil, want non-nil")
	}
	var pe *goagent.ProviderError
	if !errors.As(got.Err, &pe) {
		t.Errorf("err type = %T, want *ProviderError", got.Err)
	}
	if got.Iterations != 1 {
		t.Errorf("iterations = %d, want 1", got.Iterations)
	}
}

func TestHooks_OnRunEnd_MaxIterations(t *testing.T) {
	t.Parallel()

	var got goagent.RunResult

	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(
			toolUseRespWithUsage("t1", "calc", map[string]any{}, 100, 30),
			toolUseRespWithUsage("t2", "calc", map[string]any{}, 150, 40),
		)),
		goagent.WithTool(testutil.NewMockTool("calc", "arithmetic", "42")),
		goagent.WithMaxIterations(2),
		goagent.WithHooks(goagent.Hooks{
			OnRunEnd: func(r goagent.RunResult) { got = r },
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = agent.Run(context.Background(), "test")

	if got.Iterations != 2 {
		t.Errorf("iterations = %d, want 2", got.Iterations)
	}
	if got.TotalUsage.InputTokens != 250 {
		t.Errorf("input tokens = %d, want 250", got.TotalUsage.InputTokens)
	}
	var maxErr *goagent.MaxIterationsError
	if !errors.As(got.Err, &maxErr) {
		t.Errorf("err type = %T, want *MaxIterationsError", got.Err)
	}
}

func TestHooks_OnProviderRequest(t *testing.T) {
	t.Parallel()

	type call struct {
		iteration    int
		model        string
		messageCount int
	}
	var got []call

	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(
			toolUseResp("t1", "calc", map[string]any{}),
			endTurnResp("done"),
		)),
		goagent.WithModel("test-model"),
		goagent.WithTool(testutil.NewMockTool("calc", "arithmetic", "42")),
		goagent.WithHooks(goagent.Hooks{
			OnProviderRequest: func(iter int, model string, msgCount int) {
				got = append(got, call{iter, model, msgCount})
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

	if len(got) != 2 {
		t.Fatalf("OnProviderRequest called %d times, want 2", len(got))
	}
	// First call: iteration 0, model "test-model", 1 message (user prompt).
	if got[0].iteration != 0 {
		t.Errorf("call[0].iteration = %d, want 0", got[0].iteration)
	}
	if got[0].model != "test-model" {
		t.Errorf("call[0].model = %q, want %q", got[0].model, "test-model")
	}
	if got[0].messageCount != 1 {
		t.Errorf("call[0].messageCount = %d, want 1", got[0].messageCount)
	}
	// Second call: iteration 1, more messages (user + assistant + tool result).
	if got[1].iteration != 1 {
		t.Errorf("call[1].iteration = %d, want 1", got[1].iteration)
	}
	if got[1].messageCount <= 1 {
		t.Errorf("call[1].messageCount = %d, want > 1", got[1].messageCount)
	}
}

func TestHooks_OnProviderResponse_Success(t *testing.T) {
	t.Parallel()

	type call struct {
		iteration int
		event     goagent.ProviderEvent
	}
	var got []call

	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(
			endTurnRespWithUsage("done", 100, 50),
		)),
		goagent.WithHooks(goagent.Hooks{
			OnProviderResponse: func(iter int, ev goagent.ProviderEvent) {
				got = append(got, call{iter, ev})
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
		t.Fatalf("OnProviderResponse called %d times, want 1", len(got))
	}
	if got[0].iteration != 0 {
		t.Errorf("iteration = %d, want 0", got[0].iteration)
	}
	if got[0].event.Usage.InputTokens != 100 {
		t.Errorf("input tokens = %d, want 100", got[0].event.Usage.InputTokens)
	}
	if got[0].event.Usage.OutputTokens != 50 {
		t.Errorf("output tokens = %d, want 50", got[0].event.Usage.OutputTokens)
	}
	if got[0].event.StopReason != goagent.StopReasonEndTurn {
		t.Errorf("stop reason = %v, want EndTurn", got[0].event.StopReason)
	}
	if got[0].event.ToolCalls != 0 {
		t.Errorf("tool calls = %d, want 0", got[0].event.ToolCalls)
	}
	if got[0].event.Err != nil {
		t.Errorf("err = %v, want nil", got[0].event.Err)
	}
	if got[0].event.Duration < 0 {
		t.Errorf("duration = %v, want >= 0", got[0].event.Duration)
	}
}

func TestHooks_OnProviderResponse_Error(t *testing.T) {
	t.Parallel()

	provErr := errors.New("rate limited")
	var got goagent.ProviderEvent

	agent, err := goagent.New(
		goagent.WithProvider(&errorProvider{err: provErr}),
		goagent.WithHooks(goagent.Hooks{
			OnProviderResponse: func(_ int, ev goagent.ProviderEvent) {
				got = ev
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = agent.Run(context.Background(), "hello")

	if got.Err == nil {
		t.Fatal("OnProviderResponse: err is nil, want non-nil")
	}
	if !errors.Is(got.Err, provErr) {
		t.Errorf("err = %v, want to wrap %v", got.Err, provErr)
	}
	if got.Duration < 0 {
		t.Errorf("duration = %v, want >= 0", got.Duration)
	}
}

func TestHooks_OnProviderResponse_ToolCalls(t *testing.T) {
	t.Parallel()

	var got []goagent.ProviderEvent

	resp := goagent.CompletionResponse{
		Message: goagent.Message{
			Role: goagent.RoleAssistant,
			ToolCalls: []goagent.ToolCall{
				{ID: "t1", Name: "calc", Arguments: map[string]any{}},
				{ID: "t2", Name: "search", Arguments: map[string]any{}},
			},
		},
		StopReason: goagent.StopReasonToolUse,
		Usage:      goagent.Usage{InputTokens: 100, OutputTokens: 20},
	}

	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(resp, endTurnResp("done"))),
		goagent.WithTool(testutil.NewMockTool("calc", "arithmetic", "42")),
		goagent.WithTool(testutil.NewMockTool("search", "web search", "result")),
		goagent.WithHooks(goagent.Hooks{
			OnProviderResponse: func(_ int, ev goagent.ProviderEvent) {
				got = append(got, ev)
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

	if len(got) < 1 {
		t.Fatal("OnProviderResponse not called")
	}
	if got[0].ToolCalls != 2 {
		t.Errorf("tool calls = %d, want 2", got[0].ToolCalls)
	}
}

func TestWithRunResult(t *testing.T) {
	t.Parallel()

	var result goagent.RunResult

	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(
			toolUseRespWithUsage("t1", "calc", map[string]any{}, 100, 30),
			endTurnRespWithUsage("done", 150, 40),
		)),
		goagent.WithTool(testutil.NewMockTool("calc", "arithmetic", "42")),
		goagent.WithRunResult(&result),
	)
	if err != nil {
		t.Fatal(err)
	}

	got, err := agent.Run(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "done" {
		t.Errorf("result text = %q, want %q", got, "done")
	}

	if result.Iterations != 2 {
		t.Errorf("iterations = %d, want 2", result.Iterations)
	}
	if result.TotalUsage.InputTokens != 250 {
		t.Errorf("input tokens = %d, want 250", result.TotalUsage.InputTokens)
	}
	if result.TotalUsage.OutputTokens != 70 {
		t.Errorf("output tokens = %d, want 70", result.TotalUsage.OutputTokens)
	}
	if result.ToolCalls != 1 {
		t.Errorf("tool calls = %d, want 1", result.ToolCalls)
	}
	if result.Duration < 0 {
		t.Errorf("duration = %v, want >= 0", result.Duration)
	}
	if result.Err != nil {
		t.Errorf("err = %v, want nil", result.Err)
	}
}

func TestWithRunResult_ProviderError(t *testing.T) {
	t.Parallel()

	provErr := errors.New("api error")
	var result goagent.RunResult

	agent, err := goagent.New(
		goagent.WithProvider(&errorProvider{err: provErr}),
		goagent.WithRunResult(&result),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, runErr := agent.Run(context.Background(), "hello")
	if runErr == nil {
		t.Fatal("expected error from Run, got nil")
	}

	if result.Err == nil {
		t.Fatal("RunResult.Err is nil, want non-nil")
	}
	var pe *goagent.ProviderError
	if !errors.As(result.Err, &pe) {
		t.Errorf("RunResult.Err type = %T, want *ProviderError", result.Err)
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

func TestMergeHooks_Empty(t *testing.T) {
	t.Parallel()
	h := goagent.MergeHooks()
	// All fields should be nil on empty input.
	if h.OnRunStart != nil {
		t.Error("expected nil OnRunStart")
	}
	if h.OnToolCall != nil {
		t.Error("expected nil OnToolCall")
	}
	if h.OnRunEnd != nil {
		t.Error("expected nil OnRunEnd")
	}
}

func TestMergeHooks_NilFieldsPreserved(t *testing.T) {
	t.Parallel()
	// When no input defines a field, the merged result must have nil for that
	// field — not a no-op closure. This preserves zero-value semantics and
	// avoids false positives in "if hook != nil" checks.
	h1 := goagent.Hooks{OnRunStart: func() {}}
	h2 := goagent.Hooks{OnToolCall: func(string, map[string]any) {}}

	merged := goagent.MergeHooks(h1, h2)

	if merged.OnRunStart == nil {
		t.Error("OnRunStart should be non-nil (h1 defines it)")
	}
	if merged.OnToolCall == nil {
		t.Error("OnToolCall should be non-nil (h2 defines it)")
	}
	// Fields defined by neither h1 nor h2 must remain nil.
	if merged.OnThinking != nil {
		t.Error("OnThinking should be nil (nobody defines it)")
	}
	if merged.OnRunEnd != nil {
		t.Error("OnRunEnd should be nil (nobody defines it)")
	}
	if merged.OnToolResult != nil {
		t.Error("OnToolResult should be nil (nobody defines it)")
	}
	if merged.OnLongTermStore != nil {
		t.Error("OnLongTermStore should be nil (nobody defines it)")
	}
}

func TestMergeHooks_Single(t *testing.T) {
	t.Parallel()
	var called bool
	single := goagent.Hooks{
		OnRunStart: func() { called = true },
	}
	merged := goagent.MergeHooks(single)
	merged.OnRunStart()
	if !called {
		t.Error("single hook should be returned as-is")
	}
}

func TestMergeHooks_MultipleCalled_InOrder(t *testing.T) {
	t.Parallel()
	var order []int

	h1 := goagent.Hooks{
		OnIterationStart: func(i int) { order = append(order, 1) },
		OnToolCall:       func(name string, args map[string]any) { order = append(order, 10) },
		OnResponse:       func(text string, iterations int) { order = append(order, 100) },
	}
	h2 := goagent.Hooks{
		OnIterationStart: func(i int) { order = append(order, 2) },
		OnToolCall:       func(name string, args map[string]any) { order = append(order, 20) },
		OnResponse:       func(text string, iterations int) { order = append(order, 200) },
	}
	h3 := goagent.Hooks{
		OnIterationStart: func(i int) { order = append(order, 3) },
		OnToolCall:       func(name string, args map[string]any) { order = append(order, 30) },
		OnResponse:       func(text string, iterations int) { order = append(order, 300) },
	}

	merged := goagent.MergeHooks(h1, h2, h3)

	merged.OnIterationStart(0)
	merged.OnToolCall("test", nil)
	merged.OnResponse("done", 1)

	expected := []int{1, 2, 3, 10, 20, 30, 100, 200, 300}
	if len(order) != len(expected) {
		t.Fatalf("expected %d calls, got %d: %v", len(expected), len(order), order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("order[%d] = %d, want %d", i, order[i], v)
		}
	}
}

func TestMergeHooks_NilHooksSkipped(t *testing.T) {
	t.Parallel()
	var called bool

	// h1 has OnRunStart, h2 does not — merged should still work.
	h1 := goagent.Hooks{OnRunStart: func() { called = true }}
	h2 := goagent.Hooks{} // all nil

	merged := goagent.MergeHooks(h1, h2)
	merged.OnRunStart()
	if !called {
		t.Error("OnRunStart should have been called")
	}
}

func TestMergeHooks_AllHookFields(t *testing.T) {
	t.Parallel()

	// Track which hooks were called.
	called := make(map[string]int)

	h := goagent.Hooks{
		OnRunStart:         func() { called["OnRunStart"]++ },
		OnRunEnd:           func(r goagent.RunResult) { called["OnRunEnd"]++ },
		OnProviderRequest:  func(i int, m string, mc int) { called["OnProviderRequest"]++ },
		OnProviderResponse: func(i int, e goagent.ProviderEvent) { called["OnProviderResponse"]++ },
		OnIterationStart:   func(i int) { called["OnIterationStart"]++ },
		OnThinking:         func(t string) { called["OnThinking"]++ },
		OnToolCall:         func(n string, a map[string]any) { called["OnToolCall"]++ },
		OnToolResult:       func(n string, c []goagent.ContentBlock, d time.Duration, e error) { called["OnToolResult"]++ },
		OnCircuitOpen:      func(n string, t time.Time) { called["OnCircuitOpen"]++ },
		OnResponse:         func(t string, i int) { called["OnResponse"]++ },
		OnShortTermLoad:    func(r int, d time.Duration, e error) { called["OnShortTermLoad"]++ },
		OnShortTermAppend:  func(m int, d time.Duration, e error) { called["OnShortTermAppend"]++ },
		OnLongTermRetrieve: func(r int, d time.Duration, e error) { called["OnLongTermRetrieve"]++ },
		OnLongTermStore:    func(m int, d time.Duration, e error) { called["OnLongTermStore"]++ },
	}

	merged := goagent.MergeHooks(h, h) // merge with itself — each should be called twice

	// Invoke every hook.
	merged.OnRunStart()
	merged.OnRunEnd(goagent.RunResult{})
	merged.OnProviderRequest(0, "m", 1)
	merged.OnProviderResponse(0, goagent.ProviderEvent{})
	merged.OnIterationStart(0)
	merged.OnThinking("think")
	merged.OnToolCall("tool", nil)
	merged.OnToolResult("tool", nil, 0, nil)
	merged.OnCircuitOpen("tool", time.Now())
	merged.OnResponse("done", 1)
	merged.OnShortTermLoad(0, 0, nil)
	merged.OnShortTermAppend(0, 0, nil)
	merged.OnLongTermRetrieve(0, 0, nil)
	merged.OnLongTermStore(0, 0, nil)

	hooks := []string{
		"OnRunStart", "OnRunEnd", "OnProviderRequest", "OnProviderResponse",
		"OnIterationStart", "OnThinking", "OnToolCall", "OnToolResult",
		"OnCircuitOpen", "OnResponse", "OnShortTermLoad", "OnShortTermAppend",
		"OnLongTermRetrieve", "OnLongTermStore",
	}
	for _, name := range hooks {
		if called[name] != 2 {
			t.Errorf("%s called %d times, want 2", name, called[name])
		}
	}
}

func TestMergeHooks_Integration(t *testing.T) {
	t.Parallel()

	var log1, log2 []string

	hooks1 := goagent.Hooks{
		OnToolCall: func(name string, _ map[string]any) {
			log1 = append(log1, name)
		},
		OnResponse: func(text string, _ int) {
			log1 = append(log1, "response:"+text)
		},
	}
	hooks2 := goagent.Hooks{
		OnToolCall: func(name string, _ map[string]any) {
			log2 = append(log2, name)
		},
		OnResponse: func(text string, _ int) {
			log2 = append(log2, "response:"+text)
		},
	}

	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(
			toolUseResp("t1", "calc", map[string]any{}),
			endTurnResp("42"),
		)),
		goagent.WithTool(testutil.NewMockTool("calc", "arithmetic", "result")),
		goagent.WithHooks(goagent.MergeHooks(hooks1, hooks2)),
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := agent.Run(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if result != "42" {
		t.Errorf("got %q, want %q", result, "42")
	}

	// Both hook sets should have seen the same events.
	if len(log1) != 2 || log1[0] != "calc" || log1[1] != "response:42" {
		t.Errorf("hooks1 log: %v", log1)
	}
	if len(log2) != 2 || log2[0] != "calc" || log2[1] != "response:42" {
		t.Errorf("hooks2 log: %v", log2)
	}
}
