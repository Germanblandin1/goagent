package orchestration

import (
	"context"
	"fmt"

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
// If pb is nil, GoalOnly is used.
func NewAgentAdapter(agent agentRunner, outputKey string, pb PromptBuilder) *AgentAdapter {
	if pb == nil {
		pb = GoalOnly
	}
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
	if a.blocksBuilder == nil {
		return fmt.Errorf("orchestration: AgentBlocksAdapter %q: blocksBuilder must not be nil", a.outputKey)
	}
	blocks := a.blocksBuilder(sc)

	output, err := a.agent.RunBlocks(ctx, blocks...)
	if err != nil {
		return err
	}

	sc.SetOutput(a.outputKey, output)
	return nil
}

// AgentStage is syntactic sugar for the text case.
// The stage name registered via Stage() is automatically used as the output key,
// eliminating the redundancy of passing the name twice.
// If pb is nil, GoalOnly is used.
//
// AgentStage must be used inside a Pipeline via Stage(). Calling RunWithContext
// directly without a Pipeline context returns an error.
//
// Example:
//
//	orchestration.Stage("research", orchestration.AgentStage(
//	    researcherAgent,
//	    func(sc *orchestration.StageContext) string { return "Research: " + sc.Goal },
//	))
func AgentStage(agent agentRunner, pb PromptBuilder) Executor {
	if pb == nil {
		pb = GoalOnly
	}
	return &agentStageAdapter{agent: agent, promptBuilder: pb}
}

type agentStageAdapter struct {
	agent         agentRunner
	promptBuilder PromptBuilder
}

func (a *agentStageAdapter) RunWithContext(ctx context.Context, sc *StageContext) error {
	outputKey := StageNameFromContext(ctx)
	if outputKey == "" {
		return fmt.Errorf("orchestration: AgentStage requires a Pipeline context — register it with Stage()")
	}
	output, err := a.agent.Run(ctx, a.promptBuilder(sc))
	if err != nil {
		return err
	}
	sc.SetOutput(outputKey, output)
	return nil
}

// AgentBlocksStage is syntactic sugar for the multimodal case.
// The stage name registered via Stage() is automatically used as the output key.
// AgentBlocksStage must be used inside a Pipeline via Stage().
func AgentBlocksStage(agent agentRunner, bb BlocksBuilder) Executor {
	return &agentBlocksStageAdapter{agent: agent, blocksBuilder: bb}
}

type agentBlocksStageAdapter struct {
	agent         agentRunner
	blocksBuilder BlocksBuilder
}

func (a *agentBlocksStageAdapter) RunWithContext(ctx context.Context, sc *StageContext) error {
	if a.blocksBuilder == nil {
		return fmt.Errorf("orchestration: AgentBlocksStage: blocksBuilder must not be nil")
	}
	outputKey := StageNameFromContext(ctx)
	if outputKey == "" {
		return fmt.Errorf("orchestration: AgentBlocksStage requires a Pipeline context — register it with Stage()")
	}
	blocks := a.blocksBuilder(sc)
	output, err := a.agent.RunBlocks(ctx, blocks...)
	if err != nil {
		return err
	}
	sc.SetOutput(outputKey, output)
	return nil
}
