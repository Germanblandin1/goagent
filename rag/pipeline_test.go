package rag_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector"
	"github.com/Germanblandin1/goagent/rag"
)

// concurrentEmbedder tracks peak concurrency across concurrent Embed calls.
type concurrentEmbedder struct {
	mu     sync.Mutex
	active int
	peak   int
	delay  time.Duration
}

func (e *concurrentEmbedder) Embed(_ context.Context, blocks []goagent.ContentBlock) ([]float32, error) {
	for _, b := range blocks {
		if b.Type != goagent.ContentText {
			continue
		}
		e.mu.Lock()
		e.active++
		if e.active > e.peak {
			e.peak = e.active
		}
		e.mu.Unlock()

		time.Sleep(e.delay)

		e.mu.Lock()
		e.active--
		e.mu.Unlock()
		return []float32{0.1, 0.2}, nil
	}
	return nil, vector.ErrNoEmbeddeableContent
}

// batchRecordingEmbedder implements BatchEmbedder and records calls.
// skipIndex >= 0 causes a nil vector at that position.
type batchRecordingEmbedder struct {
	mu             sync.Mutex
	batchCallCount int
	batchInputLen  int
	embedCallCount int
	skipIndex      int
}

func (e *batchRecordingEmbedder) Embed(_ context.Context, blocks []goagent.ContentBlock) ([]float32, error) {
	for _, b := range blocks {
		if b.Type == goagent.ContentText {
			e.mu.Lock()
			e.embedCallCount++
			e.mu.Unlock()
			return []float32{0.1, 0.2}, nil
		}
	}
	return nil, vector.ErrNoEmbeddeableContent
}

func (e *batchRecordingEmbedder) BatchEmbed(_ context.Context, inputs [][]goagent.ContentBlock) ([][]float32, error) {
	e.mu.Lock()
	e.batchCallCount++
	e.batchInputLen = len(inputs)
	e.mu.Unlock()

	vecs := make([][]float32, len(inputs))
	for i := range vecs {
		if e.skipIndex >= 0 && i == e.skipIndex {
			vecs[i] = nil
			continue
		}
		vecs[i] = []float32{float32(i)*0.1 + 0.1, float32(i)*0.1 + 0.2}
	}
	return vecs, nil
}

// batchErrEmbedder implements BatchEmbedder and always fails BatchEmbed.
type batchErrEmbedder struct{ err error }

func (e *batchErrEmbedder) Embed(_ context.Context, _ []goagent.ContentBlock) ([]float32, error) {
	return []float32{0.1}, nil
}
func (e *batchErrEmbedder) BatchEmbed(_ context.Context, _ [][]goagent.ContentBlock) ([][]float32, error) {
	return nil, e.err
}

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

func (s *errStore) Search(_ context.Context, _ []float32, _ int, _ ...goagent.SearchOption) ([]goagent.ScoredMessage, error) {
	return nil, nil
}

func (s *errStore) Delete(_ context.Context, _ string) error { return nil }

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

func (s *basicStore) Search(_ context.Context, _ []float32, topK int, _ ...goagent.SearchOption) ([]goagent.ScoredMessage, error) {
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

func (s *basicStore) Delete(_ context.Context, _ string) error { return nil }

// bulkStore is a VectorStore that also implements BulkVectorStore, recording
// which path was taken so tests can assert delegation behaviour.
type bulkStore struct {
	basicStore
	bulkEntries []goagent.UpsertEntry
	upsertCalls int
}

func newBulkStore() *bulkStore {
	return &bulkStore{
		basicStore: basicStore{entries: make(map[string]struct {
			vec []float32
			msg goagent.Message
		})},
	}
}

func (s *bulkStore) Upsert(ctx context.Context, id string, vec []float32, msg goagent.Message) error {
	s.upsertCalls++
	return s.basicStore.Upsert(ctx, id, vec, msg)
}

func (s *bulkStore) BulkUpsert(_ context.Context, entries []goagent.UpsertEntry) error {
	s.bulkEntries = append(s.bulkEntries, entries...)
	return nil
}

func (s *bulkStore) BulkDelete(_ context.Context, _ []string) error { return nil }

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

// ── Parallel embed tests ──────────────────────────────────────────────────────

// TestPipeline_ParallelEmbedsChunks verifies that chunks of a single document
// are embedded concurrently when using the default (non-BatchEmbedder) path.
func TestPipeline_ParallelEmbedsChunks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	embedder := &concurrentEmbedder{delay: 10 * time.Millisecond}
	store := newBulkStore()

	// TextChunker splits the message into multiple chunks.
	longText := repeatWords("word", 30)
	p, err := rag.NewPipeline(
		vector.NewTextChunker(vector.WithMaxSize(12), vector.WithOverlap(0)),
		embedder,
		store,
	)
	if err != nil {
		t.Fatal(err)
	}

	doc := rag.Document{
		Source:  "long.md",
		Content: []goagent.ContentBlock{goagent.TextBlock(longText)},
	}
	if err := p.Index(ctx, doc); err != nil {
		t.Fatalf("Index: %v", err)
	}

	embedder.mu.Lock()
	peak := embedder.peak
	embedder.mu.Unlock()

	if peak < 2 {
		t.Errorf("expected concurrent embedding (peak > 1), got peak=%d", peak)
	}
}

// TestPipeline_ParallelEmbedsErrorPropagation verifies that an embed error in
// the parallel path is returned and the IndexObserver is notified.
func TestPipeline_ParallelEmbedsErrorPropagation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	embedErr := errors.New("embed failed")
	var observerErr error

	longText := repeatWords("word", 30)
	p, err := rag.NewPipeline(
		vector.NewTextChunker(vector.WithMaxSize(12), vector.WithOverlap(0)),
		&errEmbedder{err: embedErr},
		vector.NewInMemoryStore(),
		rag.WithIndexObserver(func(_ context.Context, _ string, _, _, _ int, _ time.Duration, err error) {
			observerErr = err
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	indexErr := p.Index(ctx, rag.Document{
		Source:  "long.md",
		Content: []goagent.ContentBlock{goagent.TextBlock(longText)},
	})
	if indexErr == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(indexErr, embedErr) {
		t.Errorf("want embedErr in chain, got: %v", indexErr)
	}
	if observerErr == nil {
		t.Error("observer was not called with error")
	}
}

// ── BatchEmbedder tests ───────────────────────────────────────────────────────

// TestPipeline_BatchEmbedderPath verifies that when the embedder implements
// BatchEmbedder, Index calls BatchEmbed once instead of Embed per chunk.
func TestPipeline_BatchEmbedderPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	embedder := &batchRecordingEmbedder{skipIndex: -1}
	store := newBulkStore()

	longText := repeatWords("word", 30)
	p, err := rag.NewPipeline(
		vector.NewTextChunker(vector.WithMaxSize(12), vector.WithOverlap(0)),
		embedder,
		store,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := p.Index(ctx, rag.Document{
		Source:  "doc.md",
		Content: []goagent.ContentBlock{goagent.TextBlock(longText)},
	}); err != nil {
		t.Fatalf("Index: %v", err)
	}

	embedder.mu.Lock()
	batchCalls := embedder.batchCallCount
	embedCalls := embedder.embedCallCount
	totalChunks := embedder.batchInputLen
	embedder.mu.Unlock()

	if batchCalls != 1 {
		t.Errorf("BatchEmbed called %d times, want 1", batchCalls)
	}
	if embedCalls != 0 {
		t.Errorf("Embed called %d times, want 0", embedCalls)
	}
	if len(store.bulkEntries) != totalChunks {
		t.Errorf("BulkUpsert got %d entries, want %d", len(store.bulkEntries), totalChunks)
	}
}

// TestPipeline_BatchEmbedderSkipsNilVectors verifies that nil vectors from
// BatchEmbed increment the skipped count and are not upserted.
func TestPipeline_BatchEmbedderSkipsNilVectors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// skipIndex=0 → first chunk gets a nil vector.
	embedder := &batchRecordingEmbedder{skipIndex: 0}
	store := newBulkStore()

	var gotSkipped int
	longText := repeatWords("word", 30)
	p, err := rag.NewPipeline(
		vector.NewTextChunker(vector.WithMaxSize(12), vector.WithOverlap(0)),
		embedder,
		store,
		rag.WithIndexObserver(func(_ context.Context, _ string, _, _, skipped int, _ time.Duration, _ error) {
			gotSkipped = skipped
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := p.Index(ctx, rag.Document{
		Source:  "doc.md",
		Content: []goagent.ContentBlock{goagent.TextBlock(longText)},
	}); err != nil {
		t.Fatalf("Index must not fail for nil vector: %v", err)
	}

	embedder.mu.Lock()
	totalChunks := embedder.batchInputLen
	embedder.mu.Unlock()

	if gotSkipped != 1 {
		t.Errorf("skipped = %d, want 1", gotSkipped)
	}
	if len(store.bulkEntries) != totalChunks-1 {
		t.Errorf("expected %d upserted entries (%d chunks − 1 skipped), got %d",
			totalChunks-1, totalChunks, len(store.bulkEntries))
	}
}

// TestPipeline_BatchEmbedderError verifies that a BatchEmbed failure is
// returned and the IndexObserver is notified.
func TestPipeline_BatchEmbedderError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	batchErr := errors.New("batch embed failed")
	var observerErr error

	p, err := rag.NewPipeline(
		vector.NewNoOpChunker(),
		&batchErrEmbedder{err: batchErr},
		vector.NewInMemoryStore(),
		rag.WithIndexObserver(func(_ context.Context, _ string, _, _, _ int, _ time.Duration, err error) {
			observerErr = err
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	indexErr := p.Index(ctx, rag.Document{
		Source:  "doc.md",
		Content: []goagent.ContentBlock{goagent.TextBlock("hello")},
	})
	if indexErr == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(indexErr, batchErr) {
		t.Errorf("want batchErr in chain, got: %v", indexErr)
	}
	if observerErr == nil {
		t.Error("observer was not called with error")
	}
}

// TestPipeline_BatchEmbedderObserverCounts verifies that embedded and skipped
// counts in the observer are correct for the BatchEmbedder path.
func TestPipeline_BatchEmbedderObserverCounts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Mix text + image in one document — image chunk will get nil vector.
	embedder := &batchRecordingEmbedder{skipIndex: 1} // chunk 1 = image chunk
	var gotEmbedded, gotSkipped int

	p, err := rag.NewPipeline(
		vector.NewNoOpChunker(),
		embedder,
		vector.NewInMemoryStore(),
		rag.WithIndexObserver(func(_ context.Context, _ string, _, embedded, skipped int, _ time.Duration, _ error) {
			gotEmbedded = embedded
			gotSkipped = skipped
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	// Two-block document: one text + one image.
	// NoOpChunker produces one chunk = one call to BatchEmbed with 1 input.
	// Use separate documents so each produces its own chunk.
	docs := []rag.Document{
		{Source: "text.md", Content: []goagent.ContentBlock{goagent.TextBlock("hello")}},
		{Source: "img.png", Content: []goagent.ContentBlock{goagent.ImageBlock([]byte("data"), "image/png")}},
	}
	// Index text doc first (not skipped), then image doc (gets nil at index 0
	// of its own batch call — but since skipIndex is global per embedder, we
	// test the counts doc-by-doc instead).
	_ = docs

	// Simpler: one doc, BatchEmbed returns nil for the single chunk → 1 skipped.
	embedder2 := &batchRecordingEmbedder{skipIndex: 0}
	var gotSkipped2 int
	p2, _ := rag.NewPipeline(
		vector.NewNoOpChunker(),
		embedder2,
		vector.NewInMemoryStore(),
		rag.WithIndexObserver(func(_ context.Context, _ string, _, _, skipped int, _ time.Duration, _ error) {
			gotSkipped2 = skipped
		}),
	)
	if err := p2.Index(ctx, rag.Document{
		Source:  "doc.md",
		Content: []goagent.ContentBlock{goagent.TextBlock("hello")},
	}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if gotSkipped2 != 1 {
		t.Errorf("skipped = %d, want 1", gotSkipped2)
	}

	// Normal doc: embedder returns vector for the chunk → 1 embedded.
	embedder3 := &batchRecordingEmbedder{skipIndex: -1}
	var gotEmbedded3 int
	p3, _ := rag.NewPipeline(
		vector.NewNoOpChunker(),
		embedder3,
		vector.NewInMemoryStore(),
		rag.WithIndexObserver(func(_ context.Context, _ string, _, embedded, _ int, _ time.Duration, _ error) {
			gotEmbedded3 = embedded
		}),
	)
	if err := p3.Index(ctx, rag.Document{
		Source:  "doc.md",
		Content: []goagent.ContentBlock{goagent.TextBlock("hello")},
	}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if gotEmbedded3 != 1 {
		t.Errorf("embedded = %d, want 1", gotEmbedded3)
	}

	_ = p
	_ = gotEmbedded
	_ = gotSkipped
}

// repeatWords returns n copies of word joined by spaces.
func repeatWords(word string, n int) string {
	words := make([]string, n)
	for i := range words {
		words[i] = word
	}
	return strings.Join(words, " ")
}

// TestPipeline_UsesBulkUpsertWhenAvailable verifies that Index delegates to
// BulkUpsert when the store implements BulkVectorStore.
func TestPipeline_UsesBulkUpsertWhenAvailable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store := newBulkStore()
	p, err := rag.NewPipeline(
		vector.NewNoOpChunker(),
		&stubEmbedder{vec: []float32{1, 0}},
		store,
	)
	if err != nil {
		t.Fatal(err)
	}

	doc := rag.Document{Source: "doc.txt", Content: []goagent.ContentBlock{goagent.TextBlock("hello")}}
	if err := p.Index(ctx, doc); err != nil {
		t.Fatalf("Index: %v", err)
	}

	if store.upsertCalls != 0 {
		t.Errorf("expected Upsert not to be called, got %d calls", store.upsertCalls)
	}
	if len(store.bulkEntries) != 1 {
		t.Errorf("expected 1 BulkUpsert entry, got %d", len(store.bulkEntries))
	}
}

// TestPipeline_BulkFallbackWhenNotSupported verifies that when the store does
// not implement BulkVectorStore, Index falls back to individual Upsert calls.
func TestPipeline_BulkFallbackWhenNotSupported(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store := newBasicStore()
	p, err := rag.NewPipeline(
		vector.NewNoOpChunker(),
		&stubEmbedder{vec: []float32{1, 0}},
		store,
	)
	if err != nil {
		t.Fatal(err)
	}

	doc := rag.Document{Source: "doc.txt", Content: []goagent.ContentBlock{goagent.TextBlock("hello")}}
	if err := p.Index(ctx, doc); err != nil {
		t.Fatalf("Index: %v", err)
	}

	store.mu.Lock()
	count := len(store.entries)
	store.mu.Unlock()
	if count != 1 {
		t.Errorf("expected 1 entry via individual Upsert, got %d", count)
	}
}

