package voyage_test

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/providers/voyage"
)

// Example demonstrates creating an Embedder with the default client and embedding
// a text query. Requires the VOYAGE_API_KEY environment variable and network access.
// No Output: because the embedding vector is non-deterministic across model versions.
func Example() {
	key := os.Getenv("VOYAGE_API_KEY")
	if key == "" {
		return // skip silently when no key is present
	}

	embedder := voyage.NewEmbedder(
		voyage.WithEmbedModel("voyage-3"),
		voyage.WithInputType("query"),
	)
	vec, err := embedder.Embed(
		context.Background(),
		[]goagent.ContentBlock{goagent.TextBlock("What is Go?")},
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(vec) > 0)
}

// ExampleNewClient shows how to construct a VoyageClient with an explicit API key
// and a custom HTTP timeout.
func ExampleNewClient() {
	client := voyage.NewClient(
		voyage.WithAPIKey(os.Getenv("VOYAGE_API_KEY")),
		voyage.WithTimeout(10*time.Second),
	)
	_ = client
}

// ExampleNewEmbedder shows constructing an Embedder that reads the API key from
// the VOYAGE_API_KEY environment variable by default.
func ExampleNewEmbedder() {
	embedder := voyage.NewEmbedder(
		voyage.WithEmbedModel("voyage-3"),
		voyage.WithInputType("document"),
		voyage.WithMaxChars(20000),
	)
	_ = embedder
}

// ExampleNewEmbedderWithClient shows sharing a single VoyageClient between a
// document embedder and a query embedder to avoid redundant HTTP client allocation.
func ExampleNewEmbedderWithClient() {
	client := voyage.NewClient(
		voyage.WithHTTPClient(&http.Client{Timeout: 30 * time.Second}),
	)

	docEmbedder := voyage.NewEmbedderWithClient(client,
		voyage.WithEmbedModel("voyage-3"),
		voyage.WithInputType("document"),
	)
	queryEmbedder := voyage.NewEmbedderWithClient(client,
		voyage.WithEmbedModel("voyage-3"),
		voyage.WithInputType("query"),
	)
	_, _ = docEmbedder, queryEmbedder
}
