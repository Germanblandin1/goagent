// Command mini-code-agent is a multi-agent "mini Claude Code" that implements
// a programming task using four specialized agents running in sequence:
//
//  1. planner  — analyzes the task and produces a structured implementation plan
//  2. coder    — implements the plan by writing Go source files
//  3. tester   — writes tests for the implementation
//  4. reviewer — reviews code and tests and produces a final report
//
// All agents run locally via Ollama and operate on a shared workspace directory.
// The orchestration package coordinates them via a sequential Pipeline — each
// stage can read the outputs of previous stages and the files they wrote.
//
// Prerequisites:
//
//	ollama pull qwen3:latest
//
// Usage:
//
//	go run ./examples/mini-code-agent -task "implement a thread-safe stack in Go"
//	go run ./examples/mini-code-agent -task "..." -workspace ./out -model qwen2.5-coder:7b
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/orchestration"
	"github.com/Germanblandin1/goagent/providers/ollama"
)

func main() {
	task      := flag.String("task",      "",                       "Programming task to implement (required)")
	workspace := flag.String("workspace", "workspace",              "Directory where agents read and write files")
	model     := flag.String("model",     "qwen3:latest",           "Ollama model name")
	host      := flag.String("host",      "http://localhost:11434", "Ollama server URL")
	debug     := flag.Bool("debug",       false,                    "Enable debug-level logs")
	flag.Parse()

	if *task == "" {
		log.Fatal("flag -task is required")
	}

	// ── Logging ──────────────────────────────────────────────────────────────
	logLevel := slog.LevelInfo
	if *debug {
		logLevel = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	})))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// ── Workspace ────────────────────────────────────────────────────────────
	workspaceAbs, err := filepath.Abs(*workspace)
	if err != nil {
		log.Fatalf("resolving workspace path: %v", err)
	}
	if err := os.MkdirAll(workspaceAbs, 0o755); err != nil {
		log.Fatalf("creating workspace: %v", err)
	}
	fmt.Printf("Workspace: %s\n\n", workspaceAbs)

	// ── Ollama provider ───────────────────────────────────────────────────────
	client := ollama.NewClient(ollama.WithBaseURL(*host))
	provider := ollama.NewWithClient(client)

	// ── Tools ─────────────────────────────────────────────────────────────────
	listFilesTool := makeListFilesTool(workspaceAbs)
	readFileTool  := makeReadFileTool(workspaceAbs)
	writeFileTool := makeWriteFileTool(workspaceAbs)

	// ── Agents ────────────────────────────────────────────────────────────────
	plannerAgent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithModel(*model),
		goagent.WithMaxIterations(20),
		goagent.WithHooks(loggingHooks("planner")),
		goagent.WithSystemPrompt(plannerPrompt),
		goagent.WithTool(listFilesTool),
		goagent.WithTool(readFileTool),
	)
	if err != nil {
		log.Fatalf("creating planner: %v", err)
	}

	coderAgent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithModel(*model),
		goagent.WithMaxIterations(30),
		goagent.WithHooks(loggingHooks("coder")),
		goagent.WithSystemPrompt(coderPrompt),
		goagent.WithTool(listFilesTool),
		goagent.WithTool(readFileTool),
		goagent.WithTool(writeFileTool),
	)
	if err != nil {
		log.Fatalf("creating coder: %v", err)
	}

	testerAgent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithModel(*model),
		goagent.WithMaxIterations(30),
		goagent.WithHooks(loggingHooks("tester")),
		goagent.WithSystemPrompt(testerPrompt),
		goagent.WithTool(listFilesTool),
		goagent.WithTool(readFileTool),
		goagent.WithTool(writeFileTool),
	)
	if err != nil {
		log.Fatalf("creating tester: %v", err)
	}

	reviewerAgent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithModel(*model),
		goagent.WithMaxIterations(20),
		goagent.WithHooks(loggingHooks("reviewer")),
		goagent.WithSystemPrompt(reviewerPrompt),
		goagent.WithTool(listFilesTool),
		goagent.WithTool(readFileTool),
	)
	if err != nil {
		log.Fatalf("creating reviewer: %v", err)
	}

	// ── Pipeline ──────────────────────────────────────────────────────────────
	// Stages run sequentially. Each stage receives the full StageContext, so
	// PromptBuilders can inject outputs from previous stages into the next prompt.
	pipeline := orchestration.NewPipeline(
		orchestration.Stage("plan",   orchestration.AgentStage("plan",   plannerAgent,  orchestration.GoalOnly)),
		orchestration.Stage("code",   orchestration.AgentStage("code",   coderAgent,    coderPB)),
		orchestration.Stage("test",   orchestration.AgentStage("test",   testerAgent,   testerPB)),
		orchestration.Stage("review", orchestration.AgentStage("review", reviewerAgent, reviewerPB)),
	)

	fmt.Printf("Running pipeline for task: %q\n\n", *task)

	sc, err := pipeline.Run(ctx, *task)
	if err != nil {
		log.Fatalf("pipeline failed: %v", err)
	}

	// ── Output ────────────────────────────────────────────────────────────────
	for _, stage := range []struct{ key, title string }{
		{"plan",   "PLAN"},
		{"code",   "CODE SUMMARY"},
		{"test",   "TEST SUMMARY"},
		{"review", "REVIEW"},
	} {
		if v, ok := sc.Output(stage.key); ok {
			fmt.Printf("\n=== %s ===\n%s\n", stage.title, v)
		}
	}

	fmt.Println("\n--- Trace ---")
	for _, t := range sc.Trace() {
		fmt.Printf("  %-8s %s\n", t.StageName+":", t.Duration.Round(time.Millisecond))
	}

	fmt.Printf("\nDone. Files written to: %s\n", workspaceAbs)
}

// ── PromptBuilders ────────────────────────────────────────────────────────────
//
// Builders are intentionally thin — they orient the agent with goal and plan
// context, but let the agent read actual workspace files via its tools.
// This keeps prompts short and mirrors what a real coding agent would do.

func coderPB(sc *orchestration.StageContext) string {
	plan, _ := sc.Output("plan")
	return fmt.Sprintf(
		"Task: %s\n\nImplementation plan:\n%s\n\n"+
			"Implement this plan. Use list_files and read_file to explore the workspace, "+
			"then use write_file to create the implementation files.",
		sc.Goal, plan,
	)
}

func testerPB(sc *orchestration.StageContext) string {
	plan, _ := sc.Output("plan")
	return fmt.Sprintf(
		"Task: %s\n\nOriginal plan:\n%s\n\n"+
			"Use list_files and read_file to examine the implementation, "+
			"then use write_file to create the test files.",
		sc.Goal, plan,
	)
}

func reviewerPB(sc *orchestration.StageContext) string {
	plan, _ := sc.Output("plan")
	return fmt.Sprintf(
		"Task: %s\n\nOriginal plan:\n%s\n\n"+
			"Use list_files and read_file to examine all files in the workspace. "+
			"Produce a structured code review.",
		sc.Goal, plan,
	)
}

// ── System prompts ────────────────────────────────────────────────────────────

const plannerPrompt = `You are a software architect. Analyze the task and produce a detailed implementation plan.

Use list_files to understand the existing workspace and read_file to examine any relevant files.

Your response must be a structured plan with:
- Summary of what needs to be built
- List of files to create with their purpose
- Key data structures and interfaces
- Implementation order and dependencies

Do not write code. Produce only the plan.`

const coderPrompt = `You are an expert Go developer. Implement the given plan by writing Go source files.

Use list_files and read_file to explore the workspace, then write_file to create files.
Write clean, idiomatic Go with proper error handling and exported GoDoc comments.
After writing all files, briefly summarize what you created and why each file exists.`

const testerPrompt = `You are a Go testing expert. Write comprehensive tests for the implementation.

Use list_files and read_file to examine the implementation files, then write_file to create test files.
Name test files with the _test.go suffix. Use table-driven tests and cover:
- Happy paths
- Edge cases (empty input, boundary values)
- Error conditions

After writing the tests, briefly summarize what you covered.`

const reviewerPrompt = `You are a senior Go code reviewer. Review the implementation and tests in the workspace.

Use list_files and read_file to examine every file.

Produce a structured review with these sections:
1. Summary — overall assessment
2. Code quality — idiomatic Go, naming, package structure
3. Error handling — completeness and correctness
4. Test quality — coverage, edge cases, table-driven patterns
5. Issues — concrete bugs or problems found (if any)
6. Top suggestions — up to 3 specific, actionable improvements`

// ── Tools ─────────────────────────────────────────────────────────────────────

const maxFileKB    = 100
const maxFileBytes = maxFileKB * 1024

func makeListFilesTool(workspace string) goagent.Tool {
	return goagent.ToolFunc(
		"list_files",
		`Lists files and directories in the workspace. Use "." for the root.`,
		goagent.SchemaFrom(struct {
			Dir string `json:"dir,omitempty" jsonschema_description:"Relative directory path. Defaults to \".\"."`
		}{}),
		func(_ context.Context, args map[string]any) (string, error) {
			dir, _ := args["dir"].(string)
			if dir == "" {
				dir = "."
			}
			abs, err := safePath(workspace, dir)
			if err != nil {
				return err.Error(), nil
			}
			return listDir(abs, workspace)
		},
	)
}

func makeReadFileTool(workspace string) goagent.Tool {
	return goagent.ToolFunc(
		"read_file",
		fmt.Sprintf("Reads a file from the workspace. Files over %d KB are rejected.", maxFileKB),
		goagent.SchemaFrom(struct {
			Path string `json:"path" jsonschema_description:"Relative path to the file (e.g. \"main.go\", \"pkg/stack.go\")."`
		}{}),
		func(_ context.Context, args map[string]any) (string, error) {
			path, _ := args["path"].(string)
			abs, err := safePath(workspace, path)
			if err != nil {
				return err.Error(), nil
			}
			return readFile(abs)
		},
	)
}

func makeWriteFileTool(workspace string) goagent.Tool {
	return goagent.ToolFunc(
		"write_file",
		"Writes or overwrites a file in the workspace. Creates parent directories as needed.",
		goagent.SchemaFrom(struct {
			Path    string `json:"path" jsonschema_description:"Relative path to the file (e.g. \"stack.go\", \"pkg/stack_test.go\")."`
			Content string `json:"content" jsonschema_description:"Full content to write to the file."`
		}{}),
		func(_ context.Context, args map[string]any) (string, error) {
			path, _ := args["path"].(string)
			content, _ := args["content"].(string)
			abs, err := safePath(workspace, path)
			if err != nil {
				return err.Error(), nil
			}
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return fmt.Sprintf("cannot create directory: %v", err), nil
			}
			if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
				return fmt.Sprintf("cannot write file: %v", err), nil
			}
			slog.Debug("wrote file", "path", path, "bytes", len(content))
			return fmt.Sprintf("wrote %d bytes to %s", len(content), path), nil
		},
	)
}

// safePath resolves userPath relative to root and verifies it does not escape.
// Prevents directory traversal: "../../etc/passwd" and absolute paths are rejected.
func safePath(root, userPath string) (string, error) {
	clean := filepath.Clean(userPath)
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("absolute paths are not allowed: use a relative path")
	}
	abs := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path %q escapes the workspace — only paths inside it are allowed", userPath)
	}
	return abs, nil
}

func listDir(abs, root string) (string, error) {
	entries, err := os.ReadDir(abs)
	if err != nil {
		return fmt.Sprintf("cannot read directory: %v", err), nil
	}
	relBase, _ := filepath.Rel(root, abs)
	var sb strings.Builder
	fmt.Fprintf(&sb, "Contents of %s:\n", relBase)
	for _, e := range entries {
		info, _ := e.Info()
		if e.IsDir() {
			fmt.Fprintf(&sb, "  [dir]  %s/\n", e.Name())
		} else if info != nil {
			fmt.Fprintf(&sb, "  [file] %-40s  %d bytes\n", e.Name(), info.Size())
		} else {
			fmt.Fprintf(&sb, "  [file] %s\n", e.Name())
		}
	}
	if len(entries) == 0 {
		sb.WriteString("  (empty)\n")
	}
	return sb.String(), nil
}

func readFile(abs string) (string, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Sprintf("cannot access path: %v", err), nil
	}
	if info.IsDir() {
		return "that path is a directory — use list_files to explore it", nil
	}
	if info.Size() > maxFileBytes {
		return fmt.Sprintf("file too large (%d bytes); maximum is %d KB", info.Size(), maxFileKB), nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Sprintf("cannot read file: %v", err), nil
	}
	return string(data), nil
}

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
