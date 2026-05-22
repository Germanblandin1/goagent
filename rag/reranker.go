package rag

import "context"

// Reranker takes a slice of retrieval results and reorders them by relevance
// to the original query, returning the best topK.
//
// Use with the over-fetch pattern: request more candidates from the store than
// needed, then let the reranker select the most relevant subset:
//
//	pipeline, _ := rag.NewPipeline(chunker, embedder, store,
//	    rag.WithReranker(reranker, 50), // fetches 50 from store, picks topK
//	)
//	results, err := pipeline.Search(ctx, query, 5)
//
// Invariants:
//   - The returned slice has at most min(topK, len(results)) elements.
//   - Order is descending by relevance (most relevant first).
//   - RerankScore is populated for all returned elements.
//   - Score (bi-encoder similarity) is not modified — the caller can compare both.
//
// If ctx is cancelled before completion, Rerank returns the context error wrapped with %w.
type Reranker interface {
	Rerank(ctx context.Context, query string, results []SearchResult, topK int) ([]SearchResult, error)
}
