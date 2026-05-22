package main

// plannerSystemPrompt instructs the Planner to decompose the user query
// into focused sub-queries. The %d placeholder is replaced with maxQueries
// at agent construction time via fmt.Sprintf.
const plannerSystemPrompt = `You are a query decomposition specialist.
Your job is to break down a complex question into focused sub-queries
that can each be answered by searching a technical knowledge base.

Rules:
- Output ONLY a JSON object, no explanation, no markdown.
- Maximum %d queries. If the question is simple, use fewer.
- Each query must be self-contained and specific.
- Prefer technical terms over natural language in queries.

Output format:
{"queries": ["query one", "query two", "query three"]}`

// synthesizerSystemPrompt instructs the Synthesizer to produce a single
// well-structured answer from the retrieved search results.
const synthesizerSystemPrompt = `You are a technical documentation expert.
You receive search results from multiple queries against a knowledge base.
Your job is to synthesize a single, accurate, well-structured answer.

Rules:
- Cite your sources inline using [source: filename] notation.
- Do not invent information not present in the search results.
- If results are contradictory, acknowledge it.
- Be concise but complete.`

// Sample goagent documentation indexed into the RAG pipeline at startup.
const memoryDoc = `# Memory in goagent

goagent supports two memory types:

## ShortTermMemory
Holds conversation history within a single session. The interface has two methods:
- Messages(ctx) returns the filtered history seen by the provider
- Append(ctx, msgs...) stores new messages

Built-in implementations: BufferMemory (unlimited history), SlidingWindowMemory (last N messages).
Configure with goagent.WithShortTermMemory(mem).

Filtering policies live in memory/policy:
- FixedWindow: keeps the last N messages
- TokenWindow: keeps messages within a token budget

Policies are applied in Messages() — never in Append(). This means writes are always complete;
the provider only sees the filtered view.

## LongTermMemory
Provides semantic retrieval across sessions using a vector store and an embedder.
The interface:
- Store(ctx, msgs...) persists messages
- Retrieve(ctx, query, topK, opts...) returns the most similar stored messages

Requires both a goagent.Embedder and a goagent.VectorStore.
Configure with goagent.WithLongTermMemory(ltm).
Use goagent.WithLongTermTopK(k) to control how many past messages are retrieved per run.
Use goagent.WithWritePolicy(p) to control what gets persisted (StoreAlways, MinLength).

## Memory vs RAG
ShortTermMemory holds the active conversation window.
LongTermMemory recalls previous conversations using semantic search.
RAG (rag package) indexes arbitrary documents — not conversation history.
All three can be combined: ShortTerm for the current turn, LongTerm for past sessions,
RAG for the knowledge base.
`

const ragDoc = `# RAG in goagent

RAG (Retrieval-Augmented Generation) is implemented in the rag package.

## Pipeline
rag.NewPipeline(chunker, embedder, store) wires together three components:
- vector.Chunker: splits documents into overlapping text segments
- goagent.Embedder: converts text blocks to float32 vectors
- goagent.VectorStore: stores and retrieves vectors by cosine similarity

Pipeline methods:
- Index(ctx, docs...): chunk -> embed -> upsert. Idempotent; re-indexing by same Source replaces existing chunks.
- Search(ctx, query, topK): embed query -> vector search -> return []SearchResult

Each SearchResult has: Message (content blocks), Score (cosine similarity in [0,1]),
RerankScore (reranker score, 0 when no reranker configured), Source (origin filename).

## Agentic RAG patterns
Pattern 1 — Tool: wrap the pipeline in rag.NewTool and give it to an agent.
  The agent decides when to search and what queries to use.

Pattern 2 — Standalone: call pipeline.Search directly in application code.
  Good for batch processing or non-conversational use cases.

Pattern 3 — Multi-agent: a Planner agent decomposes the question into sub-queries,
  a pure Go executor fans them out in parallel, and a Synthesizer merges the results.
  This example demonstrates Pattern 3.

## Reranking
rag.WithReranker(reranker, rerankN) adds a second-stage relevance scorer.
Uses the over-fetch pattern: fetches rerankN candidates, reranker selects topK.
LLMReranker is the built-in implementation using any goagent.Provider.
`

const quickstartDoc = `# goagent Quickstart

goagent is a Go-idiomatic framework for building AI agents using the ReAct loop.

## Creating an agent

agent, err := goagent.New(
    goagent.WithProvider(provider),
    goagent.WithModel("model-name"),
    goagent.WithSystemPrompt("You are a helpful assistant."),
)
response, err := agent.Run(ctx, "What is 2+2?")

## Tools
Tools allow the agent to call external functions during the ReAct loop:

calc := goagent.ToolFunc("calculator", "Evaluates math expressions",
    goagent.SchemaFrom(struct{ Expression string }{}),
    func(ctx context.Context, args map[string]any) (string, error) {
        return evaluate(args["expression"].(string)), nil
    },
)
agent, _ := goagent.New(goagent.WithTool(calc), ...)

## ReAct loop
The agent iterates: think -> act (call tools) -> observe -> repeat.
WithMaxIterations(n) caps the number of loop iterations (default 10).
The loop stops when the model produces a final answer without tool calls.

## Orchestration
Multiple agents can be composed into pipelines and graphs.
orchestration.NewPipeline chains stages sequentially — each stage is an Executor.
Executors can be agent adapters, parallel groups, or pure Go logic.
The StageContext carries outputs between stages; use RequireOutput to read them.
`
