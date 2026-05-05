package orchestration

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// PanicError is returned when an Executor inside a ParallelGroup panics.
// It wraps the recovered panic value so callers can distinguish panics
// from normal errors via errors.As.
type PanicError struct {
	// StageName is the name of the stage that panicked.
	StageName string
	// Value is the value passed to panic().
	Value any
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("parallel stage %q panicked: %v", e.StageName, e.Value)
}

// ParallelGroupOption configures a ParallelGroup.
type ParallelGroupOption func(*ParallelGroup)

// ParallelGroup executes multiple Executors concurrently.
// All stages share the same StageContext — reads and writes to Outputs,
// Artifacts, and Trace are memory-safe because StageContext uses an internal
// mutex.
//
// Two parallel stages writing to the same key produce a non-deterministic
// result (last write wins). Avoiding key collisions is the caller's
// responsibility.
//
// ParallelGroup implements Executor — it can be nested inside a Pipeline.
//
// Example:
//
//	orchestration.NewParallelGroup(
//	    orchestration.WithParallelStages(
//	        orchestration.Stage("code",  coderExecutor),
//	        orchestration.Stage("tests", testerExecutor),
//	    ),
//	)
type ParallelGroup struct {
	stages []StageDef
	hooks  OrchestrationHooks
}

// WithParallelStages sets the stages of the parallel group.
func WithParallelStages(stages ...StageDef) ParallelGroupOption {
	return func(g *ParallelGroup) {
		g.stages = stages
	}
}

// WithParallelHooks configures observability hooks for the parallel group.
// Hooks are called around each stage execution.
// The zero value of OrchestrationHooks is safe — unset fields are no-ops.
func WithParallelHooks(h OrchestrationHooks) ParallelGroupOption {
	return func(g *ParallelGroup) {
		g.hooks = h
	}
}

// NewParallelGroup constructs a ParallelGroup from the given options.
// Stages are provided via WithParallelStages; hooks via WithParallelHooks.
func NewParallelGroup(opts ...ParallelGroupOption) *ParallelGroup {
	g := &ParallelGroup{}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// Run is the top-level entry point for using a ParallelGroup standalone.
// Constructs a StageContext with the given goal and executes all stages
// concurrently. Returns the complete StageContext so the caller can inspect
// all outputs and artifacts produced.
func (g *ParallelGroup) Run(ctx context.Context, goal string) (*StageContext, error) {
	sc := NewStageContext(goal)
	return sc, g.RunWithContext(ctx, sc)
}

// RunWithContext implements Executor.
// Launches one goroutine per stage and waits for all before returning.
// Stages write directly to sc via its thread-safe methods (SetOutput,
// SetArtifact). All stages run to completion regardless of failures;
// the first error encountered is returned.
func (g *ParallelGroup) RunWithContext(ctx context.Context, sc *StageContext) error {
	type result struct {
		name     string
		duration time.Duration
		err      error
	}

	results := make(chan result, len(g.stages))

	for _, s := range g.stages {
		go func() {
			stageCtx := invokeStart(g.hooks.OnStageStart, ctx, s.name)
			start := time.Now()

			var stageErr error
			func() {
				defer func() {
					if rec := recover(); rec != nil {
						stageErr = &PanicError{StageName: s.name, Value: rec}
					}
				}()
				stageErr = s.executor.RunWithContext(stageCtx, sc)
			}()
			dur := time.Since(start)

			if fn := g.hooks.OnStageEnd; fn != nil {
				fn(stageCtx, s.name, dur, stageErr)
			}

			results <- result{s.name, dur, stageErr}
		}()
	}

	var errs []error
	for range g.stages {
		r := <-results
		sc.appendTrace(r.name, r.duration, r.err)
		if r.err != nil {
			errs = append(errs, fmt.Errorf("parallel stage %q: %w", r.name, r.err))
		}
	}

	return errors.Join(errs...)
}
