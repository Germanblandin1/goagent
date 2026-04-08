# goagent/memory/vector/qdrant

Qdrant vector store for [goagent](https://github.com/Germanblandin1/goagent).

Implements `goagent.VectorStore` over [Qdrant](https://qdrant.tech) using the
official gRPC client. Supports cosine, Euclidean, and dot-product distance
functions with HNSW indexing.

## Installation

```bash
go get github.com/Germanblandin1/goagent/memory/vector/qdrant
```

## Quick start

```go
import (
    "context"
    "log"

    "github.com/qdrant/go-client/qdrant"
    goagent_qdrant "github.com/Germanblandin1/goagent/memory/vector/qdrant"
    "github.com/Germanblandin1/goagent"
)

func main() {
    ctx := context.Background()

    client, err := qdrant.NewClient(&qdrant.Config{Host: "localhost", Port: 6334})
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // Create the collection (idempotent — safe to call on every startup).
    err = goagent_qdrant.CreateCollection(ctx, client, goagent_qdrant.CollectionConfig{
        CollectionName: "goagent_embeddings",
        Dims:           1536, // match your embedding model
    })
    if err != nil {
        log.Fatal(err)
    }

    store, err := goagent_qdrant.New(client, goagent_qdrant.Config{
        CollectionName: "goagent_embeddings",
    })
    if err != nil {
        log.Fatal(err)
    }

    vec := []float32{ /* ... 1536 floats from your embedding model ... */ }
    msg := goagent.Message{
        Role:     goagent.RoleDocument,
        Content:  []goagent.ContentBlock{goagent.TextBlock("My document text")},
        Metadata: map[string]any{"source": "readme.md"},
    }
    if err := store.Upsert(ctx, "doc-1", vec, msg); err != nil {
        log.Fatal(err)
    }

    query := []float32{ /* ... query embedding ... */ }
    results, err := store.Search(ctx, query, 5)
    if err != nil {
        log.Fatal(err)
    }
    for _, r := range results {
        _ = r.Score   // similarity score, higher = more similar
        _ = r.Message // goagent.Message with RoleDocument
    }
}
```

## Distance functions

| Constant | Qdrant distance | Score range         | Notes                                               |
|----------|----------------|---------------------|-----------------------------------------------------|
| `Cosine` | Cosine         | [0, 1] (normalized) | Default. For normalized embedding models.           |
| `Euclid` | Euclid         | (0, 1]              | `1/(1+d)`. For un-normalized vectors.               |
| `Dot`    | Dot            | positive            | Raw dot product. Equivalent to Cosine for unit vectors. |

The distance function must match the one configured in the Qdrant collection.

```go
store, err := goagent_qdrant.New(client, cfg,
    goagent_qdrant.WithDistanceFunc(goagent_qdrant.Euclid),
)
```

## Running integration tests

The integration tests require a running Qdrant instance (gRPC port 6334).

```bash
export QDRANT_TEST_ADDR=localhost:6334
go test -tags integration -race ./memory/vector/qdrant/...
```

If `QDRANT_TEST_ADDR` is not set the integration tests are skipped automatically.

## Documentation

- [pkg.go.dev](https://pkg.go.dev/github.com/Germanblandin1/goagent/memory/vector/qdrant)
- [Root module](https://pkg.go.dev/github.com/Germanblandin1/goagent)
- [Qdrant documentation](https://qdrant.tech/documentation/)
