package sqlitevec

import (
	"context"
	"database/sql"
	"fmt"
)

// MigrateConfig configures the tables that Migrate will create.
// Use this when the caller has no existing schema and wants a quick start.
type MigrateConfig struct {
	// TableName is the base name for the two tables to create:
	//   TableName       — regular data table
	//   TableName+"_vec" — vec0 virtual table for vector search
	// Default: "goagent_embeddings".
	TableName string

	// Dims is the vector dimension. Required. Must match the embedding model
	// (e.g. 768 for nomic-embed-text, 1024 for voyage-3, 1536 for
	// text-embedding-3-small).
	Dims int

	// Metric is the distance metric that will be used with New.
	// Stored only for documentation — has no effect on the schema.
	// Default: L2.
	Metric DistanceMetric
}

// Migrate creates the data table and the vec0 virtual table if they do not
// exist. It is idempotent — safe to call on every application start.
//
// Two tables are created:
//   - TableName: id TEXT PK, content TEXT, metadata TEXT (JSON), created_at INTEGER
//   - TableName+"_vec": vec0 virtual table with an embedding float[Dims] column
//
// To use the created tables with New:
//
//	cfg := sqlitevec.MigrateConfig{TableName: "goagent_embeddings", Dims: 1536}
//	if err := sqlitevec.Migrate(ctx, db, cfg); err != nil { ... }
//
//	store, err := sqlitevec.New(db, sqlitevec.TableConfig{
//	    Table:          cfg.TableName,
//	    IDColumn:       "id",
//	    VectorColumn:   "embedding",
//	    TextColumn:     "content",
//	    MetadataColumn: "metadata",
//	})
func Migrate(ctx context.Context, db *sql.DB, cfg MigrateConfig) error {
	if cfg.Dims == 0 {
		return fmt.Errorf("sqlitevec: Migrate: Dims is required")
	}
	if cfg.TableName == "" {
		cfg.TableName = "goagent_embeddings"
	}
	if cfg.Metric == "" {
		cfg.Metric = L2
	}

	stmts := []string{
		fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
    id         TEXT    PRIMARY KEY,
    content    TEXT    NOT NULL DEFAULT '',
    metadata   TEXT    NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL DEFAULT (unixepoch())
)`, cfg.TableName),
		fmt.Sprintf(`
CREATE VIRTUAL TABLE IF NOT EXISTS %s_vec USING vec0(
    embedding float[%d]
)`, cfg.TableName, cfg.Dims),
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("sqlitevec: migrate: %w", err)
		}
	}
	return nil
}
