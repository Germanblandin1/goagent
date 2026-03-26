package mcp

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"

	goagent "github.com/Germanblandin1/goagent"
)

// newTestClient creates a Client backed by an in-process Server, performing
// the initialize handshake and tool discovery. Used only in tests.
func newTestClient(t *testing.T, s *Server) *Client {
	t.Helper()
	inner, err := mcpclient.NewInProcessClient(s.Inner())
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{
		inner:     inner,
		transport: TransportStdio,
		addr:      "in-process",
		logger:    slog.Default(),
	}
	if err := c.initializeWithRetry(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.discoverTools(context.Background()); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestClientServer_ToolDiscovery(t *testing.T) {
	s := NewServer("test-server", "0.0.1")
	s.MustAddTool("echo", "Returns the same text", nil,
		func(_ context.Context, args map[string]any) (string, error) {
			text, _ := args["text"].(string)
			return text, nil
		},
	)

	client := newTestClient(t, s)

	tools := client.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Definition().Name != "echo" {
		t.Errorf("unexpected tool name: %q", tools[0].Definition().Name)
	}
}

func TestClientServer_Execute_Success(t *testing.T) {
	s := NewServer("test-server", "0.0.1")
	s.MustAddTool("echo", "Returns the same text", nil,
		func(_ context.Context, args map[string]any) (string, error) {
			text, _ := args["text"].(string)
			return text, nil
		},
	)

	client := newTestClient(t, s)
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
	s1 := NewServer("server1", "0.0.1")
	s1.MustAddTool("tool_a", "Tool A", nil, func(_ context.Context, _ map[string]any) (string, error) { return "a", nil })

	s2 := NewServer("server2", "0.0.1")
	s2.MustAddTool("tool_b", "Tool B", nil, func(_ context.Context, _ map[string]any) (string, error) { return "b", nil })

	c1 := newTestClient(t, s1)
	c2 := newTestClient(t, s2)

	router := NewRouter(slog.Default(), c1, c2)

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
	s1 := NewServer("server1", "0.0.1")
	s1.MustAddTool("shared", "From server 1", nil, func(_ context.Context, _ map[string]any) (string, error) { return "s1", nil })

	s2 := NewServer("server2", "0.0.1")
	s2.MustAddTool("shared", "From server 2", nil, func(_ context.Context, _ map[string]any) (string, error) { return "s2", nil })

	c1 := newTestClient(t, s1)
	c2 := newTestClient(t, s2)

	router := NewRouter(slog.Default(), c1, c2)

	got, ok := router.ClientFor("shared")
	if !ok {
		t.Fatal("expected to find 'shared'")
	}
	if got != c2 {
		t.Error("expected last-registered client (c2) to win")
	}
}

func TestClient_Close_Idempotent(t *testing.T) {
	s := NewServer("test-server", "0.0.1")
	s.MustAddTool("noop", "No-op", nil, func(_ context.Context, _ map[string]any) (string, error) { return "", nil })
	client := newTestClient(t, s)

	if err := client.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestMCPConnectionError_Unwrap(t *testing.T) {
	cause := errors.New("dial failed")
	err := &MCPConnectionError{Transport: TransportStdio, Addr: "cmd", Cause: cause}
	if !errors.Is(err, cause) {
		t.Error("expected errors.Is to find cause through Unwrap")
	}
}

func TestMCPDiscoveryError_Unwrap(t *testing.T) {
	cause := errors.New("timeout")
	err := &MCPDiscoveryError{Transport: TransportSSE, Cause: cause}
	if !errors.Is(err, cause) {
		t.Error("expected errors.Is to find cause through Unwrap")
	}
}
