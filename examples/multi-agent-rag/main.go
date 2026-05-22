// Command multi-agent-rag demonstrates Agentic RAG Pattern 3:
// a Planner decomposes the user query into sub-queries, parallel Go goroutines
// search the RAG pipeline for each sub-query, and a Synthesizer produces the
// final answer with inline citations.
//
// Prerequisites:
//
//	ollama pull qwen3:latest           # chat model for Planner and Synthesizer
//	ollama pull nomic-embed-text       # embedding model for RAG
//
// Usage:
//
//	go run ./examples/multi-agent-rag
//	go run ./examples/multi-agent-rag "your question here"
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector"
	"github.com/Germanblandin1/goagent/orchestration"
	"github.com/Germanblandin1/goagent/providers/ollama"
	"github.com/Germanblandin1/goagent/rag"
)

const (
	defaultQuestion = "How does goagent handle memory, and what is the difference with RAG?"
	defaultModel    = "qwen3:latest"
	embedModel      = "nomic-embed-text"
	maxQueries      = 4
)

func main() {
	question := defaultQuestion
	if len(os.Args) > 1 {
		question = strings.Join(os.Args[1:], " ")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Shared Ollama client — one connection pool for provider and embedder.
	client := ollama.NewClient()

	provider := ollama.NewWithClient(client)
	embedder := ollama.NewEmbedderWithClient(client,
		ollama.WithEmbedModel(embedModel),
	)

	// RAG pipeline: chunk -> embed -> store.
	chunker := vector.NewRecursiveChunker(vector.WithRCMaxSize(400))
	store := vector.NewInMemoryStore()
	ragPipeline, err := rag.NewPipeline(chunker, embedder, store)
	if err != nil {
		log.Fatalf("creating rag pipeline: %v", err)
	}

	if err := indexDocs(ctx, ragPipeline); err != nil {
		log.Fatalf("indexing docs: %v", err)
	}

	// Planner: decomposes the user query into sub-queries.
	plannerAgent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithModel(defaultModel),
		goagent.WithMaxIterations(5),
		goagent.WithSystemPrompt(fmt.Sprintf(plannerSystemPrompt, maxQueries)),
	)
	if err != nil {
		log.Fatalf("creating planner: %v", err)
	}

	// Synthesizer: produces the final answer from search results.
	synthAgent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithModel(defaultModel),
		goagent.WithMaxIterations(5),
		goagent.WithSystemPrompt(synthesizerSystemPrompt),
	)
	if err != nil {
		log.Fatalf("creating synthesizer: %v", err)
	}

	// Three-stage pipeline: plan -> search -> answer.
	pipeline := orchestration.NewPipeline(
		orchestration.WithStages(
			orchestration.Stage("plan",
				orchestration.AgentStage(plannerAgent, orchestration.GoalOnly),
			),
			orchestration.Stage("search",
				NewParallelRAGExecutor(ragPipeline, 4),
			),
			orchestration.Stage("answer",
				orchestration.AgentStage(synthAgent, orchestration.OutputOf("search")),
			),
		),
	)

	fmt.Printf("Question: %s\n\n", question)

	sc, err := pipeline.Run(ctx, question)
	if err != nil {
		log.Fatalf("pipeline error: %v", err)
	}

	answer, err := sc.RequireOutput("answer")
	if err != nil {
		log.Fatalf("no answer in stage context: %v", err)
	}

	fmt.Println(answer)

	fmt.Println("\n--- Trace ---")
	for _, t := range sc.Trace() {
		fmt.Printf("  %-8s %s\n", t.StageName+":", t.Duration.Round(time.Millisecond))
	}
}

// indexDocs indexes the sample documents into the RAG pipeline.
func indexDocs(ctx context.Context, p *rag.Pipeline) error {
	docs := []rag.Document{
		{Source: "memory.md",     Content: []goagent.ContentBlock{goagent.TextBlock(memoryDoc)}},
		{Source: "rag.md",        Content: []goagent.ContentBlock{goagent.TextBlock(ragDoc)}},
		{Source: "quickstart.md", Content: []goagent.ContentBlock{goagent.TextBlock(quickstartDoc)}},
	}
	return p.Index(ctx, docs...)
}
