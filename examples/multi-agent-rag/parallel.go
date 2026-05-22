package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/orchestration"
	"github.com/Germanblandin1/goagent/rag"
)

// plannerOutput is the JSON schema the PlannerAgent must return.
type plannerOutput struct {
	Queries []string `json:"queries"`
}

// searchResult groups the outcome of one sub-query.
type searchResult struct {
	query   string
	results []rag.SearchResult
	err     error
}

// ParallelRAGExecutor implements orchestration.Executor.
// It reads the planner JSON from the "plan" output key, parses the sub-queries,
// and fans out one goroutine per query against the RAG pipeline.
// All goroutines complete before RunWithContext returns — the channel is
// buffered to len(queries) so no goroutine ever blocks on send.
type ParallelRAGExecutor struct {
	pipeline *rag.Pipeline
	topK     int
}

// NewParallelRAGExecutor constructs a ParallelRAGExecutor.
func NewParallelRAGExecutor(p *rag.Pipeline, topK int) *ParallelRAGExecutor {
	return &ParallelRAGExecutor{pipeline: p, topK: topK}
}

// RunWithContext implements orchestration.Executor.
func (e *ParallelRAGExecutor) RunWithContext(ctx context.Context, sc *orchestration.StageContext) error {
	planJSON, err := sc.RequireOutput("plan")
	if err != nil {
		return fmt.Errorf("parallel rag: %w", err)
	}

	var plan plannerOutput
	if err := json.Unmarshal([]byte(planJSON), &plan); err != nil {
		return fmt.Errorf("parallel rag: parse planner output: %w", err)
	}
	if len(plan.Queries) == 0 {
		return fmt.Errorf("parallel rag: planner returned no queries")
	}

	// Fan-out: one goroutine per query. Each goroutine receives its own copy
	// of query via the function argument — no loop-variable capture issue.
	// The buffered channel guarantees goroutines never block on send.
	ch := make(chan searchResult, len(plan.Queries))
	for _, q := range plan.Queries {
		go func(query string) {
			results, searchErr := e.pipeline.Search(ctx, query, e.topK)
			ch <- searchResult{query: query, results: results, err: searchErr}
		}(q)
	}

	// Fan-in: collect all results before writing to the StageContext.
	allResults := make([]searchResult, 0, len(plan.Queries))
	for range plan.Queries {
		sr := <-ch
		if sr.err != nil {
			return fmt.Errorf("parallel rag: search %q: %w", sr.query, sr.err)
		}
		allResults = append(allResults, sr)
	}

	sc.SetOutput("search", formatResults(allResults))
	return nil
}

// formatResults converts all query results into structured text for the
// Synthesizer, deduplicating by Source so the same document is not repeated.
// The first occurrence of each source is kept — it is the highest-ranked result
// because stores return results ordered by score descending.
func formatResults(all []searchResult) string {
	seen := make(map[string]struct{})
	var sb strings.Builder

	for _, sr := range all {
		fmt.Fprintf(&sb, "## Query: %s\n\n", sr.query)
		for _, r := range sr.results {
			if _, ok := seen[r.Source]; ok {
				continue
			}
			seen[r.Source] = struct{}{}
			fmt.Fprintf(&sb, "### Source: %s (score: %.2f)\n", r.Source, r.Score)
			sb.WriteString(extractText(r.Message))
			sb.WriteString("\n\n")
		}
	}
	return sb.String()
}

// extractText returns the concatenated text from a message's content blocks.
func extractText(msg goagent.Message) string {
	var parts []string
	for _, b := range msg.Content {
		if b.Type == goagent.ContentText && strings.TrimSpace(b.Text) != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, " ")
}
