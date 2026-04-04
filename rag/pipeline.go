package rag

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector"
)

// Document is the unit of input for [Pipeline.Index].
// Source identifies the document's origin (file path, URL, logical name).
// It is used as the ID prefix in the VectorStore: "<source>:<chunk_index>".
type Document struct {
	Source  string
	Content []goagent.ContentBlock
}

// SearchResult is the enriched result of a [Pipeline.Search] call.
// Score is the cosine similarity in [0.0, 1.0] for embedders that produce
// normalised vectors (most modern text embedders do).
// Source identifies the origin document, extracted from the chunk's Metadata.
type SearchResult struct {
	Message goagent.Message
	Score   float64
	Source  string
}

// SearchObserver is a callback invoked after every [Pipeline.Search] call.
// It receives the ctx from Search — if the caller propagated an active OTel
// span, the observer can access it via trace.SpanFromContext(ctx).
//
// The observer cannot modify results or cancel the operation.
// If the observer panics, the panic propagates to the Search caller.
//
// # Logging example
//
//	rag.WithSearchObserver(func(ctx context.Context, query string,
//	    results []rag.SearchResult, dur time.Duration, err error) {
//
//	    if err != nil {
//	        slog.Error("rag search failed", "query", query, "err", err)
//	        return
//	    }
//	    topScore := 0.0
//	    if len(results) > 0 {
//	        topScore = results[0].Score
//	    }
//	    slog.Debug("rag search",
//	        "query", query, "results", len(results),
//	        "top_score", topScore, "dur", dur)
//	    if topScore < 0.5 {
//	        slog.Warn("low quality retrieval", "query", query, "top_score", topScore)
//	    }
//	})
//
// # OTel example
//
// The package rag/ does not import the OTel SDK. The caller that already
// has OTel imported can annotate the active span via the ctx argument:
//
//	rag.WithSearchObserver(func(ctx context.Context, query string,
//	    results []rag.SearchResult, dur time.Duration, err error) {
//
//	    span := trace.SpanFromContext(ctx)
//	    if err != nil {
//	        span.RecordError(err)
//	        return
//	    }
//	    span.SetAttributes(attribute.Int("rag.result_count", len(results)))
//	    if len(results) > 0 {
//	        span.SetAttributes(attribute.Float64("rag.top_score", results[0].Score))
//	    }
//	})
type SearchObserver func(
	ctx     context.Context,
	query   string,
	results []SearchResult,
	dur     time.Duration,
	err     error,
)

// Pipeline combines a Chunker, Embedder, and VectorStore into a composable
// RAG pipeline. It is independent of any agent — it can be used standalone
// for offline indexing or wrapped in a Tool via [NewTool].
//
// Usage:
//
//	pipeline, err := rag.NewPipeline(chunker, embedder, store,
//	    rag.WithSearchObserver(myObserver),
//	)
//	if err := pipeline.Index(ctx, docs...); err != nil { ... }
//	results, err := pipeline.Search(ctx, "error handling", 3)
type Pipeline struct {
	chunker  vector.Chunker
	embedder goagent.Embedder
	store    goagent.VectorStore
	observer SearchObserver // nil = no-op
}

// PipelineOption configures a Pipeline at construction time.
type PipelineOption func(*Pipeline)

// WithSearchObserver registers a callback invoked after every Search call.
// When not configured, Search has no callback overhead.
func WithSearchObserver(obs SearchObserver) PipelineOption {
	return func(p *Pipeline) { p.observer = obs }
}

// NewPipeline constructs a Pipeline with the given components.
// chunker, embedder, and store are all required — returns an error if any is nil.
func NewPipeline(
	chunker  vector.Chunker,
	embedder goagent.Embedder,
	store    goagent.VectorStore,
	opts     ...PipelineOption,
) (*Pipeline, error) {
	if chunker == nil {
		return nil, errors.New("rag: chunker is required")
	}
	if embedder == nil {
		return nil, errors.New("rag: embedder is required")
	}
	if store == nil {
		return nil, errors.New("rag: store is required")
	}
	p := &Pipeline{
		chunker:  chunker,
		embedder: embedder,
		store:    store,
	}
	for _, o := range opts {
		o(p)
	}
	return p, nil
}

// Index processes one or more Documents through the chunk → embed → upsert
// pipeline and persists them in the VectorStore. Designed for offline use
// before the agent starts.
//
// The ID of each chunk is "<source>:<chunk_index>" — deterministic and
// idempotent. Re-indexing the same document with the same Source replaces
// existing chunks.
//
// Chunks with no embeddable content (e.g. image chunks with a text-only
// embedder) are silently skipped. All other errors are returned immediately.
func (p *Pipeline) Index(ctx context.Context, docs ...Document) error {
	for _, doc := range docs {
		content := vector.ChunkContent{
			Blocks:   doc.Content,
			Metadata: map[string]any{"source": doc.Source},
		}
		chunks, err := p.chunker.Chunk(ctx, content)
		if err != nil {
			return fmt.Errorf("rag: chunking %q: %w", doc.Source, err)
		}
		for i, chunk := range chunks {
			vec, err := p.embedder.Embed(ctx, chunk.Blocks)
			if err != nil {
				if errors.Is(err, vector.ErrNoEmbeddeableContent) {
					continue
				}
				return fmt.Errorf("rag: embedding chunk %d of %q: %w", i, doc.Source, err)
			}
			id := fmt.Sprintf("%s:%d", doc.Source, i)
			payload := chunkToMessage(doc.Source, chunk)
			if err := p.store.Upsert(ctx, id, vec, payload); err != nil {
				return fmt.Errorf("rag: upserting chunk %d of %q: %w", i, doc.Source, err)
			}
		}
	}
	return nil
}

// Search embeds query, retrieves the topK most similar chunks from the
// VectorStore, and returns SearchResults with similarity scores and source info.
//
// Scores are the similarity values returned directly by the VectorStore.
//
// The SearchObserver (if configured) is invoked after every Search, including
// on error — it receives the error as its last argument.
func (p *Pipeline) Search(
	ctx   context.Context,
	query string,
	topK  int,
) ([]SearchResult, error) {
	start := time.Now()

	vec, err := p.embedder.Embed(ctx, []goagent.ContentBlock{goagent.TextBlock(query)})
	if err != nil {
		if p.observer != nil {
			p.observer(ctx, query, nil, time.Since(start), err)
		}
		return nil, fmt.Errorf("rag: embedding query: %w", err)
	}

	scored, err := p.store.Search(ctx, vec, topK)
	dur := time.Since(start)
	var results []SearchResult
	if err == nil {
		results = make([]SearchResult, len(scored))
		for i, s := range scored {
			results[i] = SearchResult{
				Message: s.Message,
				Score:   s.Score,
				Source:  extractSource(s.Message),
			}
		}
	}

	if p.observer != nil {
		p.observer(ctx, query, results, dur, err)
	}

	if err != nil {
		return nil, fmt.Errorf("rag: vector search: %w", err)
	}
	return results, nil
}

// chunkToMessage builds a Message from a chunk, populating Metadata["source"]
// so that extractSource can retrieve the origin document after a VectorStore.Search.
// Role is RoleDocument — these messages live only in the VectorStore and are
// never forwarded to a provider.
func chunkToMessage(source string, chunk vector.ChunkResult) goagent.Message {
	meta := map[string]any{"source": source}
	if idx, ok := chunk.Metadata["chunk_index"]; ok {
		meta["chunk_index"] = idx
	}
	return goagent.Message{
		Role:     goagent.RoleDocument,
		Content:  chunk.Blocks,
		Metadata: meta,
	}
}
