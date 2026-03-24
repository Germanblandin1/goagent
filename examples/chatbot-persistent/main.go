// Chatbot-persistent is a goagent example that runs an interactive multi-turn
// conversation with long-term memory persisted to a local file across sessions.
//
// # Architecture
//
// Two agents are used:
//
//   - Main agent — handles the conversation. It has short-term in-memory history
//     for the current session and long-term file-backed history across sessions.
//
//   - Judge agent — a stateless agent invoked via WritePolicy after every turn.
//     It evaluates whether the exchange contains information worth persisting and
//     responds with a strict JSON verdict: {"save": bool, "message": "..."}.
//     Only turns where save=true are written to the history file. Malformed JSON
//     or agent errors are treated as save=false so the main agent is unaffected.
//
// # Long-term memory
//
// Messages are stored as JSON Lines in a local file (one object per line).
// On each run the full file is loaded and injected as long-term context before
// the provider call. See filemem.go for trade-offs and judge.go for the verdict
// format.
//
// Requires Ollama running locally: https://ollama.com
//
// Usage:
//
//	go run ./examples/chatbot-persistent
//
// Override the memory file path with the CHATBOT_MEMORY_FILE env var
// (default: ./chatbot-memory.jsonl).
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
	memoryFile := os.Getenv("CHATBOT_MEMORY_FILE")
	if memoryFile == "" {
		memoryFile = "chatbot-memory.jsonl"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shortTerm := memory.NewShortTerm(
		memory.WithStorage(storage.NewInMemory()),
		memory.WithPolicy(policy.NewFixedWindow(20)),
	)

	longTerm := newFileLongTermMemory(memoryFile)

	// newJudgePolicy creates the stateless judge agent. The policy is a closure
	// that captures ctx so the judge respects application-level cancellation.
	judgePolicy := newJudgePolicy(ctx)

	agent := goagent.New(
		goagent.WithProvider(ollama.New()),
		goagent.WithModel("gpt-oss:120b-cloud"),
		goagent.WithShortTermMemory(shortTerm),
		goagent.WithLongTermMemory(longTerm),
		goagent.WithWritePolicy(judgePolicy),
		goagent.WithSystemPrompt("You are a helpful and concise assistant. Your name is GoAgentBot."),
	)

	scanner := bufio.NewScanner(os.Stdin)

	fmt.Printf("Chatbot ready (memory: %s). Type your message, or \"exit\" to quit.\n\n", memoryFile)

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
