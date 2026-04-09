package qdrant_test

import (
	"context"
	"fmt"
	"log"

	"github.com/qdrant/go-client/qdrant"

	"github.com/Germanblandin1/goagent"
	goagent_qdrant "github.com/Germanblandin1/goagent/memory/vector/qdrant"
)

func ExampleNew() {
	client, err := qdrant.NewClient(&qdrant.Config{
		Host: "localhost",
		Port: 6334,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	store, err := goagent_qdrant.New(client, goagent_qdrant.Config{
		CollectionName: "embeddings",
	})
	if err != nil {
		log.Fatal(err)
	}
	_ = store
	fmt.Println("store created")
}

func ExampleCreateCollection() {
	ctx := context.Background()
	client, err := qdrant.NewClient(&qdrant.Config{
		Host: "localhost",
		Port: 6334,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	cfg := goagent_qdrant.CollectionConfig{
		CollectionName: "goagent_embeddings",
		Dims:           1536, // match your embedding model (e.g. text-embedding-3-small)
	}
	if err := goagent_qdrant.CreateCollection(ctx, client, cfg); err != nil {
		log.Fatal(err)
	}

	store, err := goagent_qdrant.New(client, goagent_qdrant.Config{
		CollectionName: cfg.CollectionName,
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
// The filter is translated to Qdrant Must conditions on the "metadata.<key>"
// payload fields. All conditions use AND semantics. Filtering happens
// server-side before distance scoring — Qdrant never computes similarity for
// points that do not match the filter, so large collections stay fast.
//
// Scenario: a shared Qdrant collection for multiple teams. A query for the
// engineering team must not surface documents owned by other teams, even if
// they are semantically close.
func ExampleStore_Search_withFilter() {
	ctx := context.Background()
	client, err := qdrant.NewClient(&qdrant.Config{Host: "localhost", Port: 6334})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	store, err := goagent_qdrant.New(client, goagent_qdrant.Config{
		CollectionName: "goagent_embeddings",
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
// Both options are applied server-side: WithFilter via Qdrant's Must conditions
// and WithScoreThreshold via Qdrant's native score_threshold field. This means
// Qdrant evaluates filter → distance → threshold entirely before sending any
// data over the wire.
func ExampleStore_Search_withScoreThresholdAndFilter() {
	ctx := context.Background()
	client, err := qdrant.NewClient(&qdrant.Config{Host: "localhost", Port: 6334})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	store, err := goagent_qdrant.New(client, goagent_qdrant.Config{
		CollectionName: "goagent_embeddings",
	})
	if err != nil {
		log.Fatal(err)
	}

	queryVec := []float32{0.1, 0.88, 0.02}

	results, err := store.Search(ctx, queryVec, 10,
		goagent.WithFilter(map[string]any{"team": "engineering"}),
		goagent.WithScoreThreshold(0.80), // only results with ≥80% similarity
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("results above threshold: %d\n", len(results))
}
