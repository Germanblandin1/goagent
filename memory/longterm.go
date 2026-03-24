package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/Germanblandin1/goagent"
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
	return &longTermMemory{
		store:       cfg.store,
		embedder:    cfg.embedder,
		topK:        cfg.topK,
		writePolicy: cfg.writePolicy,
	}, nil
}

type longTermMemory struct {
	store       goagent.VectorStore
	embedder    goagent.Embedder
	topK        int
	writePolicy goagent.WritePolicy
}

func (m *longTermMemory) Store(ctx context.Context, msgs ...goagent.Message) error {
	// Fase 1: embeddear todo — si algo falla, no se persistió nada
	type prepared struct {
		id  string
		vec []float32
		msg goagent.Message
	}
	batch := make([]prepared, 0, len(msgs))
	for _, msg := range msgs {
		vec, err := m.embedder.Embed(ctx, msg.Content)
		if err != nil {
			return fmt.Errorf("embedding message (role=%s): %w", msg.Role, err)
		}
		batch = append(batch, prepared{
			id:  messageID(msg),
			vec: vec,
			msg: msg,
		})
	}

	// Fase 2: persistir — todo el embedding ya pasó sin errores
	for _, p := range batch {
		if err := m.store.Upsert(ctx, p.id, p.vec, p.msg); err != nil {
			return fmt.Errorf("upserting message (role=%s): %w", p.msg.Role, err)
		}
	}
	return nil
}

func (m *longTermMemory) Retrieve(ctx context.Context, query []goagent.ContentBlock, topK int) ([]goagent.Message, error) {
	k := topK
	if k <= 0 {
		k = m.topK
	}
	vec, err := m.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embedding query: %w", err)
	}
	return m.store.Search(ctx, vec, k)
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
