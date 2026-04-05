//go:build integration

package pgvector_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector/pgvector"
)

// openDB opens a *sql.DB from the PGVECTOR_TEST_DSN environment variable.
// Skips the test if the variable is not set.
func openDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("PGVECTOR_TEST_DSN")
	if dsn == "" {
		t.Skip("PGVECTOR_TEST_DSN not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// tableNameFor returns a sanitized table name unique to the given test.
func tableNameFor(t *testing.T) string {
	t.Helper()
	raw := "test_" + t.Name()
	b := make([]byte, len(raw))
	for i := range raw {
		c := raw[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			b[i] = c
		} else {
			b[i] = '_'
		}
	}
	return string(b)
}

// migrateAndConfig runs Migrate and returns a TableConfig pointing at the
// created table.
func migrateAndConfig(t *testing.T, db *sql.DB, dims int) pgvector.TableConfig {
	t.Helper()
	tableName := tableNameFor(t)
	cfg := pgvector.MigrateConfig{TableName: tableName, Dims: dims}
	if err := pgvector.Migrate(context.Background(), db, cfg); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DROP TABLE IF EXISTS " + tableName) //nolint:errcheck
	})
	return pgvector.TableConfig{
		Table:          tableName,
		IDColumn:       "id",
		VectorColumn:   "embedding",
		TextColumn:     "content",
		MetadataColumn: "metadata",
	}
}

func vec(vals ...float32) []float32 { return vals }

func TestUpsert_IdempotentSameID(t *testing.T) {
	db := openDB(t)
	tcfg := migrateAndConfig(t, db, 3)
	store, err := pgvector.New(db, tcfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	v := vec(1, 0, 0)
	if err := store.Upsert(ctx, "doc1", v, goagent.UserMessage("first")); err != nil {
		t.Fatalf("Upsert 1: %v", err)
	}
	if err := store.Upsert(ctx, "doc1", v, goagent.UserMessage("second")); err != nil {
		t.Fatalf("Upsert 2: %v", err)
	}

	results, err := store.Search(ctx, v, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	if got := results[0].Message.TextContent(); got != "second" {
		t.Errorf("want text %q, got %q", "second", got)
	}
}

func TestSearch_ReturnsTopK(t *testing.T) {
	db := openDB(t)
	tcfg := migrateAndConfig(t, db, 3)
	store, err := pgvector.New(db, tcfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		v := vec(float32(i+1), 0, 0)
		if err := store.Upsert(ctx, string(rune('a'+i)), v, goagent.UserMessage("doc")); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}

	results, err := store.Search(ctx, vec(1, 0, 0), 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("want 3 results, got %d", len(results))
	}
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted by score descending at index %d", i)
		}
	}
}

func TestSearch_ScoreIsHighForSimilarVectors(t *testing.T) {
	db := openDB(t)
	tcfg := migrateAndConfig(t, db, 3)
	store, err := pgvector.New(db, tcfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	v := vec(1, 0, 0)
	if err := store.Upsert(ctx, "doc1", v, goagent.UserMessage("text")); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	results, err := store.Search(ctx, v, 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results")
	}
	if results[0].Score < 0.99 {
		t.Errorf("expected score ~1.0 for identical vector, got %f", results[0].Score)
	}
}

func TestSearch_WithoutMetadataColumn(t *testing.T) {
	db := openDB(t)
	tcfg := migrateAndConfig(t, db, 3)
	tcfg.MetadataColumn = ""
	store, err := pgvector.New(db, tcfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	v := vec(1, 0, 0)
	if err := store.Upsert(ctx, "doc1", v, goagent.UserMessage("hello")); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	results, err := store.Search(ctx, v, 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results")
	}
	if results[0].Message.Metadata != nil {
		t.Error("expected nil Metadata when MetadataColumn is empty")
	}
}

func TestSearch_WithMetadataColumn(t *testing.T) {
	db := openDB(t)
	tcfg := migrateAndConfig(t, db, 3)
	store, err := pgvector.New(db, tcfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	v := vec(1, 0, 0)
	msg := goagent.Message{
		Role:     goagent.RoleUser,
		Content:  []goagent.ContentBlock{goagent.TextBlock("hello")},
		Metadata: map[string]any{"source": "test.md", "page": float64(3)},
	}
	if err := store.Upsert(ctx, "doc1", v, msg); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	results, err := store.Search(ctx, v, 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results")
	}
	meta := results[0].Message.Metadata
	if meta == nil {
		t.Fatal("expected non-nil Metadata")
	}
	if meta["source"] != "test.md" {
		t.Errorf("metadata source: want %q, got %v", "test.md", meta["source"])
	}
}

func TestDelete_RemovesFromSearch(t *testing.T) {
	db := openDB(t)
	tcfg := migrateAndConfig(t, db, 3)
	store, err := pgvector.New(db, tcfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	v := vec(1, 0, 0)
	if err := store.Upsert(ctx, "doc1", v, goagent.UserMessage("text")); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := store.Delete(ctx, "doc1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	results, err := store.Search(ctx, v, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("want 0 results after delete, got %d", len(results))
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	db := openDB(t)
	cfg := pgvector.MigrateConfig{TableName: "test_idempotent", Dims: 3}
	ctx := context.Background()
	t.Cleanup(func() { db.Exec("DROP TABLE IF EXISTS test_idempotent") }) //nolint:errcheck

	if err := pgvector.Migrate(ctx, db, cfg); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if err := pgvector.Migrate(ctx, db, cfg); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
}

func TestFullRoundtrip(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()

	cfg := pgvector.MigrateConfig{TableName: tableNameFor(t), Dims: 3}
	t.Cleanup(func() { db.Exec("DROP TABLE IF EXISTS " + cfg.TableName) }) //nolint:errcheck

	if err := pgvector.Migrate(ctx, db, cfg); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	store, err := pgvector.New(db, pgvector.TableConfig{
		Table:          cfg.TableName,
		IDColumn:       "id",
		VectorColumn:   "embedding",
		TextColumn:     "content",
		MetadataColumn: "metadata",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	docs := []struct {
		id   string
		vec  []float32
		text string
		meta map[string]any
	}{
		{"a", vec(1, 0, 0), "document A", map[string]any{"src": "a.md"}},
		{"b", vec(0, 1, 0), "document B", map[string]any{"src": "b.md"}},
		{"c", vec(0, 0, 1), "document C", map[string]any{"src": "c.md"}},
	}
	for _, d := range docs {
		msg := goagent.Message{
			Role:     goagent.RoleUser,
			Content:  []goagent.ContentBlock{goagent.TextBlock(d.text)},
			Metadata: d.meta,
		}
		if err := store.Upsert(ctx, d.id, d.vec, msg); err != nil {
			t.Fatalf("Upsert %s: %v", d.id, err)
		}
	}

	results, err := store.Search(ctx, vec(1, 0, 0), 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	if results[0].Score < 0.99 {
		t.Errorf("top result should be ~1.0 for identical vector, got %f", results[0].Score)
	}
	if results[0].Message.Metadata["src"] != "a.md" {
		t.Errorf("top result metadata: want src=a.md, got %v", results[0].Message.Metadata)
	}
	if results[0].Message.Role != goagent.RoleDocument {
		t.Errorf("want RoleDocument, got %v", results[0].Message.Role)
	}
}
