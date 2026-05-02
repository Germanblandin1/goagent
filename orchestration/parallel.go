package orchestration

import (
	"context"
	"fmt"
	"time"
)

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
//	    orchestration.Stage("code",  coderExecutor),   // calls sc.SetOutput("code", ...)
//	    orchestration.Stage("tests", testerExecutor),  // calls sc.SetOutput("tests", ...)
//	)
type ParallelGroup struct {
	stages []namedExecutor
}

// NewParallelGroup constructs a ParallelGroup.
func NewParallelGroup(stages ...namedExecutor) *ParallelGroup {
	return &ParallelGroup{stages: stages}
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
			start := time.Now()
			err := s.executor.RunWithContext(ctx, sc)
			results <- result{s.name, time.Since(start), err}
		}()
	}

	var first error
	for range g.stages {
		r := <-results
		sc.appendTrace(r.name, r.duration, r.err)
		if r.err != nil && first == nil {
			first = fmt.Errorf("parallel stage %q: %w", r.name, r.err)
		}
	}

	return first
}
