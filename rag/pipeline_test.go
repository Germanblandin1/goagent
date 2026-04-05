package rag_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector"
	"github.com/Germanblandin1/goagent/rag"
)

// ── Test helpers ─────────────────────────────────────────────────────────────

// stubEmbedder returns a fixed vector for any input.
type stubEmbedder struct{ vec []float32 }

func (e *stubEmbedder) Embed(_ context.Context, blocks []goagent.ContentBlock) ([]float32, error) {
	for _, b := range blocks {
		if b.Type == goagent.ContentText {
			return e.vec, nil
		}
	}
	return nil, vector.ErrNoEmbeddeableContent
}

// queryEmbedder returns different vectors per text, for meaningful similarity.
type queryEmbedder struct{}

func (e *queryEmbedder) Embed(_ context.Context, blocks []goagent.ContentBlock) ([]float32, error) {
	for _, b := range blocks {
		if b.Type != goagent.ContentText {
			continue
		}
		switch b.Text {
		case "query":
			return []float32{1, 0}, nil
		case "close":
			return []float32{0.9, 0.1}, nil // high similarity to query
		case "far":
			return []float32{0, 1}, nil // low similarity to query
		default:
			return []float32{1, 0}, nil
		}
	}
	return nil, vector.ErrNoEmbeddeableContent
}

// errEmbedder always returns an error.
type errEmbedder struct{ err error }

func (e *errEmbedder) Embed(_ context.Context, _ []goagent.ContentBlock) ([]float32, error) {
	return nil, e.err
}

// errChunker always returns an error from Chunk.
type errChunker struct{ err error }

func (c *errChunker) Chunk(_ context.Context, _ vector.ChunkContent) ([]vector.ChunkResult, error) {
	return nil, c.err
}

// errStore is a minimal VectorStore whose Upsert always returns an error.
type errStore struct{ err error }

func (s *errStore) Upsert(_ context.Context, _ string, _ []float32, _ goagent.Message) error {
	return s.err
}

func (s *errStore) Search(_ context.Context, _ []float32, _ int) ([]goagent.ScoredMessage, error) {
	return nil, nil
}

// basicStore is a minimal VectorStore that returns Score 0.0 for all results.
type basicStore struct {
	mu      sync.Mutex
	entries map[string]struct {
		vec []float32
		msg goagent.Message
	}
}

func newBasicStore() *basicStore {
	return &basicStore{entries: make(map[string]struct {
		vec []float32
		msg goagent.Message
	})}
}

func (s *basicStore) Upsert(_ context.Context, id string, vec []float32, msg goagent.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[id] = struct {
		vec []float32
		msg goagent.Message
	}{vec: vec, msg: msg}
	return nil
}

func (s *basicStore) Search(_ context.Context, _ []float32, topK int) ([]goagent.ScoredMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var results []goagent.ScoredMessage
	for _, e := range s.entries {
		results = append(results, goagent.ScoredMessage{Message: e.msg, Score: 0.0})
		if len(results) >= topK {
			break
		}
	}
	return results, nil
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestPipeline_IndexAndSearch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store := vector.NewInMemoryStore()
	embedder := &queryEmbedder{}
	chunker := vector.NewNoOpChunker()

	p, err := rag.NewPipeline(chunker, embedder, store)
	if err != nil {
		t.Fatal(err)
	}

	docs := []rag.Document{
		{Source: "a.md", Content: []goagent.ContentBlock{goagent.TextBlock("close")}},
		{Source: "b.md", Content: []goagent.ContentBlock{goagent.TextBlock("far")}},
	}
	if err := p.Index(ctx, docs...); err != nil {
		t.Fatalf("Index: %v", err)
	}

	results, err := p.Search(ctx, "query", 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	// First result must be "close" (higher similarity)
	if results[0].Source != "a.md" {
		t.Errorf("results[0].Source = %q, want a.md", results[0].Source)
	}
	if results[0].Score <= results[1].Score {
		t.Errorf("expected descending order: scores %.3f, %.3f", results[0].Score, results[1].Score)
	}
}

func TestPipeline_ReIndexIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store := vector.NewInMemoryStore()
	p, err := rag.NewPipeline(vector.NewNoOpChunker(), &stubEmbedder{vec: []float32{1, 0}}, store)
	if err != nil {
		t.Fatal(err)
	}

	doc := rag.Document{Source: "doc.md", Content: []goagent.ContentBlock{goagent.TextBlock("v1")}}
	if err := p.Index(ctx, doc); err != nil {
		t.Fatalf("first Index: %v", err)
	}

	// Re-index with updated content — same source, different text.
	doc.Content = []goagent.ContentBlock{goagent.TextBlock("v2")}
	if err := p.Index(ctx, doc); err != nil {
		t.Fatalf("second Index: %v", err)
	}

	results, err := p.Search(ctx, "doc", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results after re-index, want 1", len(results))
	}
	if results[0].Message.TextContent() != "v2" {
		t.Errorf("message text = %q, want v2", results[0].Message.TextContent())
	}
}

func TestPipeline_SkipsUnembeddableChunk(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Embedder that rejects image blocks.
	e := &stubEmbedder{vec: []float32{1, 0}}

	store := vector.NewInMemoryStore()
	p, err := rag.NewPipeline(vector.NewNoOpChunker(), e, store)
	if err != nil {
		t.Fatal(err)
	}

	// Image block — embedder returns ErrNoEmbeddeableContent.
	imgDoc := rag.Document{
		Source:  "img.png",
		Content: []goagent.ContentBlock{goagent.ImageBlock([]byte("data"), "image/png")},
	}
	if err := p.Index(ctx, imgDoc); err != nil {
		t.Errorf("Index of image-only doc should not return error, got: %v", err)
	}

	// Nothing should be stored.
	results, _ := p.Search(ctx, "query", 10)
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestPipeline_ObserverCalledOnSearch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store := vector.NewInMemoryStore()
	p, err := rag.NewPipeline(
		vector.NewNoOpChunker(),
		&stubEmbedder{vec: []float32{1, 0}},
		store,
		rag.WithSearchObserver(func(_ context.Context, query string, results []rag.SearchResult, dur time.Duration, err error) {
			if err != nil {
				t.Errorf("observer: unexpected error: %v", err)
			}
			if query != "hello" {
				t.Errorf("observer: query = %q, want hello", query)
			}
			if dur < 0 {
				t.Errorf("observer: negative duration")
			}
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := p.Search(ctx, "hello", 3); err != nil {
		t.Fatalf("Search: %v", err)
	}
}

func TestPipeline_ObserverCalledOnError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	embedErr := errors.New("embed fail")
	var observerErr error

	p, err := rag.NewPipeline(
		vector.NewNoOpChunker(),
		&errEmbedder{err: embedErr},
		vector.NewInMemoryStore(),
		rag.WithSearchObserver(func(_ context.Context, _ string, _ []rag.SearchResult, _ time.Duration, err error) {
			observerErr = err
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, searchErr := p.Search(ctx, "query", 3)
	if searchErr == nil {
		t.Fatal("expected Search to return error")
	}
	if observerErr == nil {
		t.Fatal("observer was not called with error")
	}
	if !errors.Is(observerErr, embedErr) {
		t.Errorf("observer error = %v, want to wrap %v", observerErr, embedErr)
	}
}

func TestPipeline_SearchWithScoredStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store := vector.NewInMemoryStore() // implements ScoredStore
	p, err := rag.NewPipeline(
		vector.NewNoOpChunker(),
		&stubEmbedder{vec: []float32{1, 0}},
		store,
	)
	if err != nil {
		t.Fatal(err)
	}

	doc := rag.Document{Source: "x.md", Content: []goagent.ContentBlock{goagent.TextBlock("text")}}
	if err := p.Index(ctx, doc); err != nil {
		t.Fatal(err)
	}

	results, err := p.Search(ctx, "text", 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	// InMemoryStore computes cosine — score must be > 0
	if results[0].Score <= 0 {
		t.Errorf("Score = %.4f, want > 0 (ScoredStore path)", results[0].Score)
	}
}

func TestPipeline_SearchFallbackNoScores(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store := newBasicStore() // minimal store that always returns Score 0.0
	p, err := rag.NewPipeline(
		vector.NewNoOpChunker(),
		&stubEmbedder{vec: []float32{1, 0}},
		store,
	)
	if err != nil {
		t.Fatal(err)
	}

	doc := rag.Document{Source: "y.md", Content: []goagent.ContentBlock{goagent.TextBlock("text")}}
	if err := p.Index(ctx, doc); err != nil {
		t.Fatal(err)
	}

	results, err := p.Search(ctx, "text", 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	// fallback path: scores are 0.0
	if results[0].Score != 0.0 {
		t.Errorf("Score = %.4f, want 0.0 (fallback path)", results[0].Score)
	}
}

func TestPipeline_Race(t *testing.T) {
	ctx := context.Background()

	store := vector.NewInMemoryStore()
	p, err := rag.NewPipeline(
		vector.NewNoOpChunker(),
		&stubEmbedder{vec: []float32{1, 0}},
		store,
	)
	if err != nil {
		t.Fatal(err)
	}

	doc := rag.Document{Source: "race.md", Content: []goagent.ContentBlock{goagent.TextBlock("text")}}
	if err := p.Index(ctx, doc); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = p.Search(ctx, "text", 3)
		}()
	}
	wg.Wait()
}

func TestNewPipeline_NilComponents(t *testing.T) {
	t.Parallel()
	store := vector.NewInMemoryStore()
	emb := &stubEmbedder{vec: []float32{1}}
	ch := vector.NewNoOpChunker()

	if _, err := rag.NewPipeline(nil, emb, store); err == nil {
		t.Error("expected error for nil chunker")
	}
	if _, err := rag.NewPipeline(ch, nil, store); err == nil {
		t.Error("expected error for nil embedder")
	}
	if _, err := rag.NewPipeline(ch, emb, nil); err == nil {
		t.Error("expected error for nil store")
	}
}

func TestPipeline_IndexObserverCalledOnSuccess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	type observation struct {
		source   string
		chunked  int
		embedded int
		skipped  int
		dur      time.Duration
		err      error
	}
	var got observation

	p, err := rag.NewPipeline(
		vector.NewNoOpChunker(),
		&stubEmbedder{vec: []float32{1, 0}},
		vector.NewInMemoryStore(),
		rag.WithIndexObserver(func(_ context.Context, source string, chunked, embedded, skipped int, dur time.Duration, err error) {
			got = observation{source, chunked, embedded, skipped, dur, err}
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	doc := rag.Document{Source: "doc.md", Content: []goagent.ContentBlock{goagent.TextBlock("hello")}}
	if err := p.Index(ctx, doc); err != nil {
		t.Fatalf("Index: %v", err)
	}

	if got.source != "doc.md" {
		t.Errorf("source = %q, want doc.md", got.source)
	}
	if got.chunked != 1 {
		t.Errorf("chunked = %d, want 1", got.chunked)
	}
	if got.embedded != 1 {
		t.Errorf("embedded = %d, want 1", got.embedded)
	}
	if got.skipped != 0 {
		t.Errorf("skipped = %d, want 0", got.skipped)
	}
	if got.dur < 0 {
		t.Errorf("negative duration")
	}
	if got.err != nil {
		t.Errorf("err = %v, want nil", got.err)
	}
}

func TestPipeline_IndexObserverCalledPerDocument(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	calls := 0
	p, err := rag.NewPipeline(
		vector.NewNoOpChunker(),
		&stubEmbedder{vec: []float32{1, 0}},
		vector.NewInMemoryStore(),
		rag.WithIndexObserver(func(_ context.Context, _ string, _, _, _ int, _ time.Duration, _ error) {
			calls++
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	docs := []rag.Document{
		{Source: "a.md", Content: []goagent.ContentBlock{goagent.TextBlock("a")}},
		{Source: "b.md", Content: []goagent.ContentBlock{goagent.TextBlock("b")}},
		{Source: "c.md", Content: []goagent.ContentBlock{goagent.TextBlock("c")}},
	}
	if err := p.Index(ctx, docs...); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if calls != 3 {
		t.Errorf("observer called %d times, want 3", calls)
	}
}

func TestPipeline_IndexObserverSkippedCount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	var gotSkipped int
	p, err := rag.NewPipeline(
		vector.NewNoOpChunker(),
		&stubEmbedder{vec: []float32{1, 0}}, // rejects image blocks
		vector.NewInMemoryStore(),
		rag.WithIndexObserver(func(_ context.Context, _ string, _, _, skipped int, _ time.Duration, _ error) {
			gotSkipped = skipped
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	doc := rag.Document{
		Source:  "img.png",
		Content: []goagent.ContentBlock{goagent.ImageBlock([]byte("data"), "image/png")},
	}
	if err := p.Index(ctx, doc); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if gotSkipped != 1 {
		t.Errorf("skipped = %d, want 1", gotSkipped)
	}
}

func TestPipeline_IndexObserverChunkError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	chunkErr := errors.New("chunker failed")
	var observerErr error

	p, err := rag.NewPipeline(
		&errChunker{err: chunkErr},
		&stubEmbedder{vec: []float32{1, 0}},
		vector.NewInMemoryStore(),
		rag.WithIndexObserver(func(_ context.Context, _ string, _, _, _ int, _ time.Duration, err error) {
			observerErr = err
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	doc := rag.Document{Source: "doc.md", Content: []goagent.ContentBlock{goagent.TextBlock("text")}}
	indexErr := p.Index(ctx, doc)
	if indexErr == nil {
		t.Fatal("expected Index to return error")
	}
	if observerErr == nil {
		t.Fatal("observer was not called with error")
	}
	if !errors.Is(observerErr, chunkErr) {
		t.Errorf("observer error = %v, want to wrap %v", observerErr, chunkErr)
	}
}

func TestPipeline_IndexObserverEmbedError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	embedErr := errors.New("embed failed")
	var observerErr error

	p, err := rag.NewPipeline(
		vector.NewNoOpChunker(),
		&errEmbedder{err: embedErr},
		vector.NewInMemoryStore(),
		rag.WithIndexObserver(func(_ context.Context, _ string, _, _, _ int, _ time.Duration, err error) {
			observerErr = err
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	doc := rag.Document{Source: "doc.md", Content: []goagent.ContentBlock{goagent.TextBlock("text")}}
	indexErr := p.Index(ctx, doc)
	if indexErr == nil {
		t.Fatal("expected Index to return error")
	}
	if observerErr == nil {
		t.Fatal("observer was not called with error")
	}
	if !errors.Is(observerErr, embedErr) {
		t.Errorf("observer error = %v, want to wrap %v", observerErr, embedErr)
	}
}

func TestPipeline_IndexObserverUpsertError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	upsertErr := errors.New("upsert failed")
	var observerErr error

	p, err := rag.NewPipeline(
		vector.NewNoOpChunker(),
		&stubEmbedder{vec: []float32{1, 0}},
		&errStore{err: upsertErr},
		rag.WithIndexObserver(func(_ context.Context, _ string, _, _, _ int, _ time.Duration, err error) {
			observerErr = err
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	doc := rag.Document{Source: "doc.md", Content: []goagent.ContentBlock{goagent.TextBlock("text")}}
	indexErr := p.Index(ctx, doc)
	if indexErr == nil {
		t.Fatal("expected Index to return error")
	}
	if observerErr == nil {
		t.Fatal("observer was not called with error")
	}
	if !errors.Is(observerErr, upsertErr) {
		t.Errorf("observer error = %v, want to wrap %v", observerErr, upsertErr)
	}
}
