// Package tiktoken provides a SizeEstimator backed by the real tiktoken
// tokenizer (github.com/pkoukk/tiktoken-go). It is a separate module so that
// the core memory/vector package has zero external dependencies.
//
// Import this package when you need exact token counts for OpenAI models:
//
//	import (
//	    "github.com/Germanblandin1/goagent/memory/vector"
//	    vektiktoken "github.com/Germanblandin1/goagent/memory/vector/tiktoken"
//	)
//
//	est, err := vektiktoken.NewEstimator("gpt-4o")
//	chunker := vector.NewTextChunker(vector.WithEstimator(est))
package tiktoken

import (
	"context"
	"fmt"

	pktiktoken "github.com/pkoukk/tiktoken-go"

	"github.com/Germanblandin1/goagent/memory/vector"
)

// Compile-time check: Estimator implements vector.SizeEstimator.
var _ vector.SizeEstimator = (*Estimator)(nil)

// Estimator measures token count using the real tiktoken tokenizer.
// Exact for OpenAI models (GPT-3.5, GPT-4, GPT-4o, text-embedding-3-*, etc.).
// For non-OpenAI models use vector.HeuristicTokenEstimator or
// vector.OllamaTokenEstimator instead.
type Estimator struct {
	enc *pktiktoken.Tiktoken
}

// NewEstimator creates an Estimator for the given model name.
// model must be a valid OpenAI model name recognised by tiktoken-go
// (e.g. "gpt-4o", "text-embedding-3-small").
//
// Returns an error if the model is not recognised by the tiktoken library.
func NewEstimator(model string) (*Estimator, error) {
	enc, err := pktiktoken.EncodingForModel(model)
	if err != nil {
		return nil, fmt.Errorf("tiktoken: encoding for model %q: %w", model, err)
	}
	return &Estimator{enc: enc}, nil
}

// Measure returns the exact token count for text as computed by tiktoken.
// ctx is accepted to satisfy the vector.SizeEstimator interface; tiktoken
// is pure CPU and does not use it.
func (e *Estimator) Measure(_ context.Context, text string) int {
	return len(e.enc.Encode(text, nil, nil))
}
