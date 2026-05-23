package openai_test

import (
	"context"
	"fmt"
	"log"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/providers/openai"
)

// ExampleNew shows the default construction reading OPENAI_API_KEY from the
// environment and targeting the standard OpenAI API.
// No Output: is verified because the example requires a live API key.
func ExampleNew() {
	p := openai.New()

	agent, err := goagent.New(
		goagent.WithProvider(p),
		goagent.WithModel("gpt-4o"),
	)
	if err != nil {
		log.Fatal(err)
	}

	resp, err := agent.Run(context.Background(), "What is the capital of France?")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(resp)
}

// ExampleNew_withAPIKey shows how to supply the API key and a custom base URL
// explicitly — useful for proxies or OpenAI-compatible endpoints.
func ExampleNew_withAPIKey() {
	p := openai.New(
		openai.WithAPIKey("sk-..."),
		openai.WithBaseURL("https://my-proxy.internal/v1"),
	)

	agent, err := goagent.New(
		goagent.WithProvider(p),
		goagent.WithModel("gpt-4o-mini"),
		goagent.WithSystemPrompt("Be concise."),
		goagent.WithMaxTokens(1024),
	)
	if err != nil {
		log.Fatal(err)
	}

	_, _ = agent.Run(context.Background(), "Explain Go interfaces in one sentence.")
}

// ExampleNew_withReasoning shows how to use an o-series reasoning model with
// an explicit reasoning effort level.
func ExampleNew_withReasoning() {
	p := openai.New()

	agent, err := goagent.New(
		goagent.WithProvider(p),
		goagent.WithModel("o3-mini"),
		goagent.WithEffort("high"),
	)
	if err != nil {
		log.Fatal(err)
	}

	_, _ = agent.Run(context.Background(), "Prove that the square root of 2 is irrational.")
}

// ExampleNew_withTool shows tool use with the OpenAI provider.
func ExampleNew_withTool() {
	p := openai.New()

	add := goagent.ToolFunc("add", "Sum two numbers",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"a": map[string]any{"type": "number"},
				"b": map[string]any{"type": "number"},
			},
			"required": []string{"a", "b"},
		},
		func(_ context.Context, args map[string]any) (string, error) {
			a, _ := args["a"].(float64)
			b, _ := args["b"].(float64)
			return fmt.Sprintf("%g", a+b), nil
		},
	)

	agent, err := goagent.New(
		goagent.WithProvider(p),
		goagent.WithModel("gpt-4o"),
		goagent.WithTool(add),
	)
	if err != nil {
		log.Fatal(err)
	}

	_, _ = agent.Run(context.Background(), "What is 2 + 3?")
}
