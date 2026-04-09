// Package qdrant implements goagent.VectorStore over Qdrant using the official
// gRPC client (github.com/qdrant/go-client).
//
// The caller supplies a *qdrant.Client and a CollectionName in Config —
// this package does not manage connection lifecycle. For a quick start without
// an existing collection, use CreateCollection.
//
// # Metadata filtering
//
// Search supports [goagent.WithFilter] via Qdrant's native filter API.
// Each key-value pair in the filter map becomes a Must condition on the
// "metadata.<key>" payload field. Filtering happens server-side before
// distance scoring, so it does not affect query latency for large collections.
//
// Supported value types: string, bool, int64, and float64 whole numbers
// (JSON numbers always unmarshal as float64). Fractional floats and nested
// maps are silently skipped.
//
// # Score threshold
//
// [goagent.WithScoreThreshold] is forwarded to Qdrant's native
// score_threshold field, so filtering also happens server-side.
// Both options can be combined in a single query.
package qdrant
