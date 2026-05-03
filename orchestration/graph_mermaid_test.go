package orchestration_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Germanblandin1/goagent/orchestration"
)

func TestGraph_Mermaid_withEdges(t *testing.T) {
	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("generate"),
		orchestration.WithNode("generate",
			func(_ context.Context, _ *orchestration.StageContext) (string, error) {
				return "review", nil
			},
			orchestration.WithToNodes("review"),
		),
		orchestration.WithNode("review",
			func(_ context.Context, _ *orchestration.StageContext) (string, error) {
				return "", nil
			},
			orchestration.WithToNodes("generate", ""),
		),
	)

	got := graph.Mermaid()

	if !strings.Contains(got, "graph TD") {
		t.Error("missing graph TD header")
	}
	if !strings.Contains(got, "generate --> review") {
		t.Errorf("missing edge generate->review, got:\n%s", got)
	}
	if !strings.Contains(got, "review --> generate") {
		t.Errorf("missing edge review->generate, got:\n%s", got)
	}
	if !strings.Contains(got, "review --> END") {
		t.Errorf("missing edge review->END, got:\n%s", got)
	}
}

func TestGraph_Mermaid_withoutEdges_showsNodes(t *testing.T) {
	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("a"),
		orchestration.WithNode("a",
			func(_ context.Context, _ *orchestration.StageContext) (string, error) {
				return "b", nil
			},
			// sin WithToNodes
		),
		orchestration.WithNode("b",
			func(_ context.Context, _ *orchestration.StageContext) (string, error) {
				return "", nil
			},
		),
	)

	got := graph.Mermaid()

	if !strings.Contains(got, "graph TD") {
		t.Error("missing graph TD header")
	}
	if !strings.Contains(got, "a") {
		t.Errorf("missing node a, got:\n%s", got)
	}
	if !strings.Contains(got, "b") {
		t.Errorf("missing node b, got:\n%s", got)
	}
}

func TestGraph_Mermaid_mixedEdgesAndIsolated(t *testing.T) {
	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("start"),
		orchestration.WithNode("start",
			func(_ context.Context, _ *orchestration.StageContext) (string, error) {
				return "end", nil
			},
			orchestration.WithToNodes("end"),
		),
		orchestration.WithNode("end",
			func(_ context.Context, _ *orchestration.StageContext) (string, error) {
				return "", nil
			},
			// sin WithToNodes — nodo aislado en el diagrama
		),
	)

	got := graph.Mermaid()

	if !strings.Contains(got, "start --> end") {
		t.Errorf("missing edge start->end, got:\n%s", got)
	}
}

func TestGraph_Mermaid_deterministicOutput(t *testing.T) {
	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("a"),
		orchestration.WithNode("a",
			func(_ context.Context, _ *orchestration.StageContext) (string, error) { return "", nil },
			orchestration.WithToNodes("b", "c"),
		),
		orchestration.WithNode("b",
			func(_ context.Context, _ *orchestration.StageContext) (string, error) { return "", nil },
			orchestration.WithToNodes(""),
		),
		orchestration.WithNode("c",
			func(_ context.Context, _ *orchestration.StageContext) (string, error) { return "", nil },
			orchestration.WithToNodes(""),
		),
	)

	first := graph.Mermaid()
	second := graph.Mermaid()

	if first != second {
		t.Errorf("Mermaid output is not deterministic:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}
