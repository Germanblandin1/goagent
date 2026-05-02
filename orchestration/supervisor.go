package orchestration

import (
	"fmt"

	"github.com/Germanblandin1/goagent"
)

// Worker describes an agent that a supervisor can delegate tasks to.
//
// Name is the identifier the supervisor LLM uses in tool calls.
// Description tells the supervisor what the worker does.
// InputDescription tells the supervisor how to formulate the input for this
// specific worker. This is the most important field for quality — the more
// specific, the better the supervisor's decisions.
//
// Workers should be configured without memory (or with a minimal window)
// because the supervisor is responsible for passing all necessary context in
// the input string on each call. Memory in workers creates implicit ordering
// dependencies that make behavior harder to predict.
type Worker struct {
	Name             string
	Description      string
	InputDescription string
	Agent            agentRunner
}

// NewSupervisor constructs an Executor that coordinates workers via tool calls.
//
// The supervisor is a goagent.Agent whose tools are AgentTools wrapping each
// worker. Internally it is an AgentAdapter — it writes its final synthesized
// output to sc.Outputs[outputKey].
//
// opts configures the underlying agent and must include at minimum
// goagent.WithProvider and goagent.WithModel. Any goagent.Option is accepted:
// WithSystemPrompt, WithMaxIterations, WithThinking, WithHooks, etc.
// The AgentTools for the workers are appended after opts, so callers must not
// add worker tools manually.
//
// pb is the PromptBuilder that constructs the supervisor's input from the
// StageContext. Pass nil to use GoalOnly (passes sc.Goal directly).
//
// The supervisor LLM decides at runtime which workers to call, in what order,
// and whether to call multiple workers in parallel (by emitting multiple tool
// calls in a single response). This behavior emerges from the model and the
// system prompt — it is not hardcoded.
//
// Example:
//
//	supervisor, err := orchestration.NewSupervisor(
//	    "result",
//	    nil,
//	    []orchestration.Worker{
//	        {
//	            Name:             "researcher",
//	            Description:      "Researches technical topics in depth",
//	            InputDescription: "The topic to research, with technology and version context.",
//	            Agent:            researcherAgent,
//	        },
//	        {
//	            Name:             "coder",
//	            Description:      "Writes idiomatic Go code",
//	            InputDescription: "Goal and any research context. Include design constraints.",
//	            Agent:            coderAgent,
//	        },
//	    },
//	    goagent.WithProvider(provider),
//	    goagent.WithModel("claude-sonnet-4-6"),
//	    goagent.WithSystemPrompt(`You are a software coordinator...`),
//	)
func NewSupervisor(
	outputKey string,
	pb PromptBuilder,
	workers []Worker,
	opts ...goagent.Option,
) (Executor, error) {
	if len(workers) == 0 {
		return nil, fmt.Errorf("orchestration: supervisor requires at least one worker")
	}

	allOpts := make([]goagent.Option, 0, len(opts)+len(workers))
	allOpts = append(allOpts, opts...)
	for _, w := range workers {
		allOpts = append(allOpts, goagent.WithTool(
			NewAgentTool(w.Name, w.Description, w.InputDescription, w.Agent),
		))
	}

	agent, err := goagent.New(allOpts...)
	if err != nil {
		return nil, fmt.Errorf("orchestration: supervisor: %w", err)
	}

	if pb == nil {
		pb = GoalOnly
	}

	return NewAgentAdapter(agent, outputKey, pb), nil
}
