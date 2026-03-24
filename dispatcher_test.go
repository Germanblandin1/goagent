package goagent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/internal/testutil"
)

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

	a := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithTool(tool),
	)
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

	a := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithTool(tool),
	)
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
	a := goagent.New(goagent.WithProvider(mp))
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

	a := goagent.New(
		goagent.WithProvider(mp),
		goagent.WithTool(goodTool),
		goagent.WithTool(badTool),
	)
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
