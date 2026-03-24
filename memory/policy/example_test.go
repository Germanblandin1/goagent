package policy_test

import (
	"context"
	"fmt"
	"log"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/policy"
)

// Example demonstrates the NoOp policy passing all messages through unchanged.
func Example() {
	p := policy.NewNoOp()
	msgs := []goagent.Message{
		goagent.UserMessage("ping"),
		goagent.AssistantMessage("pong"),
	}
	result, err := p.Apply(context.Background(), msgs)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(result))
	// Output: 2
}

// ExampleNewFixedWindow shows that FixedWindow keeps only the n most recent messages.
func ExampleNewFixedWindow() {
	p := policy.NewFixedWindow(2)
	msgs := []goagent.Message{
		goagent.UserMessage("one"),
		goagent.AssistantMessage("two"),
		goagent.UserMessage("three"),
		goagent.AssistantMessage("four"),
		goagent.UserMessage("five"),
	}
	result, err := p.Apply(context.Background(), msgs)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(result))
	fmt.Println(result[0].TextContent())
	// Output:
	// 2
	// four
}

// ExampleNewTokenWindow shows that TokenWindow keeps the most recent messages
// that fit within the estimated token budget.
// Token estimation uses len(text)/4 + 4 per message.
func ExampleNewTokenWindow() {
	// Each message here has content of ≤3 chars, so estimated cost = 0 + 4 = 4 tokens.
	// A budget of 8 fits exactly the two most recent messages.
	p := policy.NewTokenWindow(8)
	msgs := []goagent.Message{
		goagent.UserMessage("one"),
		goagent.AssistantMessage("two"),
		goagent.UserMessage("bye"),
		goagent.AssistantMessage("ok"),
	}
	result, err := p.Apply(context.Background(), msgs)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(result))
	fmt.Println(result[0].TextContent())
	// Output:
	// 2
	// bye
}

// ExampleNewTokenWindow_withTokenizer shows how to plug in a custom tokenizer
// so that TokenWindow uses exact token counts instead of the built-in heuristic.
func ExampleNewTokenWindow_withTokenizer() {
	// Pretend each message costs exactly 10 tokens (replace with a real
	// tokenizer such as tiktoken or your provider's counting endpoint).
	exact := policy.TokenizerFunc(func(msg goagent.Message) int {
		return 10
	})

	// Budget of 25 tokens: floor(25/10) = 2 messages fit.
	p := policy.NewTokenWindow(25, policy.WithTokenizer(exact))
	msgs := []goagent.Message{
		goagent.UserMessage("first"),
		goagent.AssistantMessage("second"),
		goagent.UserMessage("third"),
	}
	result, err := p.Apply(context.Background(), msgs)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(result))
	fmt.Println(result[0].TextContent())
	// Output:
	// 2
	// second
}

// ExampleNewNoOp shows that NewNoOp passes every message through unchanged.
func ExampleNewNoOp() {
	p := policy.NewNoOp()
	msgs := []goagent.Message{
		goagent.UserMessage("a"),
		goagent.AssistantMessage("b"),
		goagent.UserMessage("c"),
	}
	result, err := p.Apply(context.Background(), msgs)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(len(result))
	// Output: 3
}
