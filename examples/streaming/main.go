// streaming demonstrates RunStream with real token-by-token output.
// Tokens are printed to stdout as the model generates them.
//
// Usage:
//
//	ANTHROPIC_API_KEY=... go run ./examples/streaming
package main

import (
	"context"
	"fmt"
	"os"

	goagent "github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/providers/anthropic"
)

func main() {
	agent, err := goagent.New(
		goagent.WithProvider(anthropic.New()),
		goagent.WithModel("claude-sonnet-4-6"),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Case 1: real streaming, thinking text visible (default)
	fmt.Print("Response: ")
	result, err := agent.RunStream(
		context.Background(),
		"Explain in 3 sentences what a B+ tree is",
		goagent.TextHandler(os.Stdout),
		// No opts → showThinkingText=true by default
	)
	fmt.Println()

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	_ = result

	// Case 2: agent with tools, thinking text suppressed
	calcTool := goagent.ToolFunc(
		"calc",
		"adds two numbers",
		nil,
		func(_ context.Context, args map[string]any) (string, error) {
			a, _ := args["a"].(float64)
			b, _ := args["b"].(float64)
			return fmt.Sprintf("%v", a+b), nil
		},
	)

	agentWithTools, err := goagent.New(
		goagent.WithProvider(anthropic.New()),
		goagent.WithModel("claude-sonnet-4-6"),
		goagent.WithTool(calcTool),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Print("Result: ")
	_, err = agentWithTools.RunStream(
		context.Background(),
		"What is 1234 + 5678?",
		goagent.TextHandler(os.Stdout),
		goagent.WithShowThinkingText(false), // only show final answer
	)
	fmt.Println()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
