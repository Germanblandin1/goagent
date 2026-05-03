// Command graph-conditional-parallel demonstrates the conditional-parallelism
// graph pattern.
//
// A planner agent classifies the given task as "simple" or "complex". For
// complex tasks, a ParallelGroup runs a researcher and a coder concurrently.
// For simple tasks, only the coder runs sequentially. Both paths feed into a
// summarizer node that consolidates the outputs.
//
// The key insight: the Graph itself never knows whether execution will be
// sequential or parallel — that decision lives entirely inside the NodeFunc.
// This is different from declaring parallel stages upfront in a Pipeline.
//
// Prerequisites:
//
//	ollama pull qwen3:latest
//
// Usage:
//
//	go run ./examples/graph-conditional-parallel
//	go run ./examples/graph-conditional-parallel -task "add a logging middleware to the HTTP router"
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/orchestration"
	"github.com/Germanblandin1/goagent/providers/ollama"
)

func main() {
	task := flag.String("task", "fix a typo in the README", "Task to execute")
	model := flag.String("model", "qwen3:latest", "Ollama model name")
	host := flag.String("host", "http://localhost:11434", "Ollama server URL")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := ollama.NewClient(ollama.WithBaseURL(*host))
	provider := ollama.NewWithClient(client)

	plannerAgent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithModel(*model),
		goagent.WithSystemPrompt(`You are a task classifier. Analyze the task and respond with exactly one word:
SIMPLE — if it requires only coding (small change, typo fix, simple feature)
COMPLEX — if it requires research AND coding (new library, architecture decision, unfamiliar domain)`),
	)
	if err != nil {
		log.Fatal(err)
	}

	researcherAgent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithModel(*model),
		goagent.WithSystemPrompt("You are a technical researcher. Given a task, provide a concise research summary (3-5 bullet points) covering relevant patterns, libraries, and best practices. Be concrete and actionable."),
	)
	if err != nil {
		log.Fatal(err)
	}

	coderAgent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithModel(*model),
		goagent.WithSystemPrompt("You are a Go developer. Given a task (and optionally research context), produce a concise implementation sketch: key types, function signatures, and a brief explanation. No full working code needed."),
	)
	if err != nil {
		log.Fatal(err)
	}

	summarizerAgent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithModel(*model),
		goagent.WithSystemPrompt("You are a technical writer. Combine the provided inputs into a clear, actionable summary. Be concise."),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Adapters wrap agents as Executors for use inside the NodeFunc.
	researcherAdapter := orchestration.NewAgentAdapter(researcherAgent, "research",
		func(sc *orchestration.StageContext) string {
			return "Research this task: " + sc.Goal
		},
	)
	coderAdapter := orchestration.NewAgentAdapter(coderAgent, "code",
		func(sc *orchestration.StageContext) string {
			research, _ := sc.Output("research")
			if research != "" {
				return fmt.Sprintf("Task: %s\n\nResearch:\n%s\n\nProvide an implementation sketch.", sc.Goal, research)
			}
			return "Task: " + sc.Goal + "\n\nProvide an implementation sketch."
		},
	)

	graph, err := orchestration.NewGraph(
		orchestration.WithStart("plan"),
		orchestration.WithMaxIterations(10),

		orchestration.WithNode("plan", func(ctx context.Context, sc *orchestration.StageContext) (string, error) {
			verdict, err := plannerAgent.Run(ctx, sc.Goal)
			if err != nil {
				return "", err
			}

			isComplex := strings.Contains(strings.ToUpper(verdict), "COMPLEX")
			sc.SetArtifact("complex", isComplex)

			if isComplex {
				fmt.Println("Classification: COMPLEX — running researcher and coder in parallel")
				return "execute", nil
			}
			fmt.Println("Classification: SIMPLE — running coder only")
			return "execute", nil
		}),

		orchestration.WithNode("execute", func(ctx context.Context, sc *orchestration.StageContext) (string, error) {
			isComplex, _ := orchestration.GetArtifact[bool](sc, "complex")

			if isComplex {
				group := orchestration.NewParallelGroup(
					orchestration.Stage("research", researcherAdapter),
					orchestration.Stage("code", coderAdapter),
				)
				if err := group.RunWithContext(ctx, sc); err != nil {
					return "", err
				}
			} else {
				if err := coderAdapter.RunWithContext(ctx, sc); err != nil {
					return "", err
				}
			}
			return "summarize", nil
		}),

		orchestration.WithNode("summarize", func(ctx context.Context, sc *orchestration.StageContext) (string, error) {
			var parts []string
			parts = append(parts, "Task: "+sc.Goal)

			if research, ok := sc.Output("research"); ok {
				parts = append(parts, "Research:\n"+research)
			}
			if code, ok := sc.Output("code"); ok {
				parts = append(parts, "Implementation sketch:\n"+code)
			}

			summary, err := summarizerAgent.Run(ctx, strings.Join(parts, "\n\n"))
			if err != nil {
				return "", err
			}
			sc.SetOutput("summary", summary)
			return "", nil // END
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Processing task: %q\n\n", *task)

	sc, err := graph.Run(ctx, *task)
	if err != nil {
		log.Fatalf("graph error: %v", err)
	}

	summary, _ := sc.Output("summary")
	fmt.Printf("\n=== Summary ===\n%s\n", summary)
}
