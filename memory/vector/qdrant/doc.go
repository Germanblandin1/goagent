// Package qdrant implements goagent.VectorStore over Qdrant using the official
// gRPC client (github.com/qdrant/go-client).
//
// The caller supplies a *qdrant.Client and a CollectionName in Config —
// this package does not manage connection lifecycle. For a quick start without
// an existing collection, use CreateCollection.
//
// *Store does not directly satisfy goagent.VectorStore: its Search method
// accepts an optional ...SearchOption parameter that is not part of the
// interface. Wrap it with an adapter when a goagent.VectorStore is required.
package qdrant
