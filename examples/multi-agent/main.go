// Command multi-agent demonstrates a two-stage supervisor/worker pipeline.
//
// Stage 1 — planner: receives the goal and produces a structured execution plan.
// Stage 2 — supervisor: reads the plan, then coordinates three workers in parallel:
//   - researcher: gathers technical context
//   - coder:      writes the implementation
//   - reviewer:   reviews the final code
//
// All agents run locally via Ollama.
//
// Prerequisites:
//
//	ollama pull qwen3:latest
//
// Usage:
//
//	go run ./examples/multi-agent "Implement a Redis client in Go"
//	go run ./examples/multi-agent   # uses default goal
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/orchestration"
	"github.com/Germanblandin1/goagent/providers/ollama"
)

const (
	defaultGoal  = "Implement a rate limiter in Go using the token bucket algorithm"
	defaultModel = "qwen3:latest"
	defaultHost  = "http://localhost:11434"
)

func main() {
	goal := defaultGoal
	if len(os.Args) > 1 {
		goal = strings.Join(os.Args[1:], " ")
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// ── Ollama provider ───────────────────────────────────────────────────────
	client := ollama.NewClient(ollama.WithBaseURL(defaultHost))
	provider := ollama.NewWithClient(client)

	// ── Agents ────────────────────────────────────────────────────────────────
	plannerAgent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithModel(defaultModel),
		goagent.WithMaxIterations(10),
		goagent.WithHooks(loggingHooks("planner")),
		goagent.WithSystemPrompt(plannerPrompt),
	)
	if err != nil {
		log.Fatalf("creating planner: %v", err)
	}

	researcherAgent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithModel(defaultModel),
		goagent.WithMaxIterations(10),
		goagent.WithHooks(loggingHooks("researcher")),
		goagent.WithSystemPrompt(researcherPrompt),
	)
	if err != nil {
		log.Fatalf("creating researcher: %v", err)
	}

	coderAgent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithModel(defaultModel),
		goagent.WithMaxIterations(10),
		goagent.WithHooks(loggingHooks("coder")),
		goagent.WithSystemPrompt(coderPrompt),
	)
	if err != nil {
		log.Fatalf("creating coder: %v", err)
	}

	reviewerAgent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithModel(defaultModel),
		goagent.WithMaxIterations(10),
		goagent.WithHooks(loggingHooks("reviewer")),
		goagent.WithSystemPrompt(reviewerPrompt),
	)
	if err != nil {
		log.Fatalf("creating reviewer: %v", err)
	}

	// ── Supervisor ────────────────────────────────────────────────────────────
	supervisorPB := func(sc *orchestration.StageContext) string {
		plan, err := sc.RequireOutput("plan")
		if err != nil {
			return sc.Goal
		}
		return fmt.Sprintf(
			"Goal: %s\n\nExecution plan produced by the planner:\n%s\n\n"+
				"Execute the plan using your workers. Follow the steps in order "+
				"where dependencies exist, but call independent workers in parallel.",
			sc.Goal,
			plan,
		)
	}

	supervisor, err := orchestration.NewSupervisor(
		"final_result",
		supervisorPB,
		[]orchestration.Worker{
			{
				Name:        "researcher",
				Description: "Researches technical topics and Go APIs",
				InputDescription: "The specific topic to research. Include what aspect " +
					"is most relevant to the current task.",
				Agent: researcherAgent,
			},
			{
				Name:        "coder",
				Description: "Writes idiomatic Go code",
				InputDescription: "What to implement. Include the objective, any " +
					"relevant research findings, and design constraints.",
				Agent: coderAgent,
			},
			{
				Name:        "reviewer",
				Description: "Reviews Go code for bugs and improvements",
				InputDescription: "The Go code to review and the original objective " +
					"so correctness can be assessed.",
				Agent: reviewerAgent,
			},
		},
		goagent.WithProvider(provider),
		goagent.WithModel(defaultModel),
		goagent.WithMaxIterations(20),
		goagent.WithHooks(loggingHooks("supervisor")),
		goagent.WithSystemPrompt(supervisorPrompt),
	)
	if err != nil {
		log.Fatalf("creating supervisor: %v", err)
	}

	// ── Pipeline ──────────────────────────────────────────────────────────────
	pipeline := orchestration.NewPipeline(
		orchestration.WithStages(
			orchestration.Stage("plan", orchestration.AgentStage("plan", plannerAgent, orchestration.GoalOnly)),
			orchestration.Stage("execute", supervisor),
		),
	)

	fmt.Printf("Running pipeline for goal: %q\n\n", goal)

	sc, err := pipeline.Run(ctx, goal)
	if err != nil {
		log.Fatalf("pipeline error: %v", err)
	}

	result, err := sc.RequireOutput("final_result")
	if err != nil {
		log.Fatalf("no result: %v", err)
	}

	fmt.Println(result)

	fmt.Println("\n--- Trace ---")
	for _, t := range sc.Trace() {
		fmt.Printf("  %-10s %s\n", t.StageName+":", t.Duration.Round(time.Millisecond))
	}
}

// ── PromptBuilders ────────────────────────────────────────────────────────────

// supervisorPB is defined inline above since it closes over nothing external.

// ── System prompts ────────────────────────────────────────────────────────────

const plannerPrompt = `You are a software project planner. Given a goal, produce a concise
execution plan with 3-5 numbered steps. Be specific about what needs
to be researched, designed, and implemented. Output plain text only.`

const researcherPrompt = `You are a technical researcher. Given a topic, provide accurate and
concise technical information relevant to Go development. Focus on
practical details: APIs, patterns, trade-offs.`

const coderPrompt = `You are an expert Go developer. Write clean, idiomatic, well-documented
Go code. Follow standard Go conventions. Include package declaration and
imports. Output only the code without explanations unless asked.`

const reviewerPrompt = `You are a senior Go code reviewer. Identify bugs, design issues, and
suggest concrete improvements. Be concise and actionable.`

const supervisorPrompt = `You are a software development coordinator. A planner has already
analyzed the task and produced an execution plan for you.

Use your workers to implement the plan:
- researcher: for technical research and API details
- coder: for writing Go code
- reviewer: for reviewing the final code

Call independent workers in parallel when possible.
Pass all necessary context in each worker's input string.
After all workers finish, synthesize a complete final answer
that includes the implementation and any review findings.`

// ── Observability ─────────────────────────────────────────────────────────────

func loggingHooks(stage string) goagent.Hooks {
	return goagent.Hooks{
		OnToolCall: func(_ context.Context, name string, _ map[string]any) {
			slog.Debug("tool call", "stage", stage, "tool", name)
		},
		OnToolResult: func(_ context.Context, name string, _ []goagent.ContentBlock, dur time.Duration, err error) {
			if err != nil {
				slog.Warn("tool error", "stage", stage, "tool", name, "dur", dur, "err", err)
				return
			}
			slog.Debug("tool result", "stage", stage, "tool", name, "dur", dur)
		},
		OnRunEnd: func(_ context.Context, result goagent.RunResult) {
			if result.Err != nil {
				slog.Error("stage failed", "stage", stage, "err", result.Err)
				return
			}
			slog.Info("stage complete",
				"stage", stage,
				"iterations", result.Iterations,
				"tool_calls", result.ToolCalls,
				"dur", result.Duration,
			)
		},
	}
}
