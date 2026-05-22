// Command agentic-rag demonstrates the Agentic RAG pattern: a single agent
// that autonomously decides how many times to call a RAG search tool in order
// to answer complex questions. The LLM drives the retrieval loop — no external
// planner is needed.
//
// The agent is given a system prompt that instructs it to decompose hard
// questions into focused sub-queries and call search_knowledge_base once per
// aspect, then synthesize all retrieved context into a final answer.
//
// Prerequisites:
//
//	ollama pull nomic-embed-text   # embedding model (768-dim)
//	ollama pull qwen3:latest       # chat model (tool-capable)
//
// Usage:
//
//	go run ./agentic-rag
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector"
	"github.com/Germanblandin1/goagent/providers/ollama"
	"github.com/Germanblandin1/goagent/rag"
)

// Sample knowledge base — three documents covering different aspects of the
// fictional "Nexus" distributed task scheduler. Questions that span multiple
// aspects naturally require the agent to issue several searches.
const (
	docArchitecture = `# Nexus Architecture

Nexus is a distributed task scheduler built around three core components:

## Coordinator
The Coordinator is the central brain of Nexus. It receives task submissions,
assigns them to available Worker nodes, and tracks execution state in the
StateStore. The Coordinator exposes a gRPC API and an HTTP/JSON REST API.
It runs as a single leader with a hot standby for high availability.

## Worker
Workers pull tasks from the Coordinator's work queue via long-polling. Each
Worker runs an isolated sandbox per task (container or process). Workers report
progress and results back to the Coordinator every 5 seconds (heartbeat interval).
When a Worker crashes mid-task, the Coordinator detects the missing heartbeat
after 3 missed intervals and marks the task for retry.

## StateStore
The StateStore is a pluggable persistence layer. The default backend is
PostgreSQL; Redis and etcd are also supported. It stores task definitions,
execution history, retry counts, and scheduler configuration.

## Retry subsystem
The retry subsystem lives inside the Coordinator. When a task fails or a
Worker heartbeat is lost, the Coordinator evaluates the RetryPolicy attached
to the task. If retries remain, it re-enqueues the task with exponential
backoff. The maximum jitter applied is ±20% of the computed delay.

## Observability
All components export Prometheus metrics. Distributed tracing uses OpenTelemetry
with a configurable exporter (Jaeger, Zipkin, OTLP).`

	docAPI = `# Nexus API Reference

## Submit a task

POST /v1/tasks

Request body:
{
  "name": "string",         // human-readable label
  "payload": "base64",      // opaque bytes passed to the Worker handler
  "queue": "string",        // target queue (default: "default")
  "priority": 0-9,          // higher = processed first (default: 5)
  "retry_policy": { ... },  // see RetryPolicy object
  "timeout_seconds": 300    // max execution time (default: 300, max: 86400)
}

Response: 201 Created
{
  "task_id": "uuid",
  "status": "pending"
}

## Get task status

GET /v1/tasks/{task_id}

Response:
{
  "task_id": "uuid",
  "status": "pending|running|succeeded|failed|retrying",
  "attempt": 1,
  "created_at": "RFC3339",
  "started_at": "RFC3339 or null",
  "finished_at": "RFC3339 or null",
  "error": "string or null"
}

## Cancel a task

DELETE /v1/tasks/{task_id}

Response: 204 No Content

## RetryPolicy object

{
  "max_attempts": 3,        // total attempts including the first (default: 1 = no retry)
  "backoff": "exponential|linear|constant",
  "initial_delay_ms": 1000, // delay before the first retry (default: 1000)
  "max_delay_ms": 60000,    // cap on computed delay (default: 60000)
  "retry_on": ["timeout", "worker_crash", "task_error"]
                             // which failure kinds trigger a retry
}

## List queues

GET /v1/queues

Response: array of queue summary objects with name, depth, and worker_count.`

	docConfig = `# Nexus Configuration Reference

Configuration is loaded from nexus.yaml (or via environment variables with the
NEXUS_ prefix). All durations are in Go duration format (e.g. "5s", "2m").

## Coordinator settings

coordinator:
  listen_addr: ":8080"           # gRPC + REST listener
  leader_election_ttl: "10s"     # lease duration for HA leader election
  heartbeat_timeout: "15s"       # 3 × Worker heartbeat interval (5s × 3)
  max_tasks_per_worker: 10       # soft cap; Workers may briefly exceed this
  state_store:
    driver: "postgres"           # postgres | redis | etcd
    dsn: "postgres://..."        # connection string

## Worker settings

worker:
  id: ""                         # auto-generated UUID if empty
  coordinator_addr: "localhost:8080"
  heartbeat_interval: "5s"       # must be < coordinator.heartbeat_timeout / 3
  concurrency: 4                 # max parallel tasks per Worker process
  sandbox:
    type: "process"              # process | container
    container_image: ""          # required when type=container

## Retry defaults (task-level policy overrides these)

retry_defaults:
  max_attempts: 1                # 1 = no automatic retry
  backoff: "exponential"
  initial_delay_ms: 1000
  max_delay_ms: 60000

## Observability

observability:
  prometheus_addr: ":9090"
  tracing:
    enabled: false
    exporter: "otlp"             # otlp | jaeger | zipkin
    endpoint: "localhost:4317"

## Queue configuration

queues:
  - name: "default"
    max_depth: 100000            # 0 = unlimited
    priority_boost_after: "5m"  # promote starved tasks to max priority
  - name: "critical"
    max_depth: 1000`
)

const systemPrompt = `You are a research assistant for the Nexus distributed task scheduler.
You have access to a knowledge base containing architecture, API, and configuration documentation.

When answering questions:
1. Break complex questions into focused sub-queries covering each aspect.
2. Call search_knowledge_base once per aspect — do not try to answer from memory alone.
3. If the first search does not fully answer the question, search again with a different query.
4. Synthesize all retrieved context into a clear, accurate, and well-structured answer.
5. Always cite the source of each fact (Architecture, API Reference, or Configuration).`

func main() {
	ctx := context.Background()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// 1. Shared HTTP client — one connection pool for provider and embedder.
	client := ollama.NewClient()

	embedder := ollama.NewEmbedderWithClient(client,
		ollama.WithEmbedModel("nomic-embed-text"),
	)
	provider := ollama.NewWithClient(client)

	// 2. RAG pipeline — in-memory store, text chunker, search observer.
	chunker := vector.NewTextChunker(
		vector.WithMaxSize(400),
		vector.WithOverlap(40),
	)
	store := vector.NewInMemoryStore()

	pipeline, err := rag.NewPipeline(chunker, embedder, store,
		rag.WithSearchObserver(func(
			_ context.Context,
			query string,
			results []rag.SearchResult,
			dur time.Duration,
			searchErr error,
		) {
			if searchErr != nil {
				slog.Error("rag search failed", "query", query, "err", searchErr)
				return
			}
			topScore := 0.0
			if len(results) > 0 {
				topScore = results[0].Score
			}
			slog.Info("rag search",
				"query", query,
				"results", len(results),
				"top_score", fmt.Sprintf("%.3f", topScore),
				"dur", dur.Round(time.Millisecond),
			)
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	// 3. Index the knowledge base.
	docs := []rag.Document{
		{
			Source:  "architecture.md",
			Content: []goagent.ContentBlock{goagent.TextBlock(docArchitecture)},
		},
		{
			Source:  "api.md",
			Content: []goagent.ContentBlock{goagent.TextBlock(docAPI)},
		},
		{
			Source:  "config.md",
			Content: []goagent.ContentBlock{goagent.TextBlock(docConfig)},
		},
	}
	if err := pipeline.Index(ctx, docs...); err != nil {
		log.Fatal(err)
	}
	slog.Info("knowledge base ready", "documents", len(docs))

	// 4. RAG tool — description explicitly asks the model to call it multiple times.
	searchTool := rag.NewTool(pipeline,
		rag.WithToolName("search_knowledge_base"),
		rag.WithToolDescription(
			"Search the Nexus knowledge base for relevant documentation. "+
				"Call this tool MULTIPLE TIMES with different, focused queries to cover "+
				"all aspects of a complex question. Each call should target one specific "+
				"aspect (e.g. architecture, API, configuration).",
		),
		rag.WithTopK(3),
	)

	// 5. Agent — hooks log each tool invocation so the iterative search is visible.
	agent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithModel("qwen3:latest"),
		goagent.WithMaxIterations(20),
		goagent.WithSystemPrompt(systemPrompt),
		goagent.WithTool(searchTool),
		goagent.WithHooks(goagent.Hooks{
			OnToolCall: func(_ context.Context, name string, args map[string]any) {
				query, _ := args["query"].(string)
				slog.Info("agent calling tool", "tool", name, "query", query)
			},
			OnRunEnd: func(_ context.Context, result goagent.RunResult) {
				slog.Info("run complete",
					"iterations", result.Iterations,
					"tool_calls", result.ToolCalls,
					"dur", result.Duration.Round(time.Millisecond),
				)
			},
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	// 6. Interactive Q&A loop.
	fmt.Println("Nexus Agentic RAG — type a question, Ctrl+D to exit.")
	fmt.Println("Try: \"How do I configure retries and what component handles them?\"")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("Question: ")
		if !scanner.Scan() {
			break
		}
		question := scanner.Text()
		if question == "" {
			continue
		}

		answer, err := agent.Run(ctx, question)
		if err != nil {
			slog.Error("agent run failed", "err", err)
			continue
		}
		fmt.Printf("\nAnswer:\n%s\n\n", answer)
	}
}
