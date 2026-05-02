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

// Stage builds a namedExecutor for use in NewPipeline and NewParallelGroup.
//
// Example:
//
//	orchestration.Stage("research", myExecutor)
func Stage(name string, e Executor) namedExecutor {
	return namedExecutor{name: name, executor: e}
}

// Pipeline executes Executors sequentially.
// The StageContext travels through all stages — each one can read outputs
// from previous stages and write its own.
//
// Pipeline implements Executor — it can be nested inside another Pipeline
// or inside a ParallelGroup.
type Pipeline struct {
	stages []namedExecutor
}

// NewPipeline constructs a Pipeline. Stages execute in the given order.
func NewPipeline(stages ...namedExecutor) *Pipeline {
	return &Pipeline{stages: stages}
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
func (p *Pipeline) RunWithContext(ctx context.Context, sc *StageContext) error {
	for _, s := range p.stages {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		start := time.Now()
		err := s.executor.RunWithContext(ctx, sc)
		sc.appendTrace(s.name, time.Since(start), err)

		if err != nil {
			return fmt.Errorf("stage %q: %w", s.name, err)
		}
	}
	return nil
}
