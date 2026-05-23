package main

// EvalSample is a complete evaluation case.
// RelevantSources are the SearchResult.Source values that should appear in top-K.
// ReferenceAnswer is the correct answer — used by the RAGAS evaluator for Context Recall.
type EvalSample struct {
	Question        string
	RelevantSources []string // ground truth for Layer 1 metrics
	ReferenceAnswer string   // ground truth for Context Recall (Layer 2)
}

var evalDataset = []EvalSample{
	// --- Provider ---
	{
		Question:        "What interface must a Provider implement in goagent?",
		RelevantSources: []string{"provider.md", "architecture.md"},
		ReferenceAnswer: "A Provider must implement Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error).",
	},
	{
		Question:        "How must provider implementations wrap errors in goagent?",
		RelevantSources: []string{"provider.md", "errors.md"},
		ReferenceAnswer: "Provider implementations must wrap errors as *ProviderError with the Provider name and Cause fields. All errors must use %w for wrapping to support errors.Is and errors.As chains.",
	},
	{
		Question:        "What is StreamingProvider and when does the agent use it?",
		RelevantSources: []string{"provider.md", "agent.md"},
		ReferenceAnswer: "StreamingProvider is an optional interface with CompleteStream. The agent detects it at runtime via RunStream and falls back to Complete when the provider does not implement it.",
	},

	// --- Tools ---
	{
		Question:        "How does goagent handle tool call errors?",
		RelevantSources: []string{"tools.md", "errors.md"},
		ReferenceAnswer: "Tool errors are wrapped in ToolExecutionError and reported to the model as text — they do not abort the loop. The model decides whether to retry or give up.",
	},
	{
		Question:        "How do you create a simple tool in goagent that returns text?",
		RelevantSources: []string{"tools.md"},
		ReferenceAnswer: "Use ToolFunc with a name, description, schema from SchemaFrom, and a function returning (string, error). The agent wraps the text in ContentBlocks automatically.",
	},
	{
		Question:        "What is ToolBlocksFunc and when should you use it instead of ToolFunc?",
		RelevantSources: []string{"tools.md"},
		ReferenceAnswer: "ToolBlocksFunc is used when a tool needs to return multimodal ContentBlocks (images, documents). ToolFunc is used for tools that return plain text.",
	},
	{
		Question:        "How does the circuit breaker work for tool execution in goagent?",
		RelevantSources: []string{"tools.md"},
		ReferenceAnswer: "WithCircuitBreaker(n, d) opens the circuit after n consecutive failures, returning a CircuitOpenError that wraps ToolExecutionError. WithToolTimeout(d) cancels the tool's ctx after duration d.",
	},

	// --- Errors ---
	{
		Question:        "What typed errors does goagent define and when is each one returned?",
		RelevantSources: []string{"errors.md"},
		ReferenceAnswer: "ToolExecutionError wraps failed tool calls. ProviderError wraps LLM backend errors. MaxIterationsError is returned when the loop budget is exhausted. ErrToolNotFound is a sentinel for missing tools.",
	},

	// --- Agent ---
	{
		Question:        "How do you configure a goagent agent with a provider, model, and tools?",
		RelevantSources: []string{"agent.md"},
		ReferenceAnswer: "Use goagent.New with functional options: WithProvider (required), WithModel (required), and WithTool (repeatable). Optional: WithSystemPrompt, WithMaxIterations, WithShortTermMemory.",
	},
	{
		Question:        "What does WithMaxIterations do in goagent?",
		RelevantSources: []string{"agent.md", "architecture.md"},
		ReferenceAnswer: "WithMaxIterations sets the maximum number of ReAct loop iterations. When reached, the agent returns the last response and a MaxIterationsError — it is a safety bound, not a failure.",
	},
	{
		Question:        "What happens when the ReAct loop reaches MaxIterations in goagent?",
		RelevantSources: []string{"agent.md", "errors.md"},
		ReferenceAnswer: "The agent returns the last response along with a MaxIterationsError. The caller can detect it with errors.As(err, &maxErr) and inspect maxErr.Iterations.",
	},
	{
		Question:        "What does RunStream do differently from Run in goagent?",
		RelevantSources: []string{"agent.md"},
		ReferenceAnswer: "RunStream uses the provider's streaming interface when available, delivering tokens incrementally. It falls back to Complete when the provider does not implement StreamingProvider.",
	},

	// --- Architecture / ReAct ---
	{
		Question:        "How does goagent implement the ReAct loop?",
		RelevantSources: []string{"architecture.md", "agent.md"},
		ReferenceAnswer: "The ReAct loop calls provider.Complete, then executes any tool calls in parallel and feeds results back as messages, repeating until the model produces a final answer or MaxIterations is reached.",
	},

	// --- Memory ---
	{
		Question:        "What is the difference between RAG and LongTermMemory in goagent?",
		RelevantSources: []string{"architecture.md", "rag.md", "memory.md"},
		ReferenceAnswer: "LongTermMemory persists conversation turns automatically at the end of each Run. RAG indexes an arbitrary document corpus and retrieves on demand via a Tool — not conversation history.",
	},
	{
		Question:        "How does filtering work in ShortTermMemory?",
		RelevantSources: []string{"memory.md"},
		ReferenceAnswer: "Filtering policies (FixedWindow, TokenWindow) are applied in Messages(), never in Append(). FixedWindow keeps the last N messages; TokenWindow keeps messages within a token budget.",
	},
	{
		Question:        "How is LongTermMemory configured and how does retrieval work?",
		RelevantSources: []string{"memory.md"},
		ReferenceAnswer: "Configure with WithLongTermMemory. Use WithLongTermTopK to control how many past messages are retrieved per run. Use WithWritePolicy to control what gets persisted (StoreAlways or MinLength).",
	},

	// --- RAG ---
	{
		Question:        "How do you use the RAG pipeline as a tool for an agent?",
		RelevantSources: []string{"rag.md"},
		ReferenceAnswer: "Use rag.NewTool(pipeline) to wrap the pipeline as a goagent.Tool. The pipeline must be indexed before registering the tool. The agent invokes it when it decides to search the corpus.",
	},
	{
		Question:        "What is the over-fetch pattern in RAG reranking and how does it work?",
		RelevantSources: []string{"rag.md"},
		ReferenceAnswer: "The over-fetch pattern retrieves rerankN candidates from the vector store (more than the final topK), then the reranker selects the best topK. It improves precision without sacrificing recall.",
	},
	{
		Question:        "What retrieval metrics does the rag package provide?",
		RelevantSources: []string{"rag.md"},
		ReferenceAnswer: "PrecisionAtK: fraction of top-K retrieved that are relevant. RecallAtK: fraction of all relevant docs retrieved within top-K. ReciprocalRank: 1/position of first relevant result. MRR: mean ReciprocalRank across cases.",
	},

	// --- Hooks ---
	{
		Question:        "How does the hook system work in goagent?",
		RelevantSources: []string{"hooks.md", "architecture.md"},
		ReferenceAnswer: "Hooks are a struct with optional function fields. The agent calls each non-nil hook at the corresponding lifecycle event. Adding new hook fields never breaks existing callers.",
	},
	{
		Question:        "How do you integrate OpenTelemetry tracing into a goagent agent?",
		RelevantSources: []string{"hooks.md"},
		ReferenceAnswer: "The otel/ package provides hook implementations that emit spans and RED metrics. Use otel.WithHooks(tracer) to add tracing without modifying application code.",
	},
}
