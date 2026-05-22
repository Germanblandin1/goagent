// Command graph-loop-judge demonstrates the loop-with-LLM-as-judge graph pattern.
//
// A writer agent generates a haiku about the given topic. A critic agent scores
// it from 1 to 10. If the score is below the threshold (default: 8), the graph
// loops back to the writer, passing the critic's feedback. The loop ends when
// the haiku is approved or the iteration limit is reached.
//
// This pattern is useful for any self-improvement loop: code generation with
// automated review, document drafting with quality gates, and similar tasks.
//
// Prerequisites:
//
//	ollama pull qwen3:latest
//
// Usage:
//
//	go run ./examples/graph-loop-judge
//	go run ./examples/graph-loop-judge -topic "autumn rain" -threshold 7
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/orchestration"
	"github.com/Germanblandin1/goagent/providers/ollama"
)

func main() {
	topic := flag.String("topic", "the silence between stars", "Topic for the haiku")
	model := flag.String("model", "qwen3:latest", "Ollama model name")
	host := flag.String("host", "http://localhost:11434", "Ollama server URL")
	threshold := flag.Int("threshold", 8, "Minimum score (1-10) to accept the haiku")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := ollama.NewClient(ollama.WithBaseURL(*host))
	provider := ollama.NewWithClient(client)

	writerAgent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithModel(*model),
		goagent.WithSystemPrompt("You are a haiku master. Write beautiful, evocative haikus in the traditional 5-7-5 syllable format. Respond with only the haiku, no explanations."),
	)
	if err != nil {
		log.Fatal(err)
	}

	criticAgent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithModel(*model),
		goagent.WithSystemPrompt(`You are a haiku critic. Evaluate haikus and respond in this exact format:
SCORE: <number 1-10>
FEEDBACK: <one sentence of constructive feedback>

Be strict: only scores of 8+ mean the haiku is ready.`),
	)
	if err != nil {
		log.Fatal(err)
	}

	graph, err := orchestration.NewGraph(
		orchestration.WithStart("write"),
		orchestration.WithMaxIterations(10),

		orchestration.WithNode("write", func(ctx context.Context, sc *orchestration.StageContext) (string, error) {
			feedback, _ := orchestration.GetArtifact[string](sc, "feedback")
			prevHaiku, _ := sc.Output("haiku")

			var prompt string
			if feedback != "" {
				prompt = fmt.Sprintf(
					"Write a new haiku about %q.\nPrevious attempt:\n%s\nCritic feedback: %s\nImprove it.",
					sc.Goal, prevHaiku, feedback,
				)
			} else {
				prompt = fmt.Sprintf("Write a haiku about %q.", sc.Goal)
			}

			haiku, err := writerAgent.Run(ctx, prompt)
			if err != nil {
				return "", err
			}
			sc.SetOutput("haiku", haiku)
			fmt.Printf("\n--- Draft ---\n%s\n", haiku)
			return "critique", nil
		}),

		orchestration.WithNode("critique", func(ctx context.Context, sc *orchestration.StageContext) (string, error) {
			haiku, err := sc.RequireOutput("haiku")
			if err != nil {
				return "", err
			}

			review, err := criticAgent.Run(ctx, "Evaluate this haiku:\n"+haiku)
			if err != nil {
				return "", err
			}

			score, feedback := parseReview(review)
			sc.SetArtifact("feedback", feedback)
			fmt.Printf("Score: %d/10 — %s\n", score, feedback)

			if score >= *threshold {
				return "", nil // END: haiku approved
			}
			return "write", nil // loop: try again
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Generating haiku about %q (threshold: %d/10)...\n", *topic, *threshold)

	sc, err := graph.Run(ctx, *topic)
	if err != nil {
		log.Fatalf("graph error: %v", err)
	}

	finalHaiku, _ := sc.Output("haiku")
	trace := sc.Trace()

	// count write iterations (every other trace entry is a "write" node)
	writes := 0
	for _, entry := range trace {
		if entry.StageName == "write" {
			writes++
		}
	}

	fmt.Printf("\n=== Final Haiku (%d iteration(s)) ===\n%s\n", writes, finalHaiku)
}

// parseReview extracts score and feedback from the critic's response.
// Expected format: "SCORE: N\nFEEDBACK: ..."
func parseReview(review string) (int, string) {
	score := 5
	feedback := review

	for _, line := range strings.Split(review, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "SCORE:"); ok {
			if n, err := strconv.Atoi(strings.TrimSpace(after)); err == nil {
				score = n
			}
		}
		if after, ok := strings.CutPrefix(line, "FEEDBACK:"); ok {
			feedback = strings.TrimSpace(after)
		}
	}
	return score, feedback
}
