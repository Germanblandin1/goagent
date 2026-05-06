package orchestration

// PromptBuilder builds the input string for an AgentAdapter from the current
// StageContext state.
//
// It lives inside the AgentAdapter — the *goagent.Agent never sees the
// StageContext. The caller defines the PromptBuilder when constructing the
// pipeline, where it has visibility into which outputs exist.
type PromptBuilder func(sc *StageContext) string

// GoalOnly is the simplest PromptBuilder: it returns the Goal unchanged.
// Useful for the first stage of any pipeline.
func GoalOnly(sc *StageContext) string {
	return sc.Goal
}

// OutputOf returns a PromptBuilder that reads the output of a specific named
// stage. If that stage has not yet written an output, it returns the Goal.
//
// Prefer OutputOf over LastOutput when the pipeline contains a ParallelGroup:
// parallel stages complete in non-deterministic order, so LastOutput may pick
// the wrong stage. OutputOf selects the stage explicitly by name, making the
// intent clear and the result deterministic regardless of execution order.
//
//	orchestration.Stage("review", reviewExecutor, orchestration.WithPromptBuilder(
//	    orchestration.OutputOf("summarize"),
//	))
func OutputOf(stageName string) PromptBuilder {
	return func(sc *StageContext) string {
		if v, ok := sc.Output(stageName); ok {
			return v
		}
		return sc.Goal
	}
}

// LastOutput returns the output of the last successful stage recorded in the
// Trace. If no previous outputs exist, it returns the Goal.
//
// Useful for simple linear pipelines where each stage processes the output
// of the previous one without needing access by explicit name.
//
// Warning: do not use LastOutput after a ParallelGroup. The trace records
// parallel stages in completion order, which is non-deterministic — LastOutput
// may return the output of whichever stage happened to finish last, not the
// one intended. Use OutputOf to select a specific stage by name instead.
func LastOutput(sc *StageContext) string {
	trace := sc.Trace()
	for i := len(trace) - 1; i >= 0; i-- {
		t := trace[i]
		if t.Err == nil {
			if v, ok := sc.Output(t.StageName); ok {
				return v
			}
		}
	}
	return sc.Goal
}
