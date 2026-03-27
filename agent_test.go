package goagent_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/internal/testutil"
)

// recordingCloser counts how many times Close has been called.
type recordingCloser struct {
	closed int
}

func (c *recordingCloser) Close() error {
	c.closed++
	return nil
}

// endTurnResp builds a simple final-answer response.
func endTurnResp(content string) goagent.CompletionResponse {
	return goagent.CompletionResponse{
		Message:    goagent.AssistantMessage(content),
		StopReason: goagent.StopReasonEndTurn,
	}
}

// toolUseResp builds a response that asks for one tool call.
func toolUseResp(toolID, toolName string, args map[string]any) goagent.CompletionResponse {
	return goagent.CompletionResponse{
		Message: goagent.Message{
			Role: goagent.RoleAssistant,
			ToolCalls: []goagent.ToolCall{
				{ID: toolID, Name: toolName, Arguments: args},
			},
		},
		StopReason: goagent.StopReasonToolUse,
	}
}

func TestAgentRun(t *testing.T) {
	t.Parallel()

	calcTool := testutil.NewMockTool("calc", "arithmetic", "42")
	errTool := testutil.NewMockToolWithError("bad_tool", "always fails", errors.New("tool error"))

	tests := []struct {
		name        string
		provider    *testutil.MockProvider
		opts        []goagent.Option
		prompt      string
		wantResult  string
		wantErr     bool
		wantErrType any // pointer to target for errors.As
	}{
		{
			name:       "simple response without tools",
			provider:   testutil.NewMockProvider(endTurnResp("hello back")),
			prompt:     "hello",
			wantResult: "hello back",
		},
		{
			name: "one tool call then final answer",
			provider: testutil.NewMockProvider(
				toolUseResp("c1", "calc", map[string]any{}),
				endTurnResp("the answer is 42"),
			),
			opts:       []goagent.Option{goagent.WithTool(calcTool)},
			prompt:     "what is 6*7",
			wantResult: "the answer is 42",
		},
		{
			name: "sequential tool calls",
			provider: testutil.NewMockProvider(
				toolUseResp("c1", "calc", map[string]any{}),
				toolUseResp("c2", "calc", map[string]any{}),
				endTurnResp("done"),
			),
			opts:       []goagent.Option{goagent.WithTool(calcTool)},
			prompt:     "compute twice",
			wantResult: "done",
		},
		{
			name: "parallel tool calls in one response",
			provider: testutil.NewMockProvider(
				goagent.CompletionResponse{
					Message: goagent.Message{
						Role: goagent.RoleAssistant,
						ToolCalls: []goagent.ToolCall{
							{ID: "p1", Name: "calc", Arguments: map[string]any{}},
							{ID: "p2", Name: "calc", Arguments: map[string]any{}},
						},
					},
					StopReason: goagent.StopReasonToolUse,
				},
				endTurnResp("parallel done"),
			),
			opts:       []goagent.Option{goagent.WithTool(calcTool)},
			prompt:     "parallel",
			wantResult: "parallel done",
		},
		{
			name: "max iterations exceeded",
			provider: testutil.NewMockProvider(
				toolUseResp("c1", "calc", map[string]any{}),
				toolUseResp("c2", "calc", map[string]any{}),
				toolUseResp("c3", "calc", map[string]any{}),
			),
			opts: []goagent.Option{
				goagent.WithTool(calcTool),
				goagent.WithMaxIterations(3),
			},
			prompt:      "loop",
			wantErr:     true,
			wantErrType: &goagent.MaxIterationsError{},
		},
		{
			name: "tool not found — error as observation, model recovers",
			provider: testutil.NewMockProvider(
				toolUseResp("c1", "missing_tool", map[string]any{}),
				endTurnResp("recovered"),
			),
			opts:       []goagent.Option{goagent.WithTool(calcTool)},
			prompt:     "use missing tool",
			wantResult: "recovered",
		},
		{
			name: "tool execution error — error as observation",
			provider: testutil.NewMockProvider(
				toolUseResp("c1", "bad_tool", map[string]any{}),
				endTurnResp("handled error"),
			),
			opts:       []goagent.Option{goagent.WithTool(errTool)},
			prompt:     "use bad tool",
			wantResult: "handled error",
		},
		{
			name: "context cancellation",
			provider: testutil.NewMockProvider(
				// provider will never be reached because context is pre-cancelled
			),
			prompt:      "cancelled",
			wantErr:     true,
			wantErrType: context.Canceled,
		},
		{
			name: "provider error wrapped in ProviderError",
			provider: testutil.NewMockProvider(
			// no responses — mock returns an error
			),
			prompt:      "fail",
			wantErr:     true,
			wantErrType: &goagent.ProviderError{},
		},
		{
			name:        "no provider configured",
			provider:    nil,
			prompt:      "anything",
			wantErr:     true,
			wantErrType: nil, // plain error, not a specific type
		},
		{
			name:     "system prompt included in requests",
			provider: testutil.NewMockProvider(endTurnResp("ok")),
			opts: []goagent.Option{
				goagent.WithSystemPrompt("you are helpful"),
			},
			prompt:     "hi",
			wantResult: "ok",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Start with a default maxIterations so tests that don't set one
			// have a sensible ceiling. Test cases that need a specific value
			// include WithMaxIterations in their own opts, which will override
			// this default because functional options are applied in order.
			opts := []goagent.Option{goagent.WithMaxIterations(10)}
			opts = append(opts, tt.opts...)
			if tt.provider != nil {
				opts = append(opts, goagent.WithProvider(tt.provider))
			}
			a, err := goagent.New(opts...)
			if err != nil {
				t.Fatal(err)
			}

			ctx := context.Background()
			if tt.name == "context cancellation" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel() // pre-cancel
			}

			result, err := a.Run(ctx, tt.prompt)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				switch target := tt.wantErrType.(type) {
				case *goagent.MaxIterationsError:
					if !errors.As(err, &target) {
						t.Errorf("want MaxIterationsError, got %T: %v", err, err)
					}
				case *goagent.ProviderError:
					if !errors.As(err, &target) {
						t.Errorf("want ProviderError, got %T: %v", err, err)
					}
				case error:
					if !errors.Is(err, target) {
						t.Errorf("want %v, got %v", target, err)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.wantResult {
				t.Errorf("result = %q, want %q", result, tt.wantResult)
			}
		})
	}
}

func TestAgentRun_WithMemory_HistoryPrepended(t *testing.T) {
	t.Parallel()

	mem := testutil.NewMockMemoryWithHistory(
		goagent.UserMessage("me llamo Lucas"),
		goagent.AssistantMessage("Hola Lucas!"),
	)
	mp := testutil.NewMockProvider(endTurnResp("eres Lucas"))
	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithShortTermMemory(mem),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = a.Run(context.Background(), "¿cómo me llamo?")

	calls := mp.Calls()
	if len(calls) == 0 {
		t.Fatal("no calls recorded")
	}
	// First two messages must be the history loaded from memory.
	msgs := calls[0].Messages
	if len(msgs) < 3 {
		t.Fatalf("expected at least 3 messages (2 history + 1 prompt), got %d", len(msgs))
	}
	if msgs[0].TextContent() != "me llamo Lucas" {
		t.Errorf("msgs[0].TextContent() = %q, want %q", msgs[0].TextContent(), "me llamo Lucas")
	}
	if msgs[1].TextContent() != "Hola Lucas!" {
		t.Errorf("msgs[1].TextContent() = %q, want %q", msgs[1].TextContent(), "Hola Lucas!")
	}
	if msgs[2].TextContent() != "¿cómo me llamo?" {
		t.Errorf("msgs[2].TextContent() = %q, want %q", msgs[2].TextContent(), "¿cómo me llamo?")
	}
}

func TestAgentRun_WithMemory_AllTurnsStored(t *testing.T) {
	t.Parallel()

	calcTool := testutil.NewMockTool("calc", "arithmetic", "42")
	mem := testutil.NewMockMemory()
	mp := testutil.NewMockProvider(
		toolUseResp("c1", "calc", map[string]any{}),
		endTurnResp("the answer is 42"),
	)
	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithTool(calcTool),
		goagent.WithShortTermMemory(mem),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = a.Run(context.Background(), "what is 6*7")

	stored := mem.All()
	// Expect: user + assistant-with-toolcall + tool-result + final-assistant = 4 messages.
	if len(stored) != 4 {
		t.Fatalf("expected 4 stored messages, got %d: %v", len(stored), stored)
	}
	if stored[0].Role != goagent.RoleUser {
		t.Errorf("stored[0].Role = %q, want user", stored[0].Role)
	}
	if stored[1].Role != goagent.RoleAssistant || len(stored[1].ToolCalls) == 0 {
		t.Errorf("stored[1] should be assistant with tool calls")
	}
	if stored[2].Role != goagent.RoleTool {
		t.Errorf("stored[2].Role = %q, want tool", stored[2].Role)
	}
	if stored[3].Role != goagent.RoleAssistant || stored[3].TextContent() != "the answer is 42" {
		t.Errorf("stored[3] should be final assistant answer")
	}
}

func TestAgentRun_WithMemory_AccumulatesAcrossRuns(t *testing.T) {
	t.Parallel()

	mem := testutil.NewMockMemory()
	mp := testutil.NewMockProvider(
		endTurnResp("resp-1"),
		endTurnResp("resp-2"),
		endTurnResp("resp-3"),
	)
	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithShortTermMemory(mem),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	_, _ = a.Run(ctx, "prompt-1")
	_, _ = a.Run(ctx, "prompt-2")
	_, _ = a.Run(ctx, "prompt-3")

	// Each run stores: 1 user + 1 assistant = 2 messages.
	stored := mem.All()
	if len(stored) != 6 {
		t.Fatalf("expected 6 stored messages after 3 runs, got %d", len(stored))
	}
}

func TestAgentRun_WithMemory_NilMemory_Stateless(t *testing.T) {
	t.Parallel()

	mp := testutil.NewMockProvider(endTurnResp("ok"))
	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithShortTermMemory(nil),
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := a.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("result = %q, want %q", result, "ok")
	}
}

func TestAgentRun_WithMemory_AppendError_DoesNotFail(t *testing.T) {
	t.Parallel()

	mem := testutil.NewMockMemoryWithErrors(errors.New("storage unavailable"), nil)
	mp := testutil.NewMockProvider(endTurnResp("ok"))
	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithShortTermMemory(mem),
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := a.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Run should succeed even when Append fails, got: %v", err)
	}
	if result != "ok" {
		t.Errorf("result = %q, want %q", result, "ok")
	}
}

func TestAgentRun_WithMemory_MaxIterations_StillPersists(t *testing.T) {
	t.Parallel()

	calcTool := testutil.NewMockTool("calc", "arithmetic", "42")
	mem := testutil.NewMockMemory()
	mp := testutil.NewMockProvider(
		toolUseResp("c1", "calc", map[string]any{}),
		toolUseResp("c2", "calc", map[string]any{}),
	)
	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithTool(calcTool),
		goagent.WithShortTermMemory(mem),
		goagent.WithMaxIterations(2),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = a.Run(context.Background(), "loop")
	if err == nil {
		t.Fatal("expected MaxIterationsError")
	}
	var maxErr *goagent.MaxIterationsError
	if !errors.As(err, &maxErr) {
		t.Fatalf("want MaxIterationsError, got %T", err)
	}

	// Memory must still have been written even though Run failed.
	stored := mem.All()
	if len(stored) == 0 {
		t.Error("expected messages to be persisted even on MaxIterationsError")
	}
}

func TestAgentRun_WithShortTermTraceTools_False_StoresOnlyUserAndFinalAssistant(t *testing.T) {
	t.Parallel()

	calcTool := testutil.NewMockTool("calc", "arithmetic", "42")
	mem := testutil.NewMockMemory()
	mp := testutil.NewMockProvider(
		toolUseResp("c1", "calc", map[string]any{}),
		endTurnResp("the answer is 42"),
	)
	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithTool(calcTool),
		goagent.WithShortTermMemory(mem),
		goagent.WithShortTermTraceTools(false),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = a.Run(context.Background(), "what is 6*7")

	stored := mem.All()
	// With traceTools=false: only user + final assistant (no tool call intermediates).
	if len(stored) != 2 {
		t.Fatalf("expected 2 stored messages (user + assistant), got %d: %v", len(stored), stored)
	}
	if stored[0].Role != goagent.RoleUser {
		t.Errorf("stored[0].Role = %q, want user", stored[0].Role)
	}
	if stored[1].Role != goagent.RoleAssistant || stored[1].TextContent() != "the answer is 42" {
		t.Errorf("stored[1] should be final assistant answer, got %+v", stored[1])
	}
}

func TestAgentRun_WithLongTermMemory_RetrieveContextPrepended(t *testing.T) {
	t.Parallel()

	pastMsg := goagent.UserMessage("past context")
	ltm := testutil.NewMockLongTermMemoryWithRetrieve(pastMsg)
	mp := testutil.NewMockProvider(endTurnResp("ok"))
	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithLongTermMemory(ltm),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = a.Run(context.Background(), "current question")

	calls := mp.Calls()
	if len(calls) == 0 {
		t.Fatal("no provider calls recorded")
	}
	msgs := calls[0].Messages
	// Long-term context must be first, then the user prompt.
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(msgs))
	}
	if msgs[0].TextContent() != "past context" {
		t.Errorf("msgs[0].TextContent() = %q, want long-term context %q", msgs[0].TextContent(), "past context")
	}
	if msgs[len(msgs)-1].TextContent() != "current question" {
		t.Errorf("last message should be the user prompt")
	}
}

func TestAgentRun_WithLongTermMemory_StoreCalledAfterRun(t *testing.T) {
	t.Parallel()

	ltm := testutil.NewMockLongTermMemory()
	mp := testutil.NewMockProvider(endTurnResp("great answer"))
	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithLongTermMemory(ltm),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = a.Run(context.Background(), "my question")

	stored := ltm.AllStored()
	if len(stored) != 2 {
		t.Fatalf("expected 2 messages stored (user + assistant), got %d", len(stored))
	}
	if stored[0].Role != goagent.RoleUser || stored[0].TextContent() != "my question" {
		t.Errorf("stored[0] = %+v, want user/my question", stored[0])
	}
	if stored[1].Role != goagent.RoleAssistant || stored[1].TextContent() != "great answer" {
		t.Errorf("stored[1] = %+v, want assistant/great answer", stored[1])
	}
}

func TestAgentRun_WithLongTermMemory_WritePolicy_Skips(t *testing.T) {
	t.Parallel()

	ltm := testutil.NewMockLongTermMemory()
	mp := testutil.NewMockProvider(endTurnResp("short"))
	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithLongTermMemory(ltm),
		// Only store turns where prompt+response > 100 chars; "short" won't qualify.
		goagent.WithWritePolicy(goagent.MinLength(100)),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = a.Run(context.Background(), "hi")

	if stored := ltm.AllStored(); len(stored) != 0 {
		t.Errorf("expected nothing stored (write policy blocked), got %d messages", len(stored))
	}
}

func TestAgentRun_WithLongTermMemory_StoreError_DoesNotFail(t *testing.T) {
	t.Parallel()

	ltm := testutil.NewMockLongTermMemoryWithErrors(errors.New("storage down"), nil)
	mp := testutil.NewMockProvider(endTurnResp("ok"))
	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithLongTermMemory(ltm),
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := a.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Run should succeed even when long-term Store fails, got: %v", err)
	}
	if result != "ok" {
		t.Errorf("result = %q, want %q", result, "ok")
	}
}

func TestAgentRun_BothMemories_LongTermBeforeShortTerm(t *testing.T) {
	t.Parallel()

	ltContext := goagent.UserMessage("long-term context")
	ltm := testutil.NewMockLongTermMemoryWithRetrieve(ltContext)
	stm := testutil.NewMockMemoryWithHistory(
		goagent.UserMessage("recent history"),
	)
	mp := testutil.NewMockProvider(endTurnResp("ok"))
	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithLongTermMemory(ltm),
		goagent.WithShortTermMemory(stm),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = a.Run(context.Background(), "current prompt")

	calls := mp.Calls()
	if len(calls) == 0 {
		t.Fatal("no provider calls recorded")
	}
	msgs := calls[0].Messages
	// Order must be: [long-term context, short-term history, current prompt]
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d: %v", len(msgs), msgs)
	}
	if msgs[0].TextContent() != "long-term context" {
		t.Errorf("msgs[0] = %q, want long-term context first", msgs[0].TextContent())
	}
	if msgs[1].TextContent() != "recent history" {
		t.Errorf("msgs[1] = %q, want short-term history second", msgs[1].TextContent())
	}
	if msgs[2].TextContent() != "current prompt" {
		t.Errorf("msgs[2] = %q, want current prompt last", msgs[2].TextContent())
	}
}

func TestAgentRunBlocks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		blocks  []goagent.ContentBlock
		wantErr bool
		wantIs  error // sentinel to match with errors.Is
	}{
		{
			name:   "text only",
			blocks: []goagent.ContentBlock{goagent.TextBlock("hello")},
		},
		{
			name:   "image with valid media type",
			blocks: []goagent.ContentBlock{goagent.ImageBlock([]byte{0xFF}, "image/png")},
		},
		{
			name:   "text and image combined",
			blocks: []goagent.ContentBlock{goagent.TextBlock("describe"), goagent.ImageBlock([]byte{0xFF}, "image/jpeg")},
		},
		{
			name:    "empty blocks",
			blocks:  nil,
			wantErr: true,
		},
		{
			name:    "invalid image media type",
			blocks:  []goagent.ContentBlock{goagent.ImageBlock([]byte{0xFF}, "image/bmp")},
			wantErr: true,
			wantIs:  goagent.ErrInvalidMediaType,
		},
		{
			name:    "nil image data",
			blocks:  []goagent.ContentBlock{{Type: goagent.ContentImage, Image: nil}},
			wantErr: true,
			wantIs:  goagent.ErrInvalidMediaType,
		},
		{
			name:    "nil document data",
			blocks:  []goagent.ContentBlock{{Type: goagent.ContentDocument, Document: nil}},
			wantErr: true,
			wantIs:  goagent.ErrInvalidMediaType,
		},
		{
			name:    "invalid document media type",
			blocks:  []goagent.ContentBlock{goagent.DocumentBlock([]byte("data"), "text/html", "doc")},
			wantErr: true,
			wantIs:  goagent.ErrInvalidMediaType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a, err := goagent.New(
				goagent.WithProvider(testutil.NewMockProvider(endTurnResp("ok"))),
				goagent.WithMaxIterations(3),
			)
			if err != nil {
				t.Fatal(err)
			}

			result, err := a.RunBlocks(context.Background(), tt.blocks...)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
					t.Errorf("want errors.Is(%v), got: %v", tt.wantIs, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != "ok" {
				t.Errorf("result = %q, want %q", result, "ok")
			}
		})
	}
}

func TestAgentRun_SystemPromptForwarded(t *testing.T) {
	t.Parallel()

	mp := testutil.NewMockProvider(endTurnResp("ok"))
	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithSystemPrompt("be concise"),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = a.Run(context.Background(), "hi")

	calls := mp.Calls()
	if len(calls) == 0 {
		t.Fatal("no calls recorded")
	}
	if calls[0].SystemPrompt != "be concise" {
		t.Errorf("SystemPrompt = %q, want %q", calls[0].SystemPrompt, "be concise")
	}
}

// ── Extended Thinking & Effort ───────────────────────────────────────────────

func TestAgentRun_WithThinking_PropagatedToProvider(t *testing.T) {
	t.Parallel()

	mp := testutil.NewMockProvider(endTurnResp("ok"))
	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithThinking(8000),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = a.Run(context.Background(), "hello")

	calls := mp.Calls()
	if len(calls) == 0 {
		t.Fatal("no provider calls recorded")
	}
	cfg := calls[0].Thinking
	if cfg == nil {
		t.Fatal("Thinking is nil, want non-nil ThinkingConfig")
	}
	if !cfg.Enabled {
		t.Error("ThinkingConfig.Enabled = false, want true")
	}
	if cfg.BudgetTokens != 8000 {
		t.Errorf("ThinkingConfig.BudgetTokens = %d, want 8000", cfg.BudgetTokens)
	}
}

func TestAgentRun_WithAdaptiveThinking_PropagatedToProvider(t *testing.T) {
	t.Parallel()

	mp := testutil.NewMockProvider(endTurnResp("ok"))
	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithAdaptiveThinking(),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = a.Run(context.Background(), "hello")

	calls := mp.Calls()
	if len(calls) == 0 {
		t.Fatal("no provider calls recorded")
	}
	cfg := calls[0].Thinking
	if cfg == nil {
		t.Fatal("Thinking is nil, want non-nil ThinkingConfig")
	}
	if !cfg.Enabled {
		t.Error("ThinkingConfig.Enabled = false, want true")
	}
	if cfg.BudgetTokens != 0 {
		t.Errorf("ThinkingConfig.BudgetTokens = %d, want 0 (adaptive)", cfg.BudgetTokens)
	}
}

func TestAgentRun_WithEffort_PropagatedToProvider(t *testing.T) {
	t.Parallel()

	mp := testutil.NewMockProvider(endTurnResp("ok"))
	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithEffort("medium"),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = a.Run(context.Background(), "hello")

	calls := mp.Calls()
	if len(calls) == 0 {
		t.Fatal("no provider calls recorded")
	}
	if calls[0].Effort != "medium" {
		t.Errorf("Effort = %q, want %q", calls[0].Effort, "medium")
	}
}

func TestAgentRun_ThinkingAndEffort_Combined(t *testing.T) {
	t.Parallel()

	mp := testutil.NewMockProvider(endTurnResp("ok"))
	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithThinking(4096),
		goagent.WithEffort("low"),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = a.Run(context.Background(), "hello")

	calls := mp.Calls()
	if len(calls) == 0 {
		t.Fatal("no provider calls recorded")
	}
	req := calls[0]
	if req.Thinking == nil || !req.Thinking.Enabled || req.Thinking.BudgetTokens != 4096 {
		t.Errorf("Thinking = %+v, want {Enabled:true BudgetTokens:4096}", req.Thinking)
	}
	if req.Effort != "low" {
		t.Errorf("Effort = %q, want %q", req.Effort, "low")
	}
}

func TestAgentRun_NoThinking_ThinkingFieldNil(t *testing.T) {
	t.Parallel()

	mp := testutil.NewMockProvider(endTurnResp("ok"))
	a, err := goagent.New(goagent.WithProvider(mp))
	if err != nil {
		t.Fatal(err)
	}

	_, _ = a.Run(context.Background(), "hello")

	calls := mp.Calls()
	if len(calls) == 0 {
		t.Fatal("no provider calls recorded")
	}
	if calls[0].Thinking != nil {
		t.Errorf("Thinking = %+v, want nil (no thinking configured)", calls[0].Thinking)
	}
	if calls[0].Effort != "" {
		t.Errorf("Effort = %q, want empty string (no effort configured)", calls[0].Effort)
	}
}

func TestAgentRun_ThinkingPreservedDuringToolUse(t *testing.T) {
	t.Parallel()

	// First response: thinking block + tool call.
	// Second response: thinking block + final text.
	calcTool := testutil.NewMockTool("calc", "arithmetic", "42")
	firstResp := goagent.CompletionResponse{
		Message: goagent.Message{
			Role: goagent.RoleAssistant,
			Content: []goagent.ContentBlock{
				goagent.ThinkingBlock("I need to use calc", "sig1"),
			},
			ToolCalls: []goagent.ToolCall{
				{ID: "c1", Name: "calc", Arguments: map[string]any{}},
			},
		},
		StopReason: goagent.StopReasonToolUse,
	}
	secondResp := endTurnResp("the answer is 42")

	mp := testutil.NewMockProvider(firstResp, secondResp)
	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithTool(calcTool),
		goagent.WithThinking(4096),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = a.Run(context.Background(), "what is 6*7")

	calls := mp.Calls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 provider calls, got %d", len(calls))
	}

	// The second call must include the thinking block from the first assistant
	// message in its Messages slice.
	secondCallMsgs := calls[1].Messages
	var foundThinking bool
	for _, msg := range secondCallMsgs {
		if msg.HasContentType(goagent.ContentThinking) {
			foundThinking = true
			break
		}
	}
	if !foundThinking {
		t.Error("second provider call does not contain the thinking block from the first response; thinking continuity broken")
	}
}

func TestAgentRun_ThinkingStrippedBeforePersist(t *testing.T) {
	t.Parallel()

	// Provider returns a response with a thinking block.
	respWithThinking := goagent.CompletionResponse{
		Message: goagent.Message{
			Role: goagent.RoleAssistant,
			Content: []goagent.ContentBlock{
				goagent.ThinkingBlock("internal reasoning", "sig"),
				goagent.TextBlock("final answer"),
			},
		},
		StopReason: goagent.StopReasonEndTurn,
	}

	mem := testutil.NewMockMemory()
	mp := testutil.NewMockProvider(respWithThinking)
	a, err := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithShortTermMemory(mem),
		goagent.WithThinking(4096),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = a.Run(context.Background(), "a question")

	stored := mem.All()
	for _, msg := range stored {
		if msg.HasContentType(goagent.ContentThinking) {
			t.Errorf("thinking block found in stored message (role=%q) — should have been stripped before persist", msg.Role)
		}
	}
}

func TestNew_WithMCPConnector_ToolAvailable(t *testing.T) {
	t.Parallel()

	closer := &recordingCloser{}
	fakeTool := goagent.ToolFunc("mcp_tool", "from MCP", nil,
		func(_ context.Context, _ map[string]any) (string, error) { return "mcp_result", nil },
	)

	connector := func(_ context.Context, _ *slog.Logger) ([]goagent.Tool, io.Closer, error) {
		return []goagent.Tool{fakeTool}, closer, nil
	}

	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(
			toolUseResp("t1", "mcp_tool", map[string]any{}),
			endTurnResp("done"),
		)),
		goagent.WithMCPConnector(connector),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer agent.Close()

	result, err := agent.Run(context.Background(), "use mcp tool")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result != "done" {
		t.Errorf("result = %q, want %q", result, "done")
	}
}

func TestAgent_Close_CallsCloser(t *testing.T) {
	t.Parallel()

	closer := &recordingCloser{}
	connector := func(_ context.Context, _ *slog.Logger) ([]goagent.Tool, io.Closer, error) {
		return nil, closer, nil
	}

	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider()),
		goagent.WithMCPConnector(connector),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := agent.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if closer.closed != 1 {
		t.Errorf("closer.closed = %d, want 1", closer.closed)
	}
}

func TestAgent_Close_Idempotent(t *testing.T) {
	t.Parallel()

	closer := &recordingCloser{}
	connector := func(_ context.Context, _ *slog.Logger) ([]goagent.Tool, io.Closer, error) {
		return nil, closer, nil
	}

	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider()),
		goagent.WithMCPConnector(connector),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_ = agent.Close()
	_ = agent.Close() // second call must not invoke the closer again

	if closer.closed != 1 {
		t.Errorf("closer.closed = %d after two Close calls, want 1 (idempotent)", closer.closed)
	}
}

func TestNew_MCPConnectorError_ClosesAlreadyOpened(t *testing.T) {
	t.Parallel()

	closer1 := &recordingCloser{}
	connErr := errors.New("connection refused")

	_, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider()),
		goagent.WithMCPConnector(func(_ context.Context, _ *slog.Logger) ([]goagent.Tool, io.Closer, error) {
			return nil, closer1, nil
		}),
		goagent.WithMCPConnector(func(_ context.Context, _ *slog.Logger) ([]goagent.Tool, io.Closer, error) {
			return nil, nil, connErr
		}),
	)

	if err == nil {
		t.Fatal("expected error from failing connector, got nil")
	}
	if !errors.Is(err, connErr) {
		t.Errorf("want connErr in chain, got: %v", err)
	}
	if closer1.closed != 1 {
		t.Errorf("closer1.closed = %d, want 1 (should be closed on error)", closer1.closed)
	}
}
