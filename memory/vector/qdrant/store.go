package qdrant

import (
	"context"
	"fmt"

	"github.com/Germanblandin1/goagent"
	"github.com/qdrant/go-client/qdrant"
)

// Note: *Store does not satisfy goagent.VectorStore directly — its Search
// method accepts an optional ...SearchOption parameter which is not part of
// the interface. Wrap it with an adapter if you need to pass *Store where a
// goagent.VectorStore is required.

// Config describes the Qdrant collection to use.
// CollectionName is the only required field.
type Config struct {
	// CollectionName is the Qdrant collection to read and write.
	// Must be created before New is called (or use CreateCollection).
	CollectionName string
}

// StoreOption configures the behaviour of a Store.
type StoreOption func(*storeOptions)

type storeOptions struct {
	distanceFunc DistanceFunc
}

// WithDistanceFunc sets the distance function used for score normalization in
// Search. Must match the distance configured in the Qdrant collection.
// Default: Cosine.
func WithDistanceFunc(d DistanceFunc) StoreOption {
	return func(o *storeOptions) { o.distanceFunc = d }
}

// Store implements goagent.VectorStore over Qdrant.
type Store struct {
	client       *qdrant.Client
	cfg          Config
	distanceFunc DistanceFunc
}

// New creates a Store backed by client using the given Config and options.
// Returns an error if CollectionName is empty.
// The client must be connected and the collection must already exist.
func New(client *qdrant.Client, cfg Config, opts ...StoreOption) (*Store, error) {
	if cfg.CollectionName == "" {
		return nil, fmt.Errorf("qdrant: Config.CollectionName is required")
	}

	var o storeOptions
	for _, opt := range opts {
		opt(&o)
	}
	if o.distanceFunc.distance == 0 {
		o.distanceFunc = Cosine
	}

	return &Store{
		client:       client,
		cfg:          cfg,
		distanceFunc: o.distanceFunc,
	}, nil
}

// Upsert stores or updates the message and its embedding vector under id.
// The operation is idempotent: calling Upsert twice with the same id replaces
// the first entry. Only the text content and metadata from msg are persisted —
// Role and ToolCalls are not stored.
//
// id is converted to a deterministic uint64 point ID via UUID v5. The original
// string id is stored in the payload under key "id" so it can be recovered on
// search results.
func (s *Store) Upsert(ctx context.Context, id string, vec []float32, msg goagent.Message) error {
	text := goagent.TextFrom(msg.Content)
	pointID := stringToPointID(id)

	payload := map[string]any{
		"id":      id,
		"content": text,
	}
	if len(msg.Metadata) > 0 {
		payload["metadata"] = msg.Metadata
	}

	_, err := s.client.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: s.cfg.CollectionName,
		Points: []*qdrant.PointStruct{
			{
				Id:      qdrant.NewIDNum(pointID),
				Vectors: qdrant.NewVectorsDense(vec),
				Payload: payloadToQdrant(payload),
			},
		},
	})
	if err != nil {
		return fmt.Errorf("qdrant: upsert: %w", err)
	}
	return nil
}

// Search returns the topK messages most similar to vec, ordered by similarity
// descending. Each returned Message has RoleDocument so it is never forwarded
// to a provider.
//
// opts is reserved for future use (score threshold, metadata filters, etc.).
// Passing no options is equivalent to the default behaviour.
func (s *Store) Search(ctx context.Context, vec []float32, topK int, opts ...SearchOption) ([]goagent.ScoredMessage, error) {
	cfg := &searchConfig{}
	for _, o := range opts {
		o(cfg)
	}

	limit := uint64(topK)
	results, err := s.client.Query(ctx, &qdrant.QueryPoints{
		CollectionName: s.cfg.CollectionName,
		Query:          qdrant.NewQuery(vec...),
		Limit:          &limit,
		WithPayload:    qdrant.NewWithPayloadInclude("id", "content", "metadata"),
	})
	if err != nil {
		return nil, fmt.Errorf("qdrant: search: %w", err)
	}

	out := make([]goagent.ScoredMessage, 0, len(results))
	for _, r := range results {
		text, _, meta, err := extractPayload(r.Payload)
		if err != nil {
			return nil, fmt.Errorf("qdrant: search: %w", err)
		}
		out = append(out, goagent.ScoredMessage{
			Score: s.distanceFunc.normalizeScore(float64(r.Score)),
			Message: goagent.Message{
				Role:     goagent.RoleDocument,
				Content:  []goagent.ContentBlock{goagent.TextBlock(text)},
				Metadata: meta,
			},
		})
	}
	return out, nil
}

// Delete removes the entry with the given id from the store.
// It is a no-op if id does not exist.
func (s *Store) Delete(ctx context.Context, id string) error {
	pointID := stringToPointID(id)
	_, err := s.client.Delete(ctx, &qdrant.DeletePoints{
		CollectionName: s.cfg.CollectionName,
		Points:         qdrant.NewPointsSelector(qdrant.NewIDNum(pointID)),
	})
	if err != nil {
		return fmt.Errorf("qdrant: delete: %w", err)
	}
	return nil
}
