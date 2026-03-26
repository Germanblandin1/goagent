package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	goagent "github.com/Germanblandin1/goagent"
)

// mockCallToolClient is a minimal MCPClient that only implements CallTool.
type mockCallToolClient struct {
	result *mcp.CallToolResult
	err    error
	// embed the rest of the interface with panics
	panicMCPClient
}

func (m *mockCallToolClient) CallTool(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return m.result, m.err
}

func TestAdapt_Definition(t *testing.T) {
	mcpTool := mcp.Tool{
		Name:        "read_file",
		Description: "Reads the content of a file",
	}

	adapted := adapt(mcpTool, nil)

	def := adapted.Definition()
	if def.Name != "read_file" {
		t.Errorf("got name %q, want %q", def.Name, "read_file")
	}
	if def.Description != mcpTool.Description {
		t.Errorf("got description %q, want %q", def.Description, mcpTool.Description)
	}
}

func TestAdapt_DefinitionWithParameters(t *testing.T) {
	mcpTool := mcp.Tool{
		Name:        "search",
		Description: "Searches for files",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"query": map[string]any{"type": "string"},
			},
			Required: []string{"query"},
		},
	}

	adapted := adapt(mcpTool, nil)
	def := adapted.Definition()

	if def.Parameters == nil {
		t.Fatal("expected non-nil Parameters")
	}
	if def.Parameters["type"] != "object" {
		t.Errorf("got type %v, want %q", def.Parameters["type"], "object")
	}
}

func TestMCPToolAdapter_Execute_Success(t *testing.T) {
	mock := &mockCallToolClient{
		result: &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{Type: "text", Text: "file contents here"},
			},
			IsError: false,
		},
	}
	adapter := &mcpToolAdapter{
		def:    goagent.ToolDefinition{Name: "read_file"},
		client: &Client{inner: mock},
	}

	blocks, err := adapter.Execute(context.Background(), map[string]any{"path": "/tmp/test.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Type != goagent.ContentText {
		t.Errorf("expected ContentText, got %v", blocks[0].Type)
	}
}

func TestMCPToolAdapter_Execute_BusinessError(t *testing.T) {
	mock := &mockCallToolClient{
		result: &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.TextContent{Type: "text", Text: "file not found"},
			},
		},
	}
	adapter := &mcpToolAdapter{
		def:    goagent.ToolDefinition{Name: "read_file"},
		client: &Client{inner: mock},
	}

	_, err := adapter.Execute(context.Background(), map[string]any{"path": "/x"})

	var toolErr *MCPToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected *MCPToolError, got %T: %v", err, err)
	}
	if toolErr.Tool != "read_file" {
		t.Errorf("got tool %q, want %q", toolErr.Tool, "read_file")
	}
	if toolErr.Message != "file not found" {
		t.Errorf("got message %q, want %q", toolErr.Message, "file not found")
	}
}

func TestMCPToolAdapter_Execute_TransportError(t *testing.T) {
	transportErr := errors.New("connection refused")
	mock := &mockCallToolClient{err: transportErr}
	adapter := &mcpToolAdapter{
		def:    goagent.ToolDefinition{Name: "read_file"},
		client: &Client{inner: mock},
	}

	_, err := adapter.Execute(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, transportErr) {
		t.Errorf("expected wrapped transportErr, got %v", err)
	}
}

func TestExtractText(t *testing.T) {
	blocks := []mcp.Content{
		mcp.TextContent{Type: "text", Text: "hello "},
		mcp.TextContent{Type: "text", Text: "world"},
	}
	got := extractText(blocks)
	if got != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}
}

func TestTranslateContent_SkipsUnknown(t *testing.T) {
	blocks := []mcp.Content{
		mcp.TextContent{Type: "text", Text: "ok"},
		mcp.ImageContent{Type: "image"}, // not translated
	}
	out := translateContent(blocks)
	if len(out) != 1 {
		t.Errorf("expected 1 block, got %d", len(out))
	}
}

// panicMCPClient implements the full client.MCPClient interface by panicking
// on all methods not overridden by the embedding struct.
type panicMCPClient struct{}

func (panicMCPClient) Initialize(context.Context, mcp.InitializeRequest) (*mcp.InitializeResult, error) {
	panic("not implemented")
}
func (panicMCPClient) Ping(context.Context) error                    { panic("not implemented") }
func (panicMCPClient) ListResourcesByPage(context.Context, mcp.ListResourcesRequest) (*mcp.ListResourcesResult, error) {
	panic("not implemented")
}
func (panicMCPClient) ListResources(context.Context, mcp.ListResourcesRequest) (*mcp.ListResourcesResult, error) {
	panic("not implemented")
}
func (panicMCPClient) ListResourceTemplatesByPage(context.Context, mcp.ListResourceTemplatesRequest) (*mcp.ListResourceTemplatesResult, error) {
	panic("not implemented")
}
func (panicMCPClient) ListResourceTemplates(context.Context, mcp.ListResourceTemplatesRequest) (*mcp.ListResourceTemplatesResult, error) {
	panic("not implemented")
}
func (panicMCPClient) ReadResource(context.Context, mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	panic("not implemented")
}
func (panicMCPClient) Subscribe(context.Context, mcp.SubscribeRequest) error {
	panic("not implemented")
}
func (panicMCPClient) Unsubscribe(context.Context, mcp.UnsubscribeRequest) error {
	panic("not implemented")
}
func (panicMCPClient) ListPromptsByPage(context.Context, mcp.ListPromptsRequest) (*mcp.ListPromptsResult, error) {
	panic("not implemented")
}
func (panicMCPClient) ListPrompts(context.Context, mcp.ListPromptsRequest) (*mcp.ListPromptsResult, error) {
	panic("not implemented")
}
func (panicMCPClient) GetPrompt(context.Context, mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	panic("not implemented")
}
func (panicMCPClient) ListToolsByPage(context.Context, mcp.ListToolsRequest) (*mcp.ListToolsResult, error) {
	panic("not implemented")
}
func (panicMCPClient) ListTools(context.Context, mcp.ListToolsRequest) (*mcp.ListToolsResult, error) {
	panic("not implemented")
}
func (panicMCPClient) CallTool(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	panic("not implemented")
}
func (panicMCPClient) SetLevel(context.Context, mcp.SetLevelRequest) error {
	panic("not implemented")
}
func (panicMCPClient) Complete(context.Context, mcp.CompleteRequest) (*mcp.CompleteResult, error) {
	panic("not implemented")
}
func (panicMCPClient) Close() error                                         { return nil }
func (panicMCPClient) OnNotification(func(mcp.JSONRPCNotification))         {}
