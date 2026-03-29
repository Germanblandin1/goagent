package policy_test

import (
	"context"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/policy"
)

func TestFixedWindow_Apply(t *testing.T) {
	t.Parallel()

	user := func(c string) goagent.Message { return goagent.UserMessage(c) }
	asst := func(c string) goagent.Message { return goagent.AssistantMessage(c) }
	asstTool := func() goagent.Message {
		return goagent.Message{
			Role:      goagent.RoleAssistant,
			ToolCalls: []goagent.ToolCall{{ID: "c1", Name: "calc"}},
		}
	}
	tool := func(id string) goagent.Message {
		return goagent.Message{
			Role:       goagent.RoleTool,
			ToolCallID: id,
			Content:    []goagent.ContentBlock{goagent.TextBlock("result")},
		}
	}

	tests := []struct {
		name      string
		n         int
		input     []goagent.Message
		wantLen   int
		wantFirst goagent.Role
	}{
		{
			name:    "entrada vacía",
			n:       3,
			input:   nil,
			wantLen: 0,
		},
		{
			name:    "n mayor que total — retorna todo",
			n:       100,
			input:   []goagent.Message{user("a"), asst("b"), user("c")},
			wantLen: 3,
		},
		{
			name:    "n=0 — retorna nil",
			n:       0,
			input:   []goagent.Message{user("a"), asst("b")},
			wantLen: 0,
		},
		{
			name:      "corte entre mensajes simples",
			n:         1,
			input:     []goagent.Message{user("viejo"), user("nuevo")},
			wantLen:   1,
			wantFirst: goagent.RoleUser,
		},
		{
			// The cut would fall inside the atomic group if operating on
			// individual messages. With groups the group enters complete or not.
			// groups: {0,1}, {1,3}, {3,4} — n=2 → last 2: msgs[1:]
			name: "invariante — grupo atómico incluido completo",
			n:    2,
			input: []goagent.Message{
				user("ignorado"), // group 0 — doesn't enter
				asstTool(),       // group 1 ┐ atomic
				tool("c1"),       //          ┤
				asst("resp"),     // group 2  ┘
			},
			wantLen:   3,
			wantFirst: goagent.RoleAssistant,
		},
		{
			// n=1: only the last group enters → [asst("r")]
			// tool_result is never the first message in the window.
			name: "invariante — tool_result nunca es el primer mensaje",
			n:    1,
			input: []goagent.Message{
				user("p"),
				asstTool(),
				tool("c1"),
				asst("r"),
			},
			wantLen:   1,
			wantFirst: goagent.RoleAssistant,
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
				t.Fatalf("len = %d, want %d", len(got), tt.wantLen)
			}
			if tt.wantLen > 0 && tt.wantFirst != "" && got[0].Role != tt.wantFirst {
				t.Errorf("first role = %v, want %v", got[0].Role, tt.wantFirst)
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
