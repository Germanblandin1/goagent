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
	{
		Question:        "What interface must a Provider implement in goagent?",
		RelevantSources: []string{"provider.md", "architecture.md"},
		ReferenceAnswer: "A Provider must implement Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error).",
	},
	{
		Question:        "How does goagent handle tool call errors?",
		RelevantSources: []string{"tools.md", "errors.md"},
		ReferenceAnswer: "Tool errors are returned as values wrapped with fmt.Errorf and %w. The agent propagates them to the caller — it does not retry automatically.",
	},
	{
		Question:        "What is the difference between RAG and LongTermMemory in goagent?",
		RelevantSources: []string{"architecture.md", "rag.md", "memory.md"},
		ReferenceAnswer: "LongTermMemory persists conversation turns automatically at the end of each Run. RAG indexes a document corpus manually before the agent starts and retrieves on demand via a Tool.",
	},
	{
		Question:        "How does the hook system work in goagent?",
		RelevantSources: []string{"hooks.md", "architecture.md"},
		ReferenceAnswer: "Hooks are a struct with optional function fields. The agent calls each non-nil hook at the corresponding lifecycle event. Adding new hooks never breaks existing callers.",
	},
	{
		Question:        "What does WithMaxIterations do in goagent?",
		RelevantSources: []string{"agent.md", "architecture.md"},
		ReferenceAnswer: "WithMaxIterations sets the maximum number of ReAct loop iterations. When reached, the agent returns the last response without error — it is a safety bound, not a failure.",
	},
	{
		Question:        "How does goagent implement the ReAct loop?",
		RelevantSources: []string{"architecture.md", "agent.md"},
		ReferenceAnswer: "The ReAct loop iterates: the model thinks and optionally calls tools, the agent executes the tools and feeds results back, until the model produces a final answer or MaxIterations is reached.",
	},
}
