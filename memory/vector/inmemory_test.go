package vector_test

import (
	"context"
	"sync"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/internal/session"
	"github.com/Germanblandin1/goagent/memory/vector"
)

func TestInMemoryStore_UpsertIdempotent(t *testing.T) {
	s := vector.NewInMemoryStore()
	ctx := context.Background()

	vec := []float32{1, 0}
	msg := goagent.UserMessage("hello")

	if err := s.Upsert(ctx, "id1", vec, msg); err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(ctx, "id1", vec, msg); err != nil {
		t.Fatal(err)
	}

	results, err := s.Search(ctx, vec, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result after idempotent upsert, got %d", len(results))
	}
}

func TestInMemoryStore_SearchOrder(t *testing.T) {
	s := vector.NewInMemoryStore()
	ctx := context.Background()

	// Three messages at different similarities to query [1,0].
	// sim(a) = 1.0, sim(b) = 0.0, sim(c) ≈ 0.707
	query := vector.Normalize([]float32{1, 0})

	a := vector.Normalize([]float32{1, 0})     // closest
	b := vector.Normalize([]float32{0, 1})     // farthest
	c := vector.Normalize([]float32{1, 1})     // middle

	if err := s.Upsert(ctx, "b", b, goagent.UserMessage("B")); err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(ctx, "c", c, goagent.UserMessage("C")); err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(ctx, "a", a, goagent.UserMessage("A")); err != nil {
		t.Fatal(err)
	}

	results, err := s.Search(ctx, query, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].TextContent() != "A" {
		t.Errorf("results[0] = %q, want %q", results[0].TextContent(), "A")
	}
	if results[2].TextContent() != "B" {
		t.Errorf("results[2] = %q, want %q", results[2].TextContent(), "B")
	}
}

func TestInMemoryStore_TopKRespected(t *testing.T) {
	s := vector.NewInMemoryStore()
	ctx := context.Background()

	for i := range 10 {
		vec := vector.Normalize([]float32{float32(i + 1), 0})
		if err := s.Upsert(ctx, string(rune('a'+i)), vec, goagent.UserMessage("m")); err != nil {
			t.Fatal(err)
		}
	}

	results, err := s.Search(ctx, []float32{1, 0}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results with topK=3, got %d", len(results))
	}
}

func TestInMemoryStore_SessionFilter(t *testing.T) {
	s := vector.NewInMemoryStore()
	vec := []float32{1, 0}

	// Insert messages from two sessions using the sessionID:baseID:chunkIndex convention.
	if err := s.Upsert(context.Background(), "sess-A:aaa:0", vec, goagent.UserMessage("A")); err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(context.Background(), "sess-B:bbb:0", vec, goagent.UserMessage("B")); err != nil {
		t.Fatal(err)
	}

	ctx, err := session.NewContext(context.Background(), "sess-A")
	if err != nil {
		t.Fatal(err)
	}
	results, err := s.Search(ctx, vec, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for sess-A, got %d", len(results))
	}
	if results[0].TextContent() != "A" {
		t.Errorf("got %q, want %q", results[0].TextContent(), "A")
	}
}

func TestInMemoryStore_NoSessionFilter_ReturnsAll(t *testing.T) {
	s := vector.NewInMemoryStore()
	vec := []float32{1, 0}

	if err := s.Upsert(context.Background(), "sess-A:aaa:0", vec, goagent.UserMessage("A")); err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(context.Background(), "sess-B:bbb:0", vec, goagent.UserMessage("B")); err != nil {
		t.Fatal(err)
	}

	// No session in context — should return both.
	results, err := s.Search(context.Background(), vec, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results without session filter, got %d", len(results))
	}
}

func TestInMemoryStore_RaceCondition(t *testing.T) {
	s := vector.NewInMemoryStore()
	vec := []float32{1, 0}
	ctx := context.Background()

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	for i := range goroutines {
		i := i
		go func() {
			defer wg.Done()
			_ = s.Upsert(ctx, string(rune('a'+i)), vec, goagent.UserMessage("msg"))
		}()
		go func() {
			defer wg.Done()
			_, _ = s.Search(ctx, vec, 5)
		}()
	}
	wg.Wait()
}
