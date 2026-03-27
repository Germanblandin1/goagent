package mcp

import (
	"context"
	"log/slog"
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"
)

// NewTestClient creates a Client backed by an in-process Server, performing
// the initialize handshake and tool discovery.
//
// It is compiled only during tests and exists so that package mcp_test can
// create fully initialised Client values without accessing unexported fields.
func NewTestClient(t *testing.T, s *Server) *Client {
	t.Helper()
	inner, err := mcpclient.NewInProcessClient(s.Inner())
	if err != nil {
		t.Fatalf("NewTestClient: %v", err)
	}
	c := &Client{
		inner:     inner,
		transport: TransportStdio,
		addr:      "in-process",
		logger:    slog.Default(),
	}
	if err := c.initializeWithRetry(context.Background()); err != nil {
		t.Fatalf("NewTestClient initialize: %v", err)
	}
	if err := c.discoverTools(context.Background()); err != nil {
		t.Fatalf("NewTestClient discover: %v", err)
	}
	return c
}
