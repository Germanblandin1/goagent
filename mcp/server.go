package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// ToolHandlerFunc is the signature for a function that implements an MCP tool.
// args are the arguments sent by the caller (already deserialized).
//
// Error semantics:
//
//   - System error → return (_, err): the server cannot continue.
//     The caller receives a protocol-level error.
//     Example: the database is down.
//
//   - Business error → return ("error message", nil): the operation completed
//     but the result is negative. The agent receives the message and can reason
//     about it (retry, ask for clarification, etc.).
//     Example: the requested file does not exist, the user lacks permission.
type ToolHandlerFunc func(ctx context.Context, args map[string]any) (string, error)

// Server is an MCP server buildable with minimal boilerplate.
// Construct it with NewServer, register tools with AddTool, then start it
// with ServeStdio or ServeSSE.
//
// Not safe for concurrent use during construction (before calling Serve*).
// After Serve* returns (or the server is started), it is effectively immutable.
type Server struct {
	inner *mcpserver.MCPServer
}

// NewServer creates an MCP server with the given name and version.
// These values are exposed during the initialize handshake.
//
// Example:
//
//	s := mcp.NewServer("game-state", "1.0.0")
func NewServer(name, version string) *Server {
	return &Server{
		inner: mcpserver.NewMCPServer(name, version),
	}
}

// AddTool registers a Go function as an MCP tool.
// schema may be nil if the tool requires no arguments.
// schema may be a struct annotated with json tags.
//
// Example:
//
//	s.AddTool("get_weather",
//	    "Returns the current weather for a city",
//	    struct {
//	        City string `json:"city"`
//	    }{},
//	    func(ctx context.Context, args map[string]any) (string, error) {
//	        city, _ := args["city"].(string)
//	        return fetchWeather(city)
//	    },
//	)
func (s *Server) AddTool(name, description string, schema any, fn ToolHandlerFunc) {
	var opts []mcp.ToolOption
	opts = append(opts, mcp.WithDescription(description))
	if schema != nil {
		raw, err := json.Marshal(schema)
		if err == nil {
			// TODO: Handle error case. URGENT
			opts = append(opts, mcp.WithRawInputSchema(raw))
		}
	}

	tool := mcp.NewTool(name, opts...)

	s.inner.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := fn(ctx, req.GetArguments())
		if err != nil {
			return nil, fmt.Errorf("tool %q handler: %w", name, err)
		}
		return mcp.NewToolResultText(result), nil
	})
}

// Inner returns the underlying MCPServer for advanced use cases.
// Prefer AddTool for normal registration.
func (s *Server) Inner() *mcpserver.MCPServer {
	return s.inner
}

// ServeStdio starts the server in stdio mode (stdin/stdout).
// Blocks until stdin is closed or the process terminates.
// Use for local servers launched as subprocesses by the agent.
func (s *Server) ServeStdio() error {
	return mcpserver.ServeStdio(s.inner)
}

// ServeSSE starts an HTTP server with SSE at the given address.
// Blocks until the server is stopped.
// Use for remote servers or when multiple agents share the same server.
//
// Example:
//
//	if err := s.ServeSSE(":8080"); err != nil {
//	    log.Fatal(err)
//	}
func (s *Server) ServeSSE(addr string) error {
	return mcpserver.NewSSEServer(s.inner).Start(addr)
}
