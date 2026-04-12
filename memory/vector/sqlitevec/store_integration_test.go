package sqlitevec_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector/sqlitevec"
)

// openDB opens an in-memory SQLite database for tests.
// MaxOpenConns is set to 1 so all queries share the same connection — required
// for in-memory SQLite where each connection sees its own empty database.
func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlitevec.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	db.SetMaxOpenConns(1)
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
// created tables.
func migrateAndConfig(t *testing.T, db *sql.DB, dims int) sqlitevec.TableConfig {
	t.Helper()
	tableName := tableNameFor(t)
	cfg := sqlitevec.MigrateConfig{TableName: tableName, Dims: dims}
	if err := sqlitevec.Migrate(context.Background(), db, cfg); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DROP TABLE IF EXISTS " + tableName + "_vec") //nolint:errcheck
		db.Exec("DROP TABLE IF EXISTS " + tableName)          //nolint:errcheck
	})
	return sqlitevec.TableConfig{
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
	store, err := sqlitevec.New(db, tcfg)
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
	store, err := sqlitevec.New(db, tcfg)
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
	store, err := sqlitevec.New(db, tcfg)
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
	store, err := sqlitevec.New(db, tcfg)
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
	store, err := sqlitevec.New(db, tcfg)
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
	store, err := sqlitevec.New(db, tcfg)
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
	tableName := tableNameFor(t)
	cfg := sqlitevec.MigrateConfig{TableName: tableName, Dims: 3}
	ctx := context.Background()
	t.Cleanup(func() {
		db.Exec("DROP TABLE IF EXISTS " + tableName + "_vec") //nolint:errcheck
		db.Exec("DROP TABLE IF EXISTS " + tableName)          //nolint:errcheck
	})

	if err := sqlitevec.Migrate(ctx, db, cfg); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if err := sqlitevec.Migrate(ctx, db, cfg); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
}

func TestFullRoundtrip(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()

	tableName := tableNameFor(t)
	cfg := sqlitevec.MigrateConfig{TableName: tableName, Dims: 3}
	t.Cleanup(func() {
		db.Exec("DROP TABLE IF EXISTS " + tableName + "_vec") //nolint:errcheck
		db.Exec("DROP TABLE IF EXISTS " + tableName)          //nolint:errcheck
	})

	if err := sqlitevec.Migrate(ctx, db, cfg); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	store, err := sqlitevec.New(db, sqlitevec.TableConfig{
		Table:          tableName,
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
		t.Errorf("top result should score ~1.0 for identical vector, got %f", results[0].Score)
	}
	if results[0].Message.Metadata["src"] != "a.md" {
		t.Errorf("top result metadata: want src=a.md, got %v", results[0].Message.Metadata)
	}
	if results[0].Message.Role != goagent.RoleDocument {
		t.Errorf("want RoleDocument, got %v", results[0].Message.Role)
	}
}

func TestCount_EmptyStore(t *testing.T) {
	db := openDB(t)
	tcfg := migrateAndConfig(t, db, 3)
	store, err := sqlitevec.New(db, tcfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	n, err := store.Count(context.Background())
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 0 {
		t.Errorf("want 0, got %d", n)
	}
}

func TestCount_AfterUpserts(t *testing.T) {
	db := openDB(t)
	tcfg := migrateAndConfig(t, db, 3)
	store, err := sqlitevec.New(db, tcfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	for _, id := range []string{"a", "b", "c"} {
		if err := store.Upsert(ctx, id, vec(1, 0, 0), goagent.UserMessage(id)); err != nil {
			t.Fatalf("Upsert %s: %v", id, err)
		}
	}
	n, err := store.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 3 {
		t.Errorf("want 3, got %d", n)
	}
}

func TestCount_WithFilter_MetadataColumn(t *testing.T) {
	db := openDB(t)
	tcfg := migrateAndConfig(t, db, 3)
	store, err := sqlitevec.New(db, tcfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	data := []struct {
		id   string
		meta map[string]any
	}{
		{"a", map[string]any{"env": "prod"}},
		{"b", map[string]any{"env": "dev"}},
		{"c", map[string]any{"env": "prod"}},
	}
	for _, d := range data {
		msg := goagent.Message{
			Role:     goagent.RoleUser,
			Content:  []goagent.ContentBlock{goagent.TextBlock(d.id)},
			Metadata: d.meta,
		}
		if err := store.Upsert(ctx, d.id, vec(1, 0, 0), msg); err != nil {
			t.Fatalf("Upsert %s: %v", d.id, err)
		}
	}

	n, err := store.Count(ctx, goagent.WithFilter(map[string]any{"env": "prod"}))
	if err != nil {
		t.Fatalf("Count with filter: %v", err)
	}
	if n != 2 {
		t.Errorf("want 2, got %d", n)
	}
}

func TestCount_WithFilter_NoMetadataColumn(t *testing.T) {
	db := openDB(t)
	tcfg := migrateAndConfig(t, db, 3)
	tcfg.MetadataColumn = "" // disable metadata column
	store, err := sqlitevec.New(db, tcfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	for _, id := range []string{"a", "b"} {
		if err := store.Upsert(ctx, id, vec(1, 0, 0), goagent.UserMessage(id)); err != nil {
			t.Fatalf("Upsert %s: %v", id, err)
		}
	}

	// Filter is silently ignored when MetadataColumn is not configured.
	n, err := store.Count(ctx, goagent.WithFilter(map[string]any{"env": "prod"}))
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 2 {
		t.Errorf("want 2 (filter ignored), got %d", n)
	}
}

func TestBulkUpsert_StoreAndSearch(t *testing.T) {
	db := openDB(t)
	tcfg := migrateAndConfig(t, db, 3)
	store, err := sqlitevec.New(db, tcfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	entries := []goagent.UpsertEntry{
		{ID: "a", Vector: vec(1, 0, 0), Message: goagent.UserMessage("alpha")},
		{ID: "b", Vector: vec(0, 1, 0), Message: goagent.UserMessage("beta")},
		{ID: "c", Vector: vec(0, 0, 1), Message: goagent.UserMessage("gamma")},
	}
	if err := store.BulkUpsert(ctx, entries); err != nil {
		t.Fatalf("BulkUpsert: %v", err)
	}

	results, err := store.Search(ctx, vec(1, 0, 0), 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	if got := results[0].Message.TextContent(); got != "alpha" {
		t.Errorf("top result = %q, want %q", got, "alpha")
	}
}

func TestBulkUpsert_WithMetadata(t *testing.T) {
	db := openDB(t)
	tcfg := migrateAndConfig(t, db, 3)
	store, err := sqlitevec.New(db, tcfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	entries := []goagent.UpsertEntry{
		{
			ID:     "a",
			Vector: vec(1, 0, 0),
			Message: goagent.Message{
				Role:     goagent.RoleUser,
				Content:  []goagent.ContentBlock{goagent.TextBlock("alpha")},
				Metadata: map[string]any{"src": "a.md"},
			},
		},
	}
	if err := store.BulkUpsert(ctx, entries); err != nil {
		t.Fatalf("BulkUpsert: %v", err)
	}

	results, err := store.Search(ctx, vec(1, 0, 0), 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results")
	}
	if results[0].Message.Metadata["src"] != "a.md" {
		t.Errorf("metadata src = %v, want a.md", results[0].Message.Metadata["src"])
	}
}

func TestBulkDelete_RemovesFromSearch(t *testing.T) {
	db := openDB(t)
	tcfg := migrateAndConfig(t, db, 3)
	store, err := sqlitevec.New(db, tcfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	for _, id := range []string{"a", "b", "c"} {
		if err := store.Upsert(ctx, id, vec(1, 0, 0), goagent.UserMessage(id)); err != nil {
			t.Fatalf("Upsert %s: %v", id, err)
		}
	}

	if err := store.BulkDelete(ctx, []string{"a", "b"}); err != nil {
		t.Fatalf("BulkDelete: %v", err)
	}

	results, err := store.Search(ctx, vec(1, 0, 0), 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("want 1 result after bulk delete, got %d", len(results))
	}
	if len(results) == 1 && results[0].Message.TextContent() != "c" {
		t.Errorf("remaining doc = %q, want %q", results[0].Message.TextContent(), "c")
	}
}

func TestBulkUpsert_EmptyIsNoop(t *testing.T) {
	db := openDB(t)
	tcfg := migrateAndConfig(t, db, 3)
	store, err := sqlitevec.New(db, tcfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := store.BulkUpsert(context.Background(), nil); err != nil {
		t.Errorf("BulkUpsert(nil) = %v, want nil", err)
	}
}

func TestBulkDelete_EmptyIsNoop(t *testing.T) {
	db := openDB(t)
	tcfg := migrateAndConfig(t, db, 3)
	store, err := sqlitevec.New(db, tcfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := store.BulkDelete(context.Background(), nil); err != nil {
		t.Errorf("BulkDelete(nil) = %v, want nil", err)
	}
}

func TestSearch_WithFilter_Metadata(t *testing.T) {
	db := openDB(t)
	tcfg := migrateAndConfig(t, db, 3)
	store, err := sqlitevec.New(db, tcfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx := context.Background()
	data := []struct {
		id  string
		env string
	}{
		{"a", "prod"},
		{"b", "dev"},
		{"c", "prod"},
	}
	for _, d := range data {
		msg := goagent.Message{
			Role:     goagent.RoleUser,
			Content:  []goagent.ContentBlock{goagent.TextBlock(d.id)},
			Metadata: map[string]any{"env": d.env},
		}
		if err := store.Upsert(ctx, d.id, vec(1, 0, 0), msg); err != nil {
			t.Fatalf("Upsert %s: %v", d.id, err)
		}
	}

	results, err := store.Search(ctx, vec(1, 0, 0), 10, goagent.WithFilter(map[string]any{"env": "prod"}))
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("want 2 results matching env=prod, got %d", len(results))
	}
}

func TestWithDistanceMetric_Cosine(t *testing.T) {
	db := openDB(t)
	tcfg := migrateAndConfig(t, db, 3)
	store, err := sqlitevec.New(db, tcfg, sqlitevec.WithDistanceMetric(sqlitevec.Cosine))
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
	// Cosine score for identical vectors: 1 - cosine_distance ≈ 1 - 0 = 1.
	if results[0].Score < 0.99 {
		t.Errorf("expected score ~1.0 for identical vector with Cosine, got %f", results[0].Score)
	}
}

func TestRegister_IsIdempotent(t *testing.T) {
	// Register is safe to call multiple times — it must not panic.
	sqlitevec.Register()
	sqlitevec.Register()
}
