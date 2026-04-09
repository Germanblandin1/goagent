package sqlitevec_test

import (
	"context"
	"fmt"
	"log"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector/sqlitevec"
)

func ExampleNew() {
	db, err := sqlitevec.Open("/path/to/mydb.sqlite")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	store, err := sqlitevec.New(db, sqlitevec.TableConfig{
		Table:          "embeddings",
		IDColumn:       "id",
		VectorColumn:   "embedding",
		TextColumn:     "content",
		MetadataColumn: "metadata", // optional; omit if your table has no metadata column
	})
	if err != nil {
		log.Fatal(err)
	}
	_ = store
	fmt.Println("store created")
}

func ExampleMigrate() {
	ctx := context.Background()
	db, err := sqlitevec.Open("/path/to/mydb.sqlite")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	cfg := sqlitevec.MigrateConfig{
		TableName: "goagent_embeddings",
		Dims:      1536, // match your embedding model (e.g. text-embedding-3-small)
	}
	if err := sqlitevec.Migrate(ctx, db, cfg); err != nil {
		log.Fatal(err)
	}

	store, err := sqlitevec.New(db, sqlitevec.TableConfig{
		Table:          cfg.TableName,
		IDColumn:       "id",
		VectorColumn:   "embedding",
		TextColumn:     "content",
		MetadataColumn: "metadata",
	})
	if err != nil {
		log.Fatal(err)
	}
	_ = store
	fmt.Println("store ready")
}

// ExampleStore_Search_withFilter demonstrates metadata filtering using
// [goagent.WithFilter].
//
// For sqlitevec, filtering is applied in Go after sqlite-vec returns the
// topK nearest neighbours. This is appropriate for sqlitevec's typical scale
// (embedded, local, < 100k entries) where the post-filter overhead is
// negligible. Requires MetadataColumn to be set in TableConfig.
//
// Scenario: a local agent that indexes documents from multiple projects.
// A query for "project-alpha" must not surface documents from other projects,
// even if they are semantically similar.
func ExampleStore_Search_withFilter() {
	ctx := context.Background()
	db, err := sqlitevec.Open(":memory:") // in-memory SQLite for the example
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := sqlitevec.Migrate(ctx, db, sqlitevec.MigrateConfig{
		TableName: "embeddings",
		Dims:      3,
	}); err != nil {
		log.Fatal(err)
	}

	store, err := sqlitevec.New(db, sqlitevec.TableConfig{
		Table:          "embeddings",
		IDColumn:       "id",
		VectorColumn:   "embedding",
		TextColumn:     "content",
		MetadataColumn: "metadata", // required for WithFilter
	})
	if err != nil {
		log.Fatal(err)
	}

	// Upsert documents tagged with a project name.
	// In production this step runs at index time, not at query time.
	docs := []struct {
		id      string
		vec     []float32
		text    string
		project string
	}{
		{"alpha-001", []float32{1, 0, 0}, "Alpha: deployment checklist", "alpha"},
		{"alpha-002", []float32{0.9, 0.1, 0}, "Alpha: rollback procedure", "alpha"},
		{"beta-001", []float32{0.95, 0.05, 0}, "Beta: deployment guide", "beta"},
	}
	for _, d := range docs {
		msg := goagent.Message{
			Role:     goagent.RoleDocument,
			Content:  []goagent.ContentBlock{goagent.TextBlock(d.text)},
			Metadata: map[string]any{"project": d.project},
		}
		if err := store.Upsert(ctx, d.id, d.vec, msg); err != nil {
			log.Fatal(err)
		}
	}

	queryVec := []float32{1, 0, 0}

	// Without filter: "Beta: deployment guide" would rank highly because its
	// vector is almost identical to the alpha docs.
	// With filter: only alpha documents qualify.
	results, err := store.Search(ctx, queryVec, 5,
		goagent.WithFilter(map[string]any{"project": "alpha"}),
	)
	if err != nil {
		log.Fatal(err)
	}

	for _, r := range results {
		fmt.Printf("project=%s text=%s\n",
			r.Message.Metadata["project"],
			r.Message.TextContent(),
		)
	}
}

// ExampleStore_Search_withScoreThresholdAndFilter demonstrates combining a
// score threshold with a metadata filter.
//
// Both are applied in Go post-query: the threshold discards results below the
// minimum similarity, and the filter discards results whose metadata does not
// match. The order of application is: ScoreThreshold first, then Filter.
// Both options can yield fewer than topK results.
func ExampleStore_Search_withScoreThresholdAndFilter() {
	ctx := context.Background()
	db, err := sqlitevec.Open(":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := sqlitevec.Migrate(ctx, db, sqlitevec.MigrateConfig{
		TableName: "embeddings",
		Dims:      3,
	}); err != nil {
		log.Fatal(err)
	}

	store, err := sqlitevec.New(db, sqlitevec.TableConfig{
		Table:          "embeddings",
		IDColumn:       "id",
		VectorColumn:   "embedding",
		TextColumn:     "content",
		MetadataColumn: "metadata",
	})
	if err != nil {
		log.Fatal(err)
	}

	queryVec := []float32{1, 0, 0}

	results, err := store.Search(ctx, queryVec, 10,
		goagent.WithFilter(map[string]any{"project": "alpha"}),
		goagent.WithScoreThreshold(0.90), // only results with ≥90% similarity
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("results above threshold: %d\n", len(results))
}
