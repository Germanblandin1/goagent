// Chatbot is a goagent example that runs an interactive multi-turn conversation
// in the terminal using short-term in-memory storage. The agent remembers the
// full conversation history within the session. Requires Ollama running locally
// with the qwen3 model: https://ollama.com
//
// Usage:
//
//	go run ./examples/chatbot
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory"
	"github.com/Germanblandin1/goagent/memory/policy"
	"github.com/Germanblandin1/goagent/memory/storage"
	"github.com/Germanblandin1/goagent/providers/ollama"
)

func main() {
	mem := memory.NewShortTerm(
		memory.WithStorage(storage.NewInMemory()),
		memory.WithPolicy(policy.NewFixedWindow(20)),
	)

	agent := goagent.New(
		goagent.WithProvider(ollama.New()),
		goagent.WithModel("gpt-oss:120b-cloud"),
		goagent.WithShortTermMemory(mem),
		goagent.WithSystemPrompt("You are a helpful and concise assistant. Your name is GoAgentBot."),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println("Chatbot ready. Type your message, or \"exit\" to quit.")
	fmt.Println()

	for {
		fmt.Print("You: ")

		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "exit" || input == "quit" {
			break
		}

		reply, err := agent.Run(ctx, input)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			log.Printf("error: %v\n", err)
			continue
		}

		fmt.Printf("Bot: %s\n\n", reply)
	}

	fmt.Println("Goodbye!")
}
