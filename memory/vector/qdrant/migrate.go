package qdrant

import (
	"context"
	"fmt"

	"github.com/qdrant/go-client/qdrant"
)

// CollectionConfig configures the collection that CreateCollection will create.
// Use this when the caller has no existing collection and wants a quick start.
// For production, review the HNSW index parameters based on expected volume.
type CollectionConfig struct {
	// CollectionName is the name of the collection to create.
	// Default: "goagent_embeddings".
	CollectionName string

	// Dims is the vector dimension. Required. Must match the embedding model
	// used (e.g. 768 for nomic-embed-text, 1024 for voyage-3,
	// 1536 for text-embedding-3-small).
	Dims uint64

	// DistanceFunc sets the distance measure for the collection's vector index.
	// Must match the WithDistanceFunc option passed to New.
	// Default: Cosine.
	DistanceFunc DistanceFunc

	// HNSWm is the m parameter of the HNSW index (connections per node).
	// Common values: 8 (reduced memory), 16 (default), 32 (higher recall).
	// Default: 16.
	HNSWm uint64

	// HNSWefConstruction is the candidate set size during index construction.
	// Higher value = more accurate but slower to build.
	// Default: 100.
	HNSWefConstruction uint64

	// OnDisk controls whether vectors are stored on disk (true) or in RAM (false).
	// Default: false. Set true for large datasets that exceed available RAM.
	OnDisk bool
}

// CreateCollection creates a Qdrant collection with an HNSW index if it does
// not already exist. It is idempotent — safe to call multiple times.
//
// To use the created collection with New:
//
//	cfg := qdrant.CollectionConfig{CollectionName: "goagent_embeddings", Dims: 1536}
//	if err := qdrant.CreateCollection(ctx, client, cfg); err != nil { ... }
//
//	store, err := qdrant.New(client, qdrant.Config{CollectionName: cfg.CollectionName})
func CreateCollection(ctx context.Context, client *qdrant.Client, cfg CollectionConfig) error {
	if cfg.Dims == 0 {
		return fmt.Errorf("qdrant: CreateCollection: Dims is required")
	}
	if cfg.CollectionName == "" {
		cfg.CollectionName = "goagent_embeddings"
	}
	if cfg.DistanceFunc.distance == 0 {
		cfg.DistanceFunc = Cosine
	}
	if cfg.HNSWm == 0 {
		cfg.HNSWm = 16
	}
	if cfg.HNSWefConstruction == 0 {
		cfg.HNSWefConstruction = 100
	}

	exists, err := client.CollectionExists(ctx, cfg.CollectionName)
	if err != nil {
		return fmt.Errorf("qdrant: CreateCollection: check exists: %w", err)
	}
	if exists {
		return nil
	}

	m := cfg.HNSWm
	efConstruction := cfg.HNSWefConstruction
	onDisk := cfg.OnDisk

	err = client.CreateCollection(ctx, &qdrant.CreateCollection{
		CollectionName: cfg.CollectionName,
		VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
			Size:     cfg.Dims,
			Distance: cfg.DistanceFunc.distance,
			OnDisk:   &onDisk,
			HnswConfig: &qdrant.HnswConfigDiff{
				M:           &m,
				EfConstruct: &efConstruction,
			},
		}),
	})
	if err != nil {
		return fmt.Errorf("qdrant: CreateCollection: %w", err)
	}
	return nil
}
