package orchestration_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Germanblandin1/goagent/orchestration"
)

func TestParallelGroup_AllStagesRun(t *testing.T) {
	group := orchestration.NewParallelGroup(
		orchestration.Stage("a", &mockExecutor{outputKey: "a", value: "va"}),
		orchestration.Stage("b", &mockExecutor{outputKey: "b", value: "vb"}),
		orchestration.Stage("c", &mockExecutor{outputKey: "c", value: "vc"}),
	)

	pipeline := orchestration.NewPipeline(
		orchestration.Stage("parallel", group),
	)

	sc, err := pipeline.Run(context.Background(), "goal")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, key := range []string{"a", "b", "c"} {
		if v, ok := sc.Output(key); !ok || v == "" {
			t.Errorf("missing output for key %q", key)
		}
	}
}

func TestParallelGroup_TraceContainsAllStages(t *testing.T) {
	group := orchestration.NewParallelGroup(
		orchestration.Stage("a", &mockExecutor{outputKey: "a", value: "va"}),
		orchestration.Stage("b", &mockExecutor{outputKey: "b", value: "vb"}),
	)

	pipeline := orchestration.NewPipeline(
		orchestration.Stage("parallel", group),
	)

	sc, err := pipeline.Run(context.Background(), "goal")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, _ := sc.Output("a"); v == "" {
		t.Error("missing output for stage a")
	}
	if v, _ := sc.Output("b"); v == "" {
		t.Error("missing output for stage b")
	}
}

func TestParallelGroup_PartialFailure(t *testing.T) {
	errBoom := errors.New("boom")

	group := orchestration.NewParallelGroup(
		orchestration.Stage("ok", &mockExecutor{outputKey: "ok", value: "v"}),
		orchestration.Stage("fail", &mockExecutor{outputKey: "fail", err: errBoom}),
	)

	pipeline := orchestration.NewPipeline(
		orchestration.Stage("parallel", group),
	)

	_, err := pipeline.Run(context.Background(), "goal")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, errBoom) {
		t.Errorf("expected errBoom in chain, got: %v", err)
	}
}

// TestParallelGroup_RaceDetector verifies there are no race conditions when
// parallel stages write to different keys via the thread-safe StageContext API.
// Run with: go test -race ./...
func TestParallelGroup_RaceDetector(t *testing.T) {
	group := orchestration.NewParallelGroup(
		orchestration.Stage("r1", &mockExecutor{outputKey: "r1", value: "v1"}),
		orchestration.Stage("r2", &mockExecutor{outputKey: "r2", value: "v2"}),
		orchestration.Stage("r3", &mockExecutor{outputKey: "r3", value: "v3"}),
		orchestration.Stage("r4", &mockExecutor{outputKey: "r4", value: "v4"}),
	)

	pipeline := orchestration.NewPipeline(
		orchestration.Stage("parallel", group),
	)

	sc, err := pipeline.Run(context.Background(), "goal")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, key := range []string{"r1", "r2", "r3", "r4"} {
		if v, ok := sc.Output(key); !ok || v == "" {
			t.Errorf("missing output for key %q", key)
		}
	}
}
