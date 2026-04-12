package mcp_test

import (
	"context"
	"errors"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	goagent "github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/mcp"
)

func TestTool_Definition_NameAndDescription(t *testing.T) {
	t.Parallel()

	s := mcp.NewServer("test-server", "0.0.1")
	s.MustAddTool("read_file", "Reads the content of a file", nil,
		func(_ context.Context, _ map[string]any) (string, error) { return "", nil },
	)

	client := mcp.NewTestClient(t, s)
	tools := client.Tools()

	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	def := tools[0].Definition()
	if def.Name != "read_file" {
		t.Errorf("Name = %q, want %q", def.Name, "read_file")
	}
	if def.Description != "Reads the content of a file" {
		t.Errorf("Description = %q, want %q", def.Description, "Reads the content of a file")
	}
}

func TestTool_Definition_WithSchema(t *testing.T) {
	t.Parallel()

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string"},
		},
		"required": []string{"query"},
	}
	s := mcp.NewServer("test-server", "0.0.1")
	if err := s.AddTool("search", "Searches for files", schema,
		func(_ context.Context, _ map[string]any) (string, error) { return "", nil },
	); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	client := mcp.NewTestClient(t, s)
	def := client.Tools()[0].Definition()

	if def.Parameters == nil {
		t.Fatal("expected non-nil Parameters for tool with schema")
	}
	if def.Parameters["type"] != "object" {
		t.Errorf("Parameters[type] = %v, want %q", def.Parameters["type"], "object")
	}
}

func TestTool_Definition_NilSchema(t *testing.T) {
	t.Parallel()

	s := mcp.NewServer("test-server", "0.0.1")
	s.MustAddTool("noop", "No parameters", nil,
		func(_ context.Context, _ map[string]any) (string, error) { return "", nil },
	)

	client := mcp.NewTestClient(t, s)
	def := client.Tools()[0].Definition()

	if def.Parameters != nil {
		t.Errorf("expected nil Parameters for nil schema, got %v", def.Parameters)
	}
}

func TestTool_Execute_Success(t *testing.T) {
	t.Parallel()

	s := mcp.NewServer("test-server", "0.0.1")
	s.MustAddTool("echo", "Returns the input text", nil,
		func(_ context.Context, args map[string]any) (string, error) {
			text, _ := args["text"].(string)
			return text, nil
		},
	)

	client := mcp.NewTestClient(t, s)
	blocks, err := client.Tools()[0].Execute(context.Background(), map[string]any{"text": "hello"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Type != goagent.ContentText {
		t.Errorf("block type = %v, want ContentText", blocks[0].Type)
	}
	if blocks[0].Text != "hello" {
		t.Errorf("block text = %q, want %q", blocks[0].Text, "hello")
	}
}

// TestTool_Execute_SystemError verifies that a handler error is propagated as
// a Go error. System errors must not be wrapped as *mcp.MCPToolError — the
// agent treats them as hard failures that abort the turn.
func TestTool_Execute_SystemError(t *testing.T) {
	t.Parallel()

	s := mcp.NewServer("test-server", "0.0.1")
	s.MustAddTool("broken", "Always returns a system error", nil,
		func(_ context.Context, _ map[string]any) (string, error) {
			return "", errors.New("connection refused")
		},
	)

	client := mcp.NewTestClient(t, s)

	_, err := client.Tools()[0].Execute(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var toolErr *mcp.MCPToolError
	if errors.As(err, &toolErr) {
		t.Errorf("system error should not be *MCPToolError, got: %v", err)
	}
}

// TestTool_Execute_BusinessError verifies that a handler that returns
// IsError=true in the MCP result is translated to *MCPToolError. This exercises
// the extractText helper and the IsError branch in the adapter.
func TestTool_Execute_BusinessError(t *testing.T) {
	t.Parallel()

	s := mcp.NewServer("test-server", "0.0.1")

	// Register a raw tool handler that explicitly sets IsError = true so that
	// the adapter's extractText path is exercised.
	rawTool := mcplib.NewTool("find_file", mcplib.WithDescription("finds a file"))
	s.Inner().AddTool(rawTool, func(_ context.Context, _ mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		return &mcplib.CallToolResult{
			IsError: true,
			Content: []mcplib.Content{
				mcplib.NewTextContent("file not found"),
			},
		}, nil
	})

	client := mcp.NewTestClient(t, s)

	var toolFound goagent.Tool
	for _, t := range client.Tools() {
		if t.Definition().Name == "find_file" {
			toolFound = t
			break
		}
	}
	if toolFound == nil {
		t.Fatal("find_file tool not discovered")
	}

	_, err := toolFound.Execute(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for IsError=true result, got nil")
	}

	var toolErr *mcp.MCPToolError
	if !errors.As(err, &toolErr) {
		t.Errorf("expected *MCPToolError, got %T: %v", err, err)
	}
	if toolErr != nil && toolErr.Message != "file not found" {
		t.Errorf("MCPToolError.Message = %q, want %q", toolErr.Message, "file not found")
	}
}
