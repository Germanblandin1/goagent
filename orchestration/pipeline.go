package orchestration

import (
	"context"
	"fmt"
	"time"
)

// namedExecutor associates a name with an Executor for the Trace.
// Not exported — constructed via the Stage function.
type namedExecutor struct {
	name     string
	executor Executor
}

// Stage builds a namedExecutor for use in WithStages and WithParallelStages.
//
// Example:
//
//	orchestration.Stage("research", myExecutor)
func Stage(name string, e Executor) namedExecutor {
	return namedExecutor{name: name, executor: e}
}

// PipelineOption configures a Pipeline.
type PipelineOption func(*Pipeline)

// Pipeline executes Executors sequentially.
// The StageContext travels through all stages — each one can read outputs
// from previous stages and write its own.
//
// Pipeline implements Executor — it can be nested inside another Pipeline
// or inside a ParallelGroup.
type Pipeline struct {
	stages []namedExecutor
	hooks  OrchestrationHooks
}

// WithStages sets the stages of the pipeline in the given order.
func WithStages(stages ...namedExecutor) PipelineOption {
	return func(p *Pipeline) {
		p.stages = stages
	}
}

// WithPipelineHooks configures observability hooks for the pipeline.
// Hooks are called around each stage execution and around the pipeline itself.
// The zero value of OrchestrationHooks is safe — unset fields are no-ops.
func WithPipelineHooks(h OrchestrationHooks) PipelineOption {
	return func(p *Pipeline) {
		p.hooks = h
	}
}

// NewPipeline constructs a Pipeline from the given options.
// Stages are provided via WithStages; hooks via WithPipelineHooks.
//
// Example:
//
//	orchestration.NewPipeline(
//	    orchestration.WithStages(
//	        orchestration.Stage("research", researcherAdapter),
//	        orchestration.Stage("code",     coderAdapter),
//	    ),
//	    orchestration.WithPipelineHooks(hooks),
//	)
func NewPipeline(opts ...PipelineOption) *Pipeline {
	p := &Pipeline{}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Run is the main entry point of the Pipeline.
// Builds the StageContext with the given goal and executes all stages.
// Returns the complete StageContext (with Outputs, Artifacts, and Trace)
// so the caller can inspect intermediate results.
func (p *Pipeline) Run(ctx context.Context, goal string) (*StageContext, error) {
	sc := NewStageContext(goal)
	return sc, p.RunWithContext(ctx, sc)
}

// RunWithContext implements Executor.
// Allows nesting this Pipeline inside another Pipeline or ParallelGroup.
// Respects context cancellation before each stage.
func (p *Pipeline) RunWithContext(ctx context.Context, sc *StageContext) (err error) {
	pipelineCtx := invokeStart(p.hooks.OnPipelineStart, ctx, sc.Goal)

	defer func() {
		if fn := p.hooks.OnPipelineEnd; fn != nil {
			fn(pipelineCtx, sc, err)
		}
	}()

	for _, s := range p.stages {
		select {
		case <-pipelineCtx.Done():
			return pipelineCtx.Err()
		default:
		}

		stageCtx := invokeStart(p.hooks.OnStageStart, pipelineCtx, s.name)

		start := time.Now()
		stageErr := s.executor.RunWithContext(stageCtx, sc)
		dur := time.Since(start)
		sc.appendTrace(s.name, dur, stageErr)

		if fn := p.hooks.OnStageEnd; fn != nil {
			fn(stageCtx, s.name, dur, stageErr)
		}

		if stageErr != nil {
			return fmt.Errorf("stage %q: %w", s.name, stageErr)
		}
	}
	return nil
}
