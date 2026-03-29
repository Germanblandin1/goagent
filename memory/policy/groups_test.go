package policy

import (
	"testing"

	"github.com/Germanblandin1/goagent"
)

func TestBuildGroups(t *testing.T) {
	user := func(text string) goagent.Message {
		return goagent.UserMessage(text)
	}
	asst := func(text string) goagent.Message {
		return goagent.AssistantMessage(text)
	}
	asstTool := func(toolNames ...string) goagent.Message {
		calls := make([]goagent.ToolCall, len(toolNames))
		for i, n := range toolNames {
			calls[i] = goagent.ToolCall{ID: n, Name: n}
		}
		return goagent.Message{Role: goagent.RoleAssistant, ToolCalls: calls}
	}
	tool := func(id string) goagent.Message {
		return goagent.Message{
			Role:       goagent.RoleTool,
			ToolCallID: id,
			Content:    []goagent.ContentBlock{goagent.TextBlock("result")},
		}
	}

	tests := []struct {
		name       string
		msgs       []goagent.Message
		wantGroups []group
	}{
		{
			name:       "vacío",
			msgs:       nil,
			wantGroups: nil,
		},
		{
			name:       "solo mensajes simples",
			msgs:       []goagent.Message{user("hola"), asst("chau")},
			wantGroups: []group{{0, 1}, {1, 2}},
		},
		{
			name: "un grupo atómico — 1 tool",
			msgs: []goagent.Message{
				user("pregunta"),
				asstTool("get_weather"),
				tool("get_weather"),
				asst("respuesta final"),
			},
			wantGroups: []group{{0, 1}, {1, 3}, {3, 4}},
		},
		{
			name: "tools paralelas — 3 results",
			msgs: []goagent.Message{
				user("pregunta"),
				asstTool("tool_a", "tool_b", "tool_c"),
				tool("tool_a"),
				tool("tool_b"),
				tool("tool_c"),
				asst("respuesta"),
			},
			wantGroups: []group{{0, 1}, {1, 5}, {5, 6}},
		},
		{
			name: "dos turnos con tools",
			msgs: []goagent.Message{
				user("turno 1"),        // 0
				asstTool("t1"),         // 1
				tool("t1"),             // 2
				asst("resp 1"),         // 3
				user("turno 2"),        // 4
				asstTool("t2a", "t2b"), // 5
				tool("t2a"),            // 6
				tool("t2b"),            // 7
				asst("resp 2"),         // 8
			},
			wantGroups: []group{
				{0, 1}, {1, 3}, {3, 4},
				{4, 5}, {5, 8}, {8, 9},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildGroups(tt.msgs)
			if len(got) != len(tt.wantGroups) {
				t.Fatalf("len(groups) = %d, want %d\ngot:  %v\nwant: %v",
					len(got), len(tt.wantGroups), got, tt.wantGroups)
			}
			for i, g := range got {
				if g != tt.wantGroups[i] {
					t.Errorf("group[%d] = %v, want %v", i, g, tt.wantGroups[i])
				}
			}
		})
	}
}

func TestEstimateTokensMinimumOverhead(t *testing.T) {
	msg := goagent.Message{Role: goagent.RoleUser} // no content
	got := estimateTokens(msg)
	if got < 4 {
		t.Errorf("estimateTokens(empty) = %d, want >= 4 (overhead)", got)
	}
}
