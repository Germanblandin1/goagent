// Package goagent provides a Go-idiomatic framework for building AI agents
// with a ReAct loop and pluggable providers.
//
// # Overview
//
// goagent orchestrates the interaction between a language model and a set of
// tools. On each iteration of the ReAct loop the model either produces a final
// text answer or requests one or more tool calls. The agent dispatches those
// calls in parallel, feeds the results back, and repeats until the model stops
// or the iteration budget (WithMaxIterations) is exhausted.
//
// The framework is built around three small interfaces:
//
//   - [Provider] — wraps an LLM backend (Ollama, Anthropic, OpenAI-compatible, …).
//   - [Tool] — a capability the model can invoke (calculator, web search, …).
//   - [ShortTermMemory] / [LongTermMemory] — optional conversation persistence.
//
// All configuration uses functional options passed to [New].
//
// # Basic usage
//
//	provider := ollama.New(ollama.WithBaseURL("http://localhost:11434"))
//
//	add := goagent.ToolFunc("add", "Sum two numbers",
//	    map[string]any{
//	        "type": "object",
//	        "properties": map[string]any{
//	            "a": map[string]any{"type": "number"},
//	            "b": map[string]any{"type": "number"},
//	        },
//	        "required": []string{"a", "b"},
//	    },
//	    func(ctx context.Context, args map[string]any) (string, error) {
//	        a, _ := args["a"].(float64)
//	        b, _ := args["b"].(float64)
//	        return fmt.Sprintf("%g", a+b), nil
//	    },
//	)
//
//	agent := goagent.New(
//	    goagent.WithProvider(provider),
//	    goagent.WithTool(add),
//	    goagent.WithMaxIterations(5),
//	)
//
//	answer, err := agent.Run(ctx, "What is 2 + 3?")
//
// # Multimodal content
//
// [RunBlocks] accepts images and documents alongside text:
//
//	answer, err := agent.RunBlocks(ctx,
//	    goagent.ImageBlock(pngBytes, "image/png"),
//	    goagent.TextBlock("Describe this image"),
//	)
//
// # Memory
//
// By default each [Agent.Run] call is stateless. To maintain conversation
// context across calls, configure a [ShortTermMemory] via [WithShortTermMemory].
// For semantic retrieval across sessions, add a [LongTermMemory] via
// [WithLongTermMemory]. Implementations live in the memory sub-package.
//
// # Sub-packages
//
//   - memory — ShortTermMemory and LongTermMemory with pluggable storage and policies.
//   - memory/storage — persistence backends (in-memory; bring your own).
//   - memory/policy — read-time filters: FixedWindow, TokenWindow, NoOp.
//   - providers/anthropic — Provider for the Anthropic Messages API (Claude).
//   - providers/ollama — Provider for Ollama (OpenAI-compatible API).
//   - internal/testutil — mock implementations for testing agents.
package goagent
