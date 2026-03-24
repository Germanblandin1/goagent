package goagent

import "context"

// Tool is the interface that callers implement to give capabilities to an agent.
//
// Execute receives the arguments parsed by the model and returns content that
// is injected into the conversation as the tool result. The content can be
// text, images, documents, or any combination.
//
// For tools that only return text (the most common case), use ToolFunc which
// accepts func(...) (string, error) and wraps it automatically.
type Tool interface {
	Definition() ToolDefinition
	Execute(ctx context.Context, args map[string]any) ([]ContentBlock, error)
}

// ToolDefinition describes a tool's name, purpose, and parameter schema.
// Parameters must be a valid JSON Schema object as map[string]any.
type ToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// funcTool is the concrete type returned by ToolFunc.
type funcTool struct {
	definition ToolDefinition
	fn         func(ctx context.Context, args map[string]any) (string, error)
}

func (t *funcTool) Definition() ToolDefinition { return t.definition }

func (t *funcTool) Execute(ctx context.Context, args map[string]any) ([]ContentBlock, error) {
	result, err := t.fn(ctx, args)
	if err != nil {
		return nil, err
	}
	return []ContentBlock{TextBlock(result)}, nil
}

// ToolFunc creates a Tool from a plain function that returns text. The
// returned string is wrapped in a single text ContentBlock automatically,
// avoiding the need to define a new struct for simple tools.
//
// For tools that need to return images, documents, or mixed content, use
// ToolBlocksFunc instead.
func ToolFunc(
	name, description string,
	parameters map[string]any,
	fn func(ctx context.Context, args map[string]any) (string, error),
) Tool {
	return &funcTool{
		definition: ToolDefinition{
			Name:        name,
			Description: description,
			Parameters:  parameters,
		},
		fn: fn,
	}
}

// blocksFuncTool is the concrete type returned by ToolBlocksFunc.
type blocksFuncTool struct {
	definition ToolDefinition
	fn         func(ctx context.Context, args map[string]any) ([]ContentBlock, error)
}

func (t *blocksFuncTool) Definition() ToolDefinition { return t.definition }

func (t *blocksFuncTool) Execute(ctx context.Context, args map[string]any) ([]ContentBlock, error) {
	return t.fn(ctx, args)
}

// ToolBlocksFunc creates a Tool from a plain function that returns multimodal
// content. Use this when the tool needs to return images, documents, or a
// combination of content types.
//
// For tools that only return text, prefer ToolFunc which is more ergonomic.
func ToolBlocksFunc(
	name, description string,
	parameters map[string]any,
	fn func(ctx context.Context, args map[string]any) ([]ContentBlock, error),
) Tool {
	return &blocksFuncTool{
		definition: ToolDefinition{
			Name:        name,
			Description: description,
			Parameters:  parameters,
		},
		fn: fn,
	}
}

// ToolResult holds the outcome of a single tool execution dispatched by the agent.
type ToolResult struct {
	ToolCallID string
	Name       string
	Content    []ContentBlock
	Err        error
}
