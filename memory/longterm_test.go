package memory_test

import (
	"context"
	"errors"
	"strings"
	"testing"

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

func (s *recordingVectorStore) Search(_ context.Context, _ []float32, _ int) ([]goagent.Message, error) {
	return nil, nil
}

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
	results  []goagent.Message
	lastTopK int
}

func (s *searchableVectorStore) Upsert(_ context.Context, _ string, _ []float32, _ goagent.Message) error {
	return nil
}

func (s *searchableVectorStore) Search(_ context.Context, _ []float32, topK int) ([]goagent.Message, error) {
	s.lastTopK = topK
	return s.results, nil
}

func TestLongTerm_Retrieve(t *testing.T) {
	t.Parallel()

	want := []goagent.Message{goagent.UserMessage("past context")}
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
		if len(got) != 1 || got[0].TextContent() != "past context" {
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

	ctx := session.WithID(context.Background(), "sess-42")
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

// errChunker is a Chunker stub that always returns an error.
type errChunker struct{ err error }

func (c *errChunker) Chunk(_ context.Context, _ vector.ChunkContent) ([]vector.ChunkResult, error) {
	return nil, c.err
}
