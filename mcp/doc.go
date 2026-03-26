// Package mcp provides MCP (Model Context Protocol) integration for goagent.
//
// It wraps mark3labs/mcp-go so that MCP tools are indistinguishable from
// local tools in the ReAct loop. Connecting an MCP server is a single call:
//
//	agent, err := goagent.New(
//	    goagent.WithProvider(provider),
//	    goagent.WithMCPStdio("npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp"),
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer agent.Close()
//
// # Architecture
//
// The central piece is adapter.go, which converts mcp.Tool → goagent.Tool.
// Once adapted, the tool is opaque to the rest of the framework.
//
//   - client.go: connection lifecycle (stdio and SSE transports)
//   - adapter.go: mcp.Tool → goagent.Tool (THE key piece)
//   - server.go: builder for your own MCP servers in Go
//   - router.go: dispatch tool calls to the correct client when multiple
//     MCP servers expose tools with the same name
//
// # Error semantics in MCP servers
//
// When implementing a ToolHandlerFunc for Server.AddTool:
//
//   - System error → return (_, err): the server cannot continue.
//     Example: the database is down.
//
//   - Business error → return ("error message", nil): the operation
//     completed but the result is negative. The agent receives the message
//     and can reason about it.
//     Example: the requested file does not exist.
package mcp
