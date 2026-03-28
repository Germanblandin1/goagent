package vector

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
)

// SizeEstimator measures the size of a text string in implementation-defined
// units. The units determine the semantics of MaxSize and Overlap in chunkers.
// ctx is passed through so implementations that perform I/O (e.g.
// OllamaTokenEstimator) can be cancelled or time-limited by the caller.
type SizeEstimator interface {
	Measure(ctx context.Context, text string) int
}

// CharEstimator measures text size in Unicode code points (runes).
// No external dependencies. Useful when the model limit is expressed in
// characters rather than tokens.
type CharEstimator struct{}

// Measure returns the number of Unicode code points in text.
func (e *CharEstimator) Measure(_ context.Context, text string) int {
	return len([]rune(text))
}

// HeuristicTokenEstimator estimates token count using the heuristic
// len(text)/4 + 4. Typical error: ±15% compared to a real tokenizer.
// No external dependencies. Recommended default for most use cases.
// Uses the same formula as memory/policy.TokenWindowPolicy.
type HeuristicTokenEstimator struct{}

// Measure returns an estimated token count for text.
func (e *HeuristicTokenEstimator) Measure(_ context.Context, text string) int {
	return len(text)/4 + 4
}

// OllamaTokenEstimator obtains the exact token count via the Ollama tokenize
// API. Not suitable for hot paths — use in batch preprocessing or document
// ingestion only. The caller controls cancellation and timeouts via ctx.
// On any error (network, parse, cancellation) it silently falls back to
// HeuristicTokenEstimator.
type OllamaTokenEstimator struct {
	BaseURL string
	Model   string
	client  *http.Client
}

// NewOllamaTokenEstimator creates an OllamaTokenEstimator for the given model.
// The Ollama server is expected at http://localhost:11434.
func NewOllamaTokenEstimator(model string) *OllamaTokenEstimator {
	return &OllamaTokenEstimator{
		BaseURL: "http://localhost:11434",
		Model:   model,
		client:  &http.Client{},
	}
}

// Measure returns the token count from Ollama, falling back to the heuristic
// estimate on any error. ctx controls the lifetime of the HTTP request.
func (e *OllamaTokenEstimator) Measure(ctx context.Context, text string) int {
	heuristic := (&HeuristicTokenEstimator{}).Measure(ctx, text)

	body, err := json.Marshal(map[string]string{
		"model":  e.Model,
		"prompt": text,
	})
	if err != nil {
		return heuristic
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		e.BaseURL+"/api/tokenize", bytes.NewReader(body))
	if err != nil {
		return heuristic
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return heuristic
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return heuristic
	}

	var result struct {
		Tokens []int `json:"tokens"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return heuristic
	}
	return len(result.Tokens)
}
