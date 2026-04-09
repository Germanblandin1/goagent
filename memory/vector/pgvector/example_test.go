package pgvector_test

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector/pgvector"
)

func ExampleNew() {
	db, err := sql.Open("pgx", "postgres://user:pass@localhost/mydb")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	store, err := pgvector.New(db, pgvector.TableConfig{
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
	db, err := sql.Open("pgx", "postgres://user:pass@localhost/mydb")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	cfg := pgvector.MigrateConfig{
		TableName: "goagent_embeddings",
		Dims:      1536, // match your embedding model (e.g. text-embedding-3-small)
	}
	if err := pgvector.Migrate(ctx, db, cfg); err != nil {
		log.Fatal(err)
	}

	store, err := pgvector.New(db, pgvector.TableConfig{
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
// The filter is applied server-side with PostgreSQL's JSONB containment
// operator (metadata @> filter::jsonb), so only rows whose metadata contains
// all specified key-value pairs are candidates for similarity ranking.
//
// Scenario: a shared embedding table used by multiple teams. Each document is
// tagged with "team" and "language". A query for the engineering team must not
// surface documents owned by other teams, even if they are semantically close.
//
// For large tables, create a GIN index to make the JSONB filter index-assisted:
//
//	CREATE INDEX ON goagent_embeddings USING gin(metadata jsonb_path_ops);
func ExampleStore_Search_withFilter() {
	ctx := context.Background()
	db, err := sql.Open("pgx", "postgres://user:pass@localhost/mydb")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	store, err := pgvector.New(db, pgvector.TableConfig{
		Table:          "goagent_embeddings",
		IDColumn:       "id",
		VectorColumn:   "embedding",
		TextColumn:     "content",
		MetadataColumn: "metadata", // required for WithFilter
	})
	if err != nil {
		log.Fatal(err)
	}

	// Upsert documents with team and language metadata.
	// In production this step runs at index time, not at query time.
	docs := []struct {
		id   string
		vec  []float32
		text string
		meta map[string]any
	}{
		{
			id:   "eng-go-001",
			vec:  []float32{0.1, 0.9, 0.0},
			text: "Error handling patterns in Go use explicit return values.",
			meta: map[string]any{"team": "engineering", "language": "go"},
		},
		{
			id:   "eng-py-001",
			vec:  []float32{0.1, 0.85, 0.05},
			text: "Python exceptions propagate up the call stack automatically.",
			meta: map[string]any{"team": "engineering", "language": "python"},
		},
		{
			id:   "mkt-001",
			vec:  []float32{0.15, 0.8, 0.1},
			text: "Our error-free delivery guarantee is central to our brand.",
			meta: map[string]any{"team": "marketing", "language": "en"},
		},
	}
	for _, d := range docs {
		msg := goagent.Message{
			Role:     goagent.RoleDocument,
			Content:  []goagent.ContentBlock{goagent.TextBlock(d.text)},
			Metadata: d.meta,
		}
		if err := store.Upsert(ctx, d.id, d.vec, msg); err != nil {
			log.Fatal(err)
		}
	}

	// Query embedding for "how do I handle errors".
	queryVec := []float32{0.1, 0.88, 0.02}

	// Without filter: all three documents are candidates.
	// With filter: only the Go engineering document qualifies, even though the
	// marketing document has a similar embedding.
	results, err := store.Search(ctx, queryVec, 5,
		goagent.WithFilter(map[string]any{
			"team":     "engineering",
			"language": "go",
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	for _, r := range results {
		fmt.Printf("score=%.2f team=%s text=%s\n",
			r.Score,
			r.Message.Metadata["team"],
			r.Message.TextContent(),
		)
	}
}

// ExampleStore_Search_withScoreThresholdAndFilter demonstrates combining a
// score threshold with a metadata filter.
//
// The threshold is applied after the database returns topK results. The filter
// is applied server-side before ranking. Together they let you express:
// "give me the top 10 engineering docs, but only if they are at least 80%
// similar to my query".
func ExampleStore_Search_withScoreThresholdAndFilter() {
	ctx := context.Background()
	db, err := sql.Open("pgx", "postgres://user:pass@localhost/mydb")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	store, err := pgvector.New(db, pgvector.TableConfig{
		Table:          "goagent_embeddings",
		IDColumn:       "id",
		VectorColumn:   "embedding",
		TextColumn:     "content",
		MetadataColumn: "metadata",
	})
	if err != nil {
		log.Fatal(err)
	}

	queryVec := []float32{0.1, 0.88, 0.02}

	results, err := store.Search(ctx, queryVec, 10,
		goagent.WithFilter(map[string]any{"team": "engineering"}),
		goagent.WithScoreThreshold(0.80), // discard results below 80% similarity
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("results above threshold: %d\n", len(results))
}
