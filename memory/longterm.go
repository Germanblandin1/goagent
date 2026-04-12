package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

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
// Each message is first split into chunks by the Chunker, then the chunks are
// embedded and upserted into the VectorStore with stable IDs.
//
// ID format: "sessionID:sha256(msg):chunkIndex" when a session ID is present
// in the context (via vector.WithSessionID), or "sha256(msg):chunkIndex" otherwise.
// This convention allows InMemoryStore.Search to filter by session.
//
// When a chunk contains no embeddeable content (e.g. an image chunk with a
// text-only embedder), the chunk is silently skipped — this is not an error.
//
// Store selects between two embedding strategies at runtime:
//
//   - BatchEmbedder path: if the configured Embedder also implements
//     [goagent.BatchEmbedder], all chunks from all messages are collected and
//     embedded in a single API call, then upserted in one pass.
//   - Parallel path (default): chunks within each message are embedded
//     concurrently (fan-out / fan-in), reducing per-message latency from
//     K×embed_latency to ~max(embed_latency).
func (m *longTermMemory) Store(ctx context.Context, msgs ...goagent.Message) error {
	// session.IDFromContext guarantees that sessionID never contains ":".
	// See session.NewContext — IDs with ":" are rejected at injection time,
	// so the first ":" in "sessionID:baseID:chunkIndex" is always the boundary.
	sessionID, hasSession := session.IDFromContext(ctx)

	// BatchEmbedder fast path: collect all chunks across all messages, embed in
	// one API call, and upsert in a single pass. For remote embedding APIs this
	// trades N×K HTTP round trips for one.
	if batcher, ok := m.embedder.(goagent.BatchEmbedder); ok {
		return m.storeBatch(ctx, batcher, sessionID, hasSession, msgs)
	}

	// Default path: embed chunks of each message concurrently (fan-out / fan-in),
	// then batch-upsert per message when the store supports it.
	for _, msg := range msgs {
		if err := m.storeOne(ctx, sessionID, hasSession, msg); err != nil {
			return err
		}
	}
	return nil
}

// storeBatch implements the BatchEmbedder fast path for Store.
// It chunks all messages, calls BatchEmbed once, then upserts all entries.
func (m *longTermMemory) storeBatch(
	ctx context.Context,
	batcher goagent.BatchEmbedder,
	sessionID string,
	hasSession bool,
	msgs []goagent.Message,
) error {
	// First pass: chunk all messages and collect (msg, chunk, baseID, chunkIdx).
	type item struct {
		msg      goagent.Message
		chunk    vector.ChunkResult
		baseID   string
		chunkIdx int
	}
	var items []item
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
		for i, chunk := range chunks {
			items = append(items, item{msg, chunk, baseID, i})
		}
	}
	if len(items) == 0 {
		return nil
	}

	// Second pass: single BatchEmbed call.
	inputs := make([][]goagent.ContentBlock, len(items))
	for i, it := range items {
		inputs[i] = it.chunk.Blocks
	}
	vecs, err := batcher.BatchEmbed(ctx, inputs)
	if err != nil {
		return fmt.Errorf("batch embedding: %w", err)
	}

	// Third pass: build UpsertEntry slice, skipping nil vectors (no embeddable content).
	entries := make([]goagent.UpsertEntry, 0, len(items))
	for i, it := range items {
		if vecs[i] == nil {
			continue
		}
		var id string
		if hasSession {
			id = fmt.Sprintf("%s:%s:%d", sessionID, it.baseID, it.chunkIdx)
		} else {
			id = fmt.Sprintf("%s:%d", it.baseID, it.chunkIdx)
		}
		entries = append(entries, goagent.UpsertEntry{
			ID:      id,
			Vector:  vecs[i],
			Message: vector.ChunkToMessage(it.msg, it.chunk),
		})
	}

	if bulk, ok := m.store.(goagent.BulkVectorStore); ok {
		if err := bulk.BulkUpsert(ctx, entries); err != nil {
			return fmt.Errorf("bulk upserting: %w", err)
		}
	} else {
		for _, e := range entries {
			if err := m.store.Upsert(ctx, e.ID, e.Vector, e.Message); err != nil {
				return fmt.Errorf("upserting %s: %w", e.ID, err)
			}
		}
	}
	return nil
}

// storeOne embeds a single message's chunks concurrently and upserts them.
// Chunks are embedded in parallel (fan-out / fan-in); all goroutines run to
// completion even if one fails — the first error found after wg.Wait is returned.
// Context cancellation propagates to all Embed calls naturally.
func (m *longTermMemory) storeOne(ctx context.Context, sessionID string, hasSession bool, msg goagent.Message) error {
	content := vector.ChunkContent{
		Blocks:   msg.Content,
		Metadata: map[string]any{"role": string(msg.Role)},
	}
	chunks, err := m.chunker.Chunk(ctx, content)
	if err != nil {
		return fmt.Errorf("chunking message (role=%s): %w", msg.Role, err)
	}

	baseID := messageID(msg)

	// Embed all chunks concurrently. Each goroutine writes to its own index
	// in results — no mutex needed (same pattern as dispatcher.dispatch).
	type embedResult struct {
		entry goagent.UpsertEntry
		err   error
		skip  bool
	}
	results := make([]embedResult, len(chunks))
	var wg sync.WaitGroup
	for i, chunk := range chunks {
		wg.Add(1)
		go func(idx int, ch vector.ChunkResult) {
			defer wg.Done()
			vec, embedErr := m.embedder.Embed(ctx, ch.Blocks)
			if embedErr != nil {
				if errors.Is(embedErr, vector.ErrNoEmbeddeableContent) {
					results[idx].skip = true // image/doc with text-only embedder — skip silently
					return
				}
				results[idx].err = embedErr
				return
			}
			var id string
			if hasSession {
				id = fmt.Sprintf("%s:%s:%d", sessionID, baseID, idx)
			} else {
				id = fmt.Sprintf("%s:%d", baseID, idx)
			}
			results[idx].entry = goagent.UpsertEntry{
				ID:      id,
				Vector:  vec,
				Message: vector.ChunkToMessage(msg, ch),
			}
		}(i, chunk)
	}
	wg.Wait()

	// Collect results; return the first embedding error encountered.
	type pendingItem struct {
		chunkIdx int
		entry    goagent.UpsertEntry
	}
	var pending []pendingItem
	for i, r := range results {
		if r.skip {
			continue
		}
		if r.err != nil {
			return fmt.Errorf("embedding chunk %d (role=%s): %w", i, msg.Role, r.err)
		}
		pending = append(pending, pendingItem{chunkIdx: i, entry: r.entry})
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
				return fmt.Errorf("upserting chunk %d (role=%s): %w", p.chunkIdx, msg.Role, err)
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
	results, err := m.store.Search(ctx, vec, k, opts...)
	if err != nil {
		return nil, err
	}
	return applyTokenBudget(ctx, results, opts...), nil
}

// applyTokenBudget trims results to fit within [goagent.SearchOptions.TokenBudget].
// results must already be sorted by score descending (as returned by [goagent.VectorStore.Search]).
// The loop stops as soon as the next result would exceed the remaining budget.
// If TokenBudget ≤ 0 or TokenEstimator is nil, results are returned unchanged.
func applyTokenBudget(ctx context.Context, results []goagent.ScoredMessage, opts ...goagent.SearchOption) []goagent.ScoredMessage {
	cfg := &goagent.SearchOptions{}
	for _, o := range opts {
		o(cfg)
	}
	if cfg.TokenBudget <= 0 || cfg.TokenEstimator == nil {
		return results
	}
	remaining := cfg.TokenBudget
	for i, r := range results {
		cost := cfg.TokenEstimator(ctx, vector.ExtractText(r.Message.Content))
		if cost > remaining {
			return results[:i]
		}
		remaining -= cost
	}
	return results
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
