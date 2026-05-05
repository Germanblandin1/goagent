package orchestration_test

import (
	"context"
	"strings"
	"testing"

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
