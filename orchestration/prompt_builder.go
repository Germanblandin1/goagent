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

// LastOutput returns the output of the last successful stage recorded in the
// Trace. If no previous outputs exist, it returns the Goal.
//
// Useful for simple linear pipelines where each stage processes the output
// of the previous one without needing access by explicit name.
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
