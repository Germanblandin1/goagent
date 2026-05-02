package orchestration

import (
	"context"
	"fmt"

	"github.com/Germanblandin1/goagent"
)

// AgentTool wraps an agentRunner as a goagent.Tool so that a supervisor
// agent can invoke it via tool calls.
//
// The tool schema always exposes a single "input" string field.
// The supervisor LLM constructs the full message for the worker in that field.
//
// description tells the supervisor what the worker does.
// inputDescription tells the supervisor how to formulate the input for this
// specific worker — this directly affects the quality of the supervisor's
// decisions and should be as specific as possible.
type AgentTool struct {
	name             string
	description      string
	inputDescription string
	agent            agentRunner
}

// NewAgentTool constructs an AgentTool.
//
// name is the tool name the supervisor LLM uses to invoke the worker.
// description explains what the worker does.
// inputDescription explains how to formulate the input string for this worker.
//
// Example:
//
//	orchestration.NewAgentTool(
//	    "researcher",
//	    "Researches technical topics in depth",
//	    "The topic to research. Include technology, version, and project context.",
//	    researcherAgent,
//	)
func NewAgentTool(name, description, inputDescription string, agent agentRunner) *AgentTool {
	return &AgentTool{
		name:             name,
		description:      description,
		inputDescription: inputDescription,
		agent:            agent,
	}
}

// Definition implements goagent.Tool.
// Returns a ToolDefinition with a single required "input" string field.
func (t *AgentTool) Definition() goagent.ToolDefinition {
	return goagent.ToolDefinition{
		Name:        t.name,
		Description: t.description,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{
					"type":        "string",
					"description": t.inputDescription,
				},
			},
			"required": []string{"input"},
		},
	}
}

// Execute implements goagent.Tool.
// Extracts "input" from args, calls the worker agent, and returns its output
// as a TextBlock.
func (t *AgentTool) Execute(ctx context.Context, args map[string]any) ([]goagent.ContentBlock, error) {
	input, ok := args["input"].(string)
	if !ok || input == "" {
		return nil, fmt.Errorf("orchestration: agent tool %q: missing or empty \"input\" argument", t.name)
	}

	output, err := t.agent.Run(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("orchestration: agent tool %q: %w", t.name, err)
	}

	return []goagent.ContentBlock{goagent.TextBlock(output)}, nil
}
