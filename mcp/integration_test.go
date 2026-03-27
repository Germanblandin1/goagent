package mcp_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	goagent "github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/mcp"
)

func TestClientServer_ToolDiscovery(t *testing.T) {
	s := mcp.NewServer("test-server", "0.0.1")
	s.MustAddTool("echo", "Returns the same text", nil,
		func(_ context.Context, args map[string]any) (string, error) {
			text, _ := args["text"].(string)
			return text, nil
		},
	)

	client := mcp.NewTestClient(t, s)

	tools := client.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Definition().Name != "echo" {
		t.Errorf("unexpected tool name: %q", tools[0].Definition().Name)
	}
}

func TestClientServer_Execute_Success(t *testing.T) {
	s := mcp.NewServer("test-server", "0.0.1")
	s.MustAddTool("echo", "Returns the same text", nil,
		func(_ context.Context, args map[string]any) (string, error) {
			text, _ := args["text"].(string)
			return text, nil
		},
	)

	client := mcp.NewTestClient(t, s)
	tools := client.Tools()

	result, err := tools[0].Execute(context.Background(), map[string]any{"text": "hola"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) == 0 {
		t.Fatal("expected non-empty result")
	}
	if result[0].Type != goagent.ContentText {
		t.Errorf("expected ContentText, got %v", result[0].Type)
	}
}

func TestRouter_ClientFor(t *testing.T) {
	s1 := mcp.NewServer("server1", "0.0.1")
	s1.MustAddTool("tool_a", "Tool A", nil, func(_ context.Context, _ map[string]any) (string, error) { return "a", nil })

	s2 := mcp.NewServer("server2", "0.0.1")
	s2.MustAddTool("tool_b", "Tool B", nil, func(_ context.Context, _ map[string]any) (string, error) { return "b", nil })

	c1 := mcp.NewTestClient(t, s1)
	c2 := mcp.NewTestClient(t, s2)

	router := mcp.NewRouter(slog.Default(), c1, c2)

	if got, ok := router.ClientFor("tool_a"); !ok || got != c1 {
		t.Errorf("ClientFor(tool_a): got %v, ok=%v; want c1", got, ok)
	}
	if got, ok := router.ClientFor("tool_b"); !ok || got != c2 {
		t.Errorf("ClientFor(tool_b): got %v, ok=%v; want c2", got, ok)
	}
	if _, ok := router.ClientFor("unknown"); ok {
		t.Error("ClientFor(unknown): expected not found")
	}
}

func TestRouter_DuplicateName_LastWins(t *testing.T) {
	s1 := mcp.NewServer("server1", "0.0.1")
	s1.MustAddTool("shared", "From server 1", nil, func(_ context.Context, _ map[string]any) (string, error) { return "s1", nil })

	s2 := mcp.NewServer("server2", "0.0.1")
	s2.MustAddTool("shared", "From server 2", nil, func(_ context.Context, _ map[string]any) (string, error) { return "s2", nil })

	c1 := mcp.NewTestClient(t, s1)
	c2 := mcp.NewTestClient(t, s2)

	router := mcp.NewRouter(slog.Default(), c1, c2)

	got, ok := router.ClientFor("shared")
	if !ok {
		t.Fatal("expected to find 'shared'")
	}
	if got != c2 {
		t.Error("expected last-registered client (c2) to win")
	}
}

func TestClient_Close_Idempotent(t *testing.T) {
	s := mcp.NewServer("test-server", "0.0.1")
	s.MustAddTool("noop", "No-op", nil, func(_ context.Context, _ map[string]any) (string, error) { return "", nil })
	client := mcp.NewTestClient(t, s)

	if err := client.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestClient_Transport(t *testing.T) {
	t.Parallel()

	s := mcp.NewServer("test-server", "0.0.1")
	s.MustAddTool("noop", "No-op", nil, func(_ context.Context, _ map[string]any) (string, error) { return "", nil })
	client := mcp.NewTestClient(t, s)

	if client.Transport() != mcp.TransportStdio {
		t.Errorf("Transport = %q, want %q", client.Transport(), mcp.TransportStdio)
	}
}

func TestMCPConnectionError_Unwrap(t *testing.T) {
	t.Parallel()

	cause := errors.New("dial failed")
	err := &mcp.MCPConnectionError{Transport: mcp.TransportStdio, Addr: "cmd", Cause: cause}
	if !errors.Is(err, cause) {
		t.Error("expected errors.Is to find cause through Unwrap")
	}
	msg := err.Error()
	if msg == "" {
		t.Error("Error() should return a non-empty string")
	}
}

func TestMCPDiscoveryError_Unwrap(t *testing.T) {
	t.Parallel()

	cause := errors.New("timeout")
	err := &mcp.MCPDiscoveryError{Transport: mcp.TransportSSE, Cause: cause}
	if !errors.Is(err, cause) {
		t.Error("expected errors.Is to find cause through Unwrap")
	}
	msg := err.Error()
	if msg == "" {
		t.Error("Error() should return a non-empty string")
	}
}

func TestMCPToolError_Error(t *testing.T) {
	t.Parallel()

	err := &mcp.MCPToolError{Tool: "read_file", Message: "file not found"}
	got := err.Error()
	want := `mcp tool "read_file": file not found`
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
