// streaming demonstrates RunStream with real token-by-token output using a
// local Ollama model. Requires Ollama running on localhost:11434.
//
// Usage:
//
//	go run ./examples/streaming
//
// Override the Ollama base URL or model via environment variables:
//
//	OLLAMA_HOST=http://localhost:11434 OLLAMA_MODEL=llama3.2 go run ./examples/streaming
package main

import (
	"context"
	"fmt"
	"os"

	goagent "github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/providers/ollama"
)

func main() {
	model := os.Getenv("OLLAMA_MODEL")
	if model == "" {
		model = "llama3.2"
	}

	var clientOpts []ollama.ClientOption
	if host := os.Getenv("OLLAMA_HOST"); host != "" {
		clientOpts = append(clientOpts, ollama.WithBaseURL(host))
	}

	// Case 1: real streaming, no tools
	agent, err := goagent.New(
		goagent.WithProvider(ollama.NewWithClient(ollama.NewClient(clientOpts...))),
		goagent.WithModel(model),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Print("Response: ")
	_, err = agent.RunStream(
		context.Background(),
		"Explain in 3 sentences what a B+ tree is",
		goagent.TextHandler(os.Stdout),
	)
	fmt.Println()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	// Case 2: agent with a tool — demonstrates that tool calls work through
	// the streaming path (tool calls arrive in the done chunk and are surfaced
	// as StreamEventToolStart/ToolDelta events before the final text response).
	calcTool := goagent.ToolFunc(
		"calc",
		"adds two numbers, parameters: a (number), b (number)",
		nil,
		func(_ context.Context, args map[string]any) (string, error) {
			a, _ := args["a"].(float64)
			b, _ := args["b"].(float64)
			return fmt.Sprintf("%v", a+b), nil
		},
	)

	agentWithTools, err := goagent.New(
		goagent.WithProvider(ollama.NewWithClient(ollama.NewClient(clientOpts...))),
		goagent.WithModel(model),
		goagent.WithTool(calcTool),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Print("Result: ")
	_, err = agentWithTools.RunStream(
		context.Background(),
		"What is 1234 + 5678? Use the calc tool.",
		goagent.TextHandler(os.Stdout),
	)
	fmt.Println()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
