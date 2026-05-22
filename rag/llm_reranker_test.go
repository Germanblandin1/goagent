package rag_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/rag"
)

// scoringProvider is a mock Provider that returns a pre-configured score string
// for each passage. It matches by looking for the passage text inside the user message.
type scoringProvider struct {
	mu     sync.Mutex
	calls  int
	scores map[string]string // substring of passage → score string to return
	err    error
}

func (p *scoringProvider) Complete(_ context.Context, req goagent.CompletionRequest) (goagent.CompletionResponse, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()

	if p.err != nil {
		return goagent.CompletionResponse{}, p.err
	}

	for _, msg := range req.Messages {
		if msg.Role != goagent.RoleUser {
			continue
		}
		for _, block := range msg.Content {
			if block.Type != goagent.ContentText {
				continue
			}
			for passage, score := range p.scores {
				if strings.Contains(block.Text, passage) {
					return goagent.CompletionResponse{
						Message: goagent.Message{
							Role:    goagent.RoleAssistant,
							Content: []goagent.ContentBlock{goagent.TextBlock(score)},
						},
					}, nil
				}
			}
		}
	}

	// default score when no passage matches
	return goagent.CompletionResponse{
		Message: goagent.Message{
			Role:    goagent.RoleAssistant,
			Content: []goagent.ContentBlock{goagent.TextBlock("0.5")},
		},
	}, nil
}

func makeResults(texts ...string) []rag.SearchResult {
	results := make([]rag.SearchResult, len(texts))
	for i, t := range texts {
		results[i] = rag.SearchResult{
			Message: goagent.Message{
				Role:    goagent.RoleDocument,
				Content: []goagent.ContentBlock{goagent.TextBlock(t)},
			},
			Score:  float64(len(texts)-i) * 0.1, // descending bi-encoder scores
			Source: fmt.Sprintf("doc%d", i),
		}
	}
	return results
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestLLMReranker_ReordersCorrectly verifies that results are sorted descending
// by the scores returned by the provider.
func TestLLMReranker_ReordersCorrectly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	provider := &scoringProvider{
		scores: map[string]string{
			"low relevance":  "0.1",
			"high relevance": "0.9",
			"mid relevance":  "0.5",
		},
	}
	reranker := rag.NewLLMReranker(provider, "test-model")

	results := makeResults("low relevance", "high relevance", "mid relevance")
	got, err := reranker.Rerank(ctx, "query", results, 3)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d results, want 3", len(got))
	}

	// expect descending order: high (0.9), mid (0.5), low (0.1)
	if got[0].RerankScore <= got[1].RerankScore || got[1].RerankScore <= got[2].RerankScore {
		t.Errorf("results not sorted descending: scores %.2f, %.2f, %.2f",
			got[0].RerankScore, got[1].RerankScore, got[2].RerankScore)
	}
	if got[0].RerankScore != 0.9 {
		t.Errorf("top RerankScore = %.2f, want 0.9", got[0].RerankScore)
	}
}

// TestLLMReranker_TruncatesToTopK verifies that len(result) == topK when
// the input has more elements.
func TestLLMReranker_TruncatesToTopK(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	provider := &scoringProvider{scores: map[string]string{}}
	reranker := rag.NewLLMReranker(provider, "test-model")

	results := makeResults("a", "b", "c", "d", "e", "f", "g", "h", "i", "j")
	got, err := reranker.Rerank(ctx, "query", results, 3)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("got %d results, want 3", len(got))
	}
}

// TestLLMReranker_EmptyResultsNoProviderCalls verifies that an empty input
// returns immediately without calling the provider.
func TestLLMReranker_EmptyResultsNoProviderCalls(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	provider := &scoringProvider{scores: map[string]string{}}
	reranker := rag.NewLLMReranker(provider, "test-model")

	got, err := reranker.Rerank(ctx, "query", []rag.SearchResult{}, 5)
	if err != nil {
		t.Fatalf("Rerank on empty slice: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d results, want 0", len(got))
	}

	provider.mu.Lock()
	calls := provider.calls
	provider.mu.Unlock()
	if calls != 0 {
		t.Errorf("provider called %d times on empty input, want 0", calls)
	}
}

// TestLLMReranker_ProviderErrorPropagates verifies that a provider failure
// is returned wrapped with context.
func TestLLMReranker_ProviderErrorPropagates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	providerErr := errors.New("api unavailable")
	provider := &scoringProvider{err: providerErr}
	reranker := rag.NewLLMReranker(provider, "test-model")

	_, err := reranker.Rerank(ctx, "query", makeResults("doc"), 1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, providerErr) {
		t.Errorf("error chain does not wrap providerErr: %v", err)
	}
}

// TestLLMReranker_ScoresClamped verifies that out-of-range scores are clamped
// to [0.0, 1.0].
func TestLLMReranker_ScoresClamped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	provider := &scoringProvider{
		scores: map[string]string{
			"high": "1.5",  // above 1.0 → should clamp to 1.0
			"low":  "-0.2", // below 0.0 → should clamp to 0.0
		},
	}
	reranker := rag.NewLLMReranker(provider, "test-model")

	results := makeResults("high", "low")
	got, err := reranker.Rerank(ctx, "query", results, 2)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}

	for _, r := range got {
		if r.RerankScore < 0 || r.RerankScore > 1 {
			t.Errorf("RerankScore %.2f is outside [0.0, 1.0]", r.RerankScore)
		}
	}
}

// TestLLMReranker_BiEncoderScorePreserved verifies that the original Score
// field is not overwritten by the reranker.
func TestLLMReranker_BiEncoderScorePreserved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	provider := &scoringProvider{
		scores: map[string]string{"doc": "0.8"},
	}
	reranker := rag.NewLLMReranker(provider, "test-model")

	input := makeResults("doc")
	originalScore := input[0].Score

	got, err := reranker.Rerank(ctx, "query", input, 1)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if got[0].Score != originalScore {
		t.Errorf("bi-encoder Score changed: got %.3f, want %.3f", got[0].Score, originalScore)
	}
	if got[0].RerankScore == 0 {
		t.Errorf("RerankScore not populated")
	}
}

// TestLLMReranker_CustomSystemPrompt verifies that WithLLMRerankerSystemPrompt
// changes the system prompt forwarded to the provider.
func TestLLMReranker_CustomSystemPrompt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const customPrompt = "custom prompt"
	var gotSystemPrompt string

	capture := &capturingProvider{score: "0.5", captureSystemPrompt: &gotSystemPrompt}
	reranker := rag.NewLLMReranker(capture, "test-model",
		rag.WithLLMRerankerSystemPrompt(customPrompt),
	)

	_, err := reranker.Rerank(ctx, "query", makeResults("doc"), 1)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if gotSystemPrompt != customPrompt {
		t.Errorf("SystemPrompt = %q, want %q", gotSystemPrompt, customPrompt)
	}
}

// capturingProvider records the SystemPrompt from CompletionRequests.
type capturingProvider struct {
	score               string
	captureSystemPrompt *string
}

func (p *capturingProvider) Complete(_ context.Context, req goagent.CompletionRequest) (goagent.CompletionResponse, error) {
	if p.captureSystemPrompt != nil {
		*p.captureSystemPrompt = req.SystemPrompt
	}
	return goagent.CompletionResponse{
		Message: goagent.Message{
			Role:    goagent.RoleAssistant,
			Content: []goagent.ContentBlock{goagent.TextBlock(p.score)},
		},
	}, nil
}

// TestLLMReranker_Race verifies no data races with go test -race.
func TestLLMReranker_Race(t *testing.T) {
	ctx := context.Background()

	provider := &scoringProvider{scores: map[string]string{}}
	reranker := rag.NewLLMReranker(provider, "test-model")

	results := makeResults("a", "b", "c", "d", "e")

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = reranker.Rerank(ctx, "query", results, 3)
		}()
	}
	wg.Wait()
}

// TestNewLLMReranker_PanicsOnNilProvider verifies the constructor panics on nil.
func TestNewLLMReranker_PanicsOnNilProvider(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil provider, got none")
		}
	}()
	rag.NewLLMReranker(nil, "test-model")
}

// TestNewLLMReranker_PanicsOnEmptyModel verifies the constructor panics on empty model.
func TestNewLLMReranker_PanicsOnEmptyModel(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for empty model, got none")
		}
	}()
	rag.NewLLMReranker(&scoringProvider{}, "")
}
