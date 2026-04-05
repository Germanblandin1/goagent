// Package vector provides embeddings, chunking, similarity search, and an
// in-process vector store for goagent's long-term memory subsystem.
//
// # Overview
//
// The package is built around four orthogonal concerns:
//
//  1. Embedding — converting message content into dense float32 vectors.
//  2. Chunking — splitting content into pieces that fit a model's context window.
//  3. Similarity — measuring how close two vectors are in semantic space.
//  4. Storage — persisting (id, vector, message) triples and searching by similarity.
//
// # Embedders
//
// OllamaEmbedder calls a local Ollama server and supports any text embedding
// model (e.g. "nomic-embed-text"):
//
//	e := vector.NewOllamaEmbedder("nomic-embed-text")
//
// FallbackEmbedder filters blocks by type before delegating to a primary
// embedder, enabling text-only embedders to gracefully handle multimodal input:
//
//	fe := vector.NewFallbackEmbedder(
//	    vector.NewOllamaEmbedder("nomic-embed-text"),
//	    vector.WithSupportedType(goagent.ContentText),
//	)
//
// # Chunkers
//
// NoOpChunker (default) passes content through as a single chunk:
//
//	c := vector.NewNoOpChunker()
//
// TextChunker splits text at word boundaries with configurable overlap:
//
//	c := vector.NewTextChunker(
//	    vector.WithMaxSize(500),
//	    vector.WithOverlap(50),
//	)
//
// SentenceChunker splits text at sentence boundaries, grouping complete
// sentences until the size limit is reached. Overlap is counted in sentences,
// not tokens, which keeps adjacent chunks semantically coherent:
//
//	c := vector.NewSentenceChunker(
//	    vector.WithSentenceMaxSize(300),
//	    vector.WithSentenceOverlap(2),
//	)
//
// BlockChunker processes mixed content (text, images, PDFs) per block:
//
//	c := vector.NewBlockChunker(vector.WithPDFExtractor(myExtractor))
//
// # Estimators
//
// Estimators measure text size in model-specific units for the Chunker:
//
//	&vector.HeuristicTokenEstimator{} // default, ±15% error, no deps
//	&vector.CharEstimator{}           // character count
//	vector.NewOllamaTokenEstimator("nomic-embed-text") // exact, via Ollama
//
// # Storage
//
// InMemoryStore is a thread-safe in-process store suitable for development and
// small deployments (< ~10,000 entries). Search is O(n):
//
//	store := vector.NewInMemoryStore()
//
// # Session filtering
//
// InMemoryStore.Search can filter results to a single session when the context
// carries a session ID (injected via internal/session.NewContext). Agent.Run
// sets this automatically when WithName is configured. IDs must follow the
// "sessionID:baseID:chunkIndex" convention used by memory.LongTermMemory.
// session.NewContext rejects IDs containing ":" so the first ":" always marks
// the boundary between the session prefix and the base ID:
//
//	ctx, err := session.NewContext(ctx, "user-42")
//	results, err := store.Search(ctx, queryVec, 3)
//	// results[i].Message holds the message; results[i].Score is cosine similarity.
//
// # Similarity
//
// CosineSimilarity assumes unit-length vectors (as returned by most modern
// models). Use CosineSimilarityRaw for non-normalized vectors, or call
// Normalize explicitly:
//
//	v := vector.Normalize(rawVec)
//	score := vector.CosineSimilarity(v, queryVec)
package vector
