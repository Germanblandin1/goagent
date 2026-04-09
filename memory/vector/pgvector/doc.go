// Package pgvector implements goagent.VectorStore over PostgreSQL with the
// pgvector extension.
//
// The caller describes their existing table via TableConfig — this package does
// not impose any schema. For a quick start without an existing table, use Migrate.
//
// # Metadata filtering
//
// When MetadataColumn is set to a JSONB column, Search supports
// [goagent.WithFilter] to restrict results server-side using PostgreSQL's JSONB
// containment operator (@>). All key-value pairs in the filter map must be
// present in the stored metadata (AND semantics).
//
// For best performance on large tables, create a GIN index on the metadata column:
//
//	CREATE INDEX ON embeddings USING gin(metadata jsonb_path_ops);
//
// Without the index, PostgreSQL falls back to a sequential scan. For tables
// under ~100k rows, the sequential scan is typically fast enough.
//
// # Score threshold
//
// [goagent.WithScoreThreshold] is applied in Go after the database returns
// results. topK is applied by the database first, so a selective threshold may
// yield fewer than topK results.
package pgvector
