package pgvector

import (
	"context"
	"database/sql"
	"fmt"
)

// MigrateConfig configures the table that Migrate will create.
// Use this when the caller has no existing table and wants a quick start.
// For production, review the HNSW index parameters based on expected volume.
type MigrateConfig struct {
	// TableName is the name of the table to create. Default: "goagent_embeddings".
	TableName string

	// Dims is the vector dimension. Required. Must match the embedding model
	// used (768, 1024, 1536, etc.).
	Dims int

	// DistanceFunc sets the HNSW operator class for the index.
	// Must match the WithDistanceFunc option passed to New.
	// Default: Cosine.
	DistanceFunc DistanceFunc

	// HNSWm is the m parameter of the HNSW index (connections per node).
	// Common values: 8 (reduced memory), 16 (default), 32 (higher recall).
	// Default: 16.
	HNSWm int

	// HNSWefConstruction is the candidate set size during index construction.
	// Higher value = more accurate but slower to build.
	// Default: 64.
	HNSWefConstruction int
}

// Migrate creates the vector extension, table, and HNSW index if they do not
// exist. It is idempotent — safe to call multiple times without error.
// The created table has columns: id TEXT PK, embedding vector(Dims),
// content TEXT, metadata JSONB, created_at TIMESTAMPTZ.
//
// To use the created table with New:
//
//	cfg := pgvector.MigrateConfig{TableName: "goagent_embeddings", Dims: 1536}
//	if err := pgvector.Migrate(ctx, db, cfg); err != nil { ... }
//
//	store, err := pgvector.New(db, pgvector.TableConfig{
//	    Table:          cfg.TableName,
//	    IDColumn:       "id",
//	    VectorColumn:   "embedding",
//	    TextColumn:     "content",
//	    MetadataColumn: "metadata",
//	})
func Migrate(ctx context.Context, db *sql.DB, cfg MigrateConfig) error {
	if cfg.Dims == 0 {
		return fmt.Errorf("pgvector: Migrate: Dims is required")
	}
	if cfg.TableName == "" {
		cfg.TableName = "goagent_embeddings"
	}
	if cfg.DistanceFunc.operator == "" {
		cfg.DistanceFunc = Cosine
	}
	if cfg.HNSWm == 0 {
		cfg.HNSWm = 16
	}
	if cfg.HNSWefConstruction == 0 {
		cfg.HNSWefConstruction = 64
	}

	stmts := []string{
		`CREATE EXTENSION IF NOT EXISTS vector`,
		fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
    id         TEXT PRIMARY KEY,
    embedding  vector(%d),
    content    TEXT NOT NULL DEFAULT '',
    metadata   JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`, cfg.TableName, cfg.Dims),
		fmt.Sprintf(`
CREATE INDEX IF NOT EXISTS %s_hnsw_idx
    ON %s
    USING hnsw (embedding %s)
    WITH (m = %d, ef_construction = %d)`,
			cfg.TableName, cfg.TableName, cfg.DistanceFunc.opsClass, cfg.HNSWm, cfg.HNSWefConstruction),
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("pgvector: migrate: %w", err)
		}
	}
	return nil
}
