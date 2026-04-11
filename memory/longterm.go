package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/internal/session"
	"github.com/Germanblandin1/goagent/memory/vector"
)

var (
	// ErrMissingVectorStore is returned by NewLongTerm when no VectorStore
	// is provided via WithVectorStore.
	ErrMissingVectorStore = errors.New("memory: NewLongTerm requires WithVectorStore")

	// ErrMissingEmbedder is returned by NewLongTerm when no Embedder is
	// provided via WithEmbedder.
	ErrMissingEmbedder = errors.New("memory: NewLongTerm requires WithEmbedder")
)

type longTermConfig struct {
	store       goagent.VectorStore
	embedder    goagent.Embedder
	chunker     vector.Chunker
	topK        int
	writePolicy goagent.WritePolicy
}

// LongTermOption configures a LongTermMemory created by NewLongTerm.
type LongTermOption func(*longTermConfig)

// WithVectorStore sets the vector store backend. Required.
func WithVectorStore(s goagent.VectorStore) LongTermOption {
	return func(c *longTermConfig) { c.store = s }
}

// WithEmbedder sets the embedding model used to vectorize messages. Required.
func WithEmbedder(e goagent.Embedder) LongTermOption {
	return func(c *longTermConfig) { c.embedder = e }
}

// WithChunker sets the chunking strategy used by Store before embedding.
// Default: NoOpChunker — one message produces exactly one chunk.
// For long documents or text that may exceed the embedding model's context
// window, use TextChunker or BlockChunker.
func WithChunker(c vector.Chunker) LongTermOption {
	return func(cfg *longTermConfig) { cfg.chunker = c }
}

// WithTopK sets the default number of messages returned by Retrieve.
// Default: 3. Overridden by the topK argument passed to Retrieve directly.
func WithTopK(k int) LongTermOption {
	return func(c *longTermConfig) { c.topK = k }
}

// WithWritePolicy sets the function that decides whether a turn should be
// stored. Default: StoreAlways.
func WithWritePolicy(p goagent.WritePolicy) LongTermOption {
	return func(c *longTermConfig) { c.writePolicy = p }
}

// NewLongTerm creates a LongTermMemory with the given options.
// Both WithVectorStore and WithEmbedder are required.
//
// Possible errors:
//   - ErrMissingVectorStore — no VectorStore was provided
//   - ErrMissingEmbedder — no Embedder was provided
func NewLongTerm(opts ...LongTermOption) (goagent.LongTermMemory, error) {
	cfg := &longTermConfig{topK: 3}
	for _, o := range opts {
		o(cfg)
	}
	if cfg.store == nil {
		return nil, ErrMissingVectorStore
	}
	if cfg.embedder == nil {
		return nil, ErrMissingEmbedder
	}
	if cfg.writePolicy == nil {
		cfg.writePolicy = goagent.StoreAlways
	}
	if cfg.chunker == nil {
		cfg.chunker = vector.NewNoOpChunker()
	}
	return &longTermMemory{
		store:       cfg.store,
		embedder:    cfg.embedder,
		chunker:     cfg.chunker,
		topK:        cfg.topK,
		writePolicy: cfg.writePolicy,
	}, nil
}

type longTermMemory struct {
	store       goagent.VectorStore
	embedder    goagent.Embedder
	chunker     vector.Chunker
	topK        int
	writePolicy goagent.WritePolicy
}

// Store persists msgs via the configured chunking and embedding pipeline.
// Each message is first split into chunks by the Chunker, then each chunk is
// embedded and upserted into the VectorStore with a stable ID.
//
// ID format: "sessionID:sha256(msg):chunkIndex" when a session ID is present
// in the context (via vector.WithSessionID), or "sha256(msg):chunkIndex" otherwise.
// This convention allows InMemoryStore.Search to filter by session.
//
// When a chunk contains no embeddeable content (e.g. an image chunk with a
// text-only embedder), the chunk is silently skipped — this is not an error.
func (m *longTermMemory) Store(ctx context.Context, msgs ...goagent.Message) error {
	// session.IDFromContext guarantees that sessionID never contains ":".
	// See session.NewContext — IDs with ":" are rejected at injection time,
	// so the first ":" in "sessionID:baseID:chunkIndex" is always the boundary.
	sessionID, hasSession := session.IDFromContext(ctx)

	for _, msg := range msgs {
		content := vector.ChunkContent{
			Blocks:   msg.Content,
			Metadata: map[string]any{"role": string(msg.Role)},
		}
		chunks, err := m.chunker.Chunk(ctx, content)
		if err != nil {
			return fmt.Errorf("chunking message (role=%s): %w", msg.Role, err)
		}

		baseID := messageID(msg)

		// Embed all chunks first, then batch-upsert when the store supports it.
		type pendingEntry struct {
			idx   int
			entry goagent.UpsertEntry
		}
		var pending []pendingEntry

		for i, chunk := range chunks {
			vec, err := m.embedder.Embed(ctx, chunk.Blocks)
			if err != nil {
				if errors.Is(err, vector.ErrNoEmbeddeableContent) {
					continue // image/doc chunk with text-only embedder — skip silently
				}
				return fmt.Errorf("embedding chunk %d (role=%s): %w", i, msg.Role, err)
			}

			var id string
			if hasSession {
				id = fmt.Sprintf("%s:%s:%d", sessionID, baseID, i)
			} else {
				id = fmt.Sprintf("%s:%d", baseID, i)
			}

			pending = append(pending, pendingEntry{
				idx:   i,
				entry: goagent.UpsertEntry{ID: id, Vector: vec, Message: vector.ChunkToMessage(msg, chunk)},
			})
		}

		if bulk, ok := m.store.(goagent.BulkVectorStore); ok {
			entries := make([]goagent.UpsertEntry, len(pending))
			for i, p := range pending {
				entries[i] = p.entry
			}
			if err := bulk.BulkUpsert(ctx, entries); err != nil {
				return fmt.Errorf("bulk upserting chunks (role=%s): %w", msg.Role, err)
			}
		} else {
			for _, p := range pending {
				if err := m.store.Upsert(ctx, p.entry.ID, p.entry.Vector, p.entry.Message); err != nil {
					return fmt.Errorf("upserting chunk %d (role=%s): %w", p.idx, msg.Role, err)
				}
			}
		}
	}
	return nil
}

func (m *longTermMemory) Retrieve(ctx context.Context, query []goagent.ContentBlock, topK int, opts ...goagent.SearchOption) ([]goagent.ScoredMessage, error) {
	k := topK
	if k <= 0 {
		k = m.topK
	}
	vec, err := m.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embedding query: %w", err)
	}
	return m.store.Search(ctx, vec, k, opts...)
}

// messageID returns a stable content-based identifier for a message.
// It hashes the role and every ContentBlock — including binary data for
// images and documents — so that two messages with the same text but
// different images always produce distinct IDs.
func messageID(msg goagent.Message) string {
	h := sha256.New()
	h.Write([]byte(msg.Role))
	h.Write([]byte{0x00}) // field separator
	for _, b := range msg.Content {
		h.Write([]byte(b.Type))
		h.Write([]byte{0x01}) // type/data separator
		switch b.Type {
		case goagent.ContentText:
			h.Write([]byte(b.Text))
		case goagent.ContentImage:
			if b.Image != nil {
				h.Write([]byte(b.Image.MediaType))
				h.Write([]byte{0x01})
				h.Write(b.Image.Data)
			}
		case goagent.ContentDocument:
			if b.Document != nil {
				h.Write([]byte(b.Document.MediaType))
				h.Write([]byte{0x01})
				h.Write([]byte(b.Document.Title))
				h.Write([]byte{0x01})
				h.Write(b.Document.Data)
			}
		}
		h.Write([]byte{0x02}) // block separator
	}
	return hex.EncodeToString(h.Sum(nil))
}
