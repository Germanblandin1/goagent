package anthropic_test

import (
	"context"
	"log"

	"github.com/Germanblandin1/goagent"
	provider "github.com/Germanblandin1/goagent/providers/anthropic"
)

// ExampleNew demonstrates creating an agent with the Anthropic provider.
// This example requires a valid ANTHROPIC_API_KEY environment variable and
// network access — no Output: is verified.
func ExampleNew() {
	p := provider.New()

	agent, err := goagent.New(
		goagent.WithProvider(p),
		goagent.WithModel("claude-sonnet-4-6"),
	)
	if err != nil {
		log.Fatal(err)
	}

	_, _ = agent.Run(context.Background(), "What is 2+2?")
}

// ExampleNew_withAPIKey shows how to supply the API key explicitly using a
// custom client, and pair it with a Provider that limits output tokens.
func ExampleNew_withAPIKey() {
	client := provider.NewClient(
		provider.WithAPIKey("sk-ant-..."),
	)
	p := provider.NewWithClient(client,
		provider.WithMaxTokens(1024),
	)

	agent, err := goagent.New(
		goagent.WithProvider(p),
		goagent.WithModel("claude-haiku-4-5"),
		goagent.WithSystemPrompt("Be concise."),
	)
	if err != nil {
		log.Fatal(err)
	}

	_, _ = agent.Run(context.Background(), "Explain Go interfaces in one sentence.")
}

// ExampleNew_withTool shows tool use with the Anthropic provider.
func ExampleNew_withTool() {
	p := provider.New()

	add := goagent.ToolFunc("add", "Sum two numbers",
		goagent.SchemaFrom(struct {
			A float64 `json:"a"`
			B float64 `json:"b"`
		}{}),
		func(_ context.Context, args map[string]any) (string, error) {
			a, _ := args["a"].(float64)
			b, _ := args["b"].(float64)
			return goagent.TextBlock(string(rune(a+b))).Text, nil
		},
	)

	agent, err := goagent.New(
		goagent.WithProvider(p),
		goagent.WithModel("claude-sonnet-4-6"),
		goagent.WithTool(add),
	)
	if err != nil {
		log.Fatal(err)
	}

	_, _ = agent.Run(context.Background(), "What is 2 + 3?")
}
