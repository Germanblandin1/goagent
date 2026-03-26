package goagent_test

import (
	"context"
	"fmt"
	"log"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/internal/testutil"
	"github.com/Germanblandin1/goagent/memory"
)

// Example demonstrates creating an Agent with a mock provider and running it.
func Example() {
	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(
			goagent.CompletionResponse{
				Message:    goagent.AssistantMessage("4"),
				StopReason: goagent.StopReasonEndTurn,
			},
		)),
	)
	if err != nil {
		log.Fatal(err)
	}

	result, err := agent.Run(context.Background(), "what is 2+2?")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result)
	// Output: 4
}

// ExampleAgent_Run_withTools shows how a tool is registered and invoked during
// a ReAct loop. The mock provider first requests a tool call, then produces the
// final answer after receiving the tool result.
func ExampleAgent_Run_withTools() {
	echo := goagent.ToolFunc("echo", "returns the input text unchanged", nil,
		func(_ context.Context, args map[string]any) (string, error) {
			return fmt.Sprintf("%v", args["text"]), nil
		},
	)

	mock := testutil.NewMockProvider(
		// Iteration 1: model requests the echo tool.
		goagent.CompletionResponse{
			Message: goagent.Message{
				Role: goagent.RoleAssistant,
				ToolCalls: []goagent.ToolCall{
					{ID: "call-1", Name: "echo", Arguments: map[string]any{"text": "hello"}},
				},
			},
			StopReason: goagent.StopReasonToolUse,
		},
		// Iteration 2: model produces the final answer.
		goagent.CompletionResponse{
			Message:    goagent.AssistantMessage("hello"),
			StopReason: goagent.StopReasonEndTurn,
		},
	)

	agent, err := goagent.New(
		goagent.WithProvider(mock),
		goagent.WithTool(echo),
	)
	if err != nil {
		log.Fatal(err)
	}

	result, err := agent.Run(context.Background(), "echo hello")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result)
	// Output: hello
}

// ExampleAgent_Run_withMemory shows that completed turns are persisted to
// ShortTermMemory and available for the next Run call.
func ExampleAgent_Run_withMemory() {
	mem := memory.NewShortTerm()

	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(
			goagent.CompletionResponse{
				Message:    goagent.AssistantMessage("pong"),
				StopReason: goagent.StopReasonEndTurn,
			},
		)),
		goagent.WithShortTermMemory(mem),
	)
	if err != nil {
		log.Fatal(err)
	}

	if _, err := agent.Run(context.Background(), "ping"); err != nil {
		log.Fatal(err)
	}

	msgs, err := mem.Messages(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(msgs))
	fmt.Println(msgs[0].TextContent())
	fmt.Println(msgs[1].TextContent())
	// Output:
	// 2
	// ping
	// pong
}

// ExampleToolFunc shows how to create a Tool from a plain function.
func ExampleToolFunc() {
	t := goagent.ToolFunc("add", "adds two numbers", nil,
		func(_ context.Context, _ map[string]any) (string, error) {
			return "42", nil
		},
	)
	fmt.Println(t.Definition().Name)
	fmt.Println(t.Definition().Description)
	// Output:
	// add
	// adds two numbers
}

// ExampleMinLength shows how MinLength filters out short exchanges from
// long-term memory storage.
// A nil return means the turn is discarded; a non-nil slice is stored as-is.
func ExampleMinLength() {
	policy := goagent.MinLength(10)

	// 4 chars total ("hi" + "ok") — below threshold, turn is discarded.
	fmt.Println(policy(goagent.UserMessage("hi"), goagent.AssistantMessage("ok")) == nil)

	// 15 chars total ("hello world" + "fine") — above threshold, returns the
	// default user+assistant pair ready to be passed to LongTermMemory.Store.
	msgs := policy(goagent.UserMessage("hello world"), goagent.AssistantMessage("fine"))
	fmt.Printf("%d messages: %s / %s\n", len(msgs), msgs[0].Role, msgs[1].Role)
	// Output:
	// true
	// 2 messages: user / assistant
}

// ExampleAgent_RunBlocks shows how to use RunBlocks with multimodal content.
// Here we send a text block; in practice you would combine text with image or
// document blocks.
func ExampleAgent_RunBlocks() {
	agent, err := goagent.New(
		goagent.WithProvider(testutil.NewMockProvider(
			goagent.CompletionResponse{
				Message:    goagent.AssistantMessage("It's a cat"),
				StopReason: goagent.StopReasonEndTurn,
			},
		)),
	)
	if err != nil {
		log.Fatal(err)
	}

	result, err := agent.RunBlocks(context.Background(),
		goagent.TextBlock("What animal is this?"),
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result)
	// Output: It's a cat
}

// ExampleToolBlocksFunc shows how to create a Tool that returns multimodal
// content blocks instead of plain text.
func ExampleToolBlocksFunc() {
	t := goagent.ToolBlocksFunc("screenshot", "captures the current screen", nil,
		func(_ context.Context, _ map[string]any) ([]goagent.ContentBlock, error) {
			return []goagent.ContentBlock{
				goagent.TextBlock("captured"),
			}, nil
		},
	)
	fmt.Println(t.Definition().Name)
	fmt.Println(t.Definition().Description)
	// Output:
	// screenshot
	// captures the current screen
}

// ExampleTextFrom shows how TextFrom extracts text from a mixed slice of
// ContentBlocks, ignoring non-text blocks like images.
func ExampleTextFrom() {
	blocks := []goagent.ContentBlock{
		goagent.TextBlock("hello"),
		goagent.ImageBlock([]byte{0xFF}, "image/png"),
		goagent.TextBlock("world"),
	}
	fmt.Println(goagent.TextFrom(blocks))
	// Output: hello world
}
