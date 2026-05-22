package rag

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Germanblandin1/goagent"
)

// rerankSystemPrompt instructs the model to output only a relevance score.
// MaxTokens is not set at the request level (the project does not expose it
// per-request); the scoring prompt is designed so that any compliant float
// response fits well within typical provider defaults.
const rerankSystemPrompt = `You are a relevance scorer for a retrieval system.
Given a query and a document passage, output a single decimal number between 0.0 and 1.0
representing how relevant the passage is to answer the query.

Scoring guide:
  1.0 — the passage directly answers the query
  0.7 — the passage contains useful related information
  0.4 — the passage is tangentially related
  0.1 — the passage is not relevant

Output ONLY the number. No explanation. No units. No extra text.
Valid outputs: 0.95  or  0.3  or  0.72`

// LLMRerankerOption configures an [LLMReranker] at construction time.
type LLMRerankerOption func(*LLMReranker)

// WithLLMRerankerSystemPrompt overrides the system prompt used for relevance scoring.
// The default prompt instructs the model to output a single float in [0.0, 1.0].
// If overriding, keep the same constraint — scoreOne expects a parseable float.
func WithLLMRerankerSystemPrompt(prompt string) LLMRerankerOption {
	return func(r *LLMReranker) { r.systemPrompt = prompt }
}

// LLMReranker scores the relevance of each (query, document) pair using
// the LLM of the configured Provider and reorders results accordingly.
//
// It performs N parallel LLM calls (one per result), where N = len(results).
// Use with moderate candidate sets (≤50) to keep cost and latency reasonable.
// For high-throughput production workloads, prefer CohereReranker (future).
//
// Example:
//
//	provider := anthropic.New()
//
//	pipeline, _ := rag.NewPipeline(chunker, embedder, store,
//	    rag.WithReranker(
//	        rag.NewLLMReranker(provider, "claude-haiku-4-5-20251001"),
//	        50,
//	    ),
//	)
type LLMReranker struct {
	provider     goagent.Provider
	model        string
	systemPrompt string
}

// NewLLMReranker constructs an LLMReranker with the given Provider and model.
// Panics if provider is nil or model is empty — both are programming errors, not
// runtime conditions.
func NewLLMReranker(p goagent.Provider, model string, opts ...LLMRerankerOption) *LLMReranker {
	if p == nil {
		panic("rag: NewLLMReranker: provider must not be nil")
	}
	if model == "" {
		panic("rag: NewLLMReranker: model must not be empty")
	}
	r := &LLMReranker{
		provider:     p,
		model:        model,
		systemPrompt: rerankSystemPrompt,
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Rerank scores all results in parallel and returns the topK most relevant,
// ordered descending by RerankScore.
//
// If len(results) == 0, Rerank returns immediately without calling the provider.
// If any provider call fails, Rerank returns the first error encountered;
// in-flight goroutines write to a fully-buffered channel and complete without
// blocking.
func (r *LLMReranker) Rerank(
	ctx     context.Context,
	query   string,
	results []SearchResult,
	topK    int,
) ([]SearchResult, error) {
	if len(results) == 0 {
		return results, nil
	}

	type scoredIdx struct {
		idx   int
		score float64
		err   error
	}

	ch := make(chan scoredIdx, len(results))

	// Fan-out: one goroutine per result. The channel is fully buffered so
	// goroutines never block even if Rerank returns early on error.
	for i, res := range results {
		go func() {
			passage := extractText(res.Message.Content)
			score, err := r.scoreOne(ctx, query, passage)
			ch <- scoredIdx{idx: i, score: score, err: err}
		}()
	}

	// Fan-in: collect all scores. Work on a copy so the caller's slice is
	// never mutated.
	ranked := make([]SearchResult, len(results))
	copy(ranked, results)

	for range results {
		s := <-ch
		if s.err != nil {
			return nil, s.err
		}
		ranked[s.idx].RerankScore = s.score
	}

	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].RerankScore > ranked[j].RerankScore
	})

	if len(ranked) > topK {
		ranked = ranked[:topK]
	}
	return ranked, nil
}

// scoreOne scores the relevance of a single (query, passage) pair.
// Returns a float64 clamped to [0.0, 1.0].
func (r *LLMReranker) scoreOne(ctx context.Context, query, passage string) (float64, error) {
	userMsg := fmt.Sprintf("Query: %s\n\nPassage: %s", query, passage)

	resp, err := r.provider.Complete(ctx, goagent.CompletionRequest{
		Model:        r.model,
		SystemPrompt: r.systemPrompt,
		Messages: []goagent.Message{
			{
				Role:    goagent.RoleUser,
				Content: []goagent.ContentBlock{goagent.TextBlock(userMsg)},
			},
		},
	})
	if err != nil {
		return 0, fmt.Errorf("llm reranker: complete: %w", err)
	}

	raw := strings.TrimSpace(rerankExtractText(resp))
	score, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("llm reranker: parse score %q: %w", raw, err)
	}
	return clampScore(score), nil
}

// rerankExtractText extracts the first text block from a CompletionResponse.
func rerankExtractText(resp goagent.CompletionResponse) string {
	for _, block := range resp.Message.Content {
		if block.Type == goagent.ContentText {
			return block.Text
		}
	}
	return ""
}

// clampScore forces v into [0.0, 1.0], protecting against models that return
// out-of-range values despite the scoring prompt.
func clampScore(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
