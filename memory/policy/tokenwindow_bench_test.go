package policy_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/policy"
)

// makeConversation builds a realistic message slice alternating user/assistant
// turns. Each message has a ~100-token text block.
func makeConversation(pairs int) []goagent.Message {
	text := strings.Repeat("word ", 100) // ~100 tokens per block (heuristic: len/4)
	msgs := make([]goagent.Message, 0, pairs*2)
	for range pairs {
		msgs = append(msgs,
			goagent.Message{
				Role:    goagent.RoleUser,
				Content: []goagent.ContentBlock{goagent.TextBlock(text)},
			},
			goagent.Message{
				Role:    goagent.RoleAssistant,
				Content: []goagent.ContentBlock{goagent.TextBlock(text)},
			},
		)
	}
	return msgs
}

// makeToolConversation builds a message slice with tool-call groups.
// Each group is: assistant(tool_call) + tool(result).
// This exercises the buildGroups invariant path inside Apply.
func makeToolConversation(groups int) []goagent.Message {
	text := strings.Repeat("word ", 50)
	msgs := make([]goagent.Message, 0, groups*2)
	for i := range groups {
		msgs = append(msgs,
			goagent.Message{
				Role: goagent.RoleAssistant,
				ToolCalls: []goagent.ToolCall{
					{ID: "tc", Name: "mytool", Arguments: map[string]any{"n": i}},
				},
			},
			goagent.Message{
				Role:    goagent.RoleTool,
				Content: []goagent.ContentBlock{goagent.TextBlock(text)},
			},
		)
	}
	return msgs
}

// BenchmarkTokenWindow_Apply_10 measures Apply on a short conversation (5 pairs
// = 10 messages). Baseline for the buildGroups + estimation overhead.
func BenchmarkTokenWindow_Apply_10(b *testing.B) {
	p := policy.NewTokenWindow(2000)
	msgs := makeConversation(5)
	ctx := context.Background()
	for b.Loop() {
		_, _ = p.Apply(ctx, msgs)
	}
}

// BenchmarkTokenWindow_Apply_100 measures Apply on a 50-pair conversation.
// Represents a moderately long session that still fits within a typical budget.
func BenchmarkTokenWindow_Apply_100(b *testing.B) {
	p := policy.NewTokenWindow(50_000) // budget large enough to include all
	msgs := makeConversation(50)
	ctx := context.Background()
	for b.Loop() {
		_, _ = p.Apply(ctx, msgs)
	}
}

// BenchmarkTokenWindow_Apply_1000 measures Apply on a 500-pair (1000 message)
// conversation — a stress test for buildGroups at O(n) complexity.
func BenchmarkTokenWindow_Apply_1000(b *testing.B) {
	p := policy.NewTokenWindow(500_000)
	msgs := makeConversation(500)
	ctx := context.Background()
	for b.Loop() {
		_, _ = p.Apply(ctx, msgs)
	}
}

// BenchmarkTokenWindow_Apply_Trimming measures the trimming path: budget is
// tight so Apply must iterate from the end and discard older groups.
// This exercises the inner budget-reduction loop fully.
func BenchmarkTokenWindow_Apply_Trimming(b *testing.B) {
	// Each pair is ~200 heuristic tokens; 1000 budget keeps ~5 pairs of the 50.
	p := policy.NewTokenWindow(1000)
	msgs := makeConversation(50)
	ctx := context.Background()
	for b.Loop() {
		_, _ = p.Apply(ctx, msgs)
	}
}

// BenchmarkTokenWindow_Apply_ToolGroups measures Apply when history contains
// tool-call groups. buildGroups must walk the RoleTool chain per group.
func BenchmarkTokenWindow_Apply_ToolGroups(b *testing.B) {
	p := policy.NewTokenWindow(50_000)
	msgs := makeToolConversation(50)
	ctx := context.Background()
	for b.Loop() {
		_, _ = p.Apply(ctx, msgs)
	}
}
