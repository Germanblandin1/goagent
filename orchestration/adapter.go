package orchestration

import (
	"context"

	"github.com/Germanblandin1/goagent"
)

// agentRunner is the minimal interface the adapters need from the agent.
// Allows mocking in tests without depending on the concrete *goagent.Agent type.
type agentRunner interface {
	Run(ctx context.Context, prompt string) (string, error)
	RunBlocks(ctx context.Context, blocks ...goagent.ContentBlock) (string, error)
}

// AgentAdapter wraps an agent and adapts it to the Executor interface
// for use with text prompts.
//
// outputKey is the key under which the result is stored via sc.SetOutput.
// The PromptBuilder decides how to build the input from the StageContext.
type AgentAdapter struct {
	agent         agentRunner
	outputKey     string
	promptBuilder PromptBuilder
}

// NewAgentAdapter constructs an AgentAdapter.
func NewAgentAdapter(agent agentRunner, outputKey string, pb PromptBuilder) *AgentAdapter {
	return &AgentAdapter{
		agent:         agent,
		outputKey:     outputKey,
		promptBuilder: pb,
	}
}

// RunWithContext implements Executor.
// Builds the prompt via PromptBuilder, calls agent.Run,
// and stores the result via sc.SetOutput(outputKey, ...).
func (a *AgentAdapter) RunWithContext(ctx context.Context, sc *StageContext) error {
	input := a.promptBuilder(sc)

	output, err := a.agent.Run(ctx, input)
	if err != nil {
		return err
	}

	sc.SetOutput(a.outputKey, output)
	return nil
}

// BlocksBuilder builds multimodal content blocks for a stage using RunBlocks.
type BlocksBuilder func(sc *StageContext) []goagent.ContentBlock

// AgentBlocksAdapter wraps an agent and adapts it to the Executor interface
// for use with multimodal content (images, documents, combined text).
type AgentBlocksAdapter struct {
	agent         agentRunner
	outputKey     string
	blocksBuilder BlocksBuilder
}

// NewAgentBlocksAdapter constructs an AgentBlocksAdapter.
func NewAgentBlocksAdapter(agent agentRunner, outputKey string, bb BlocksBuilder) *AgentBlocksAdapter {
	return &AgentBlocksAdapter{
		agent:         agent,
		outputKey:     outputKey,
		blocksBuilder: bb,
	}
}

// RunWithContext implements Executor.
// Builds the blocks via BlocksBuilder, calls agent.RunBlocks,
// and stores the result via sc.SetOutput(outputKey, ...).
func (a *AgentBlocksAdapter) RunWithContext(ctx context.Context, sc *StageContext) error {
	blocks := a.blocksBuilder(sc)

	output, err := a.agent.RunBlocks(ctx, blocks...)
	if err != nil {
		return err
	}

	sc.SetOutput(a.outputKey, output)
	return nil
}

// AgentStage is syntactic sugar for the text case.
// Equivalent to NewAgentAdapter with the same name for outputKey and Stage.
//
// Example:
//
//	orchestration.AgentStage("research", researcherAgent, func(sc *orchestration.StageContext) string {
//	    return "Investigá: " + sc.Goal
//	})
func AgentStage(name string, agent agentRunner, pb PromptBuilder) Executor {
	return NewAgentAdapter(agent, name, pb)
}

// AgentBlocksStage is syntactic sugar for the multimodal case.
func AgentBlocksStage(name string, agent agentRunner, bb BlocksBuilder) Executor {
	return NewAgentBlocksAdapter(agent, name, bb)
}
