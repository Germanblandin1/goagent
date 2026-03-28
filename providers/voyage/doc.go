// Package voyage provides a [goagent.Embedder] backed by the Voyage AI
// embeddings API — the embedding service recommended by Anthropic for use
// with Claude.
//
// # Authentication
//
// By default the client reads the API key from the VOYAGE_API_KEY environment
// variable. To supply it explicitly, create a client with [NewClient]:
//
//	client := voyage.NewClient(voyage.WithAPIKey("pa-..."))
//	embedder := voyage.NewEmbedderWithClient(client,
//	    voyage.WithEmbedModel("voyage-3"),
//	)
//
// # Shared client
//
// [VoyageClient] holds the underlying HTTP client and API key. Create one with
// [NewClient] to share transport settings across multiple embedders or to
// target a custom base URL (e.g. a test server):
//
//	client := voyage.NewClient(
//	    voyage.WithAPIKey("pa-..."),
//	    voyage.WithBaseURL("http://localhost:9090"),
//	)
//
// # Models
//
// Commonly used embedding models:
//
//   - "voyage-3"       — high-quality general-purpose embeddings (default)
//   - "voyage-3-lite"  — optimised for latency and cost
//   - "voyage-finance-2"  — finance domain
//   - "voyage-code-3"    — code retrieval
//
// See https://docs.voyageai.com/docs/embeddings for the full list.
//
// # Input type
//
// Set [WithInputType] to "document" when embedding corpus entries and to
// "query" when embedding search queries. Omit it (default) for symmetric
// similarity tasks (e.g. clustering, deduplication).
//
// # Usage
//
//	embedder := voyage.NewEmbedder(
//	    voyage.WithEmbedModel("voyage-3"),
//	    voyage.WithInputType("document"),
//	)
//	vec, err := embedder.Embed(ctx, []goagent.ContentBlock{
//	    goagent.TextBlock("some document text"),
//	})
package voyage
