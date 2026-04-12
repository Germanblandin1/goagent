package mcp_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/Germanblandin1/goagent/mcp"
)

func TestNewRouter_Empty(t *testing.T) {
	t.Parallel()

	r := mcp.NewRouter(slog.Default())
	if r == nil {
		t.Fatal("NewRouter returned nil")
	}

	_, ok := r.ClientFor("any_tool")
	if ok {
		t.Error("ClientFor on empty router returned ok=true, want false")
	}
}

func TestNewRouter_ClientFor_KnownTool(t *testing.T) {
	t.Parallel()

	s := mcp.NewServer("test", "0.0.1")
	s.MustAddTool("read_file", "reads a file", nil,
		func(_ context.Context, _ map[string]any) (string, error) { return "", nil },
	)
	s.MustAddTool("write_file", "writes a file", nil,
		func(_ context.Context, _ map[string]any) (string, error) { return "", nil },
	)

	client := mcp.NewTestClient(t, s)
	r := mcp.NewRouter(slog.Default(), client)

	c, ok := r.ClientFor("read_file")
	if !ok {
		t.Fatal("ClientFor(read_file) = false, want true")
	}
	if c == nil {
		t.Fatal("ClientFor(read_file) returned nil client")
	}

	c2, ok2 := r.ClientFor("write_file")
	if !ok2 {
		t.Fatal("ClientFor(write_file) = false, want true")
	}
	if c2 == nil {
		t.Fatal("ClientFor(write_file) returned nil client")
	}
}

func TestNewRouter_ClientFor_UnknownTool(t *testing.T) {
	t.Parallel()

	s := mcp.NewServer("test", "0.0.1")
	s.MustAddTool("known", "known tool", nil,
		func(_ context.Context, _ map[string]any) (string, error) { return "", nil },
	)
	client := mcp.NewTestClient(t, s)
	r := mcp.NewRouter(slog.Default(), client)

	_, ok := r.ClientFor("unknown_tool")
	if ok {
		t.Error("ClientFor(unknown_tool) = true, want false")
	}
}

func TestNewRouter_MultiClient_RoutesByTool(t *testing.T) {
	t.Parallel()

	s1 := mcp.NewServer("server1", "1.0")
	s1.MustAddTool("tool_a", "from server 1", nil,
		func(_ context.Context, _ map[string]any) (string, error) { return "a", nil },
	)

	s2 := mcp.NewServer("server2", "1.0")
	s2.MustAddTool("tool_b", "from server 2", nil,
		func(_ context.Context, _ map[string]any) (string, error) { return "b", nil },
	)

	c1 := mcp.NewTestClient(t, s1)
	c2 := mcp.NewTestClient(t, s2)
	r := mcp.NewRouter(slog.Default(), c1, c2)

	ca, okA := r.ClientFor("tool_a")
	cb, okB := r.ClientFor("tool_b")

	if !okA || ca == nil {
		t.Error("tool_a not routed")
	}
	if !okB || cb == nil {
		t.Error("tool_b not routed")
	}

	// The two tools must be served by different clients.
	if ca == cb {
		t.Error("tool_a and tool_b returned the same client, want different")
	}
}

func TestNewRouter_DuplicateToolName_LastWins(t *testing.T) {
	t.Parallel()

	// Both servers expose a tool with the same name. Last-registered wins.
	s1 := mcp.NewServer("server1", "1.0")
	s1.MustAddTool("shared", "first", nil,
		func(_ context.Context, _ map[string]any) (string, error) { return "first", nil },
	)
	s2 := mcp.NewServer("server2", "1.0")
	s2.MustAddTool("shared", "second", nil,
		func(_ context.Context, _ map[string]any) (string, error) { return "second", nil },
	)

	c1 := mcp.NewTestClient(t, s1)
	c2 := mcp.NewTestClient(t, s2)

	// NewRouter logs a warning for the duplicate but must not panic.
	r := mcp.NewRouter(slog.Default(), c1, c2)

	c, ok := r.ClientFor("shared")
	if !ok {
		t.Fatal("shared tool not found after duplicate registration")
	}
	// The last-registered client (c2) should win.
	if c != c2 {
		t.Error("expected last-registered client (c2) to win for duplicate tool name")
	}
}
