package rag

import (
	"context"
	"fmt"
	"strings"

	"github.com/Germanblandin1/goagent"
)

// ragTool implements [goagent.Tool] using a Pipeline as the search backend.
// Construct via [NewTool] — do not instantiate directly.
type ragTool struct {
	def      goagent.ToolDefinition
	pipeline *Pipeline
	topK     int
	format   func([]SearchResult) []goagent.ContentBlock
}

// NewTool converts a Pipeline into a [goagent.Tool]. The agent will invoke it
// when it decides to search the knowledge base. The Pipeline must be indexed
// before registering the tool with the agent.
//
// Panics if pipeline is nil — this is a programming error, not a runtime one.
//
// Example:
//
//	pipeline, _ := rag.NewPipeline(chunker, embedder, store)
//	if err := pipeline.Index(ctx, docs...); err != nil { ... }
//
//	tool := rag.NewTool(pipeline,
//	    rag.WithToolName("search_docs"),
//	    rag.WithToolDescription("Search goagent internal documentation."),
//	    rag.WithTopK(3),
//	)
//	agent, _ := goagent.New(goagent.WithTool(tool), ...)
func NewTool(pipeline *Pipeline, opts ...ToolOption) goagent.Tool {
	if pipeline == nil {
		panic("rag: NewTool requires a non-nil Pipeline")
	}
	cfg := &toolConfig{
		name:        "search_knowledge_base",
		description: "Search the knowledge base for relevant information.",
		topK:        3,
		format:      defaultFormat,
	}
	for _, o := range opts {
		o(cfg)
	}
	return &ragTool{
		def: goagent.ToolDefinition{
			Name:        cfg.name,
			Description: cfg.description,
			Parameters: goagent.SchemaFrom(struct {
				Query string `json:"query" jsonschema_description:"The search query to find relevant information"`
			}{}),
		},
		pipeline: pipeline,
		topK:     cfg.topK,
		format:   cfg.format,
	}
}

func (t *ragTool) Definition() goagent.ToolDefinition { return t.def }

func (t *ragTool) Execute(
	ctx  context.Context,
	args map[string]any,
) ([]goagent.ContentBlock, error) {
	query, ok := args["query"].(string)
	if !ok || strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("rag: missing or empty 'query' argument")
	}

	results, err := t.pipeline.Search(ctx, query, t.topK)
	if err != nil {
		return nil, fmt.Errorf("rag: search failed: %w", err)
	}

	return t.format(results), nil
}

// toolConfig holds the configuration for a ragTool.
type toolConfig struct {
	name        string
	description string
	topK        int
	format      func([]SearchResult) []goagent.ContentBlock
}

// ToolOption configures a ragTool at construction time.
type ToolOption func(*toolConfig)

// WithToolName overrides the tool name visible to the model.
// Default: "search_knowledge_base".
func WithToolName(name string) ToolOption {
	return func(c *toolConfig) { c.name = name }
}

// WithToolDescription overrides the tool description.
// The description is critical — the model uses it to decide when to invoke RAG.
// Be specific about the corpus: "Search goagent documentation" is better than
// "Search for information".
func WithToolDescription(desc string) ToolOption {
	return func(c *toolConfig) { c.description = desc }
}

// WithTopK sets how many chunks to retrieve per search.
// Default: 3. Higher values provide more context but consume more tokens.
// Trade-off: more context vs. risk of retrieving irrelevant chunks (noise).
func WithTopK(k int) ToolOption {
	return func(c *toolConfig) { c.topK = k }
}

// WithFormatter replaces the result serialisation sent to the model.
// Default: plain text with source and score (suitable for all providers).
// For image corpora with multimodal embeddings: [MultimodalFormat].
// For custom formats: any func([]SearchResult) []goagent.ContentBlock.
func WithFormatter(f func([]SearchResult) []goagent.ContentBlock) ToolOption {
	return func(c *toolConfig) { c.format = f }
}
