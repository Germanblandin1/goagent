package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	goagent "github.com/Germanblandin1/goagent"
)

// mcpToolAdapter implements goagent.Tool for a tool discovered on an MCP server.
// It is immutable after construction and not exported — created exclusively via adapt.
type mcpToolAdapter struct {
	def    goagent.ToolDefinition
	client *Client
}

// adapt converts an MCP protocol tool into a goagent.Tool.
// The InputSchema is marshaled to map[string]any to match goagent.ToolDefinition.Parameters.
func adapt(t mcp.Tool, c *Client) goagent.Tool {
	params := schemaToMap(t.InputSchema)
	return &mcpToolAdapter{
		def: goagent.ToolDefinition{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  params,
		},
		client: c,
	}
}

// schemaToMap converts a mcp.ToolInputSchema to map[string]any via JSON round-trip.
// Returns nil if the schema is empty.
func schemaToMap(schema mcp.ToolInputSchema) map[string]any {
	data, err := json.Marshal(schema)
	if err != nil || string(data) == "null" || string(data) == "{}" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	return m
}

// Definition implements goagent.Tool.
func (a *mcpToolAdapter) Definition() goagent.ToolDefinition { return a.def }

// Execute implements goagent.Tool.
// It calls the real MCP server and translates the result to the framework format.
//
// MCP distinguishes two error categories:
//
//   - result.IsError == true: business error (e.g. file not found).
//     Modeled as MCPToolError so the dispatcher formats it as text and the
//     model can reason about it (retry, ask clarification, etc.).
//
//   - err != nil: transport or protocol error (server down, broken JSON-RPC).
//     Propagated as a Go error — the ReAct loop treats it as ToolExecutionError.
func (a *mcpToolAdapter) Execute(ctx context.Context, args map[string]any) ([]goagent.ContentBlock, error) {
	result, err := a.client.inner.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      a.def.Name,
			Arguments: args,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("mcp call %q: %w", a.def.Name, err)
	}

	if result.IsError {
		return nil, &MCPToolError{
			Tool:    a.def.Name,
			Message: extractText(result.Content),
		}
	}

	return translateContent(result.Content), nil
}

// extractText concatenates all "text" content blocks into a single string.
// Used to format business errors from MCP tool results.
func extractText(blocks []mcp.Content) string {
	var sb strings.Builder
	for _, b := range blocks {
		if tc, ok := b.(mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// translateContent converts []mcp.Content to []goagent.ContentBlock.
// Only "text" blocks are translated. Unknown block types are silently dropped.
func translateContent(blocks []mcp.Content) []goagent.ContentBlock {
	out := make([]goagent.ContentBlock, 0, len(blocks))
	for _, b := range blocks {
		if tc, ok := b.(mcp.TextContent); ok {
			out = append(out, goagent.TextBlock(tc.Text))
		}
	}
	return out
}
