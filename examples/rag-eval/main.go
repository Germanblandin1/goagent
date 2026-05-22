// Command rag-eval measures RAG pipeline quality using two layers of metrics:
//
// Layer 1 (Go-only): PrecisionAtK, RecallAtK, MRR — measure retrieval quality.
// Layer 2 (RAGAS):   Faithfulness, AnswerRelevancy, ContextPrecision, ContextRecall
//                    — measure generation quality using an LLM as judge.
//
// The example runs two modes on the same dataset:
//
//	Mode A: basic RAG pipeline (no reranker)
//	Mode B: RAG pipeline with LLMReranker (over-fetch 10, pick top 3)
//
// The comparative report shows the measurable improvement from the reranker.
//
// Prerequisites:
//
//	ollama pull nomic-embed-text    # embedding model
//	export ANTHROPIC_API_KEY=...    # API key for answer generation and evaluation
//
// Usage:
//
//	go run ./examples/rag-eval
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector"
	"github.com/Germanblandin1/goagent/providers/anthropic"
	"github.com/Germanblandin1/goagent/providers/ollama"
	"github.com/Germanblandin1/goagent/rag"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Provider for answer generation and LLM reranking (reads ANTHROPIC_API_KEY).
	provider := anthropic.New()

	// Separate provider for the RAGAS evaluator: small MaxTokens because the
	// RAGAS response is a short JSON of four floats (~60 tokens max).
	evaluatorProvider := anthropic.New(anthropic.WithMaxTokens(120))

	// Ollama embedder — same pattern as all other RAG examples in this repo.
	embedder := ollama.NewEmbedder(ollama.WithEmbedModel("nomic-embed-text"))

	chunker := vector.NewRecursiveChunker(vector.WithRCMaxSize(400))

	// Shared in-memory vector store — docs are indexed once, both pipelines search it.
	store := vector.NewInMemoryStore()

	// Index docs before creating pipelines so both modes query the same corpus.
	indexPipeline, err := rag.NewPipeline(chunker, embedder, store)
	if err != nil {
		log.Fatalf("creating index pipeline: %v", err)
	}
	if err := indexGoagentDocs(ctx, indexPipeline); err != nil {
		log.Fatalf("indexing docs: %v", err)
	}

	// Answer agent — generates a grounded answer from retrieved context.
	answerer := newAnswerAgent(provider)

	// Mode A: basic RAG pipeline (no reranker).
	basicPipeline, err := rag.NewPipeline(chunker, embedder, store)
	if err != nil {
		log.Fatalf("creating basic pipeline: %v", err)
	}

	fmt.Println("=== Mode A: basic RAG (no reranker) ===")
	basicResults, err := RunEvalSuite(ctx,
		basicPipeline, answerer, evaluatorProvider, evalDataset, 3,
	)
	if err != nil {
		log.Fatalf("eval suite (basic): %v", err)
	}
	printReport(basicResults)

	// Mode B: RAG pipeline with LLMReranker (over-fetch 10, return top 3).
	rerankedPipeline, err := rag.NewPipeline(chunker, embedder, store,
		rag.WithReranker(rag.NewLLMReranker(provider, evalModel), 10),
	)
	if err != nil {
		log.Fatalf("creating reranked pipeline: %v", err)
	}

	fmt.Println("\n=== Mode B: RAG with LLMReranker (rerankN=10, topK=3) ===")
	rerankedResults, err := RunEvalSuite(ctx,
		rerankedPipeline, answerer, evaluatorProvider, evalDataset, 3,
	)
	if err != nil {
		log.Fatalf("eval suite (reranked): %v", err)
	}
	printReport(rerankedResults)

	// Comparative report.
	printComparison(basicResults, rerankedResults)
}

// indexGoagentDocs indexes the goagent documentation excerpts defined in docs.go.
func indexGoagentDocs(ctx context.Context, p *rag.Pipeline) error {
	docs := []rag.Document{
		{Source: "provider.md", Content: []goagent.ContentBlock{goagent.TextBlock(providerDoc)}},
		{Source: "architecture.md", Content: []goagent.ContentBlock{goagent.TextBlock(architectureDoc)}},
		{Source: "tools.md", Content: []goagent.ContentBlock{goagent.TextBlock(toolsDoc)}},
		{Source: "errors.md", Content: []goagent.ContentBlock{goagent.TextBlock(errorsDoc)}},
		{Source: "rag.md", Content: []goagent.ContentBlock{goagent.TextBlock(ragDoc)}},
		{Source: "memory.md", Content: []goagent.ContentBlock{goagent.TextBlock(memoryDoc)}},
		{Source: "hooks.md", Content: []goagent.ContentBlock{goagent.TextBlock(hooksDoc)}},
		{Source: "agent.md", Content: []goagent.ContentBlock{goagent.TextBlock(agentDoc)}},
	}
	return p.Index(ctx, docs...)
}

// agentAnswerer wraps a goagent.Agent to implement AnswerAgent.
type agentAnswerer struct{ inner *goagent.Agent }

func (a *agentAnswerer) Answer(ctx context.Context, question, retrievedCtx string) (string, error) {
	prompt := fmt.Sprintf("Context:\n%s\n\nQuestion: %s", retrievedCtx, question)
	return a.inner.Run(ctx, prompt)
}

// newAnswerAgent creates an agent that generates answers grounded in provided context.
func newAnswerAgent(provider goagent.Provider) AnswerAgent {
	agent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithModel(evalModel),
		goagent.WithMaxIterations(1),
		goagent.WithSystemPrompt(answerSystemPrompt),
	)
	if err != nil {
		log.Fatalf("creating answer agent: %v", err)
	}
	return &agentAnswerer{inner: agent}
}

// printReport prints aggregated metrics for a set of results.
func printReport(results []EvalResult) {
	precision, recall, mrr, ragas := AverageScores(results)
	fmt.Println("  Retrieval:")
	fmt.Printf("    Precision@3:       %.2f\n", precision)
	fmt.Printf("    Recall@3:          %.2f\n", recall)
	fmt.Printf("    MRR:               %.2f\n", mrr)
	fmt.Println("  Generation (RAGAS):")
	fmt.Printf("    Faithfulness:      %.2f\n", ragas.Faithfulness)
	fmt.Printf("    Answer Relevancy:  %.2f\n", ragas.AnswerRelevancy)
	fmt.Printf("    Context Precision: %.2f\n", ragas.ContextPrecision)
	fmt.Printf("    Context Recall:    %.2f\n", ragas.ContextRecall)
}

// printComparison shows the delta between Mode A and Mode B for each metric.
func printComparison(basic, reranked []EvalResult) {
	p1, r1, m1, g1 := AverageScores(basic)
	p2, r2, m2, g2 := AverageScores(reranked)

	fmt.Println("\n=== Comparison A vs B (+ = improvement with reranker) ===")
	fmt.Printf("  Precision@3:       %+.2f\n", p2-p1)
	fmt.Printf("  Recall@3:          %+.2f\n", r2-r1)
	fmt.Printf("  MRR:               %+.2f\n", m2-m1)
	fmt.Printf("  Faithfulness:      %+.2f\n", g2.Faithfulness-g1.Faithfulness)
	fmt.Printf("  Answer Relevancy:  %+.2f\n", g2.AnswerRelevancy-g1.AnswerRelevancy)
	fmt.Printf("  Context Precision: %+.2f\n", g2.ContextPrecision-g1.ContextPrecision)
	fmt.Printf("  Context Recall:    %+.2f\n", g2.ContextRecall-g1.ContextRecall)
}
