// Package memory provides ShortTermMemory and LongTermMemory implementations
// for the goagent framework.
//
// The design separates two orthogonal concerns:
//
//   - Storage (memory/storage): where messages live — load and save the full slice.
//   - Policy (memory/policy): which messages the model sees — filter at read time.
//
// # Short-term memory
//
// ShortTermMemory maintains the active conversation context across Run calls.
// Construct one with NewShortTerm, combining any Storage with any Policy:
//
//	mem := memory.NewShortTerm(
//	    memory.WithStorage(storage.NewInMemory()),
//	    memory.WithPolicy(policy.NewTokenWindow(4096)),
//	)
//	agent := goagent.New(goagent.WithShortTermMemory(mem))
//
// # Long-term memory
//
// LongTermMemory retrieves semantically relevant messages across sessions
// using vector similarity search.
//
// This package does not ship a VectorStore or Embedder implementation.
// Both are interfaces defined in the root goagent package; the caller must
// supply concrete implementations (e.g. a pgvector client, a Chroma adapter,
// or any embedding API). NewLongTerm returns an error if either is missing.
//
//	mem, err := memory.NewLongTerm(
//	    memory.WithVectorStore(myStore),   // caller-provided
//	    memory.WithEmbedder(myEmbedder),   // caller-provided
//	)
package memory
