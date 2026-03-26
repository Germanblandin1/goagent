package goagent

import (
	"testing"
)

func TestStripThinking(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []Message
		want []Message
	}{
		{
			name: "no thinking blocks — passthrough",
			in: []Message{
				{Role: RoleUser, Content: []ContentBlock{TextBlock("hello")}},
				{Role: RoleAssistant, Content: []ContentBlock{TextBlock("world")}},
			},
			want: []Message{
				{Role: RoleUser, Content: []ContentBlock{TextBlock("hello")}},
				{Role: RoleAssistant, Content: []ContentBlock{TextBlock("world")}},
			},
		},
		{
			name: "thinking + text — thinking removed",
			in: []Message{
				{Role: RoleAssistant, Content: []ContentBlock{
					ThinkingBlock("reason", "sig"),
					TextBlock("answer"),
				}},
			},
			want: []Message{
				{Role: RoleAssistant, Content: []ContentBlock{TextBlock("answer")}},
			},
		},
		{
			name: "thinking + text + image — thinking removed, others preserved",
			in: []Message{
				{Role: RoleAssistant, Content: []ContentBlock{
					ThinkingBlock("reason", "sig"),
					TextBlock("text"),
					ImageBlock([]byte{1, 2, 3}, "image/png"),
				}},
			},
			want: []Message{
				{Role: RoleAssistant, Content: []ContentBlock{
					TextBlock("text"),
					ImageBlock([]byte{1, 2, 3}, "image/png"),
				}},
			},
		},
		{
			name: "thinking only — content becomes nil",
			in: []Message{
				{Role: RoleAssistant, Content: []ContentBlock{
					ThinkingBlock("reason", "sig"),
				}},
			},
			want: []Message{
				{Role: RoleAssistant, Content: nil},
			},
		},
		{
			name: "empty message — no Content",
			in:   []Message{{Role: RoleUser}},
			want: []Message{{Role: RoleUser}},
		},
		{
			name: "tool call fields preserved",
			in: []Message{
				{
					Role:       RoleTool,
					ToolCallID: "tc1",
					Content:    []ContentBlock{TextBlock("result")},
				},
			},
			want: []Message{
				{
					Role:       RoleTool,
					ToolCallID: "tc1",
					Content:    []ContentBlock{TextBlock("result")},
				},
			},
		},
		{
			name: "multiple messages — only strip thinking from each",
			in: []Message{
				{Role: RoleUser, Content: []ContentBlock{TextBlock("prompt")}},
				{Role: RoleAssistant, Content: []ContentBlock{
					ThinkingBlock("r1", "s1"),
					TextBlock("step 1"),
				}},
				{Role: RoleTool, ToolCallID: "t1", Content: []ContentBlock{TextBlock("tool result")}},
				{Role: RoleAssistant, Content: []ContentBlock{
					ThinkingBlock("r2", "s2"),
					TextBlock("final"),
				}},
			},
			want: []Message{
				{Role: RoleUser, Content: []ContentBlock{TextBlock("prompt")}},
				{Role: RoleAssistant, Content: []ContentBlock{TextBlock("step 1")}},
				{Role: RoleTool, ToolCallID: "t1", Content: []ContentBlock{TextBlock("tool result")}},
				{Role: RoleAssistant, Content: []ContentBlock{TextBlock("final")}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := stripThinking(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				gm, wm := got[i], tt.want[i]
				if gm.Role != wm.Role {
					t.Errorf("[%d] Role = %q, want %q", i, gm.Role, wm.Role)
				}
				if gm.ToolCallID != wm.ToolCallID {
					t.Errorf("[%d] ToolCallID = %q, want %q", i, gm.ToolCallID, wm.ToolCallID)
				}
				if len(gm.Content) != len(wm.Content) {
					t.Errorf("[%d] len(Content) = %d, want %d", i, len(gm.Content), len(wm.Content))
					continue
				}
				for j := range gm.Content {
					gc, wc := gm.Content[j], wm.Content[j]
					if gc.Type != wc.Type {
						t.Errorf("[%d][%d] Type = %q, want %q", i, j, gc.Type, wc.Type)
					}
				}
			}
		})
	}
}

func TestStripThinking_DoesNotMutateInput(t *testing.T) {
	t.Parallel()

	original := []Message{
		{Role: RoleAssistant, Content: []ContentBlock{
			ThinkingBlock("reason", "sig"),
			TextBlock("text"),
		}},
	}
	originalLen := len(original[0].Content)

	_ = stripThinking(original)

	if len(original[0].Content) != originalLen {
		t.Error("stripThinking mutated the original slice")
	}
	if original[0].Content[0].Type != ContentThinking {
		t.Error("stripThinking removed thinking from original message")
	}
}
