package ollama_test

import (
	"context"
	"fmt"
	"log"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/providers/ollama"
)

// Example shows how to wire an Ollama provider with an Agent.
// It requires a locally running Ollama instance on the default address
// (http://localhost:11434) and a pulled model — no Output: is verified.
func Example() {
	provider := ollama.New()

	agent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithModel("qwen3"),
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

// ExampleNew shows the default construction targeting localhost:11434.
// No Output: is verified because the example requires Ollama to be running.
func ExampleNew() {
	p := ollama.New()

	_ = p // pass to goagent.WithProvider
}

// ExampleWithBaseURL shows how to target a non-default Ollama endpoint,
// for example when Ollama runs on a different host or port.
// No Output: is verified because the example requires a live Ollama instance.
func ExampleWithBaseURL() {
	p := ollama.New(
		ollama.WithBaseURL("http://ollama.internal:11434/v1"),
	)

	agent, err := goagent.New(
		goagent.WithProvider(p),
		goagent.WithModel("llama3"),
	)
	if err != nil {
		log.Fatal(err)
	}

	_, _ = agent.Run(context.Background(), "ping")
}

// ExampleNew_stateless shows that a stateless provider can be shared safely
// across concurrent Agent instances — no memory backend means no shared state.
// No Output: is verified because the example requires Ollama to be running.
func ExampleNew_stateless() {
	p := ollama.New()

	// A single stateless agent shared across goroutines is safe.
	agent, err := goagent.New(
		goagent.WithProvider(p),
		goagent.WithModel("qwen3"),
	)
	if err != nil {
		log.Fatal(err)
	}

	prompts := []string{"hello", "what is Go?", "what is 2+2?"}
	results := make(chan string, len(prompts))

	for _, prompt := range prompts {
		go func(q string) {
			resp, err := agent.Run(context.Background(), q)
			if err != nil {
				results <- fmt.Sprintf("error: %v", err)
				return
			}
			results <- resp
		}(prompt)
	}

	for range prompts {
		fmt.Println(<-results)
	}
}
