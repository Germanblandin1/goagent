package goagent_test

import (
	"context"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/internal/testutil"
)

// BenchmarkAgent_Run_NoTools measures the cost of a single-turn conversation
// with no tools — the minimum viable ReAct loop iteration.
func BenchmarkAgent_Run_NoTools(b *testing.B) {
	resp := endTurnResp("42")
	for b.Loop() {
		b.StopTimer()
		agent, err := goagent.New(
			goagent.WithProvider(testutil.NewMockProvider(resp)),
		)
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		if _, err := agent.Run(context.Background(), "hello"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAgent_Run_OneToolCall measures a two-turn conversation: one tool
// call followed by a final answer. Exercises the full dispatch/result cycle.
func BenchmarkAgent_Run_OneToolCall(b *testing.B) {
	calcTool := testutil.NewMockTool("calc", "arithmetic", "42")
	for b.Loop() {
		b.StopTimer()
		agent, err := goagent.New(
			goagent.WithProvider(testutil.NewMockProvider(
				toolUseResp("c1", "calc", map[string]any{}),
				endTurnResp("answer is 42"),
			)),
			goagent.WithTool(calcTool),
		)
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		if _, err := agent.Run(context.Background(), "what is 6*7"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAgent_Run_FiveParallelTools measures fan-out dispatch: the model
// requests 5 tools in parallel, then delivers a final answer.
func BenchmarkAgent_Run_FiveParallelTools(b *testing.B) {
	tools := make([]goagent.Tool, 5)
	toolCalls := make([]goagent.ToolCall, 5)
	for i := range tools {
		name := "tool" + string(rune('0'+i))
		tools[i] = testutil.NewMockTool(name, "bench tool", "ok")
		toolCalls[i] = goagent.ToolCall{
			ID:        name + "id",
			Name:      name,
			Arguments: map[string]any{},
		}
	}

	parallelResp := goagent.CompletionResponse{
		Message: goagent.Message{
			Role:      goagent.RoleAssistant,
			ToolCalls: toolCalls,
		},
		StopReason: goagent.StopReasonToolUse,
	}

	opts := []goagent.Option{}
	for _, t := range tools {
		opts = append(opts, goagent.WithTool(t))
	}

	for b.Loop() {
		b.StopTimer()
		allOpts := append(opts,
			goagent.WithProvider(testutil.NewMockProvider(
				parallelResp,
				endTurnResp("done"),
			)),
		)
		agent, err := goagent.New(allOpts...)
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		if _, err := agent.Run(context.Background(), "run tools"); err != nil {
			b.Fatal(err)
		}
	}
}
