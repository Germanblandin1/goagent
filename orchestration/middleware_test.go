package orchestration_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	goagent "github.com/Germanblandin1/goagent"
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
			orchestration.WithNodeMiddleware(orchestration.RetryMiddleware(goagent.RetryPolicy{
				MaxAttempts:  3,
				InitialDelay: time.Millisecond,
			})),
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
			orchestration.WithNodeMiddleware(orchestration.RetryMiddleware(goagent.RetryPolicy{
				MaxAttempts:  3,
				InitialDelay: time.Millisecond,
			})),
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
			orchestration.WithNodeMiddleware(orchestration.RetryMiddleware(goagent.RetryPolicy{
				MaxAttempts:  5,
				InitialDelay: 5 * time.Second, // long delay — context cancel must short-circuit it
			})),
		),
	)

	start := time.Now()
	_, err := graph.Run(ctx, "goal")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if attempts != 1 {
		t.Errorf("expected exactly 1 attempt before cancellation is detected, got %d", attempts)
	}
	if elapsed > time.Second {
		t.Errorf("context cancel should short-circuit the 5s delay, elapsed=%v", elapsed)
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
			orchestration.WithNodeMiddleware(orchestration.RetryMiddleware(goagent.RetryPolicy{
				MaxAttempts:  3,
				InitialDelay: time.Millisecond,
			})),
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

// --- RetryMiddleware backoff behaviour ---

func TestRetryMiddleware_retryAfterOverridesBackoff(t *testing.T) {
	t.Parallel()

	rateLimitErr := errors.New("429 rate limited")
	attempts := 0
	var retryAfterCalled bool

	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("work"),
		orchestration.WithNode("work",
			func(_ context.Context, sc *orchestration.StageContext) (string, error) {
				attempts++
				if attempts == 1 {
					return "", rateLimitErr
				}
				sc.SetOutput("result", "ok")
				return "", nil
			},
			orchestration.WithNodeMiddleware(orchestration.RetryMiddleware(goagent.RetryPolicy{
				MaxAttempts:  2,
				InitialDelay: 5 * time.Second, // would make the test slow if not overridden
				RetryAfter: func(err error) time.Duration {
					retryAfterCalled = true
					if err.Error() == "429 rate limited" {
						return time.Millisecond
					}
					return 0
				},
			})),
		),
	)

	start := time.Now()
	sc, err := graph.Run(context.Background(), "goal")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, _ := sc.Output("result"); v != "ok" {
		t.Errorf("output = %q, want %q", v, "ok")
	}
	if !retryAfterCalled {
		t.Error("RetryAfter was never called")
	}
	if elapsed > time.Second {
		t.Errorf("RetryAfter should have used 1ms delay, elapsed=%v", elapsed)
	}
}

func TestRetryMiddleware_retryableStopsEarly(t *testing.T) {
	t.Parallel()

	nonRetryable := errors.New("400 bad request")
	attempts := 0

	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("work"),
		orchestration.WithNode("work",
			func(_ context.Context, _ *orchestration.StageContext) (string, error) {
				attempts++
				return "", nonRetryable
			},
			orchestration.WithNodeMiddleware(orchestration.RetryMiddleware(goagent.RetryPolicy{
				MaxAttempts:  5,
				InitialDelay: time.Millisecond,
				Retryable:    func(error) bool { return false },
			})),
		),
	)

	_, err := graph.Run(context.Background(), "goal")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, nonRetryable) {
		t.Errorf("error chain should contain nonRetryable, got: %v", err)
	}
	if attempts != 1 {
		t.Errorf("Retryable=false should stop after 1 attempt, got %d", attempts)
	}
}

func TestRetryMiddleware_defaultPolicyApplied(t *testing.T) {
	t.Parallel()

	// Zero-value policy should use defaults (MaxAttempts=3).
	attempts := 0
	errTransient := errors.New("transient")

	graph, _ := orchestration.NewGraph(
		orchestration.WithStart("work"),
		orchestration.WithNode("work",
			func(_ context.Context, sc *orchestration.StageContext) (string, error) {
				attempts++
				if attempts < 3 {
					return "", errTransient
				}
				sc.SetOutput("result", "done")
				return "", nil
			},
			orchestration.WithNodeMiddleware(orchestration.RetryMiddleware(goagent.RetryPolicy{
				InitialDelay: time.Millisecond, // only override delay to keep test fast
			})),
		),
	)

	sc, err := graph.Run(context.Background(), "goal")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 3 {
		t.Errorf("default MaxAttempts=3, expected 3 attempts, got %d", attempts)
	}
	if v, _ := sc.Output("result"); v != "done" {
		t.Errorf("output = %q, want %q", v, "done")
	}
}
