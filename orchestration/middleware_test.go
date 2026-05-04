package orchestration_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Germanblandin1/goagent/orchestration"
)

// --- WithNodeMiddleware integration ---

func TestWithNodeMiddleware_wrapsNodeFunc(t *testing.T) {
	calls := 0

	countingMiddleware := func(next orchestration.NodeFunc) orchestration.NodeFunc {
		return func(ctx context.Context, sc *orchestration.StageContext) (string, error) {
			calls++
			return next(ctx, sc)
		}
	}

	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("work"),
		orchestration.WithNode("work",
			func(_ context.Context, _ *orchestration.StageContext) (string, error) {
				return "", nil
			},
			orchestration.WithNodeMiddleware(countingMiddleware),
		),
	)

	graph.Run(context.Background(), "goal")

	if calls != 1 {
		t.Errorf("expected middleware called once, got %d", calls)
	}
}

func TestWithNodeMiddleware_orderIsInverse(t *testing.T) {
	// primer middleware registrado = más interno (más cercano al NodeFunc)
	// segundo middleware registrado = más externo
	var order []string

	makeMiddleware := func(name string) orchestration.NodeMiddleware {
		return func(next orchestration.NodeFunc) orchestration.NodeFunc {
			return func(ctx context.Context, sc *orchestration.StageContext) (string, error) {
				order = append(order, name+":before")
				n, err := next(ctx, sc)
				order = append(order, name+":after")
				return n, err
			}
		}
	}

	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("work"),
		orchestration.WithNode("work",
			func(_ context.Context, _ *orchestration.StageContext) (string, error) {
				order = append(order, "fn")
				return "", nil
			},
			orchestration.WithNodeMiddleware(makeMiddleware("A")), // más interno
			orchestration.WithNodeMiddleware(makeMiddleware("B")), // más externo
		),
	)

	graph.Run(context.Background(), "goal")

	// esperado: B:before → A:before → fn → A:after → B:after
	expected := []string{"B:before", "A:before", "fn", "A:after", "B:after"}
	if len(order) != len(expected) {
		t.Fatalf("got order %v, want %v", order, expected)
	}
	for i := range expected {
		if order[i] != expected[i] {
			t.Errorf("order[%d]: got %q, want %q", i, order[i], expected[i])
		}
	}
}

// --- RetryMiddleware ---

func TestRetryMiddleware_retriesOnError(t *testing.T) {
	attempts := 0
	errFlaky := errors.New("flaky")

	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("work"),
		orchestration.WithNode("work",
			func(_ context.Context, sc *orchestration.StageContext) (string, error) {
				attempts++
				if attempts < 3 {
					return "", errFlaky
				}
				sc.SetOutput("result", "ok")
				return "", nil
			},
			orchestration.WithNodeMiddleware(orchestration.RetryMiddleware(3)),
		),
	)

	sc, err := graph.Run(context.Background(), "goal")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
	if v, _ := sc.Output("result"); v != "ok" {
		t.Errorf("expected output ok, got %q", v)
	}
}

func TestRetryMiddleware_exhaustedRetriesReturnsError(t *testing.T) {
	errPersistent := errors.New("persistent")

	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("work"),
		orchestration.WithNode("work",
			func(_ context.Context, _ *orchestration.StageContext) (string, error) {
				return "", errPersistent
			},
			orchestration.WithNodeMiddleware(orchestration.RetryMiddleware(3)),
		),
	)

	_, err := graph.Run(context.Background(), "goal")

	if err == nil {
		t.Fatal("expected error after exhausted retries, got nil")
	}
	if !errors.Is(err, errPersistent) {
		t.Errorf("expected errPersistent in chain, got: %v", err)
	}
	var maxErr *orchestration.MaxRetriesError
	if !errors.As(err, &maxErr) {
		t.Errorf("expected MaxRetriesError in chain, got: %T", err)
	}
}

func TestRetryMiddleware_respectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	attempts := 0
	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("work"),
		orchestration.WithNode("work",
			func(_ context.Context, _ *orchestration.StageContext) (string, error) {
				attempts++
				if attempts == 1 {
					cancel()
				}
				return "", errors.New("error")
			},
			orchestration.WithNodeMiddleware(orchestration.RetryMiddleware(5)),
		),
	)

	_, err := graph.Run(ctx, "goal")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if attempts > 2 {
		t.Errorf("should not retry after cancellation, got %d attempts", attempts)
	}
}

// --- TimeoutMiddleware ---

func TestTimeoutMiddleware_cancelsSlowNode(t *testing.T) {
	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("slow"),
		orchestration.WithNode("slow",
			func(ctx context.Context, _ *orchestration.StageContext) (string, error) {
				select {
				case <-time.After(10 * time.Second):
					return "", nil
				case <-ctx.Done():
					return "", ctx.Err()
				}
			},
			orchestration.WithNodeMiddleware(orchestration.TimeoutMiddleware(10*time.Millisecond)),
		),
	)

	_, err := graph.Run(context.Background(), "goal")

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got: %v", err)
	}
}

func TestTimeoutMiddleware_fastNodeSucceeds(t *testing.T) {
	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("fast"),
		orchestration.WithNode("fast",
			func(_ context.Context, sc *orchestration.StageContext) (string, error) {
				sc.SetOutput("result", "done")
				return "", nil
			},
			orchestration.WithNodeMiddleware(orchestration.TimeoutMiddleware(5*time.Second)),
		),
	)

	sc, err := graph.Run(context.Background(), "goal")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, _ := sc.Output("result"); v != "done" {
		t.Errorf("expected done, got %q", v)
	}
}

// --- RecoverMiddleware ---

func TestRecoverMiddleware_catchesPanic(t *testing.T) {
	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("panic_node"),
		orchestration.WithNode("panic_node",
			func(_ context.Context, _ *orchestration.StageContext) (string, error) {
				panic("something went wrong")
			},
			orchestration.WithNodeMiddleware(orchestration.RecoverMiddleware),
		),
	)

	_, err := graph.Run(context.Background(), "goal")

	if err == nil {
		t.Fatal("expected error from recovered panic, got nil")
	}
	if !strings.Contains(err.Error(), "panic") {
		t.Errorf("error should mention panic, got: %v", err)
	}
}

func TestRecoverMiddleware_nonPanicNodeUnaffected(t *testing.T) {
	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("safe"),
		orchestration.WithNode("safe",
			func(_ context.Context, sc *orchestration.StageContext) (string, error) {
				sc.SetOutput("result", "ok")
				return "", nil
			},
			orchestration.WithNodeMiddleware(orchestration.RecoverMiddleware),
		),
	)

	sc, err := graph.Run(context.Background(), "goal")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, _ := sc.Output("result"); v != "ok" {
		t.Errorf("expected ok, got %q", v)
	}
}

// --- Combination: retry + timeout ---

func TestNodeMiddleware_retryWithTimeout(t *testing.T) {
	// timeout aplica a cada intento individual, no al total
	attempts := 0

	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("work"),
		orchestration.WithNode("work",
			func(ctx context.Context, sc *orchestration.StageContext) (string, error) {
				attempts++
				if attempts < 3 {
					return "", errors.New("not ready yet")
				}
				sc.SetOutput("result", "success")
				return "", nil
			},
			// orden: TimeoutMiddleware más externo, RetryMiddleware más interno
			orchestration.WithNodeMiddleware(orchestration.RetryMiddleware(3)),
			orchestration.WithNodeMiddleware(orchestration.TimeoutMiddleware(5*time.Second)),
		),
	)

	sc, err := graph.Run(context.Background(), "goal")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
	if v, _ := sc.Output("result"); v != "success" {
		t.Error("expected success output")
	}
}
