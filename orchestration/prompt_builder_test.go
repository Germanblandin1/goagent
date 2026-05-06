package orchestration_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Germanblandin1/goagent/orchestration"
)

func TestGoalOnly_returnsGoal(t *testing.T) {
	sc := orchestration.NewStageContext("my goal")
	got := orchestration.GoalOnly(sc)
	if got != "my goal" {
		t.Errorf("got %q, want %q", got, "my goal")
	}
}

func TestLastOutput_noTrace_returnsGoal(t *testing.T) {
	sc := orchestration.NewStageContext("my goal")
	got := orchestration.LastOutput(sc)
	if got != "my goal" {
		t.Errorf("got %q, want %q", got, "my goal")
	}
}

func TestLastOutput_withSuccessfulStage_returnsOutput(t *testing.T) {
	pipeline := orchestration.NewPipeline(
		orchestration.WithStages(
			orchestration.Stage("step", &mockExecutor{outputKey: "step", value: "stage output"}),
		),
	)
	sc, err := pipeline.Run(context.Background(), "goal")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := orchestration.LastOutput(sc)
	if got != "stage output" {
		t.Errorf("got %q, want %q", got, "stage output")
	}
}

func TestLastOutput_multipleStages_returnsLastOutput(t *testing.T) {
	pipeline := orchestration.NewPipeline(
		orchestration.WithStages(
			orchestration.Stage("s1", &mockExecutor{outputKey: "s1", value: "first"}),
			orchestration.Stage("s2", &mockExecutor{outputKey: "s2", value: "second"}),
			orchestration.Stage("s3", &mockExecutor{outputKey: "s3", value: "third"}),
		),
	)
	sc, err := pipeline.Run(context.Background(), "goal")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := orchestration.LastOutput(sc)
	if got != "third" {
		t.Errorf("got %q, want %q", got, "third")
	}
}

func TestLastOutput_skipsErroredStages_returnsLastSuccessful(t *testing.T) {
	errBoom := errors.New("boom")
	pipeline := orchestration.NewPipeline(
		orchestration.WithStages(
			orchestration.Stage("s1", &mockExecutor{outputKey: "s1", value: "first output"}),
			orchestration.Stage("s2", &mockExecutor{outputKey: "s2", err: errBoom}),
		),
	)
	sc, _ := pipeline.Run(context.Background(), "goal")

	got := orchestration.LastOutput(sc)
	if got != "first output" {
		t.Errorf("got %q, want %q", got, "first output")
	}
}

func TestLastOutput_successfulStageWithNoOutput_returnsGoal(t *testing.T) {
	pipeline := orchestration.NewPipeline(
		orchestration.WithStages(
			orchestration.Stage("nooutput", executorFunc(func(_ context.Context, _ *orchestration.StageContext) error {
				return nil // succeeds but sets no output under its stage name
			})),
		),
	)
	sc, err := pipeline.Run(context.Background(), "fallback goal")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := orchestration.LastOutput(sc)
	if got != "fallback goal" {
		t.Errorf("got %q, want %q", got, "fallback goal")
	}
}

func TestOutputOf_existingStage_returnsOutput(t *testing.T) {
	pipeline := orchestration.NewPipeline(
		orchestration.WithStages(
			orchestration.Stage("summarize", &mockExecutor{outputKey: "summarize", value: "the summary"}),
			orchestration.Stage("translate", &mockExecutor{outputKey: "translate", value: "la traducción"}),
		),
	)
	sc, err := pipeline.Run(context.Background(), "goal")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := orchestration.OutputOf("summarize")(sc)
	if got != "the summary" {
		t.Errorf("got %q, want %q", got, "the summary")
	}
}

func TestOutputOf_missingStage_returnsGoal(t *testing.T) {
	sc := orchestration.NewStageContext("my goal")

	got := orchestration.OutputOf("nonexistent")(sc)
	if got != "my goal" {
		t.Errorf("got %q, want %q", got, "my goal")
	}
}

func TestOutputOf_afterParallelGroup_deterministic(t *testing.T) {
	// Run many times to expose non-determinism that LastOutput would exhibit.
	for range 50 {
		group := orchestration.NewParallelGroup(
			orchestration.WithParallelStages(
				orchestration.Stage("summarize", &mockExecutor{outputKey: "summarize", value: "the summary"}),
				orchestration.Stage("translate", &mockExecutor{outputKey: "translate", value: "la traducción"}),
			),
		)
		sc, err := group.Run(context.Background(), "goal")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		got := orchestration.OutputOf("summarize")(sc)
		if got != "the summary" {
			t.Errorf("got %q, want %q", got, "the summary")
		}
	}
}
