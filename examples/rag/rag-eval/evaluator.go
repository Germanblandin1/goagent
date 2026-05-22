package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/rag"
)

// RAGPipeline is implemented by *rag.Pipeline.
// Defined as an interface to allow test doubles.
type RAGPipeline interface {
	Search(ctx context.Context, query string, topK int) ([]rag.SearchResult, error)
}

// AnswerAgent generates an answer given a question and retrieved context.
type AnswerAgent interface {
	Answer(ctx context.Context, question, retrievedCtx string) (string, error)
}

// RAGASScores groups the four RAGAS evaluation dimensions.
// All fields are in [0.0, 1.0].
type RAGASScores struct {
	Faithfulness     float64 `json:"faithfulness"`
	AnswerRelevancy  float64 `json:"answer_relevancy"`
	ContextPrecision float64 `json:"context_precision"`
	ContextRecall    float64 `json:"context_recall"`
}

// EvalResult groups retrieval (Layer 1) and generation (Layer 2) metrics for
// one evaluated case.
type EvalResult struct {
	Sample     EvalSample
	Retrieved  []string    // source IDs returned by the pipeline
	Context    string      // formatted context passed to the answer agent
	Answer     string      // generated answer
	Precision3 float64     // PrecisionAtK with k=topK
	Recall3    float64     // RecallAtK with k=topK
	RR         float64     // ReciprocalRank
	RAGAS      RAGASScores // generation quality scores
}

// Evaluate evaluates a single case using the LLM as judge (RAGAS pattern).
// provider must be configured with a small MaxTokens (e.g. 120) — a correct
// RAGAS response is a short JSON of four floats.
func Evaluate(ctx context.Context, provider goagent.Provider,
	question, retrievedCtx, answer string) (RAGASScores, error) {

	userMsg := fmt.Sprintf(
		"Question: %s\n\nRetrieved Context:\n%s\n\nGenerated Answer:\n%s",
		question, retrievedCtx, answer,
	)

	resp, err := provider.Complete(ctx, goagent.CompletionRequest{
		Model:        evalModel,
		SystemPrompt: ragasEvaluatorPrompt,
		Messages: []goagent.Message{
			{
				Role:    goagent.RoleUser,
				Content: []goagent.ContentBlock{goagent.TextBlock(userMsg)},
			},
		},
	})
	if err != nil {
		return RAGASScores{}, fmt.Errorf("ragas evaluate: %w", err)
	}

	raw := strings.TrimSpace(responseText(resp))
	var scores RAGASScores
	if err := json.Unmarshal([]byte(raw), &scores); err != nil {
		return RAGASScores{}, fmt.Errorf("ragas evaluate: parse scores %q: %w", raw, err)
	}
	return scores, nil
}

// RunEvalSuite runs the full pipeline (retrieval + answer + RAGAS) over all samples.
// Sequential to avoid exceeding the provider's rate limit.
func RunEvalSuite(ctx context.Context,
	pipeline RAGPipeline,
	answerer AnswerAgent,
	evaluatorProvider goagent.Provider,
	samples []EvalSample,
	topK int,
) ([]EvalResult, error) {

	results := make([]EvalResult, 0, len(samples))

	for _, sample := range samples {
		// 1. Retrieval
		searchResults, err := pipeline.Search(ctx, sample.Question, topK)
		if err != nil {
			return nil, fmt.Errorf("eval suite: search %q: %w", sample.Question, err)
		}

		// 2. Extract source IDs and formatted context
		retrieved := make([]string, len(searchResults))
		var ctxBuilder strings.Builder
		for i, r := range searchResults {
			retrieved[i] = r.Source
			ctxBuilder.WriteString(fmt.Sprintf("[%s]\n%s\n\n", r.Source, messageText(r.Message)))
		}
		retrievedCtx := ctxBuilder.String()

		// 3. Generate answer
		answer, err := answerer.Answer(ctx, sample.Question, retrievedCtx)
		if err != nil {
			return nil, fmt.Errorf("eval suite: answer %q: %w", sample.Question, err)
		}

		// 4. Retrieval metrics (Layer 1)
		precision := rag.PrecisionAtK(retrieved, sample.RelevantSources, topK)
		recall := rag.RecallAtK(retrieved, sample.RelevantSources, topK)
		rr := rag.ReciprocalRank(retrieved, sample.RelevantSources)

		// 5. Generation metrics (Layer 2 — RAGAS)
		ragasScores, err := Evaluate(ctx, evaluatorProvider, sample.Question, retrievedCtx, answer)
		if err != nil {
			return nil, fmt.Errorf("eval suite: ragas %q: %w", sample.Question, err)
		}

		results = append(results, EvalResult{
			Sample:     sample,
			Retrieved:  retrieved,
			Context:    retrievedCtx,
			Answer:     answer,
			Precision3: precision,
			Recall3:    recall,
			RR:         rr,
			RAGAS:      ragasScores,
		})
	}
	return results, nil
}

// AverageScores computes the mean of each metric over all results.
func AverageScores(results []EvalResult) (precision, recall, mrr float64, ragas RAGASScores) {
	if len(results) == 0 {
		return
	}
	for _, r := range results {
		precision += r.Precision3
		recall += r.Recall3
		mrr += r.RR
		ragas.Faithfulness += r.RAGAS.Faithfulness
		ragas.AnswerRelevancy += r.RAGAS.AnswerRelevancy
		ragas.ContextPrecision += r.RAGAS.ContextPrecision
		ragas.ContextRecall += r.RAGAS.ContextRecall
	}
	n := float64(len(results))
	precision /= n
	recall /= n
	mrr /= n
	ragas.Faithfulness /= n
	ragas.AnswerRelevancy /= n
	ragas.ContextPrecision /= n
	ragas.ContextRecall /= n
	return
}

// responseText extracts the first text block from a CompletionResponse.
func responseText(resp goagent.CompletionResponse) string {
	for _, b := range resp.Message.Content {
		if b.Type == goagent.ContentText {
			return b.Text
		}
	}
	return ""
}

// messageText concatenates all text blocks from a Message, joined by a space.
func messageText(msg goagent.Message) string {
	var parts []string
	for _, b := range msg.Content {
		if b.Type == goagent.ContentText && strings.TrimSpace(b.Text) != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, " ")
}
