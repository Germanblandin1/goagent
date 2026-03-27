package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	goagent "github.com/Germanblandin1/goagent"
)

// Transport identifies the connection mechanism of a Client.
type Transport string

const (
	TransportStdio Transport = "stdio"
	TransportSSE   Transport = "sse"
)

// Client represents an active connection to an MCP server.
// It is safe for concurrent use after construction.
// It must be closed with Close when no longer needed.
type Client struct {
	inner     mcpclient.MCPClient
	tools     []goagent.Tool // discovered tools, immutable after construction
	transport Transport
	addr      string // cmd for stdio, url for SSE
	logger    *slog.Logger
}

// NewStdioClient starts cmd with args as a subprocess, performs the MCP
// handshake, and discovers all available tools.
// The subprocess is terminated when Close is called or the context is cancelled.
//
// Example:
//
//	client, err := mcp.NewStdioClient(ctx, logger,
//	    "npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp",
//	)
func NewStdioClient(ctx context.Context, logger *slog.Logger, cmd string, args ...string) (*Client, error) {
	inner, err := mcpclient.NewStdioMCPClient(cmd, nil, args...)
	if err != nil {
		return nil, &MCPConnectionError{Transport: TransportStdio, Addr: cmd, Cause: err}
	}

	c := &Client{inner: inner, transport: TransportStdio, addr: cmd, logger: logger}

	if err := c.initializeWithRetry(ctx); err != nil {
		_ = inner.Close()
		return nil, err
	}
	if err := c.discoverTools(ctx); err != nil {
		_ = inner.Close()
		return nil, err
	}
	return c, nil
}

// NewSSEClient connects to an HTTP+SSE MCP server already running at url,
// performs the handshake, and discovers all available tools.
//
// Example:
//
//	client, err := mcp.NewSSEClient(ctx, logger, "http://localhost:8080/sse")
func NewSSEClient(ctx context.Context, logger *slog.Logger, url string) (*Client, error) {
	inner, err := mcpclient.NewSSEMCPClient(url)
	if err != nil {
		return nil, &MCPConnectionError{Transport: TransportSSE, Addr: url, Cause: err}
	}

	c := &Client{inner: inner, transport: TransportSSE, addr: url, logger: logger}

	if err := c.initializeWithRetry(ctx); err != nil {
		_ = inner.Close()
		return nil, err
	}
	if err := c.discoverTools(ctx); err != nil {
		_ = inner.Close()
		return nil, err
	}
	return c, nil
}

// initializeWithRetry performs the MCP handshake with exponential backoff.
// Maximum 3 attempts. Delays: 100ms, 200ms, 400ms.
// Respects context cancellation between retries.
func (c *Client) initializeWithRetry(ctx context.Context) error {
	const maxAttempts = 3
	delay := 100 * time.Millisecond

	var lastErr error
	for attempt := range maxAttempts {
		_, err := c.inner.Initialize(ctx, mcp.InitializeRequest{})
		if err == nil {
			return nil
		}

		lastErr = err
		c.logger.Debug("mcp initialize attempt failed",
			"attempt", attempt+1,
			"max", maxAttempts,
			"delay", delay,
			"err", err,
		)

		if attempt == maxAttempts-1 {
			break
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
			delay *= 2
		}
	}

	return &MCPConnectionError{
		Transport: c.transport,
		Addr:      c.addr,
		Cause:     lastErr,
	}
}

// discoverTools calls tools/list, adapts each tool, and fills c.tools.
// Called once during construction — tools do not change at runtime.
func (c *Client) discoverTools(ctx context.Context) error {
	result, err := c.inner.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return &MCPDiscoveryError{Transport: c.transport, Cause: fmt.Errorf("mcp tools/list: %w", err)}
	}

	c.tools = make([]goagent.Tool, len(result.Tools))
	for i, t := range result.Tools {
		c.tools[i] = adapt(t, c)
	}

	c.logger.Debug("mcp tools discovered",
		"transport", c.transport,
		"count", len(c.tools),
	)
	return nil
}

// Tools returns the tools discovered during construction.
// The returned slice is immutable — do not modify.
func (c *Client) Tools() []goagent.Tool { return c.tools }

// Transport returns the connection mechanism used by this client.
func (c *Client) Transport() Transport { return c.transport }

// Close closes the connection cleanly.
// For stdio: terminates the subprocess.
// For SSE: closes the HTTP connection.
// Idempotent — multiple calls are safe.
func (c *Client) Close() error {
	return c.inner.Close()
}
