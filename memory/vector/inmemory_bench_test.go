package vector_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector"
)

// populatedStore returns an InMemoryStore pre-loaded with n entries of the
// given vector dimension. Entry IDs have the form "id-<i>".
// makeVec is defined in similarity_bench_test.go (same package).
func populatedStore(n, dim int) *vector.InMemoryStore {
	s := vector.NewInMemoryStore()
	ctx := context.Background()
	for i := range n {
		vec := makeVec(dim)
		_ = s.Upsert(ctx, fmt.Sprintf("id-%d", i), vec, goagent.Message{
			Role:    goagent.RoleAssistant,
			Content: []goagent.ContentBlock{goagent.TextBlock("message content")},
		})
	}
	return s
}

// BenchmarkInMemoryStore_Search_100 measures Search over 100 entries at 384
// dimensions — a small in-process store typical for development or testing.
func BenchmarkInMemoryStore_Search_100(b *testing.B) {
	s := populatedStore(100, 384)
	query := makeVec(384)
	ctx := context.Background()
	for b.Loop() {
		_, _ = s.Search(ctx, query, 5)
	}
}

// BenchmarkInMemoryStore_Search_1k measures Search over 1 000 entries.
// This is the upper-end "small production" scenario for in-memory vector search.
func BenchmarkInMemoryStore_Search_1k(b *testing.B) {
	s := populatedStore(1_000, 384)
	query := makeVec(384)
	ctx := context.Background()
	for b.Loop() {
		_, _ = s.Search(ctx, query, 5)
	}
}

// BenchmarkInMemoryStore_Search_10k measures Search over 10 000 entries — the
// documented O(n) upper bound before a dedicated vector DB is recommended.
func BenchmarkInMemoryStore_Search_10k(b *testing.B) {
	s := populatedStore(10_000, 384)
	query := makeVec(384)
	ctx := context.Background()
	for b.Loop() {
		_, _ = s.Search(ctx, query, 5)
	}
}

// BenchmarkInMemoryStore_Search_1536 measures Search with 1 536-dimensional
// vectors (OpenAI text-embedding-3-small / ada-002) at 1 000 entries.
// Compared with _1k, this isolates the cost of wider CosineSimilarity calls.
func BenchmarkInMemoryStore_Search_1536(b *testing.B) {
	s := populatedStore(1_000, 1536)
	query := makeVec(1536)
	ctx := context.Background()
	for b.Loop() {
		_, _ = s.Search(ctx, query, 5)
	}
}

// BenchmarkInMemoryStore_Search_WithFilter_1k measures the metadata-filter path.
// reflect.DeepEqual is called on every result that passes the score threshold.
func BenchmarkInMemoryStore_Search_WithFilter_1k(b *testing.B) {
	s := vector.NewInMemoryStore()
	ctx := context.Background()
	for i := range 1_000 {
		_ = s.Upsert(ctx, fmt.Sprintf("id-%d", i), makeVec(384), goagent.Message{
			Role:     goagent.RoleAssistant,
			Content:  []goagent.ContentBlock{goagent.TextBlock("hello")},
			Metadata: map[string]any{"source": "doc", "page": i % 10},
		})
	}
	query := makeVec(384)
	for b.Loop() {
		_, _ = s.Search(ctx, query, 5, goagent.WithFilter(map[string]any{"source": "doc"}))
	}
}

// BenchmarkInMemoryStore_Upsert measures a single write under a write lock.
// Compare with BulkUpsert to quantify the per-lock-acquisition overhead.
func BenchmarkInMemoryStore_Upsert(b *testing.B) {
	s := vector.NewInMemoryStore()
	ctx := context.Background()
	vec := makeVec(384)
	msg := goagent.Message{Role: goagent.RoleAssistant, Content: []goagent.ContentBlock{goagent.TextBlock("x")}}
	for b.Loop() {
		_ = s.Upsert(ctx, "fixed-id", vec, msg)
	}
}

// BenchmarkInMemoryStore_BulkUpsert_100 measures writing 100 entries in a
// single lock acquisition. The ratio vs 100×Upsert shows lock-overhead savings.
func BenchmarkInMemoryStore_BulkUpsert_100(b *testing.B) {
	s := vector.NewInMemoryStore()
	ctx := context.Background()
	entries := make([]goagent.UpsertEntry, 100)
	for i := range entries {
		entries[i] = goagent.UpsertEntry{
			ID:      fmt.Sprintf("id-%d", i),
			Vector:  makeVec(384),
			Message: goagent.Message{Role: goagent.RoleAssistant, Content: []goagent.ContentBlock{goagent.TextBlock("x")}},
		}
	}
	for b.Loop() {
		_ = s.BulkUpsert(ctx, entries)
	}
}
