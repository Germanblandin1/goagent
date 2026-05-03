// Command graph-nested demonstrates nesting a Graph inside a Pipeline.
//
// Graph implements the Executor interface, so it can participate in a Pipeline
// as any other stage. This example builds a three-stage pipeline:
//
//  1. outline  — an agent produces a structured outline for the given topic
//  2. draft    — a Graph that iteratively drafts and self-critiques the content
//     (generate → critique → loop if not approved → END)
//  3. format   — an agent applies final formatting to the approved draft
//
// The Pipeline does not know that the "draft" stage contains an internal loop —
// it just calls RunWithContext and receives control back when the Graph ends.
//
// Prerequisites:
//
//	ollama pull qwen3:latest
//
// Usage:
//
//	go run ./examples/graph-nested
//	go run ./examples/graph-nested -topic "benefits of interface-driven design in Go"
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
	topic := flag.String("topic", "why Go error handling is a feature, not a bug", "Topic to write about")
	model := flag.String("model", "qwen3:latest", "Ollama model name")
	host := flag.String("host", "http://localhost:11434", "Ollama server URL")
	maxDrafts := flag.Int("max-drafts", 5, "Maximum draft iterations inside the graph")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := ollama.NewClient(ollama.WithBaseURL(*host))
	provider := ollama.NewWithClient(client)

	outlineAgent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithModel(*model),
		goagent.WithSystemPrompt("You are a technical writer. Given a topic, produce a concise 3-point outline for a short blog post. Format: one sentence per point, no preamble."),
	)
	if err != nil {
		log.Fatal(err)
	}

	writerAgent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithModel(*model),
		goagent.WithSystemPrompt("You are a technical blogger. Write a short, engaging blog post (3-4 paragraphs) based on the provided outline and topic. Be concrete, use examples."),
	)
	if err != nil {
		log.Fatal(err)
	}

	editorAgent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithModel(*model),
		goagent.WithSystemPrompt(`You are a strict technical editor. Evaluate a blog post draft.
Respond in this exact format:
APPROVED
or
REVISE: <one sentence describing what needs improvement>`),
	)
	if err != nil {
		log.Fatal(err)
	}

	formatterAgent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithModel(*model),
		goagent.WithSystemPrompt("You are a markdown formatter. Add a clear title (# Title) and ensure the blog post is well-formatted markdown. Return only the formatted post."),
	)
	if err != nil {
		log.Fatal(err)
	}

	// draftGraph runs an internal write→critique loop until the editor approves.
	draftGraph, err := orchestration.NewGraph(
		orchestration.WithStart("write"),
		orchestration.WithMaxIterations(*maxDrafts*2), // each iteration uses 2 nodes
		orchestration.WithNode("write", func(ctx context.Context, sc *orchestration.StageContext) (string, error) {
			outline, _ := sc.RequireOutput("outline")
			feedback, _ := orchestration.GetArtifact[string](sc, "editor_feedback")
			prevDraft, _ := sc.Output("draft")

			var prompt string
			if feedback != "" {
				prompt = fmt.Sprintf(
					"Topic: %s\nOutline:\n%s\n\nPrevious draft:\n%s\n\nEditor feedback: %s\n\nRewrite the post addressing the feedback.",
					sc.Goal, outline, prevDraft, feedback,
				)
			} else {
				prompt = fmt.Sprintf("Topic: %s\nOutline:\n%s\n\nWrite the blog post.", sc.Goal, outline)
			}

			draft, err := writerAgent.Run(ctx, prompt)
			if err != nil {
				return "", err
			}
			sc.SetOutput("draft", draft)

			drafts, _ := orchestration.GetArtifact[int](sc, "draft_count")
			drafts++
			sc.SetArtifact("draft_count", drafts)
			fmt.Printf("  [draft %d written]\n", drafts)
			return "critique", nil
		}),
		orchestration.WithNode("critique", func(ctx context.Context, sc *orchestration.StageContext) (string, error) {
			draft, _ := sc.RequireOutput("draft")

			verdict, err := editorAgent.Run(ctx, "Review this blog post:\n\n"+draft)
			if err != nil {
				return "", err
			}

			verdict = strings.TrimSpace(verdict)
			if strings.HasPrefix(strings.ToUpper(verdict), "APPROVED") {
				fmt.Println("  [editor approved]")
				return "", nil // END graph
			}

			feedback := extractFeedback(verdict)
			sc.SetArtifact("editor_feedback", feedback)
			fmt.Printf("  [editor: %s]\n", feedback)
			return "write", nil // loop
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	// The Pipeline treats draftGraph as just another Executor stage.
	pipeline := orchestration.NewPipeline(
		orchestration.Stage("outline", orchestration.NewAgentAdapter(
			outlineAgent, "outline",
			func(sc *orchestration.StageContext) string {
				return "Create an outline for a blog post about: " + sc.Goal
			},
		)),
		orchestration.Stage("draft", draftGraph), // Graph as Executor
		orchestration.Stage("format", orchestration.NewAgentAdapter(
			formatterAgent, "formatted",
			func(sc *orchestration.StageContext) string {
				draft, _ := sc.RequireOutput("draft")
				return fmt.Sprintf("Topic: %s\n\nPost to format:\n%s", sc.Goal, draft)
			},
		)),
	)

	fmt.Printf("Writing blog post about %q...\n", *topic)

	sc, err := pipeline.Run(ctx, *topic)
	if err != nil {
		log.Fatalf("pipeline error: %v", err)
	}

	result, _ := sc.Output("formatted")
	drafts, _ := orchestration.GetArtifact[int](sc, "draft_count")
	fmt.Printf("\n=== Final Post (%d draft(s)) ===\n\n%s\n", drafts, result)
}

// extractFeedback parses "REVISE: <message>" or returns the full verdict.
func extractFeedback(verdict string) string {
	for _, line := range strings.Split(verdict, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(strings.ToUpper(line), "REVISE:"); ok {
			// find original case version
			idx := strings.Index(strings.ToUpper(verdict), "REVISE:")
			if idx >= 0 {
				return strings.TrimSpace(verdict[idx+len("REVISE:"):])
			}
			_ = after
		}
	}
	// try to extract score context for loop-judge pattern reuse
	if _, err := strconv.Atoi(strings.TrimSpace(verdict)); err == nil {
		return verdict
	}
	return verdict
}
