package orchestration_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Germanblandin1/goagent/orchestration"
)

// --- NewGraph validation ---

func TestNewGraph_missingStart_returnsError(t *testing.T) {
	_, err := orchestration.NewGraph(
		orchestration.WithNode("a", func(_ context.Context, _ *orchestration.StageContext) (string, error) {
			return "", nil
		}),
		// sin WithStart
	)

	if err == nil {
		t.Fatal("expected error for missing start, got nil")
	}
}

func TestNewGraph_startNotRegistered_returnsError(t *testing.T) {
	_, err := orchestration.NewGraph(
		orchestration.WithStart("nonexistent"),
		orchestration.WithNode("a", func(_ context.Context, _ *orchestration.StageContext) (string, error) {
			return "", nil
		}),
	)

	if err == nil {
		t.Fatal("expected error for unregistered start node, got nil")
	}
}

// --- Basic execution ---

func TestGraph_SingleNode_runsAndEnds(t *testing.T) {
	ran := false

	graph, err := orchestration.NewGraph(
		orchestration.WithStart("only"),
		orchestration.WithNode("only", func(_ context.Context, sc *orchestration.StageContext) (string, error) {
			ran = true
			sc.SetOutput("result", "done")
			return "", nil // END
		}),
	)
	if err != nil {
		t.Fatalf("NewGraph: %v", err)
	}

	sc, err := graph.Run(context.Background(), "goal")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ran {
		t.Error("node did not run")
	}
	if v, _ := sc.Output("result"); v != "done" {
		t.Errorf("got %q, want %q", v, "done")
	}
}

func TestGraph_LinearFlow_executesInOrder(t *testing.T) {
	var order []string

	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("a"),
		orchestration.WithNode("a", func(_ context.Context, _ *orchestration.StageContext) (string, error) {
			order = append(order, "a")
			return "b", nil
		}),
		orchestration.WithNode("b", func(_ context.Context, _ *orchestration.StageContext) (string, error) {
			order = append(order, "b")
			return "c", nil
		}),
		orchestration.WithNode("c", func(_ context.Context, _ *orchestration.StageContext) (string, error) {
			order = append(order, "c")
			return "", nil
		}),
	)

	_, err := graph.Run(context.Background(), "goal")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 3 || order[0] != "a" || order[1] != "b" || order[2] != "c" {
		t.Errorf("wrong execution order: %v", order)
	}
}

func TestGraph_GoalIsPreserved(t *testing.T) {
	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("n"),
		orchestration.WithNode("n", func(_ context.Context, _ *orchestration.StageContext) (string, error) {
			return "", nil
		}),
	)

	sc, _ := graph.Run(context.Background(), "objetivo original")

	if sc.Goal != "objetivo original" {
		t.Errorf("Goal modified: got %q", sc.Goal)
	}
}

// --- Branching ---

func TestGraph_Branching_takesCorrectPath(t *testing.T) {
	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("decide"),
		orchestration.WithNode("decide", func(_ context.Context, sc *orchestration.StageContext) (string, error) {
			flag, _ := orchestration.GetArtifact[bool](sc, "go_left")
			if flag {
				return "left", nil
			}
			return "right", nil
		}),
		orchestration.WithNode("left", func(_ context.Context, sc *orchestration.StageContext) (string, error) {
			sc.SetOutput("path", "left")
			return "", nil
		}),
		orchestration.WithNode("right", func(_ context.Context, sc *orchestration.StageContext) (string, error) {
			sc.SetOutput("path", "right")
			return "", nil
		}),
	)

	// caso left
	sc := orchestration.NewStageContext("goal")
	sc.SetArtifact("go_left", true)
	graph.RunWithContext(context.Background(), sc) //nolint:errcheck
	if v, _ := sc.Output("path"); v != "left" {
		t.Errorf("left branch: got %q", v)
	}

	// caso right
	sc = orchestration.NewStageContext("goal")
	sc.SetArtifact("go_left", false)
	graph.RunWithContext(context.Background(), sc) //nolint:errcheck
	if v, _ := sc.Output("path"); v != "right" {
		t.Errorf("right branch: got %q", v)
	}
}

// --- Loop ---

func TestGraph_Loop_runsUntilCondition(t *testing.T) {
	iterations := 0

	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("work"),
		orchestration.WithNode("work", func(_ context.Context, sc *orchestration.StageContext) (string, error) {
			iterations++
			count, _ := orchestration.GetArtifact[int](sc, "count")
			count++
			sc.SetArtifact("count", count)
			if count >= 3 {
				return "", nil // END después de 3 iteraciones
			}
			return "work", nil // loop
		}),
	)

	_, err := graph.Run(context.Background(), "goal")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if iterations != 3 {
		t.Errorf("expected 3 iterations, got %d", iterations)
	}
}

// --- MaxCycles per node ---

func TestGraph_MaxCyclesPerNode_returnsError(t *testing.T) {
	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("loop"),
		orchestration.WithNode("loop",
			func(_ context.Context, _ *orchestration.StageContext) (string, error) {
				return "loop", nil
			},
			orchestration.WithMaxCycles(3),
		),
	)

	_, err := graph.Run(context.Background(), "goal")

	if err == nil {
		t.Fatal("expected error for max cycles, got nil")
	}
	if !strings.Contains(err.Error(), "loop") {
		t.Errorf("error should mention node name, got: %v", err)
	}
	if !strings.Contains(err.Error(), "max cycles") {
		t.Errorf("error should mention max cycles, got: %v", err)
	}
}

func TestGraph_MaxCyclesPerNode_doesNotAffectOtherNodes(t *testing.T) {
	// node "a" tiene maxCycles=2, node "b" no tiene límite
	// el flujo va a→b→a→b→a — "a" corre 3 veces, debe fallar
	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("a"),
		orchestration.WithMaxIterations(20),
		orchestration.WithNode("a",
			func(_ context.Context, _ *orchestration.StageContext) (string, error) {
				return "b", nil
			},
			orchestration.WithMaxCycles(2),
		),
		orchestration.WithNode("b",
			func(_ context.Context, _ *orchestration.StageContext) (string, error) {
				return "a", nil
			},
			// sin MaxCycles — solo "a" tiene límite
		),
	)

	_, err := graph.Run(context.Background(), "goal")

	if err == nil {
		t.Fatal("expected error when node a exceeds max cycles")
	}
	if !strings.Contains(err.Error(), "\"a\"") {
		t.Errorf("error should mention node a, got: %v", err)
	}
}

// --- MaxIterations safety net ---

func TestGraph_MaxIterations_returnsError(t *testing.T) {
	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("loop"),
		orchestration.WithMaxIterations(5),
		orchestration.WithNode("loop", func(_ context.Context, _ *orchestration.StageContext) (string, error) {
			return "loop", nil // loop infinito
		}),
	)

	_, err := graph.Run(context.Background(), "goal")

	if err == nil {
		t.Fatal("expected error for max iterations, got nil")
	}
	if !strings.Contains(err.Error(), "max iterations") {
		t.Errorf("error should mention max iterations, got: %v", err)
	}
}

// --- Error handling ---

func TestGraph_NodeError_wrapsWithNodeName(t *testing.T) {
	errBoom := errors.New("boom")

	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("fail"),
		orchestration.WithNode("fail", func(_ context.Context, _ *orchestration.StageContext) (string, error) {
			return "", errBoom
		}),
	)

	_, err := graph.Run(context.Background(), "goal")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, errBoom) {
		t.Errorf("expected errBoom in chain, got: %v", err)
	}
	if !strings.Contains(err.Error(), "fail") {
		t.Errorf("error should mention node name, got: %v", err)
	}
}

func TestGraph_UnknownNextNode_returnsError(t *testing.T) {
	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("a"),
		orchestration.WithNode("a", func(_ context.Context, _ *orchestration.StageContext) (string, error) {
			return "nonexistent", nil // typo
		}),
	)

	_, err := graph.Run(context.Background(), "goal")

	if err == nil {
		t.Fatal("expected error for unknown node, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error should mention unknown node name, got: %v", err)
	}
}

func TestGraph_CancelledContext_stopsExecution(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("a"),
		orchestration.WithNode("a", func(_ context.Context, _ *orchestration.StageContext) (string, error) {
			return "", nil
		}),
	)

	_, err := graph.Run(ctx, "goal")

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

// --- Trace ---

func TestGraph_Trace_recordsAllNodes(t *testing.T) {
	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("a"),
		orchestration.WithNode("a", func(_ context.Context, _ *orchestration.StageContext) (string, error) {
			return "b", nil
		}),
		orchestration.WithNode("b", func(_ context.Context, _ *orchestration.StageContext) (string, error) {
			return "", nil
		}),
	)

	sc, err := graph.Run(context.Background(), "goal")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	trace := sc.Trace()
	if len(trace) != 2 {
		t.Fatalf("expected 2 trace entries, got %d", len(trace))
	}
	if trace[0].StageName != "a" || trace[1].StageName != "b" {
		t.Errorf("wrong trace names: %v", trace)
	}
}

func TestGraph_Trace_recordsLoopIterations(t *testing.T) {
	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("work"),
		orchestration.WithNode("work", func(_ context.Context, sc *orchestration.StageContext) (string, error) {
			count, _ := orchestration.GetArtifact[int](sc, "count")
			count++
			sc.SetArtifact("count", count)
			if count >= 3 {
				return "", nil
			}
			return "work", nil
		}),
	)

	sc, _ := graph.Run(context.Background(), "goal")

	// el mismo nodo "work" aparece 3 veces en el trace
	trace := sc.Trace()
	if len(trace) != 3 {
		t.Fatalf("expected 3 trace entries for 3 loop iterations, got %d", len(trace))
	}
	for i, entry := range trace {
		if entry.StageName != "work" {
			t.Errorf("trace[%d]: got %q, want %q", i, entry.StageName, "work")
		}
	}
}

// --- ExecutorNode ---

func TestExecutorNode_wrapsExecutorWithFixedNext(t *testing.T) {
	executed := false
	exec := executorFunc(func(_ context.Context, sc *orchestration.StageContext) error {
		executed = true
		sc.SetOutput("exec_output", "value")
		return nil
	})

	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("step"),
		orchestration.WithNode("step", orchestration.ExecutorNode(exec, "end")),
		orchestration.WithNode("end", func(_ context.Context, _ *orchestration.StageContext) (string, error) {
			return "", nil
		}),
	)

	_, err := graph.Run(context.Background(), "goal")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !executed {
		t.Error("executor was not called")
	}
}

// --- Graph implements Executor (nested in Pipeline) ---

func TestGraph_ImplementsExecutor_nestedInPipeline(t *testing.T) {
	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("gnode"),
		orchestration.WithNode("gnode", func(_ context.Context, sc *orchestration.StageContext) (string, error) {
			sc.SetOutput("graph_output", "from_graph")
			return "", nil
		}),
	)

	pipeline := orchestration.NewPipeline(
		orchestration.Stage("before", &mockExecutor{outputKey: "before", value: "v_before"}),
		orchestration.Stage("graph", graph),
		orchestration.Stage("after", &mockExecutor{outputKey: "after", value: "v_after"}),
	)

	sc, err := pipeline.Run(context.Background(), "goal")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	outputs := sc.Outputs()
	for _, key := range []string{"before", "graph_output", "after"} {
		if outputs[key] == "" {
			t.Errorf("missing output for key %q", key)
		}
	}
}

// --- ParallelGroup inside NodeFunc ---

// TestGraph_ParallelGroupInsideNode verifica que un nodo puede construir
// y ejecutar un ParallelGroup dinámicamente sin que el Graph lo sepa.
// Correr con -race para verificar ausencia de data races.
func TestGraph_ParallelGroupInsideNode(t *testing.T) {
	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("dispatch"),
		orchestration.WithNode("dispatch", func(ctx context.Context, sc *orchestration.StageContext) (string, error) {
			group := orchestration.NewParallelGroup(
				orchestration.Stage("worker_a", &mockExecutor{outputKey: "worker_a", value: "va"}),
				orchestration.Stage("worker_b", &mockExecutor{outputKey: "worker_b", value: "vb"}),
				orchestration.Stage("worker_c", &mockExecutor{outputKey: "worker_c", value: "vc"}),
			)
			if err := group.RunWithContext(ctx, sc); err != nil {
				return "", err
			}
			return "collect", nil
		}),
		orchestration.WithNode("collect", func(_ context.Context, sc *orchestration.StageContext) (string, error) {
			for _, key := range []string{"worker_a", "worker_b", "worker_c"} {
				if _, ok := sc.Output(key); !ok {
					return "", fmt.Errorf("missing output for %q", key)
				}
			}
			sc.SetOutput("collected", "ok")
			return "", nil
		}),
	)

	sc, err := graph.Run(context.Background(), "goal")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, _ := sc.Output("collected"); v != "ok" {
		t.Errorf("collect node did not run correctly")
	}
}

// TestGraph_ConditionalParallelism verifica el patrón central:
// el nodo decide en runtime si usar paralelismo o no
// basándose en un artefacto del StageContext.
func TestGraph_ConditionalParallelism(t *testing.T) {
	buildGraph := func() *orchestration.Graph {
		g, _ := orchestration.NewGraph(
			orchestration.WithStart("analyze"),
			orchestration.WithNode("analyze", func(ctx context.Context, sc *orchestration.StageContext) (string, error) {
				useParallel, _ := orchestration.GetArtifact[bool](sc, "use_parallel")

				if useParallel {
					group := orchestration.NewParallelGroup(
						orchestration.Stage("p1", &mockExecutor{outputKey: "p1", value: "v1"}),
						orchestration.Stage("p2", &mockExecutor{outputKey: "p2", value: "v2"}),
					)
					if err := group.RunWithContext(ctx, sc); err != nil {
						return "", err
					}
					return "merge", nil
				}

				if err := (&mockExecutor{outputKey: "single", value: "vs"}).RunWithContext(ctx, sc); err != nil {
					return "", err
				}
				return "merge", nil
			}),
			orchestration.WithNode("merge", func(_ context.Context, sc *orchestration.StageContext) (string, error) {
				sc.SetOutput("result", "merged")
				return "", nil
			}),
		)
		return g
	}

	t.Run("parallel", func(t *testing.T) {
		sc := orchestration.NewStageContext("goal")
		sc.SetArtifact("use_parallel", true)
		buildGraph().RunWithContext(context.Background(), sc) //nolint:errcheck

		outputs := sc.Outputs()
		if outputs["p1"] == "" || outputs["p2"] == "" {
			t.Error("parallel workers did not produce output")
		}
		if outputs["single"] != "" {
			t.Error("sequential worker should not have run")
		}
	})

	t.Run("sequential", func(t *testing.T) {
		sc := orchestration.NewStageContext("goal")
		sc.SetArtifact("use_parallel", false)
		buildGraph().RunWithContext(context.Background(), sc) //nolint:errcheck

		outputs := sc.Outputs()
		if outputs["single"] == "" {
			t.Error("sequential worker did not produce output")
		}
		if outputs["p1"] != "" || outputs["p2"] != "" {
			t.Error("parallel workers should not have run")
		}
	})
}

// --- HumanApprovalNode ---

func TestHumanApprovalNode_approved_continuesOnApprovedPath(t *testing.T) {
	requestCh := make(chan orchestration.ApprovalRequest, 1)
	responseCh := make(chan orchestration.ApprovalResponse, 1)

	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("work"),
		orchestration.WithNode("work", func(_ context.Context, sc *orchestration.StageContext) (string, error) {
			sc.SetOutput("draft", "some output")
			return "approve", nil
		}),
		orchestration.WithNode("approve", orchestration.HumanApprovalNode(
			requestCh, responseCh, "finalize", "work",
		)),
		orchestration.WithNode("finalize", func(_ context.Context, sc *orchestration.StageContext) (string, error) {
			sc.SetOutput("final", "approved")
			return "", nil
		}),
	)

	go func() {
		<-requestCh
		responseCh <- orchestration.ApprovalResponse{Approved: true}
	}()

	sc, err := graph.Run(context.Background(), "goal")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, _ := sc.Output("final"); v != "approved" {
		t.Errorf("expected finalize to run, got output %q", v)
	}
}

func TestHumanApprovalNode_rejected_continuesOnRejectedPath(t *testing.T) {
	requestCh := make(chan orchestration.ApprovalRequest, 1)
	responseCh := make(chan orchestration.ApprovalResponse, 1)

	attempts := 0
	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("work"),
		orchestration.WithMaxIterations(10),
		orchestration.WithNode("work", func(_ context.Context, sc *orchestration.StageContext) (string, error) {
			attempts++
			sc.SetOutput("draft", "output")
			return "approve", nil
		}),
		orchestration.WithNode("approve", orchestration.HumanApprovalNode(
			requestCh, responseCh, "finalize", "work",
		)),
		orchestration.WithNode("finalize", func(_ context.Context, sc *orchestration.StageContext) (string, error) {
			sc.SetOutput("final", "done")
			return "", nil
		}),
	)

	go func() {
		<-requestCh
		responseCh <- orchestration.ApprovalResponse{Approved: false, Reason: "needs more detail"}
		<-requestCh
		responseCh <- orchestration.ApprovalResponse{Approved: true}
	}()

	sc, err := graph.Run(context.Background(), "goal")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
	if v, _ := sc.Output("final"); v != "done" {
		t.Error("finalize should have run after approval")
	}
}

func TestHumanApprovalNode_cancelledContext_returnsError(t *testing.T) {
	requestCh := make(chan orchestration.ApprovalRequest) // sin buffer — bloquea
	responseCh := make(chan orchestration.ApprovalResponse)

	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("approve"),
		orchestration.WithNode("approve", orchestration.HumanApprovalNode(
			requestCh, responseCh, "done", "approve",
		)),
		orchestration.WithNode("done", func(_ context.Context, _ *orchestration.StageContext) (string, error) {
			return "", nil
		}),
	)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		_, err := graph.Run(ctx, "goal")
		errCh <- err
	}()

	cancel()

	err := <-errCh
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}
