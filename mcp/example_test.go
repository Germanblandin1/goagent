package mcp_test

import (
	"context"
	"fmt"
	"log"
	"log/slog"

	goagent "github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/internal/testutil"
	"github.com/Germanblandin1/goagent/mcp"
)

// Example shows the two sides of the MCP integration:
//
//   - Server side: a Go process exposing tools via [NewServer] + [Server.ServeStdio].
//   - Agent side: an agent discovering those tools via [WithStdio].
//
// No Output is provided because the agent side requires a live subprocess.
func Example() {
	// --- Server side (runs in a separate process) ---
	//
	// s := mcp.NewServer("my-tools", "1.0.0")
	// s.MustAddTool("echo", "Returns the input text unchanged", nil,
	//     func(_ context.Context, args map[string]any) (string, error) {
	//         text, _ := args["text"].(string)
	//         return text, nil
	//     },
	// )
	// log.Fatal(s.ServeStdio())

	// --- Agent side ---
	//
	// agent, err := goagent.New(
	//     goagent.WithProvider(provider),
	//     mcp.WithStdio("./my-tools-server"),
	// )
	// if err != nil {
	//     log.Fatal(err)
	// }
	// defer agent.Close()
	//
	// result, err := agent.Run(context.Background(), "echo hello")
	// fmt.Println(result)
}

// ExampleNewServer shows how to build a server with one tool per concept.
// Call [Server.ServeStdio] or [Server.ServeSSE] to start accepting connections.
func ExampleNewServer() {
	s := mcp.NewServer("file-tools", "1.0.0")

	s.MustAddTool("read_file", "Returns the content of a file at the given path", nil,
		func(_ context.Context, args map[string]any) (string, error) {
			path, _ := args["path"].(string)
			// real implementation would read the file
			return "content of " + path, nil
		},
	)

	s.MustAddTool("list_dir", "Lists the files in a directory", nil,
		func(_ context.Context, args map[string]any) (string, error) {
			dir, _ := args["dir"].(string)
			return "files in " + dir, nil
		},
	)

	fmt.Println("server ready")
	// Output: server ready
}

// ExampleServer_AddTool_withSchema shows how to attach a JSON Schema to a
// tool so that the model knows which arguments to provide.
func ExampleServer_AddTool_withSchema() {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"city": map[string]any{"type": "string", "description": "City name"},
		},
		"required": []string{"city"},
	}

	s := mcp.NewServer("weather", "1.0.0")
	err := s.AddTool("get_weather", "Returns current weather for a city", schema,
		func(_ context.Context, args map[string]any) (string, error) {
			city, _ := args["city"].(string)
			return "Sunny, 22 °C in " + city, nil
		},
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("registered")
	// Output: registered
}

// ExampleServer_AddTool_invalidSchema shows that [Server.AddTool] returns an
// error when the schema cannot be marshalled to JSON.
// Use [Server.MustAddTool] during startup if a bad schema is a programming
// error that should halt the process.
func ExampleServer_AddTool_invalidSchema() {
	s := mcp.NewServer("test", "0.0.0")
	// Channels cannot be marshalled to JSON.
	err := s.AddTool("bad", "bad schema", make(chan int), nil)
	fmt.Println(err != nil)
	// Output: true
}

// ExampleWithStdio shows how to connect a stdio MCP server to an Agent.
// The agent discovers all tools exposed by the server during [goagent.New]
// and makes them available in the ReAct loop.
//
// No Output is provided because WithStdio starts a real subprocess.
func ExampleWithStdio() {
	mock := testutil.NewMockProvider(
		goagent.CompletionResponse{
			Message:    goagent.AssistantMessage("done"),
			StopReason: goagent.StopReasonEndTurn,
		},
	)

	// In production replace mock with a real provider and the command with
	// a real MCP server binary:
	//
	//   mcp.WithStdio("npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp")
	//
	agent, err := goagent.New(
		goagent.WithProvider(mock),
		// mcp.WithStdio("my-mcp-server", "--flag"),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer agent.Close()

	_, _ = agent.Run(context.Background(), "list files in /tmp")
}

// ExampleWithSSE shows how to connect an HTTP+SSE MCP server to an Agent.
// The server must already be running at the given URL before New is called.
//
// No Output is provided because WithSSE connects to a real HTTP server.
func ExampleWithSSE() {
	mock := testutil.NewMockProvider(
		goagent.CompletionResponse{
			Message:    goagent.AssistantMessage("done"),
			StopReason: goagent.StopReasonEndTurn,
		},
	)

	// In production replace mock with a real provider and the URL with
	// the running MCP server address:
	//
	//   mcp.WithSSE("http://localhost:8080/sse")
	//
	agent, err := goagent.New(
		goagent.WithProvider(mock),
		// mcp.WithSSE("http://localhost:8080/sse"),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer agent.Close()

	_, _ = agent.Run(context.Background(), "query the remote tools")
}

// ExampleNewRouter shows how to aggregate tools from multiple MCP clients
// and look up which client owns a given tool name.
// [NewRouter] is used internally by the agent when multiple [WithStdio] or
// [WithSSE] options are provided.
//
// No Output is provided because creating real clients requires live servers.
func ExampleNewRouter() {
	// In practice c1 and c2 come from NewStdioClient or NewSSEClient.
	// Here we use in-process clients created for illustration only.
	s1 := mcp.NewServer("server1", "0.0.1")
	s1.MustAddTool("tool_a", "Tool A", nil,
		func(_ context.Context, _ map[string]any) (string, error) { return "a", nil },
	)
	s2 := mcp.NewServer("server2", "0.0.1")
	s2.MustAddTool("tool_b", "Tool B", nil,
		func(_ context.Context, _ map[string]any) (string, error) { return "b", nil },
	)

	// NewTestClient is only available during tests — see export_test.go.
	// Production code obtains clients via NewStdioClient or NewSSEClient.

	_ = mcp.NewRouter(slog.Default() /* , c1, c2 */)
}
