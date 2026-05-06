package orchestration_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	goagent "github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/orchestration"
)

// mockAgentRunner is defined in supervisor_test.go (same package).
// executorFunc is defined in pipeline_test.go (same package).

func TestNewAgentAdapter_nilPromptBuilder_usesGoalOnly(t *testing.T) {
	agent := &mockAgentRunner{response: "result"}

	adapter := orchestration.NewAgentAdapter(agent, "out", nil)

	sc := orchestration.NewStageContext("my goal")
	err := adapter.RunWithContext(context.Background(), sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agent.lastInput != "my goal" {
		t.Errorf("got prompt %q, want %q", agent.lastInput, "my goal")
	}
}

func TestAgentBlocksAdapter_nilBlocksBuilder_returnsError(t *testing.T) {
	adapter := orchestration.NewAgentBlocksAdapter(&mockAgentRunner{}, "out", nil)

	sc := orchestration.NewStageContext("goal")
	err := adapter.RunWithContext(context.Background(), sc)

	if err == nil {
		t.Fatal("expected error for nil blocksBuilder, got nil")
	}
	if !strings.Contains(err.Error(), "out") {
		t.Errorf("error should mention outputKey, got: %v", err)
	}
}

// --- AgentStage ---

func TestAgentStage_insidePipeline_usesStageNameAsOutputKey(t *testing.T) {
	agent := &mockAgentRunner{response: "result"}

	pipeline := orchestration.NewPipeline(
		orchestration.WithStages(
			orchestration.Stage("research", orchestration.AgentStage(agent, nil)),
		),
	)

	sc, err := pipeline.Run(context.Background(), "my goal")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := sc.Output("research"); !ok || v != "result" {
		t.Errorf("output[research]: got %q ok=%v, want %q ok=true", v, ok, "result")
	}
}

func TestAgentStage_nilPromptBuilder_usesGoalOnly(t *testing.T) {
	agent := &mockAgentRunner{response: "result"}

	pipeline := orchestration.NewPipeline(
		orchestration.WithStages(
			orchestration.Stage("step", orchestration.AgentStage(agent, nil)),
		),
	)

	_, err := pipeline.Run(context.Background(), "the goal")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agent.lastInput != "the goal" {
		t.Errorf("expected GoalOnly default, got input %q", agent.lastInput)
	}
}

func TestAgentStage_outsidePipeline_returnsError(t *testing.T) {
	adapter := orchestration.AgentStage(&mockAgentRunner{response: "r"}, nil)

	sc := orchestration.NewStageContext("goal")
	err := adapter.RunWithContext(context.Background(), sc)

	if err == nil {
		t.Fatal("expected error outside Pipeline, got nil")
	}
}

// --- AgentAdapter direct contract (T-03) ---

func TestAgentAdapter_storesOutputUnderKey(t *testing.T) {
	agent := &mockAgentRunner{response: "output text"}
	adapter := orchestration.NewAgentAdapter(agent, "my_key", nil)

	sc := orchestration.NewStageContext("goal")
	if err := adapter.RunWithContext(context.Background(), sc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v, ok := sc.Output("my_key")
	if !ok || v != "output text" {
		t.Errorf("Output[my_key]: got %q ok=%v, want %q ok=true", v, ok, "output text")
	}
}

func TestAgentAdapter_propagatesAgentError(t *testing.T) {
	errBoom := errors.New("agent failed")
	adapter := orchestration.NewAgentAdapter(&mockAgentRunner{err: errBoom}, "out", nil)

	sc := orchestration.NewStageContext("goal")
	err := adapter.RunWithContext(context.Background(), sc)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, errBoom) {
		t.Errorf("expected errBoom in chain, got: %v", err)
	}
}

func TestAgentAdapter_customPromptBuilder(t *testing.T) {
	agent := &mockAgentRunner{response: "ok"}
	pb := func(sc *orchestration.StageContext) string {
		return "prefix: " + sc.Goal
	}
	adapter := orchestration.NewAgentAdapter(agent, "out", pb)

	sc := orchestration.NewStageContext("my goal")
	if err := adapter.RunWithContext(context.Background(), sc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agent.lastInput != "prefix: my goal" {
		t.Errorf("got prompt %q, want %q", agent.lastInput, "prefix: my goal")
	}
}

// --- AgentBlocksAdapter (T-04) ---

func TestAgentBlocksAdapter_storesOutputUnderKey(t *testing.T) {
	agent := &mockAgentRunner{response: "blocks output"}
	bb := func(_ *orchestration.StageContext) []goagent.ContentBlock {
		return []goagent.ContentBlock{goagent.TextBlock("input")}
	}
	adapter := orchestration.NewAgentBlocksAdapter(agent, "result_key", bb)

	sc := orchestration.NewStageContext("goal")
	if err := adapter.RunWithContext(context.Background(), sc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v, ok := sc.Output("result_key")
	if !ok || v != "blocks output" {
		t.Errorf("Output[result_key]: got %q ok=%v, want %q ok=true", v, ok, "blocks output")
	}
}

func TestAgentBlocksAdapter_propagatesAgentError(t *testing.T) {
	errBoom := errors.New("agent failed")
	bb := func(_ *orchestration.StageContext) []goagent.ContentBlock {
		return []goagent.ContentBlock{goagent.TextBlock("input")}
	}
	adapter := orchestration.NewAgentBlocksAdapter(&mockAgentRunner{err: errBoom}, "out", bb)

	sc := orchestration.NewStageContext("goal")
	err := adapter.RunWithContext(context.Background(), sc)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, errBoom) {
		t.Errorf("expected errBoom in chain, got: %v", err)
	}
}

func TestAgentBlocksAdapter_blocksBuilderReceivesStageContext(t *testing.T) {
	var capturedGoal string
	agent := &mockAgentRunner{response: "ok"}
	bb := func(sc *orchestration.StageContext) []goagent.ContentBlock {
		capturedGoal = sc.Goal
		return []goagent.ContentBlock{goagent.TextBlock(sc.Goal)}
	}
	adapter := orchestration.NewAgentBlocksAdapter(agent, "out", bb)

	sc := orchestration.NewStageContext("the test goal")
	if err := adapter.RunWithContext(context.Background(), sc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedGoal != "the test goal" {
		t.Errorf("BlocksBuilder received wrong goal: %q", capturedGoal)
	}
}

func TestAgentBlocksAdapter_nilBlocksBuilder_usableInPipelineInline(t *testing.T) {
	pipeline := orchestration.NewPipeline(
		orchestration.WithStages(orchestration.Stage("vision", orchestration.NewAgentBlocksAdapter(
			&mockAgentRunner{response: "ok"}, "vision", nil,
		))),
	)

	sc := orchestration.NewStageContext("goal")
	err := pipeline.RunWithContext(context.Background(), sc)

	if err == nil {
		t.Fatal("expected error for nil blocksBuilder, got nil")
	}
}
