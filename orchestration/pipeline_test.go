package orchestration_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Germanblandin1/goagent/orchestration"
)

// mockExecutor implements Executor for tests.
// Writes a fixed value via sc.SetOutput(outputKey, value).
type mockExecutor struct {
	outputKey string
	value     string
	err       error
}

func (m *mockExecutor) RunWithContext(_ context.Context, sc *orchestration.StageContext) error {
	if m.err != nil {
		return m.err
	}
	sc.SetOutput(m.outputKey, m.value)
	return nil
}

// executorFunc adapts a function to the Executor interface.
// Useful in tests where the execution logic is trivial and defining
// a full struct is not warranted.
type executorFunc func(context.Context, *orchestration.StageContext) error

func (f executorFunc) RunWithContext(ctx context.Context, sc *orchestration.StageContext) error {
	return f(ctx, sc)
}

func TestPipeline_PassesOutputBetweenStages(t *testing.T) {
	pipeline := orchestration.NewPipeline(
		orchestration.Stage("upper", &mockExecutor{outputKey: "upper", value: "HELLO"}),
		orchestration.Stage("exclaim", &mockExecutor{outputKey: "exclaim", value: "HELLO!"}),
	)

	sc, err := pipeline.Run(context.Background(), "hello")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, _ := sc.Output("upper"); v != "HELLO" {
		t.Errorf("upper: got %q", v)
	}
	if v, _ := sc.Output("exclaim"); v != "HELLO!" {
		t.Errorf("exclaim: got %q", v)
	}
}

func TestPipeline_GoalIsPreserved(t *testing.T) {
	pipeline := orchestration.NewPipeline(
		orchestration.Stage("s1", &mockExecutor{outputKey: "s1", value: "output1"}),
		orchestration.Stage("s2", &mockExecutor{outputKey: "s2", value: "output2"}),
	)

	sc, err := pipeline.Run(context.Background(), "objetivo original")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sc.Goal != "objetivo original" {
		t.Errorf("Goal was modified: got %q", sc.Goal)
	}
}

func TestPipeline_StopsOnError(t *testing.T) {
	errBoom := errors.New("boom")
	var executed bool

	pipeline := orchestration.NewPipeline(
		orchestration.Stage("fail", &mockExecutor{outputKey: "fail", err: errBoom}),
		orchestration.Stage("should_not_run", executorFunc(func(_ context.Context, _ *orchestration.StageContext) error {
			executed = true
			return nil
		})),
	)

	_, err := pipeline.Run(context.Background(), "goal")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, errBoom) {
		t.Errorf("expected errBoom in chain, got: %v", err)
	}
	if executed {
		t.Error("stage after failing stage should not have run")
	}
}

func TestPipeline_ErrorWrapsWithStageName(t *testing.T) {
	pipeline := orchestration.NewPipeline(
		orchestration.Stage("my_stage", &mockExecutor{
			outputKey: "x",
			err:       errors.New("inner error"),
		}),
	)

	_, err := pipeline.Run(context.Background(), "goal")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "my_stage") {
		t.Errorf("error should mention stage name, got: %v", err)
	}
}

func TestPipeline_TraceRecordsAllStages(t *testing.T) {
	pipeline := orchestration.NewPipeline(
		orchestration.Stage("s1", &mockExecutor{outputKey: "s1", value: "v1"}),
		orchestration.Stage("s2", &mockExecutor{outputKey: "s2", value: "v2"}),
		orchestration.Stage("s3", &mockExecutor{outputKey: "s3", value: "v3"}),
	)

	sc, err := pipeline.Run(context.Background(), "goal")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	trace := sc.Trace()
	if len(trace) != 3 {
		t.Fatalf("expected 3 trace entries, got %d", len(trace))
	}
	for i, name := range []string{"s1", "s2", "s3"} {
		if trace[i].StageName != name {
			t.Errorf("trace[%d]: got %q, want %q", i, trace[i].StageName, name)
		}
		if trace[i].Err != nil {
			t.Errorf("trace[%d]: unexpected error: %v", i, trace[i].Err)
		}
	}
}

func TestPipeline_TraceRecordsErrorStage(t *testing.T) {
	errBoom := errors.New("boom")
	pipeline := orchestration.NewPipeline(
		orchestration.Stage("ok", &mockExecutor{outputKey: "ok", value: "v"}),
		orchestration.Stage("fail", &mockExecutor{outputKey: "fail", err: errBoom}),
	)

	sc, _ := pipeline.Run(context.Background(), "goal")

	trace := sc.Trace()
	if len(trace) != 2 {
		t.Fatalf("expected 2 trace entries, got %d", len(trace))
	}
	if trace[1].Err == nil {
		t.Error("expected error in trace for failing stage")
	}
}

func TestPipeline_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before running

	pipeline := orchestration.NewPipeline(
		orchestration.Stage("s1", &mockExecutor{outputKey: "s1", value: "v1"}),
	)

	_, err := pipeline.Run(ctx, "goal")

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestPipeline_NestedPipeline(t *testing.T) {
	inner := orchestration.NewPipeline(
		orchestration.Stage("inner1", &mockExecutor{outputKey: "inner1", value: "vi1"}),
		orchestration.Stage("inner2", &mockExecutor{outputKey: "inner2", value: "vi2"}),
	)

	outer := orchestration.NewPipeline(
		orchestration.Stage("outer1", &mockExecutor{outputKey: "outer1", value: "vo1"}),
		orchestration.Stage("inner", inner),
		orchestration.Stage("outer2", &mockExecutor{outputKey: "outer2", value: "vo2"}),
	)

	sc, err := outer.Run(context.Background(), "goal")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, key := range []string{"outer1", "inner1", "inner2", "outer2"} {
		if v, ok := sc.Output(key); !ok || v == "" {
			t.Errorf("missing output for key %q", key)
		}
	}
}
