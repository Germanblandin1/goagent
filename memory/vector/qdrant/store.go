package qdrant

import (
	"context"
	"fmt"

	"github.com/Germanblandin1/goagent"
	"github.com/qdrant/go-client/qdrant"
)

// Compile-time checks.
var _ goagent.VectorStore = (*Store)(nil)
var _ goagent.BulkVectorStore = (*Store)(nil)

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
// WithScoreThreshold is forwarded to Qdrant's native score_threshold field,
// so filtering happens server-side before topK is applied.
// WithFilter converts each key-value pair to a Qdrant Must condition on
// "metadata.<key>". Supported value types: string, bool, int64, and float64
// whole numbers. Fractional floats and unsupported types are silently skipped.
func (s *Store) Search(ctx context.Context, vec []float32, topK int, opts ...goagent.SearchOption) ([]goagent.ScoredMessage, error) {
	cfg := &goagent.SearchOptions{}
	for _, o := range opts {
		o(cfg)
	}

	limit := uint64(topK)
	req := &qdrant.QueryPoints{
		CollectionName: s.cfg.CollectionName,
		Query:          qdrant.NewQuery(vec...),
		Limit:          &limit,
		WithPayload:    qdrant.NewWithPayloadInclude("id", "content", "metadata"),
	}
	if cfg.ScoreThreshold != nil {
		t := float32(*cfg.ScoreThreshold)
		req.ScoreThreshold = &t
	}
	if conditions := filterToConditions(cfg.Filter); len(conditions) > 0 {
		req.Filter = &qdrant.Filter{Must: conditions}
	}
	results, err := s.client.Query(ctx, req)
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

// BulkUpsert stores or updates all entries in a single Qdrant UpsertPoints
// RPC call. This is the native Qdrant batch operation and is significantly
// more efficient than N individual Upsert calls.
func (s *Store) BulkUpsert(ctx context.Context, entries []goagent.UpsertEntry) error {
	if len(entries) == 0 {
		return nil
	}

	points := make([]*qdrant.PointStruct, len(entries))
	for i, e := range entries {
		text := goagent.TextFrom(e.Message.Content)
		pointID := stringToPointID(e.ID)

		payload := map[string]any{
			"id":      e.ID,
			"content": text,
		}
		if len(e.Message.Metadata) > 0 {
			payload["metadata"] = e.Message.Metadata
		}

		points[i] = &qdrant.PointStruct{
			Id:      qdrant.NewIDNum(pointID),
			Vectors: qdrant.NewVectorsDense(e.Vector),
			Payload: payloadToQdrant(payload),
		}
	}

	_, err := s.client.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: s.cfg.CollectionName,
		Points:         points,
	})
	if err != nil {
		return fmt.Errorf("qdrant: bulk upsert: %w", err)
	}
	return nil
}

// BulkDelete removes all entries with the given ids in a single Qdrant
// DeletePoints RPC call. IDs that do not exist are silently ignored.
func (s *Store) BulkDelete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	pointIDs := make([]*qdrant.PointId, len(ids))
	for i, id := range ids {
		pointIDs[i] = qdrant.NewIDNum(stringToPointID(id))
	}
	_, err := s.client.Delete(ctx, &qdrant.DeletePoints{
		CollectionName: s.cfg.CollectionName,
		Points:         qdrant.NewPointsSelector(pointIDs...),
	})
	if err != nil {
		return fmt.Errorf("qdrant: bulk delete: %w", err)
	}
	return nil
}
