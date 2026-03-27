package mcp_test

import (
	"errors"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/internal/testutil"
	"github.com/Germanblandin1/goagent/mcp"
)

// TestWithStdio_ConnectionError verifies that WithStdio wraps the connection
// failure as *mcp.MCPConnectionError when the command cannot be started.
func TestWithStdio_ConnectionError(t *testing.T) {
	// "nonexistent-binary-xyz" must not be present on any test machine.
	_, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider()),
		mcp.WithStdio("nonexistent-binary-xyz"),
	)
	if err == nil {
		t.Fatal("expected error for nonexistent command, got nil")
	}
	var connErr *mcp.MCPConnectionError
	if !errors.As(err, &connErr) {
		t.Errorf("want *MCPConnectionError in chain, got %T: %v", err, err)
	}
}

// TestWithSSE_ConnectionError verifies that WithSSE wraps failures as
// *mcp.MCPConnectionError when the server is unreachable.
func TestWithSSE_ConnectionError(t *testing.T) {
	// Port 1 is reserved/always refused; use it to get an immediate rejection.
	_, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider()),
		mcp.WithSSE("http://127.0.0.1:1/sse"),
	)
	if err == nil {
		t.Fatal("expected error for unreachable SSE server, got nil")
	}
	var connErr *mcp.MCPConnectionError
	if !errors.As(err, &connErr) {
		t.Errorf("want *MCPConnectionError in chain, got %T: %v", err, err)
	}
}
