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
//	ollama pull nomic-embed-text       # embedding model
//	ollama pull gpt-oss:120b-cloud     # LLM for answer generation and RAGAS evaluation
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
	"strings"
	"syscall"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector"
	"github.com/Germanblandin1/goagent/providers/ollama"
	"github.com/Germanblandin1/goagent/rag"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Provider for answer generation and LLM reranking — targets local Ollama.
	provider := ollama.New()

	// Separate provider for the RAGAS evaluator — no MaxTokens limit so the
	// thinking block does not crowd out the JSON output. The model stops naturally
	// after emitting the ~90-token JSON object specified in the prompt.
	evaluatorProvider := ollama.New()

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
	docs := goagentDocs()
	logChunkDistribution(ctx, chunker, docs)
	if err := indexPipeline.Index(ctx, docs...); err != nil {
		log.Fatalf("indexing docs: %v", err)
	}
	if n, err := store.Count(ctx); err == nil {
		fmt.Printf("  Chunks indexed:  %d (total)\n", n)
	}

	// Answer agent — generates a grounded answer from retrieved context.
	answerer := newAnswerAgent(provider)

	// Mode A: basic RAG pipeline (no reranker).
	basicPipeline, err := rag.NewPipeline(chunker, embedder, store)
	if err != nil {
		log.Fatalf("creating basic pipeline: %v", err)
	}

	printConfig(len(evalDataset), 3, 5)

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
		rag.WithReranker(rag.NewLLMReranker(provider, rerankerModel), 5),
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
	printSummaryTable(basicResults, rerankedResults)
}

// goagentDocs returns the documentation corpus used for indexing and chunk analysis.
func goagentDocs() []rag.Document {
	return []rag.Document{
		{Source: "provider.md", Content: []goagent.ContentBlock{goagent.TextBlock(providerDoc)}},
		{Source: "architecture.md", Content: []goagent.ContentBlock{goagent.TextBlock(architectureDoc)}},
		{Source: "tools.md", Content: []goagent.ContentBlock{goagent.TextBlock(toolsDoc)}},
		{Source: "errors.md", Content: []goagent.ContentBlock{goagent.TextBlock(errorsDoc)}},
		{Source: "rag.md", Content: []goagent.ContentBlock{goagent.TextBlock(ragDoc)}},
		{Source: "memory.md", Content: []goagent.ContentBlock{goagent.TextBlock(memoryDoc)}},
		{Source: "hooks.md", Content: []goagent.ContentBlock{goagent.TextBlock(hooksDoc)}},
		{Source: "agent.md", Content: []goagent.ContentBlock{goagent.TextBlock(agentDoc)}},
	}
}

// logChunkDistribution prints how many chunks each document produces after chunking.
func logChunkDistribution(ctx context.Context, c vector.Chunker, docs []rag.Document) {
	fmt.Println("  Chunk distribution:")
	for _, doc := range docs {
		chunks, err := c.Chunk(ctx, vector.ChunkContent{Blocks: doc.Content})
		if err != nil {
			fmt.Printf("    %-16s error: %v\n", doc.Source, err)
			continue
		}
		fmt.Printf("    %-16s %d chunks\n", doc.Source, len(chunks))
	}
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

// printConfig prints the evaluation parameters before any run starts.
func printConfig(numSamples, topK, rerankN int) {
	fmt.Println("=== rag-eval configuration ===")
	fmt.Printf("  Answer model:    %s\n", evalModel)
	fmt.Printf("  RAGAS model:     %s\n", ragasModel)
	fmt.Printf("  Reranker model:  %s\n", rerankerModel)
	fmt.Printf("  Embedding model: nomic-embed-text\n")
	fmt.Printf("  TopK:            %d\n", topK)
	fmt.Printf("  RerankN:         %d\n", rerankN)
	fmt.Printf("  Eval samples:    %d\n", numSamples)
	fmt.Println()
}

// printSummaryTable prints a side-by-side comparison table of both eval modes.
func printSummaryTable(basic, reranked []EvalResult) {
	p1, r1, m1, g1 := AverageScores(basic)
	p2, r2, m2, g2 := AverageScores(reranked)

	sep := strings.Repeat("-", 58)
	row := func(name string, a, b float64) {
		delta := b - a
		sign := "+"
		if delta < 0 {
			sign = ""
		}
		fmt.Printf("  %-22s %8.2f   %8.2f   %s%.2f\n", name, a, b, sign, delta)
	}

	fmt.Println("\n" + sep)
	fmt.Printf("  %-22s %8s   %8s   %s\n", "Metric", "A basic", "B rerank", "Delta")
	fmt.Println(sep)
	fmt.Println("  Retrieval")
	row("Precision@3", p1, p2)
	row("Recall@3", r1, r2)
	row("MRR", m1, m2)
	fmt.Println("  Generation (RAGAS)")
	row("Faithfulness", g1.Faithfulness, g2.Faithfulness)
	row("Answer Relevancy", g1.AnswerRelevancy, g2.AnswerRelevancy)
	row("Context Precision", g1.ContextPrecision, g2.ContextPrecision)
	row("Context Recall", g1.ContextRecall, g2.ContextRecall)
	fmt.Println(sep)
}
