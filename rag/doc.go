// Package rag provides a composable Retrieval-Augmented Generation (RAG)
// pipeline for the goagent framework.
//
// # Overview
//
// The package centres on [Pipeline], which combines a [vector.Chunker],
// a [goagent.Embedder], and a [goagent.VectorStore] into two phases:
//
//   - Index: chunk documents, embed each chunk, persist in the vector store.
//   - Search: embed a query, retrieve the most similar chunks.
//
// A [Pipeline] can be wrapped in a [goagent.Tool] via [NewTool], making the
// knowledge base available to any agent without the agent knowing about RAG.
//
// # Minimal usage
//
//	chunker  := vector.NewTextChunker(vector.WithMaxSize(400), vector.WithOverlap(40))
//	embedder := ollama.NewEmbedder("nomic-embed-text")
//	store    := vector.NewInMemoryStore()
//
//	pipeline, err := rag.NewPipeline(chunker, embedder, store)
//	if err != nil { /* handle */ }
//
//	docs := []rag.Document{{Source: "readme.md", Content: []goagent.ContentBlock{goagent.TextBlock(text)}}}
//	if err := pipeline.Index(ctx, docs...); err != nil { /* handle */ }
//
//	tool := rag.NewTool(pipeline, rag.WithToolName("search_docs"),
//	    rag.WithToolDescription("Search the project documentation."))
//
//	agent, _ := goagent.New(goagent.WithProvider(provider), goagent.WithTool(tool))
//
// # OTel integration
//
// This package does not import the OpenTelemetry SDK. To instrument Search
// calls, register a [SearchObserver] via [WithSearchObserver]. The observer
// receives the active ctx and can access the current span via
// trace.SpanFromContext(ctx).
package rag
