package orchestration_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Germanblandin1/goagent/orchestration"
)

func TestParallelGroup_AllStagesRun(t *testing.T) {
	group := orchestration.NewParallelGroup(
		orchestration.WithParallelStages(
			orchestration.Stage("a", &mockExecutor{outputKey: "a", value: "va"}),
			orchestration.Stage("b", &mockExecutor{outputKey: "b", value: "vb"}),
			orchestration.Stage("c", &mockExecutor{outputKey: "c", value: "vc"}),
		),
	)

	pipeline := orchestration.NewPipeline(
		orchestration.WithStages(orchestration.Stage("parallel", group)),
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
		orchestration.WithParallelStages(
			orchestration.Stage("a", &mockExecutor{outputKey: "a", value: "va"}),
			orchestration.Stage("b", &mockExecutor{outputKey: "b", value: "vb"}),
		),
	)

	sc, err := group.Run(context.Background(), "goal")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	trace := sc.Trace()
	if len(trace) != 2 {
		t.Fatalf("expected 2 trace entries, got %d", len(trace))
	}
	found := make(map[string]bool, 2)
	for _, e := range trace {
		found[e.StageName] = true
	}
	for _, want := range []string{"a", "b"} {
		if !found[want] {
			t.Errorf("trace missing entry for stage %q", want)
		}
	}
}

func TestParallelGroup_PartialFailure(t *testing.T) {
	errBoom := errors.New("boom")

	group := orchestration.NewParallelGroup(
		orchestration.WithParallelStages(
			orchestration.Stage("ok", &mockExecutor{outputKey: "ok", value: "v"}),
			orchestration.Stage("fail", &mockExecutor{outputKey: "fail", err: errBoom}),
		),
	)

	pipeline := orchestration.NewPipeline(
		orchestration.WithStages(orchestration.Stage("parallel", group)),
	)

	_, err := pipeline.Run(context.Background(), "goal")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, errBoom) {
		t.Errorf("expected errBoom in chain, got: %v", err)
	}
}

func TestParallelGroup_MultipleFailures_allErrorsReported(t *testing.T) {
	errA := errors.New("error-a")
	errB := errors.New("error-b")
	errC := errors.New("error-c")

	group := orchestration.NewParallelGroup(
		orchestration.WithParallelStages(
			orchestration.Stage("a", &mockExecutor{outputKey: "a", err: errA}),
			orchestration.Stage("b", &mockExecutor{outputKey: "b", err: errB}),
			orchestration.Stage("c", &mockExecutor{outputKey: "c", err: errC}),
		),
	)

	_, err := group.Run(context.Background(), "goal")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	for _, sentinel := range []error{errA, errB, errC} {
		if !errors.Is(err, sentinel) {
			t.Errorf("expected %v in error chain, got: %v", sentinel, err)
		}
	}
}

func TestParallelGroup_PanicInStage_returnsPanicError(t *testing.T) {
	group := orchestration.NewParallelGroup(
		orchestration.WithParallelStages(
			orchestration.Stage("ok", &mockExecutor{outputKey: "ok", value: "v"}),
			orchestration.Stage("panic", executorFunc(func(_ context.Context, _ *orchestration.StageContext) error {
				panic("something went wrong")
			})),
		),
	)

	pipeline := orchestration.NewPipeline(
		orchestration.WithStages(orchestration.Stage("parallel", group)),
	)

	_, err := pipeline.Run(context.Background(), "goal")

	if err == nil {
		t.Fatal("expected error from panicking stage, got nil")
	}
	var panicErr *orchestration.PanicError
	if !errors.As(err, &panicErr) {
		t.Fatalf("expected PanicError in chain, got: %T — %v", err, err)
	}
	if panicErr.StageName != "panic" {
		t.Errorf("StageName: got %q, want %q", panicErr.StageName, "panic")
	}
	if panicErr.Value != "something went wrong" {
		t.Errorf("Value: got %v, want %q", panicErr.Value, "something went wrong")
	}
}

func TestParallelGroup_PanicInStage_otherStagesComplete(t *testing.T) {
	group := orchestration.NewParallelGroup(
		orchestration.WithParallelStages(
			orchestration.Stage("ok", &mockExecutor{outputKey: "ok", value: "v"}),
			orchestration.Stage("panic", executorFunc(func(_ context.Context, _ *orchestration.StageContext) error {
				panic("boom")
			})),
		),
	)

	pipeline := orchestration.NewPipeline(
		orchestration.WithStages(orchestration.Stage("parallel", group)),
	)

	sc, _ := pipeline.Run(context.Background(), "goal")

	if v, found := sc.Output("ok"); !found || v == "" {
		t.Error("ok stage should have completed despite the panic in another stage")
	}
}

// --- Run (standalone) ---

func TestParallelGroup_Run_storesOutputsAndPreservesGoal(t *testing.T) {
	group := orchestration.NewParallelGroup(
		orchestration.WithParallelStages(
			orchestration.Stage("a", &mockExecutor{outputKey: "a", value: "va"}),
			orchestration.Stage("b", &mockExecutor{outputKey: "b", value: "vb"}),
		),
	)

	sc, err := group.Run(context.Background(), "my goal")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sc.Goal != "my goal" {
		t.Errorf("Goal: got %q, want %q", sc.Goal, "my goal")
	}
	for _, key := range []string{"a", "b"} {
		if v, ok := sc.Output(key); !ok || v == "" {
			t.Errorf("missing output for key %q", key)
		}
	}
}

// --- WithMaxConcurrency ---

func TestParallelGroup_MaxConcurrency_limitsInflight(t *testing.T) {
	const limit = 2
	const numStages = 6

	var mu sync.Mutex
	current, peak := 0, 0

	makeStage := func(key string) orchestration.StageDef {
		return orchestration.Stage(key, executorFunc(func(ctx context.Context, sc *orchestration.StageContext) error {
			mu.Lock()
			current++
			if current > peak {
				peak = current
			}
			mu.Unlock()

			time.Sleep(10 * time.Millisecond)

			mu.Lock()
			current--
			mu.Unlock()

			sc.SetOutput(key, "v")
			return nil
		}))
	}

	stages := make([]orchestration.StageDef, numStages)
	for i := range numStages {
		stages[i] = makeStage(fmt.Sprintf("s%d", i))
	}

	group := orchestration.NewParallelGroup(
		orchestration.WithParallelStages(stages...),
		orchestration.WithMaxConcurrency(limit),
	)

	_, err := group.Run(context.Background(), "goal")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if peak > limit {
		t.Errorf("peak concurrency %d exceeded limit %d", peak, limit)
	}
}

func TestParallelGroup_WithParallelTimeout_firesAfterDeadline(t *testing.T) {
	group := orchestration.NewParallelGroup(
		orchestration.WithParallelStages(
			orchestration.Stage("slow", executorFunc(func(ctx context.Context, _ *orchestration.StageContext) error {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(10 * time.Second):
					return nil
				}
			})),
		),
		orchestration.WithParallelTimeout(20*time.Millisecond),
	)

	_, err := group.Run(context.Background(), "goal")

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got: %v", err)
	}
}

func TestParallelGroup_MaxConcurrency_zeroMeansUnlimited(t *testing.T) {
	group := orchestration.NewParallelGroup(
		orchestration.WithParallelStages(
			orchestration.Stage("a", &mockExecutor{outputKey: "a", value: "va"}),
			orchestration.Stage("b", &mockExecutor{outputKey: "b", value: "vb"}),
		),
		orchestration.WithMaxConcurrency(0),
	)

	sc, err := group.Run(context.Background(), "goal")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, key := range []string{"a", "b"} {
		if v, ok := sc.Output(key); !ok || v == "" {
			t.Errorf("missing output for key %q", key)
		}
	}
}

// TestParallelGroup_RaceDetector verifies there are no race conditions when
// parallel stages write to different keys via the thread-safe StageContext API.
// Run with: go test -race ./...
func TestParallelGroup_RaceDetector(t *testing.T) {
	group := orchestration.NewParallelGroup(
		orchestration.WithParallelStages(
			orchestration.Stage("r1", &mockExecutor{outputKey: "r1", value: "v1"}),
			orchestration.Stage("r2", &mockExecutor{outputKey: "r2", value: "v2"}),
			orchestration.Stage("r3", &mockExecutor{outputKey: "r3", value: "v3"}),
			orchestration.Stage("r4", &mockExecutor{outputKey: "r4", value: "v4"}),
		),
	)

	pipeline := orchestration.NewPipeline(
		orchestration.WithStages(orchestration.Stage("parallel", group)),
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
