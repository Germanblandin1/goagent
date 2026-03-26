package mcp

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	goagent "github.com/Germanblandin1/goagent"
)

// WithStdio returns a goagent.Option that starts cmd with args as a subprocess
// MCP server, discovers its tools, and adds them to the agent.
// The subprocess is terminated when Agent.Close is called.
//
// The connection is established during goagent.New, which returns an error if
// the handshake or tool discovery fails.
//
// Example:
//
//	agent, err := goagent.New(
//	    goagent.WithProvider(provider),
//	    mcp.WithStdio("npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp"),
//	)
func WithStdio(cmd string, args ...string) goagent.Option {
	return goagent.WithMCPConnector(func(ctx context.Context, logger *slog.Logger) ([]goagent.Tool, io.Closer, error) {
		client, err := NewStdioClient(ctx, logger, cmd, args...)
		if err != nil {
			return nil, nil, fmt.Errorf("WithStdio %q: %w", cmd, err)
		}
		return client.Tools(), client, nil
	})
}

// WithSSE returns a goagent.Option that connects to an HTTP+SSE MCP server
// already running at url, discovers its tools, and adds them to the agent.
//
// The connection is established during goagent.New, which returns an error if
// the handshake or tool discovery fails.
//
// Example:
//
//	agent, err := goagent.New(
//	    goagent.WithProvider(provider),
//	    mcp.WithSSE("http://localhost:8080/sse"),
//	)
func WithSSE(url string) goagent.Option {
	return goagent.WithMCPConnector(func(ctx context.Context, logger *slog.Logger) ([]goagent.Tool, io.Closer, error) {
		client, err := NewSSEClient(ctx, logger, url)
		if err != nil {
			return nil, nil, fmt.Errorf("WithSSE %q: %w", url, err)
		}
		return client.Tools(), client, nil
	})
}
