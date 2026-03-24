package policy_test

import (
	"context"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/policy"
)

func TestFixedWindow_PanicsOnZero(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for n=0")
		}
	}()
	policy.NewFixedWindow(0)
}

func TestFixedWindow_PanicsOnNegative(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for n=-1")
		}
	}()
	policy.NewFixedWindow(-1)
}

func TestFixedWindow_Apply(t *testing.T) {
	t.Parallel()

	user := func(c string) goagent.Message { return goagent.UserMessage(c) }
	asst := func(c string) goagent.Message { return goagent.AssistantMessage(c) }
	tool := func(c string) goagent.Message { return goagent.TextMessage(goagent.RoleTool, c) }
	asstTool := func() goagent.Message {
		return goagent.Message{
			Role:      goagent.RoleAssistant,
			ToolCalls: []goagent.ToolCall{{ID: "c1", Name: "calc"}},
		}
	}

	tests := []struct {
		name      string
		n         int
		input     []goagent.Message
		wantLen   int
		wantFirst string
	}{
		{
			name:    "empty input",
			n:       3,
			input:   nil,
			wantLen: 0,
		},
		{
			name:      "fewer messages than window",
			n:         5,
			input:     []goagent.Message{user("a"), asst("b")},
			wantLen:   2,
			wantFirst: "a",
		},
		{
			name:      "exactly window size",
			n:         2,
			input:     []goagent.Message{user("a"), asst("b")},
			wantLen:   2,
			wantFirst: "a",
		},
		{
			name:      "over window — clean cut",
			n:         2,
			input:     []goagent.Message{user("a"), asst("b"), user("c"), asst("d")},
			wantLen:   2,
			wantFirst: "c",
		},
		{
			name: "tool call invariant — skips orphaned tool result",
			n:    3,
			// history: [user, assistant+toolcall, tool_result, assistant, user]
			// window of 3 = [tool_result, assistant, user] — tool_result is orphaned
			// adjustStart skips it → [assistant, user] → 2 messages
			input: []goagent.Message{
				user("q1"), asstTool(), tool("42"), asst("answer"), user("q2"),
			},
			wantLen:   2,
			wantFirst: "answer",
		},
		{
			name: "tool call invariant — intact pair kept",
			n:    4,
			// window of 4 = [asstTool, tool_result, asst, user] — pair is intact
			input: []goagent.Message{
				user("q1"), asstTool(), tool("42"), asst("answer"), user("q2"),
			},
			wantLen: 4,
		},
		{
			name: "all messages are tool results — returns empty",
			n:    3,
			input: []goagent.Message{
				tool("r1"), tool("r2"), tool("r3"),
			},
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := policy.NewFixedWindow(tt.n)
			got, err := p.Apply(context.Background(), tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tt.wantLen {
				t.Errorf("len(got) = %d, want %d; msgs = %v", len(got), tt.wantLen, got)
			}
			if tt.wantFirst != "" && len(got) > 0 && got[0].TextContent() != tt.wantFirst {
				t.Errorf("got[0].TextContent() = %q, want %q", got[0].TextContent(), tt.wantFirst)
			}
		})
	}
}

func TestFixedWindow_Apply_DefensiveCopy(t *testing.T) {
	t.Parallel()
	p := policy.NewFixedWindow(5)
	input := []goagent.Message{goagent.UserMessage("original")}
	got, _ := p.Apply(context.Background(), input)
	got[0].Content = []goagent.ContentBlock{goagent.TextBlock("mutated")}
	if input[0].TextContent() != "original" {
		t.Error("Apply modified the input slice")
	}
}
