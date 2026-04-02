package ratelimit_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/ratelimit"
)

// passthrough is a DispatchFunc that returns immediately.
func passthrough(_ context.Context, _ string, _ map[string]any) ([]goagent.ContentBlock, error) {
	return []goagent.ContentBlock{goagent.TextBlock("ok")}, nil
}

// --- Middleware ---

func TestMiddleware_AllowsBurst(t *testing.T) {
	t.Parallel()

	mw, err := ratelimit.Middleware(100, 5)
	if err != nil {
		t.Fatal(err)
	}
	fn := mw(passthrough)

	for i := range 5 {
		if _, err := fn(context.Background(), "tool", nil); err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
	}
}

func TestMiddleware_RespectsContextCancellation(t *testing.T) {
	t.Parallel()

	mw, err := ratelimit.Middleware(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	fn := mw(passthrough)

	_, _ = fn(context.Background(), "tool", nil) // consume the token

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err = fn(ctx, "tool", nil)
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

func TestMiddleware_InvalidRPS(t *testing.T) {
	t.Parallel()

	_, err := ratelimit.Middleware(0, 1)
	if err == nil {
		t.Fatal("expected error for rps <= 0")
	}

	_, err = ratelimit.Middleware(-5, 1)
	if err == nil {
		t.Fatal("expected error for negative rps")
	}
}

func TestMiddleware_InvalidBurst(t *testing.T) {
	t.Parallel()

	_, err := ratelimit.Middleware(10, 0)
	if err == nil {
		t.Fatal("expected error for burst < 1")
	}
}

func TestMustMiddleware_Panics(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for rps <= 0")
		}
	}()
	ratelimit.MustMiddleware(0, 1)
}

func TestMustMiddleware_Returns(t *testing.T) {
	t.Parallel()

	mw := ratelimit.MustMiddleware(10, 5)
	fn := mw(passthrough)

	_, err := fn(context.Background(), "tool", nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestMiddleware_ThrottlesConcurrentCalls(t *testing.T) {
	t.Parallel()

	mw, err := ratelimit.Middleware(10, 2)
	if err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	counting := func(ctx context.Context, name string, args map[string]any) ([]goagent.ContentBlock, error) {
		calls.Add(1)
		return passthrough(ctx, name, args)
	}

	fn := mw(counting)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = fn(ctx, "tool", nil)
		}()
	}
	wg.Wait()

	if c := calls.Load(); c != 5 {
		t.Errorf("expected 5 calls, got %d", c)
	}
}

// --- PerTool ---

func TestPerTool_IndependentLimits(t *testing.T) {
	t.Parallel()

	mw, err := ratelimit.PerTool(100, 3)
	if err != nil {
		t.Fatal(err)
	}
	fn := mw(passthrough)

	// Both tools should allow their own burst independently.
	for i := range 3 {
		if _, err := fn(context.Background(), "toolA", nil); err != nil {
			t.Fatalf("toolA call %d: %v", i, err)
		}
		if _, err := fn(context.Background(), "toolB", nil); err != nil {
			t.Fatalf("toolB call %d: %v", i, err)
		}
	}
}

func TestPerTool_RespectsContextCancellation(t *testing.T) {
	t.Parallel()

	mw, err := ratelimit.PerTool(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	fn := mw(passthrough)

	_, _ = fn(context.Background(), "tool", nil) // consume the token

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err = fn(ctx, "tool", nil)
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

func TestPerTool_InvalidRPS(t *testing.T) {
	t.Parallel()

	_, err := ratelimit.PerTool(-1, 1)
	if err == nil {
		t.Fatal("expected error for rps <= 0")
	}
}

func TestPerTool_InvalidBurst(t *testing.T) {
	t.Parallel()

	_, err := ratelimit.PerTool(10, 0)
	if err == nil {
		t.Fatal("expected error for burst < 1")
	}
}

func TestMustPerTool_Panics(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for burst < 1")
		}
	}()
	ratelimit.MustPerTool(10, 0)
}

func TestMustPerTool_Returns(t *testing.T) {
	t.Parallel()

	mw := ratelimit.MustPerTool(10, 5)
	fn := mw(passthrough)

	_, err := fn(context.Background(), "tool", nil)
	if err != nil {
		t.Fatal(err)
	}
}
