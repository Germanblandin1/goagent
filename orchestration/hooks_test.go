package orchestration_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Germanblandin1/goagent/orchestration"
)

// mockExecutorCtx implements Executor and captures the ctx it receives.
// Used to verify that the enriched ctx from OnStageStart reaches the executor.
type mockExecutorCtx struct {
	receivedCtx context.Context
}

func (m *mockExecutorCtx) RunWithContext(ctx context.Context, _ *orchestration.StageContext) error {
	m.receivedCtx = ctx
	return nil
}

// --- OrchestrationHooks zero value ---

func TestOrchestrationHooks_ZeroValue_doesNotPanic(t *testing.T) {
	hooks := orchestration.OrchestrationHooks{}

	pipeline := orchestration.NewPipeline(
		orchestration.WithStages(orchestration.Stage("s", &mockExecutor{outputKey: "s", value: "v"})),
		orchestration.WithPipelineHooks(hooks),
	)
	_, err := pipeline.Run(context.Background(), "goal")
	if err != nil {
		t.Fatalf("Pipeline with zero hooks: %v", err)
	}

	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("n"),
		orchestration.WithGraphHooks(hooks),
		orchestration.WithNode("n", func(_ context.Context, _ *orchestration.StageContext) (string, error) {
			return "", nil
		}),
	)
	_, err = graph.Run(context.Background(), "goal")
	if err != nil {
		t.Fatalf("Graph with zero hooks: %v", err)
	}
}

// --- Pipeline hooks ---

func TestPipeline_OnPipelineStart_enrichesCtx(t *testing.T) {
	type keyType struct{}
	enriched := false

	hooks := orchestration.OrchestrationHooks{
		OnPipelineStart: func(ctx context.Context, goal string) context.Context {
			return context.WithValue(ctx, keyType{}, "pipeline_value")
		},
		OnStageStart: func(ctx context.Context, name string) context.Context {
			if ctx.Value(keyType{}) == "pipeline_value" {
				enriched = true
			}
			return ctx
		},
	}

	orchestration.NewPipeline(
		orchestration.WithStages(orchestration.Stage("s", &mockExecutor{outputKey: "s", value: "v"})),
		orchestration.WithPipelineHooks(hooks),
	).Run(context.Background(), "goal")

	if !enriched {
		t.Error("OnStageStart did not receive enriched ctx from OnPipelineStart")
	}
}

func TestPipeline_OnStageStart_ctxPassedToExecutor(t *testing.T) {
	type keyType struct{}
	exec := &mockExecutorCtx{}

	hooks := orchestration.OrchestrationHooks{
		OnStageStart: func(ctx context.Context, name string) context.Context {
			return context.WithValue(ctx, keyType{}, "stage_value")
		},
	}

	orchestration.NewPipeline(
		orchestration.WithStages(orchestration.Stage("s", exec)),
		orchestration.WithPipelineHooks(hooks),
	).Run(context.Background(), "goal")

	if exec.receivedCtx == nil {
		t.Fatal("executor did not run")
	}
	if exec.receivedCtx.Value(keyType{}) != "stage_value" {
		t.Error("executor did not receive enriched ctx from OnStageStart")
	}
}

func TestPipeline_OnStageStart_calledForEachStage(t *testing.T) {
	var calledFor []string

	hooks := orchestration.OrchestrationHooks{
		OnStageStart: func(ctx context.Context, name string) context.Context {
			calledFor = append(calledFor, name)
			return ctx
		},
	}

	orchestration.NewPipeline(
		orchestration.WithStages(
			orchestration.Stage("first", &mockExecutor{outputKey: "first", value: "v1"}),
			orchestration.Stage("second", &mockExecutor{outputKey: "second", value: "v2"}),
			orchestration.Stage("third", &mockExecutor{outputKey: "third", value: "v3"}),
		),
		orchestration.WithPipelineHooks(hooks),
	).Run(context.Background(), "goal")

	if len(calledFor) != 3 {
		t.Fatalf("expected 3 OnStageStart calls, got %d", len(calledFor))
	}
	for i, name := range []string{"first", "second", "third"} {
		if calledFor[i] != name {
			t.Errorf("call %d: got %q, want %q", i, calledFor[i], name)
		}
	}
}

func TestPipeline_OnStageEnd_calledWithDurationAndError(t *testing.T) {
	errBoom := errors.New("boom")
	hookCalled := false
	var capturedDur time.Duration
	var capturedErr error

	hooks := orchestration.OrchestrationHooks{
		OnStageEnd: func(ctx context.Context, name string, dur time.Duration, err error) {
			hookCalled = true
			capturedDur = dur
			capturedErr = err
		},
	}

	orchestration.NewPipeline(
		orchestration.WithStages(orchestration.Stage("fail", &mockExecutor{outputKey: "fail", err: errBoom})),
		orchestration.WithPipelineHooks(hooks),
	).Run(context.Background(), "goal")

	if !hookCalled {
		t.Fatal("OnStageEnd was not called")
	}
	if capturedDur < 0 {
		t.Error("duration should be non-negative")
	}
	if !errors.Is(capturedErr, errBoom) {
		t.Errorf("expected errBoom, got: %v", capturedErr)
	}
}

func TestPipeline_OnPipelineEnd_alwaysCalled(t *testing.T) {
	errBoom := errors.New("boom")
	endCalled := false
	var endErr error

	hooks := orchestration.OrchestrationHooks{
		OnPipelineEnd: func(ctx context.Context, sc *orchestration.StageContext, err error) {
			endCalled = true
			endErr = err
		},
	}

	orchestration.NewPipeline(
		orchestration.WithStages(orchestration.Stage("fail", &mockExecutor{outputKey: "fail", err: errBoom})),
		orchestration.WithPipelineHooks(hooks),
	).Run(context.Background(), "goal")

	if !endCalled {
		t.Error("OnPipelineEnd was not called")
	}
	if !errors.Is(endErr, errBoom) {
		t.Errorf("OnPipelineEnd: expected errBoom, got: %v", endErr)
	}
}

// --- Graph hooks ---

func TestGraph_OnNodeEnter_calledForEachNode(t *testing.T) {
	var entered []string

	hooks := orchestration.OrchestrationHooks{
		OnNodeEnter: func(ctx context.Context, name string) context.Context {
			entered = append(entered, name)
			return ctx
		},
	}

	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("a"),
		orchestration.WithGraphHooks(hooks),
		orchestration.WithNode("a", func(_ context.Context, _ *orchestration.StageContext) (string, error) {
			return "b", nil
		}),
		orchestration.WithNode("b", func(_ context.Context, _ *orchestration.StageContext) (string, error) {
			return "", nil
		}),
	)

	graph.Run(context.Background(), "goal")

	if len(entered) != 2 || entered[0] != "a" || entered[1] != "b" {
		t.Errorf("expected [a b], got %v", entered)
	}
}

func TestGraph_OnNodeEnter_ctxPassedToNodeFunc(t *testing.T) {
	type keyType struct{}
	nodeReceivedEnrichedCtx := false

	hooks := orchestration.OrchestrationHooks{
		OnNodeEnter: func(ctx context.Context, name string) context.Context {
			return context.WithValue(ctx, keyType{}, "node_value")
		},
	}

	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("n"),
		orchestration.WithGraphHooks(hooks),
		orchestration.WithNode("n", func(ctx context.Context, _ *orchestration.StageContext) (string, error) {
			if ctx.Value(keyType{}) == "node_value" {
				nodeReceivedEnrichedCtx = true
			}
			return "", nil
		}),
	)

	graph.Run(context.Background(), "goal")

	if !nodeReceivedEnrichedCtx {
		t.Error("NodeFunc did not receive enriched ctx from OnNodeEnter")
	}
}

func TestGraph_OnNodeExit_calledWithNextAndDuration(t *testing.T) {
	var exits []struct {
		name string
		next string
		dur  time.Duration
	}

	hooks := orchestration.OrchestrationHooks{
		OnNodeExit: func(ctx context.Context, name string, next string, dur time.Duration, err error) {
			exits = append(exits, struct {
				name string
				next string
				dur  time.Duration
			}{name, next, dur})
		},
	}

	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("a"),
		orchestration.WithGraphHooks(hooks),
		orchestration.WithNode("a", func(_ context.Context, _ *orchestration.StageContext) (string, error) {
			return "b", nil
		}),
		orchestration.WithNode("b", func(_ context.Context, _ *orchestration.StageContext) (string, error) {
			return "", nil
		}),
	)

	graph.Run(context.Background(), "goal")

	if len(exits) != 2 {
		t.Fatalf("expected 2 OnNodeExit calls, got %d", len(exits))
	}
	if exits[0].name != "a" || exits[0].next != "b" {
		t.Errorf("exit[0]: got name=%q next=%q", exits[0].name, exits[0].next)
	}
	if exits[1].name != "b" || exits[1].next != "" {
		t.Errorf("exit[1]: got name=%q next=%q", exits[1].name, exits[1].next)
	}
	for i, e := range exits {
		if e.dur < 0 {
			t.Errorf("exit[%d]: duration should be non-negative", i)
		}
	}
}

func TestGraph_OnGraphEnd_alwaysCalled(t *testing.T) {
	endCalled := false

	hooks := orchestration.OrchestrationHooks{
		OnGraphEnd: func(ctx context.Context, sc *orchestration.StageContext, err error) {
			endCalled = true
		},
	}

	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("n"),
		orchestration.WithGraphHooks(hooks),
		orchestration.WithNode("n", func(_ context.Context, _ *orchestration.StageContext) (string, error) {
			return "", nil
		}),
	)

	graph.Run(context.Background(), "goal")

	if !endCalled {
		t.Error("OnGraphEnd was not called")
	}
}

func TestGraph_OnGraphEnd_calledOnMaxIterations(t *testing.T) {
	var endErr error

	hooks := orchestration.OrchestrationHooks{
		OnGraphEnd: func(ctx context.Context, sc *orchestration.StageContext, err error) {
			endErr = err
		},
	}

	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("loop"),
		orchestration.WithMaxIterations(3),
		orchestration.WithGraphHooks(hooks),
		orchestration.WithNode("loop", func(_ context.Context, _ *orchestration.StageContext) (string, error) {
			return "loop", nil
		}),
	)

	graph.Run(context.Background(), "goal")

	if endErr == nil {
		t.Error("OnGraphEnd should receive max iterations error")
	}
}

// --- MergeOrchestrationHooks ---

func TestMergeOrchestrationHooks_callsBothInOrder(t *testing.T) {
	var order []string

	h1 := orchestration.OrchestrationHooks{
		OnStageStart: func(ctx context.Context, name string) context.Context {
			order = append(order, "h1:"+name)
			return ctx
		},
	}
	h2 := orchestration.OrchestrationHooks{
		OnStageStart: func(ctx context.Context, name string) context.Context {
			order = append(order, "h2:"+name)
			return ctx
		},
	}

	merged := orchestration.MergeOrchestrationHooks(h1, h2)

	orchestration.NewPipeline(
		orchestration.WithStages(orchestration.Stage("work", &mockExecutor{outputKey: "work", value: "v"})),
		orchestration.WithPipelineHooks(merged),
	).Run(context.Background(), "goal")

	if len(order) != 2 || order[0] != "h1:work" || order[1] != "h2:work" {
		t.Errorf("expected [h1:work h2:work], got %v", order)
	}
}

func TestMergeOrchestrationHooks_chainsCtxBetweenHooks(t *testing.T) {
	type key1Type struct{}
	type key2Type struct{}
	bothValuesPresent := false

	h1 := orchestration.OrchestrationHooks{
		OnStageStart: func(ctx context.Context, name string) context.Context {
			return context.WithValue(ctx, key1Type{}, "v1")
		},
	}
	h2 := orchestration.OrchestrationHooks{
		OnStageStart: func(ctx context.Context, name string) context.Context {
			if ctx.Value(key1Type{}) == "v1" {
				bothValuesPresent = true
			}
			return context.WithValue(ctx, key2Type{}, "v2")
		},
	}

	merged := orchestration.MergeOrchestrationHooks(h1, h2)

	orchestration.NewPipeline(
		orchestration.WithStages(orchestration.Stage("work", &mockExecutor{outputKey: "work", value: "v"})),
		orchestration.WithPipelineHooks(merged),
	).Run(context.Background(), "goal")

	if !bothValuesPresent {
		t.Error("h2 did not receive enriched ctx from h1")
	}
}

func TestMergeOrchestrationHooks_unpopulatedFieldsRemainNil(t *testing.T) {
	// Only OnStageStart is set — all other fields must be nil in the result.
	h := orchestration.OrchestrationHooks{
		OnStageStart: func(ctx context.Context, name string) context.Context { return ctx },
	}

	merged := orchestration.MergeOrchestrationHooks(h)

	if merged.OnPipelineStart != nil {
		t.Error("OnPipelineStart should be nil")
	}
	if merged.OnPipelineEnd != nil {
		t.Error("OnPipelineEnd should be nil")
	}
	if merged.OnStageStart == nil {
		t.Error("OnStageStart should be non-nil")
	}
	if merged.OnStageEnd != nil {
		t.Error("OnStageEnd should be nil")
	}
	if merged.OnGraphStart != nil {
		t.Error("OnGraphStart should be nil")
	}
	if merged.OnGraphEnd != nil {
		t.Error("OnGraphEnd should be nil")
	}
	if merged.OnNodeEnter != nil {
		t.Error("OnNodeEnter should be nil")
	}
	if merged.OnNodeExit != nil {
		t.Error("OnNodeExit should be nil")
	}
}

func TestMergeOrchestrationHooks_emptyInputProducesAllNilFields(t *testing.T) {
	merged := orchestration.MergeOrchestrationHooks()

	if merged.OnPipelineStart != nil || merged.OnPipelineEnd != nil ||
		merged.OnStageStart != nil || merged.OnStageEnd != nil ||
		merged.OnGraphStart != nil || merged.OnGraphEnd != nil ||
		merged.OnNodeEnter != nil || merged.OnNodeExit != nil {
		t.Error("MergeOrchestrationHooks() with no args should produce all-nil fields")
	}
}
