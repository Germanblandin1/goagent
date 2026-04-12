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
	if results[0].Message.TextContent() != "A" {
		t.Errorf("results[0] = %q, want %q", results[0].Message.TextContent(), "A")
	}
	if results[2].Message.TextContent() != "B" {
		t.Errorf("results[2] = %q, want %q", results[2].Message.TextContent(), "B")
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
	if results[0].Message.TextContent() != "A" {
		t.Errorf("got %q, want %q", results[0].Message.TextContent(), "A")
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

func TestInMemoryStore_BulkUpsert(t *testing.T) {
	ctx := context.Background()

	t.Run("inserts all entries", func(t *testing.T) {
		s := vector.NewInMemoryStore()
		entries := []goagent.UpsertEntry{
			{ID: "a", Vector: []float32{1, 0}, Message: goagent.UserMessage("A")},
			{ID: "b", Vector: []float32{0, 1}, Message: goagent.UserMessage("B")},
			{ID: "c", Vector: []float32{1, 1}, Message: goagent.UserMessage("C")},
		}
		if err := s.BulkUpsert(ctx, entries); err != nil {
			t.Fatal(err)
		}
		results, err := s.Search(ctx, []float32{1, 0}, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 3 {
			t.Errorf("expected 3 results, got %d", len(results))
		}
	})

	t.Run("idempotent on duplicate id", func(t *testing.T) {
		s := vector.NewInMemoryStore()
		first := goagent.UpsertEntry{ID: "x", Vector: []float32{1, 0}, Message: goagent.UserMessage("first")}
		if err := s.BulkUpsert(ctx, []goagent.UpsertEntry{first}); err != nil {
			t.Fatal(err)
		}
		second := goagent.UpsertEntry{ID: "x", Vector: []float32{1, 0}, Message: goagent.UserMessage("second")}
		if err := s.BulkUpsert(ctx, []goagent.UpsertEntry{second}); err != nil {
			t.Fatal(err)
		}
		results, err := s.Search(ctx, []float32{1, 0}, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0].Message.TextContent() != "second" {
			t.Errorf("expected last-write-wins, got %q", results[0].Message.TextContent())
		}
	})

	t.Run("empty slice is a no-op", func(t *testing.T) {
		s := vector.NewInMemoryStore()
		if err := s.BulkUpsert(ctx, nil); err != nil {
			t.Fatal(err)
		}
		results, err := s.Search(ctx, []float32{1, 0}, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 0 {
			t.Errorf("expected 0 results, got %d", len(results))
		}
	})

	t.Run("vector copy protects against mutation", func(t *testing.T) {
		s := vector.NewInMemoryStore()
		vec := []float32{1, 0}
		entries := []goagent.UpsertEntry{{ID: "m", Vector: vec, Message: goagent.UserMessage("M")}}
		if err := s.BulkUpsert(ctx, entries); err != nil {
			t.Fatal(err)
		}
		vec[0] = 999 // mutate after upsert
		results, err := s.Search(ctx, []float32{1, 0}, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) == 0 {
			t.Fatal("expected 1 result")
		}
		if results[0].Score < 0.99 {
			t.Errorf("expected high similarity after vector copy, got %.4f", results[0].Score)
		}
	})
}

func TestInMemoryStore_BulkDelete(t *testing.T) {
	ctx := context.Background()

	t.Run("removes existing ids", func(t *testing.T) {
		s := vector.NewInMemoryStore()
		vec := []float32{1, 0}
		for _, id := range []string{"a", "b", "c"} {
			if err := s.Upsert(ctx, id, vec, goagent.UserMessage(id)); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.BulkDelete(ctx, []string{"a", "b"}); err != nil {
			t.Fatal(err)
		}
		results, err := s.Search(ctx, vec, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 {
			t.Errorf("expected 1 result after bulk delete, got %d", len(results))
		}
		if results[0].Message.TextContent() != "c" {
			t.Errorf("expected remaining entry %q, got %q", "c", results[0].Message.TextContent())
		}
	})

	t.Run("nonexistent ids are no-ops", func(t *testing.T) {
		s := vector.NewInMemoryStore()
		if err := s.BulkDelete(ctx, []string{"ghost1", "ghost2"}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("empty slice is a no-op", func(t *testing.T) {
		s := vector.NewInMemoryStore()
		vec := []float32{1, 0}
		if err := s.Upsert(ctx, "keep", vec, goagent.UserMessage("keep")); err != nil {
			t.Fatal(err)
		}
		if err := s.BulkDelete(ctx, nil); err != nil {
			t.Fatal(err)
		}
		results, err := s.Search(ctx, vec, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 {
			t.Errorf("expected 1 result, got %d", len(results))
		}
	})
}

func TestInMemoryStore_WithFilter(t *testing.T) {
	ctx := context.Background()

	upsertMsg := func(t *testing.T, s *vector.InMemoryStore, id string, meta map[string]any) {
		t.Helper()
		msg := goagent.Message{
			Role:     goagent.RoleUser,
			Content:  []goagent.ContentBlock{goagent.TextBlock(id)},
			Metadata: meta,
		}
		if err := s.Upsert(ctx, id, []float32{1, 0}, msg); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name    string
		filter  map[string]any
		wantIDs []string
	}{
		{
			name:    "single key match",
			filter:  map[string]any{"project": "alpha"},
			wantIDs: []string{"a", "c"},
		},
		{
			name:    "multiple keys must all match",
			filter:  map[string]any{"project": "alpha", "env": "prod"},
			wantIDs: []string{"c"},
		},
		{
			name:    "no match returns empty",
			filter:  map[string]any{"project": "gamma"},
			wantIDs: []string{},
		},
		{
			name:    "nil filter returns all",
			filter:  nil,
			wantIDs: []string{"a", "b", "c"},
		},
		{
			name:    "entry without metadata excluded when filter active",
			filter:  map[string]any{"project": "alpha"},
			wantIDs: []string{"a", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := vector.NewInMemoryStore()
			// a: project=alpha, env=dev
			upsertMsg(t, s, "a", map[string]any{"project": "alpha", "env": "dev"})
			// b: project=beta  (no env)
			upsertMsg(t, s, "b", map[string]any{"project": "beta"})
			// c: project=alpha, env=prod
			upsertMsg(t, s, "c", map[string]any{"project": "alpha", "env": "prod"})

			var opts []goagent.SearchOption
			if tt.filter != nil {
				opts = append(opts, goagent.WithFilter(tt.filter))
			}

			results, err := s.Search(ctx, []float32{1, 0}, 10, opts...)
			if err != nil {
				t.Fatal(err)
			}

			got := make(map[string]bool, len(results))
			for _, r := range results {
				got[r.Message.TextContent()] = true
			}
			if len(got) != len(tt.wantIDs) {
				t.Errorf("got %d results, want %d: %v", len(got), len(tt.wantIDs), got)
				return
			}
			for _, id := range tt.wantIDs {
				if !got[id] {
					t.Errorf("expected result %q not found in %v", id, got)
				}
			}
		})
	}
}

func TestInMemoryStore_WithFilter_NoMetadata_Excluded(t *testing.T) {
	s := vector.NewInMemoryStore()
	ctx := context.Background()

	// Message with no metadata.
	if err := s.Upsert(ctx, "no-meta", []float32{1, 0}, goagent.UserMessage("no-meta")); err != nil {
		t.Fatal(err)
	}

	results, err := s.Search(ctx, []float32{1, 0}, 10, goagent.WithFilter(map[string]any{"k": "v"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for message without metadata, got %d", len(results))
	}
}

func TestInMemoryStore_WithFilter_CombinedWithScoreThreshold(t *testing.T) {
	s := vector.NewInMemoryStore()
	ctx := context.Background()

	// High similarity (score ≈ 1.0), matching filter.
	msgA := goagent.Message{
		Role:     goagent.RoleUser,
		Content:  []goagent.ContentBlock{goagent.TextBlock("A")},
		Metadata: map[string]any{"env": "prod"},
	}
	if err := s.Upsert(ctx, "a", vector.Normalize([]float32{1, 0}), msgA); err != nil {
		t.Fatal(err)
	}
	// Low similarity (score ≈ 0.0), matching filter.
	msgB := goagent.Message{
		Role:     goagent.RoleUser,
		Content:  []goagent.ContentBlock{goagent.TextBlock("B")},
		Metadata: map[string]any{"env": "prod"},
	}
	if err := s.Upsert(ctx, "b", vector.Normalize([]float32{0, 1}), msgB); err != nil {
		t.Fatal(err)
	}

	threshold := 0.5
	results, err := s.Search(ctx, []float32{1, 0}, 10,
		goagent.WithFilter(map[string]any{"env": "prod"}),
		goagent.WithScoreThreshold(threshold),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (A passes threshold, B does not), got %d", len(results))
	}
	if results[0].Message.TextContent() != "A" {
		t.Errorf("expected A, got %q", results[0].Message.TextContent())
	}
}

func TestInMemoryStore_Count(t *testing.T) {
	ctx := context.Background()

	t.Run("empty store returns zero", func(t *testing.T) {
		s := vector.NewInMemoryStore()
		n, err := s.Count(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("want 0, got %d", n)
		}
	})

	t.Run("counts all entries after upsert", func(t *testing.T) {
		s := vector.NewInMemoryStore()
		for _, id := range []string{"a", "b", "c"} {
			if err := s.Upsert(ctx, id, []float32{1, 0}, goagent.UserMessage(id)); err != nil {
				t.Fatal(err)
			}
		}
		n, err := s.Count(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if n != 3 {
			t.Errorf("want 3, got %d", n)
		}
	})

	t.Run("idempotent upsert does not increase count", func(t *testing.T) {
		s := vector.NewInMemoryStore()
		if err := s.Upsert(ctx, "x", []float32{1, 0}, goagent.UserMessage("first")); err != nil {
			t.Fatal(err)
		}
		if err := s.Upsert(ctx, "x", []float32{1, 0}, goagent.UserMessage("second")); err != nil {
			t.Fatal(err)
		}
		n, err := s.Count(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("want 1, got %d", n)
		}
	})

	t.Run("session filter counts only session entries", func(t *testing.T) {
		s := vector.NewInMemoryStore()
		if err := s.Upsert(context.Background(), "sess-A:doc:0", []float32{1, 0}, goagent.UserMessage("A")); err != nil {
			t.Fatal(err)
		}
		if err := s.Upsert(context.Background(), "sess-B:doc:0", []float32{1, 0}, goagent.UserMessage("B")); err != nil {
			t.Fatal(err)
		}

		sessCtx, err := session.NewContext(context.Background(), "sess-A")
		if err != nil {
			t.Fatal(err)
		}
		n, err := s.Count(sessCtx)
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("want 1 for sess-A, got %d", n)
		}
	})

	t.Run("WithFilter counts only matching entries", func(t *testing.T) {
		s := vector.NewInMemoryStore()
		msgs := []struct {
			id   string
			meta map[string]any
		}{
			{"a", map[string]any{"env": "prod"}},
			{"b", map[string]any{"env": "dev"}},
			{"c", map[string]any{"env": "prod"}},
		}
		for _, m := range msgs {
			msg := goagent.Message{
				Role:     goagent.RoleUser,
				Content:  []goagent.ContentBlock{goagent.TextBlock(m.id)},
				Metadata: m.meta,
			}
			if err := s.Upsert(ctx, m.id, []float32{1, 0}, msg); err != nil {
				t.Fatal(err)
			}
		}
		n, err := s.Count(ctx, goagent.WithFilter(map[string]any{"env": "prod"}))
		if err != nil {
			t.Fatal(err)
		}
		if n != 2 {
			t.Errorf("want 2, got %d", n)
		}
	})

	t.Run("session and filter combined", func(t *testing.T) {
		s := vector.NewInMemoryStore()
		entries := []struct {
			id   string
			meta map[string]any
		}{
			{"sess-A:1:0", map[string]any{"env": "prod"}},
			{"sess-A:2:0", map[string]any{"env": "dev"}},
			{"sess-B:3:0", map[string]any{"env": "prod"}},
		}
		for _, e := range entries {
			msg := goagent.Message{
				Role:     goagent.RoleUser,
				Content:  []goagent.ContentBlock{goagent.TextBlock(e.id)},
				Metadata: e.meta,
			}
			if err := s.Upsert(context.Background(), e.id, []float32{1, 0}, msg); err != nil {
				t.Fatal(err)
			}
		}
		sessCtx, err := session.NewContext(context.Background(), "sess-A")
		if err != nil {
			t.Fatal(err)
		}
		n, err := s.Count(sessCtx, goagent.WithFilter(map[string]any{"env": "prod"}))
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("want 1 (sess-A + prod), got %d", n)
		}
	})
}

func TestInMemoryStore_BulkUpsert_RaceCondition(t *testing.T) {
	s := vector.NewInMemoryStore()
	vec := []float32{1, 0}
	ctx := context.Background()

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	for i := range goroutines {
		entries := []goagent.UpsertEntry{
			{ID: string(rune('a' + i)), Vector: vec, Message: goagent.UserMessage("msg")},
		}
		go func() {
			defer wg.Done()
			_ = s.BulkUpsert(ctx, entries)
		}()
		go func() {
			defer wg.Done()
			_ = s.BulkDelete(ctx, []string{string(rune('a' + i))})
		}()
	}
	wg.Wait()
}
