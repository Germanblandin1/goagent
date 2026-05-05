package orchestration_test

import (
	"context"
	"testing"

	"github.com/Germanblandin1/goagent/orchestration"
)

func BenchmarkPipeline_threeStages(b *testing.B) {
	pipeline := orchestration.NewPipeline(
		orchestration.WithStages(
			orchestration.Stage("s1", &mockExecutor{outputKey: "s1", value: "v1"}),
			orchestration.Stage("s2", &mockExecutor{outputKey: "s2", value: "v2"}),
			orchestration.Stage("s3", &mockExecutor{outputKey: "s3", value: "v3"}),
		),
	)
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		if _, err := pipeline.Run(ctx, "goal"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGraph_threeNodes(b *testing.B) {
	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("a"),
		orchestration.WithNode("a", func(_ context.Context, _ *orchestration.StageContext) (string, error) {
			return "b", nil
		}),
		orchestration.WithNode("b", func(_ context.Context, _ *orchestration.StageContext) (string, error) {
			return "c", nil
		}),
		orchestration.WithNode("c", func(_ context.Context, _ *orchestration.StageContext) (string, error) {
			return "", nil
		}),
	)
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		if _, err := graph.Run(ctx, "goal"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParallelGroup_fourStages(b *testing.B) {
	group := orchestration.NewParallelGroup(
		orchestration.WithParallelStages(
			orchestration.Stage("a", &mockExecutor{outputKey: "a", value: "va"}),
			orchestration.Stage("b", &mockExecutor{outputKey: "b", value: "vb"}),
			orchestration.Stage("c", &mockExecutor{outputKey: "c", value: "vc"}),
			orchestration.Stage("d", &mockExecutor{outputKey: "d", value: "vd"}),
		),
	)
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		if _, err := group.Run(ctx, "goal"); err != nil {
			b.Fatal(err)
		}
	}
}
