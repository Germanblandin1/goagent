# goagent/rag

Retrieval-Augmented Generation (RAG) pipeline for [goagent](https://github.com/Germanblandin1/goagent).

Combines a chunker, embedder, vector store, and optional reranker into a single `Pipeline`. Exposes the pipeline as an agent tool via `NewTool` so the model can search a document corpus on demand.

```bash
go get github.com/Germanblandin1/goagent/rag
```

## Documentation

- [pkg.go.dev/github.com/Germanblandin1/goagent/rag](https://pkg.go.dev/github.com/Germanblandin1/goagent/rag)
- [Root module](https://pkg.go.dev/github.com/Germanblandin1/goagent)
