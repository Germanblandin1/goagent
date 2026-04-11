package memory_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/internal/session"
	"github.com/Germanblandin1/goagent/memory"
	"github.com/Germanblandin1/goagent/memory/vector"
)

// recordingVectorStore tracks the IDs passed to Upsert.
type recordingVectorStore struct {
	ids []string
}

func (s *recordingVectorStore) Upsert(_ context.Context, id string, _ []float32, _ goagent.Message) error {
	s.ids = append(s.ids, id)
	return nil
}

func (s *recordingVectorStore) Search(_ context.Context, _ []float32, _ int, _ ...goagent.SearchOption) ([]goagent.ScoredMessage, error) {
	return nil, nil
}

func (s *recordingVectorStore) Delete(_ context.Context, _ string) error { return nil }

// errEmbedder always returns an error from Embed.
type errEmbedder struct{ err error }

func (e *errEmbedder) Embed(_ context.Context, _ []goagent.ContentBlock) ([]float32, error) {
	return nil, e.err
}

func newRecordingLongTerm(t *testing.T) (goagent.LongTermMemory, *recordingVectorStore) {
	t.Helper()
	store := &recordingVectorStore{}
	m, err := memory.NewLongTerm(
		memory.WithVectorStore(store),
		memory.WithEmbedder(stubEmbedder{}),
	)
	if err != nil {
		t.Fatalf("NewLongTerm: %v", err)
	}
	return m, store
}

// TestMessageIDUniqueness verifies that Store produces distinct IDs when
// messages differ in binary content (images/documents), and equal IDs when
// messages are identical.
func TestMessageIDUniqueness(t *testing.T) {
	imageA := []byte("binary-data-A")
	imageB := []byte("binary-data-B")

	tests := []struct {
		name      string
		msgA      goagent.Message
		msgB      goagent.Message
		wantEqual bool
	}{
		{
			name:      "identical text messages produce same ID",
			msgA:      goagent.UserMessage("hello"),
			msgB:      goagent.UserMessage("hello"),
			wantEqual: true,
		},
		{
			name:      "different text messages produce different IDs",
			msgA:      goagent.UserMessage("hello"),
			msgB:      goagent.UserMessage("world"),
			wantEqual: false,
		},
		{
			name:      "different roles produce different IDs",
			msgA:      goagent.UserMessage("hello"),
			msgB:      goagent.AssistantMessage("hello"),
			wantEqual: false,
		},
		{
			name: "same text different images produce different IDs",
			msgA: goagent.Message{
				Role: goagent.RoleUser,
				Content: []goagent.ContentBlock{
					goagent.TextBlock("¿Qué es esto?"),
					goagent.ImageBlock(imageA, "image/jpeg"),
				},
			},
			msgB: goagent.Message{
				Role: goagent.RoleUser,
				Content: []goagent.ContentBlock{
					goagent.TextBlock("¿Qué es esto?"),
					goagent.ImageBlock(imageB, "image/jpeg"),
				},
			},
			wantEqual: false,
		},
		{
			name: "identical image messages produce same ID",
			msgA: goagent.Message{
				Role:    goagent.RoleUser,
				Content: []goagent.ContentBlock{goagent.ImageBlock(imageA, "image/jpeg")},
			},
			msgB: goagent.Message{
				Role:    goagent.RoleUser,
				Content: []goagent.ContentBlock{goagent.ImageBlock(imageA, "image/jpeg")},
			},
			wantEqual: true,
		},
		{
			name: "same image data different media type produce different IDs",
			msgA: goagent.Message{
				Role:    goagent.RoleUser,
				Content: []goagent.ContentBlock{goagent.ImageBlock(imageA, "image/jpeg")},
			},
			msgB: goagent.Message{
				Role:    goagent.RoleUser,
				Content: []goagent.ContentBlock{goagent.ImageBlock(imageA, "image/png")},
			},
			wantEqual: false,
		},
		{
			name: "same text different document data produce different IDs",
			msgA: goagent.Message{
				Role:    goagent.RoleUser,
				Content: []goagent.ContentBlock{goagent.DocumentBlock([]byte("docA"), "application/pdf", "report")},
			},
			msgB: goagent.Message{
				Role:    goagent.RoleUser,
				Content: []goagent.ContentBlock{goagent.DocumentBlock([]byte("docB"), "application/pdf", "report")},
			},
			wantEqual: false,
		},
	}

	ctx := context.Background()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memA, storeA := newRecordingLongTerm(t)
			memB, storeB := newRecordingLongTerm(t)

			if err := memA.Store(ctx, tt.msgA); err != nil {
				t.Fatalf("Store A: %v", err)
			}
			if err := memB.Store(ctx, tt.msgB); err != nil {
				t.Fatalf("Store B: %v", err)
			}

			if len(storeA.ids) != 1 || len(storeB.ids) != 1 {
				t.Fatal("expected exactly one Upsert per Store call")
			}

			idA, idB := storeA.ids[0], storeB.ids[0]
			if tt.wantEqual && idA != idB {
				t.Errorf("expected equal IDs: A=%s B=%s", idA, idB)
			}
			if !tt.wantEqual && idA == idB {
				t.Errorf("expected different IDs, both are %s", idA)
			}
		})
	}
}

// searchableVectorStore records the topK passed to Search and returns fixed results.
type searchableVectorStore struct {
	results  []goagent.ScoredMessage
	lastTopK int
}

func (s *searchableVectorStore) Upsert(_ context.Context, _ string, _ []float32, _ goagent.Message) error {
	return nil
}

func (s *searchableVectorStore) Search(_ context.Context, _ []float32, topK int, _ ...goagent.SearchOption) ([]goagent.ScoredMessage, error) {
	s.lastTopK = topK
	return s.results, nil
}

func (s *searchableVectorStore) Delete(_ context.Context, _ string) error { return nil }

func TestLongTerm_Retrieve(t *testing.T) {
	t.Parallel()

	want := []goagent.ScoredMessage{{Message: goagent.UserMessage("past context")}}
	store := &searchableVectorStore{results: want}
	m, err := memory.NewLongTerm(
		memory.WithVectorStore(store),
		memory.WithEmbedder(stubEmbedder{}),
	)
	if err != nil {
		t.Fatalf("NewLongTerm: %v", err)
	}

	t.Run("topK=0 uses default", func(t *testing.T) {
		got, err := m.Retrieve(context.Background(), []goagent.ContentBlock{goagent.TextBlock("query")}, 0)
		if err != nil {
			t.Fatalf("Retrieve: %v", err)
		}
		if len(got) != 1 || got[0].Message.TextContent() != "past context" {
			t.Errorf("got %v, want %v", got, want)
		}
		if store.lastTopK != 3 {
			t.Errorf("topK = %d, want default 3", store.lastTopK)
		}
	})

	t.Run("explicit topK", func(t *testing.T) {
		_, err := m.Retrieve(context.Background(), []goagent.ContentBlock{goagent.TextBlock("query")}, 7)
		if err != nil {
			t.Fatalf("Retrieve: %v", err)
		}
		if store.lastTopK != 7 {
			t.Errorf("topK = %d, want 7", store.lastTopK)
		}
	})
}

func TestLongTerm_RetrieveEmbedError(t *testing.T) {
	t.Parallel()

	errEmbed := errors.New("embed failed")
	m, err := memory.NewLongTerm(
		memory.WithVectorStore(&recordingVectorStore{}),
		memory.WithEmbedder(&errEmbedder{err: errEmbed}),
	)
	if err != nil {
		t.Fatalf("NewLongTerm: %v", err)
	}

	_, err = m.Retrieve(context.Background(), []goagent.ContentBlock{goagent.TextBlock("q")}, 0)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, errEmbed) {
		t.Errorf("want errEmbed in chain, got: %v", err)
	}
}

func TestNewLongTerm_WithTopK(t *testing.T) {
	t.Parallel()

	store := &searchableVectorStore{}
	m, err := memory.NewLongTerm(
		memory.WithVectorStore(store),
		memory.WithEmbedder(stubEmbedder{}),
		memory.WithTopK(5),
	)
	if err != nil {
		t.Fatalf("NewLongTerm: %v", err)
	}

	// Retrieve with topK=0 should use the configured default of 5.
	_, _ = m.Retrieve(context.Background(), []goagent.ContentBlock{goagent.TextBlock("q")}, 0)
	if store.lastTopK != 5 {
		t.Errorf("topK = %d, want 5", store.lastTopK)
	}
}

func TestNewLongTerm_WithWritePolicy(t *testing.T) {
	t.Parallel()

	store := &recordingVectorStore{}
	// Policy that discards everything.
	discardAll := func(_, _ goagent.Message) []goagent.Message { return nil }

	m, err := memory.NewLongTerm(
		memory.WithVectorStore(store),
		memory.WithEmbedder(stubEmbedder{}),
		memory.WithWritePolicy(discardAll),
	)
	if err != nil {
		t.Fatalf("NewLongTerm: %v", err)
	}

	// Store should still work — writePolicy is applied at the Agent level, not
	// inside LongTermMemory.Store. But we verify construction succeeds.
	if err := m.Store(context.Background(), goagent.UserMessage("hi")); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if len(store.ids) != 1 {
		t.Errorf("expected 1 upsert, got %d", len(store.ids))
	}
}

// TestLongTermStoreEmbedError verifies that embedding failures are propagated.
func TestLongTermStoreEmbedError(t *testing.T) {
	errEmbed := errors.New("embed failed")
	m, err := memory.NewLongTerm(
		memory.WithVectorStore(&recordingVectorStore{}),
		memory.WithEmbedder(&errEmbedder{err: errEmbed}),
	)
	if err != nil {
		t.Fatalf("NewLongTerm: %v", err)
	}

	err = m.Store(context.Background(), goagent.UserMessage("hi"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, errEmbed) {
		t.Errorf("want errEmbed in chain, got: %v", err)
	}
}

// ── Chunker integration tests ─────────────────────────────────────────────────

// TestLongTerm_WithChunker_NoOp verifies that the default NoOpChunker produces
// the same behaviour as the previous chunk-free implementation: exactly one
// Upsert per message.
func TestLongTerm_WithChunker_NoOp(t *testing.T) {
	store := &recordingVectorStore{}
	m, err := memory.NewLongTerm(
		memory.WithVectorStore(store),
		memory.WithEmbedder(stubEmbedder{}),
		memory.WithChunker(vector.NewNoOpChunker()),
	)
	if err != nil {
		t.Fatalf("NewLongTerm: %v", err)
	}

	if err := m.Store(context.Background(), goagent.UserMessage("hello")); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if len(store.ids) != 1 {
		t.Errorf("expected 1 upsert with NoOpChunker, got %d", len(store.ids))
	}
}

// TestLongTerm_WithChunker_TextChunker verifies that a long message produces
// multiple Upserts when using TextChunker.
func TestLongTerm_WithChunker_TextChunker(t *testing.T) {
	store := &recordingVectorStore{}
	// Build a message long enough to be split.
	words := make([]string, 30)
	for i := range words {
		words[i] = "word"
	}
	longText := strings.Join(words, " ")

	m, err := memory.NewLongTerm(
		memory.WithVectorStore(store),
		memory.WithEmbedder(stubEmbedder{}),
		memory.WithChunker(vector.NewTextChunker(
			vector.WithMaxSize(12),
			vector.WithOverlap(0),
		)),
	)
	if err != nil {
		t.Fatalf("NewLongTerm: %v", err)
	}

	if err := m.Store(context.Background(), goagent.UserMessage(longText)); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if len(store.ids) < 2 {
		t.Errorf("expected multiple upserts with TextChunker, got %d", len(store.ids))
	}
}

// TestLongTerm_ChunkerError verifies that a Chunker error is propagated by Store.
func TestLongTerm_ChunkerError(t *testing.T) {
	errChunk := errors.New("chunker failed")
	m, err := memory.NewLongTerm(
		memory.WithVectorStore(&recordingVectorStore{}),
		memory.WithEmbedder(stubEmbedder{}),
		memory.WithChunker(&errChunker{err: errChunk}),
	)
	if err != nil {
		t.Fatalf("NewLongTerm: %v", err)
	}

	err = m.Store(context.Background(), goagent.UserMessage("hi"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, errChunk) {
		t.Errorf("want errChunk in chain, got: %v", err)
	}
}

// TestLongTerm_ErrNoEmbeddeableContent_Skipped verifies that chunks for which
// the embedder returns ErrNoEmbeddeableContent are silently skipped.
func TestLongTerm_ErrNoEmbeddeableContent_Skipped(t *testing.T) {
	store := &recordingVectorStore{}
	m, err := memory.NewLongTerm(
		memory.WithVectorStore(store),
		memory.WithEmbedder(&errEmbedder{err: vector.ErrNoEmbeddeableContent}),
	)
	if err != nil {
		t.Fatalf("NewLongTerm: %v", err)
	}

	// Store should succeed even though the embedder returns ErrNoEmbeddeableContent.
	if err := m.Store(context.Background(), goagent.UserMessage("hello")); err != nil {
		t.Fatalf("Store must not fail on ErrNoEmbeddeableContent: %v", err)
	}
	if len(store.ids) != 0 {
		t.Errorf("expected 0 upserts (all skipped), got %d", len(store.ids))
	}
}

// TestLongTerm_SessionIDPrefixedIDs verifies that when a session ID is in the
// context, Upsert IDs are prefixed with "sessionID:".
func TestLongTerm_SessionIDPrefixedIDs(t *testing.T) {
	store := &recordingVectorStore{}
	m, err := memory.NewLongTerm(
		memory.WithVectorStore(store),
		memory.WithEmbedder(stubEmbedder{}),
	)
	if err != nil {
		t.Fatalf("NewLongTerm: %v", err)
	}

	ctx, err := session.NewContext(context.Background(), "sess-42")
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	if err := m.Store(ctx, goagent.UserMessage("hello")); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if len(store.ids) != 1 {
		t.Fatalf("expected 1 upsert, got %d", len(store.ids))
	}
	if !strings.HasPrefix(store.ids[0], "sess-42:") {
		t.Errorf("ID %q does not start with 'sess-42:'", store.ids[0])
	}
}

// ── Parallel embed tests ──────────────────────────────────────────────────────

// concurrentEmbedder tracks peak concurrency across concurrent Embed calls.
// A small delay forces goroutines to overlap, making the peak observable.
type concurrentEmbedder struct {
	mu    sync.Mutex
	active int
	peak   int
	delay  time.Duration
}

func (e *concurrentEmbedder) Embed(_ context.Context, _ []goagent.ContentBlock) ([]float32, error) {
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

// TestLongTerm_StoreParallelEmbeds verifies that chunks within a message are
// embedded concurrently when using the default (non-BatchEmbedder) path.
func TestLongTerm_StoreParallelEmbeds(t *testing.T) {
	t.Parallel()

	embedder := &concurrentEmbedder{delay: 10 * time.Millisecond}
	store := &recordingVectorStore{}

	words := make([]string, 30)
	for i := range words {
		words[i] = "word"
	}
	longText := strings.Join(words, " ")

	m, err := memory.NewLongTerm(
		memory.WithVectorStore(store),
		memory.WithEmbedder(embedder),
		memory.WithChunker(vector.NewTextChunker(
			vector.WithMaxSize(12),
			vector.WithOverlap(0),
		)),
	)
	if err != nil {
		t.Fatalf("NewLongTerm: %v", err)
	}

	if err := m.Store(context.Background(), goagent.UserMessage(longText)); err != nil {
		t.Fatalf("Store: %v", err)
	}

	embedder.mu.Lock()
	peak := embedder.peak
	embedder.mu.Unlock()

	if peak < 2 {
		t.Errorf("expected concurrent embedding (peak > 1), got peak=%d; upserts=%d", peak, len(store.ids))
	}
}

// TestLongTerm_StoreParallelEmbedsErrorPropagation verifies that when a chunk
// embed fails in the parallel path, the error is propagated by Store.
func TestLongTerm_StoreParallelEmbedsErrorPropagation(t *testing.T) {
	t.Parallel()

	errEmbed := errors.New("embed failed")
	words := make([]string, 30)
	for i := range words {
		words[i] = "word"
	}

	m, err := memory.NewLongTerm(
		memory.WithVectorStore(&recordingVectorStore{}),
		memory.WithEmbedder(&errEmbedder{err: errEmbed}),
		memory.WithChunker(vector.NewTextChunker(
			vector.WithMaxSize(12),
			vector.WithOverlap(0),
		)),
	)
	if err != nil {
		t.Fatalf("NewLongTerm: %v", err)
	}

	err = m.Store(context.Background(), goagent.UserMessage(strings.Join(words, " ")))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, errEmbed) {
		t.Errorf("want errEmbed in chain, got: %v", err)
	}
}

// ── BatchEmbedder tests ───────────────────────────────────────────────────────

// batchRecordingEmbedder implements BatchEmbedder and records each call.
// skipIndex, when >= 0, causes a nil vector at that position to simulate
// a chunk with no embeddable content.
type batchRecordingEmbedder struct {
	mu             sync.Mutex
	batchCallCount int
	batchInputLen  int
	embedCallCount int
	skipIndex      int
}

func (e *batchRecordingEmbedder) Embed(_ context.Context, _ []goagent.ContentBlock) ([]float32, error) {
	e.mu.Lock()
	e.embedCallCount++
	e.mu.Unlock()
	return []float32{0.1, 0.2}, nil
}

func (e *batchRecordingEmbedder) BatchEmbed(_ context.Context, inputs [][]goagent.ContentBlock) ([][]float32, error) {
	e.mu.Lock()
	e.batchCallCount++
	e.batchInputLen = len(inputs)
	e.mu.Unlock()

	vecs := make([][]float32, len(inputs))
	for i := range vecs {
		if e.skipIndex >= 0 && i == e.skipIndex {
			vecs[i] = nil // nil signals no embeddable content for this chunk
			continue
		}
		vecs[i] = []float32{float32(i)*0.1 + 0.1, float32(i)*0.1 + 0.2}
	}
	return vecs, nil
}

// TestLongTerm_BatchEmbedderPath verifies that when the embedder implements
// BatchEmbedder, Store calls BatchEmbed once instead of Embed per chunk.
func TestLongTerm_BatchEmbedderPath(t *testing.T) {
	t.Parallel()

	embedder := &batchRecordingEmbedder{skipIndex: -1}
	store := &bulkRecordingVectorStore{}

	m, err := memory.NewLongTerm(
		memory.WithVectorStore(store),
		memory.WithEmbedder(embedder),
	)
	if err != nil {
		t.Fatalf("NewLongTerm: %v", err)
	}

	msgs := []goagent.Message{
		goagent.UserMessage("first"),
		goagent.AssistantMessage("second"),
	}
	if err := m.Store(context.Background(), msgs...); err != nil {
		t.Fatalf("Store: %v", err)
	}

	embedder.mu.Lock()
	batchCalls := embedder.batchCallCount
	embedCalls := embedder.embedCallCount
	inputLen := embedder.batchInputLen
	embedder.mu.Unlock()

	if batchCalls != 1 {
		t.Errorf("BatchEmbed called %d times, want 1", batchCalls)
	}
	if embedCalls != 0 {
		t.Errorf("Embed called %d times, want 0", embedCalls)
	}
	if inputLen != 2 {
		t.Errorf("BatchEmbed received %d inputs, want 2 (one per message)", inputLen)
	}
	if len(store.bulkEntries) != 2 {
		t.Errorf("BulkUpsert got %d entries, want 2", len(store.bulkEntries))
	}
}

// TestLongTerm_BatchEmbedderSkipsNilVectors verifies that nil vectors returned
// by BatchEmbed are silently skipped, matching the ErrNoEmbeddeableContent
// behaviour of the per-chunk path.
func TestLongTerm_BatchEmbedderSkipsNilVectors(t *testing.T) {
	t.Parallel()

	// skipIndex=0 → first chunk gets a nil vector.
	embedder := &batchRecordingEmbedder{skipIndex: 0}
	store := &bulkRecordingVectorStore{}

	words := make([]string, 30)
	for i := range words {
		words[i] = "word"
	}
	longText := strings.Join(words, " ")

	m, err := memory.NewLongTerm(
		memory.WithVectorStore(store),
		memory.WithEmbedder(embedder),
		memory.WithChunker(vector.NewTextChunker(
			vector.WithMaxSize(12),
			vector.WithOverlap(0),
		)),
	)
	if err != nil {
		t.Fatalf("NewLongTerm: %v", err)
	}

	if err := m.Store(context.Background(), goagent.UserMessage(longText)); err != nil {
		t.Fatalf("Store must not fail for nil vector: %v", err)
	}

	embedder.mu.Lock()
	totalChunks := embedder.batchInputLen
	embedder.mu.Unlock()

	// One chunk was skipped (nil vector at index 0).
	wantEntries := totalChunks - 1
	if len(store.bulkEntries) != wantEntries {
		t.Errorf("expected %d upserted entries (%d chunks − 1 skipped), got %d",
			wantEntries, totalChunks, len(store.bulkEntries))
	}
}

// batchErrEmbedder implements BatchEmbedder and always fails BatchEmbed.
type batchErrEmbedder struct{ err error }

func (e *batchErrEmbedder) Embed(_ context.Context, _ []goagent.ContentBlock) ([]float32, error) {
	return []float32{0.1}, nil
}

func (e *batchErrEmbedder) BatchEmbed(_ context.Context, _ [][]goagent.ContentBlock) ([][]float32, error) {
	return nil, e.err
}

// TestLongTerm_BatchEmbedderError verifies that a BatchEmbed failure is
// propagated as an error from Store.
func TestLongTerm_BatchEmbedderError(t *testing.T) {
	t.Parallel()

	errBatch := errors.New("batch embed failed")
	m, err := memory.NewLongTerm(
		memory.WithVectorStore(&recordingVectorStore{}),
		memory.WithEmbedder(&batchErrEmbedder{err: errBatch}),
	)
	if err != nil {
		t.Fatalf("NewLongTerm: %v", err)
	}

	err = m.Store(context.Background(), goagent.UserMessage("hi"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, errBatch) {
		t.Errorf("want errBatch in chain, got: %v", err)
	}
}

// ── Benchmarks ────────────────────────────────────────────────────────────────

// sleepEmbedder simulates a remote embed API with fixed per-call latency.
type sleepEmbedder struct{ delay time.Duration }

func (e *sleepEmbedder) Embed(_ context.Context, _ []goagent.ContentBlock) ([]float32, error) {
	time.Sleep(e.delay)
	return []float32{0.1, 0.2}, nil
}

// batchSleepEmbedder simulates a batch embed API: one call regardless of N.
type batchSleepEmbedder struct{ delay time.Duration }

func (e *batchSleepEmbedder) Embed(_ context.Context, _ []goagent.ContentBlock) ([]float32, error) {
	time.Sleep(e.delay)
	return []float32{0.1, 0.2}, nil
}

func (e *batchSleepEmbedder) BatchEmbed(_ context.Context, inputs [][]goagent.ContentBlock) ([][]float32, error) {
	time.Sleep(e.delay) // single call regardless of chunk count
	vecs := make([][]float32, len(inputs))
	for i := range vecs {
		vecs[i] = []float32{float32(i)*0.01, float32(i)*0.02 + 0.1}
	}
	return vecs, nil
}

// makeLongMessage returns a message long enough to produce ~10 chunks with
// TextChunker(maxSize=12).
func makeLongMessage() goagent.Message {
	words := make([]string, 120)
	for i := range words {
		words[i] = "word"
	}
	return goagent.UserMessage(strings.Join(words, " "))
}

// BenchmarkStore_Parallel measures Store throughput using the parallel embed
// path with a simulated 5 ms per-call latency.
func BenchmarkStore_Parallel(b *testing.B) {
	msg := makeLongMessage()
	store := &bulkRecordingVectorStore{}
	m, err := memory.NewLongTerm(
		memory.WithVectorStore(store),
		memory.WithEmbedder(&sleepEmbedder{delay: 5 * time.Millisecond}),
		memory.WithChunker(vector.NewTextChunker(
			vector.WithMaxSize(12),
			vector.WithOverlap(0),
		)),
	)
	if err != nil {
		b.Fatalf("NewLongTerm: %v", err)
	}

	for b.Loop() {
		store.bulkEntries = store.bulkEntries[:0]
		if err := m.Store(context.Background(), msg); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkStore_BatchEmbedder measures Store throughput using the BatchEmbedder
// fast path with a simulated 5 ms single-call latency (one call per iteration).
func BenchmarkStore_BatchEmbedder(b *testing.B) {
	msg := makeLongMessage()
	store := &bulkRecordingVectorStore{}
	m, err := memory.NewLongTerm(
		memory.WithVectorStore(store),
		memory.WithEmbedder(&batchSleepEmbedder{delay: 5 * time.Millisecond}),
		memory.WithChunker(vector.NewTextChunker(
			vector.WithMaxSize(12),
			vector.WithOverlap(0),
		)),
	)
	if err != nil {
		b.Fatalf("NewLongTerm: %v", err)
	}

	for b.Loop() {
		store.bulkEntries = store.bulkEntries[:0]
		if err := m.Store(context.Background(), msg); err != nil {
			b.Fatal(err)
		}
	}
}

// errChunker is a Chunker stub that always returns an error.
type errChunker struct{ err error }

func (c *errChunker) Chunk(_ context.Context, _ vector.ChunkContent) ([]vector.ChunkResult, error) {
	return nil, c.err
}

// bulkRecordingVectorStore records BulkUpsert calls and also satisfies VectorStore.
type bulkRecordingVectorStore struct {
	bulkEntries []goagent.UpsertEntry
	upsertIDs   []string
}

func (s *bulkRecordingVectorStore) Upsert(_ context.Context, id string, _ []float32, _ goagent.Message) error {
	s.upsertIDs = append(s.upsertIDs, id)
	return nil
}

func (s *bulkRecordingVectorStore) Search(_ context.Context, _ []float32, _ int, _ ...goagent.SearchOption) ([]goagent.ScoredMessage, error) {
	return nil, nil
}

func (s *bulkRecordingVectorStore) Delete(_ context.Context, _ string) error { return nil }

func (s *bulkRecordingVectorStore) BulkUpsert(_ context.Context, entries []goagent.UpsertEntry) error {
	s.bulkEntries = append(s.bulkEntries, entries...)
	return nil
}

func (s *bulkRecordingVectorStore) BulkDelete(_ context.Context, _ []string) error { return nil }

// TestLongTerm_UsesBulkUpsertWhenAvailable verifies that Store delegates to
// BulkUpsert when the underlying VectorStore implements BulkVectorStore.
func TestLongTerm_UsesBulkUpsertWhenAvailable(t *testing.T) {
	store := &bulkRecordingVectorStore{}
	m, err := memory.NewLongTerm(
		memory.WithVectorStore(store),
		memory.WithEmbedder(stubEmbedder{}),
	)
	if err != nil {
		t.Fatalf("NewLongTerm: %v", err)
	}

	if err := m.Store(context.Background(), goagent.UserMessage("hello")); err != nil {
		t.Fatalf("Store: %v", err)
	}

	if len(store.upsertIDs) != 0 {
		t.Errorf("expected Upsert not to be called, but got %d calls", len(store.upsertIDs))
	}
	if len(store.bulkEntries) != 1 {
		t.Errorf("expected 1 BulkUpsert entry, got %d", len(store.bulkEntries))
	}
}

// TestLongTerm_BulkFallbackWhenNotSupported verifies that when the store does
// not implement BulkVectorStore, individual Upsert calls are made.
func TestLongTerm_BulkFallbackWhenNotSupported(t *testing.T) {
	store := &recordingVectorStore{}
	m, err := memory.NewLongTerm(
		memory.WithVectorStore(store),
		memory.WithEmbedder(stubEmbedder{}),
	)
	if err != nil {
		t.Fatalf("NewLongTerm: %v", err)
	}

	if err := m.Store(context.Background(), goagent.UserMessage("hello")); err != nil {
		t.Fatalf("Store: %v", err)
	}

	if len(store.ids) != 1 {
		t.Errorf("expected 1 individual Upsert call, got %d", len(store.ids))
	}
}
