package mcp

import "fmt"

// MCPConnectionError is returned when the initial connection with an MCP server
// cannot be established (handshake failed or process did not start).
type MCPConnectionError struct {
	Transport Transport
	Addr      string // command for stdio, URL for SSE
	Cause     error
}

func (e *MCPConnectionError) Error() string {
	return fmt.Sprintf("mcp %s connection to %q failed: %v", e.Transport, e.Addr, e.Cause)
}

func (e *MCPConnectionError) Unwrap() error { return e.Cause }

// MCPToolError represents a business-level error returned by an MCP tool.
// This corresponds to result.IsError == true in the protocol.
// The ReAct loop formats it as text so the model can reason about it
// (retry, ask for clarification, offer an alternative, etc.).
type MCPToolError struct {
	Tool    string
	Message string
}

func (e *MCPToolError) Error() string {
	return fmt.Sprintf("mcp tool %q: %s", e.Tool, e.Message)
}

// MCPDiscoveryError is returned when tools/list fails after the handshake.
type MCPDiscoveryError struct {
	Transport Transport
	Cause     error
}

func (e *MCPDiscoveryError) Error() string {
	return fmt.Sprintf("mcp %s tool discovery failed: %v", e.Transport, e.Cause)
}

func (e *MCPDiscoveryError) Unwrap() error { return e.Cause }
