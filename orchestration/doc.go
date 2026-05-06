// Package orchestration provides primitives for coordinating multiple goagent
// agents in sequential pipelines, parallel groups, and dynamic graphs.
//
// # Choosing between Pipeline, Graph, and Supervisor
//
// Use Pipeline when the flow is linear and deterministic: each stage runs in
// order and passes its output to the next. Branching and loops are not
// supported.
//
// Use Graph when routing depends on node outputs: nodes return the name of
// the next node to run, enabling conditional branching and feedback loops.
//
// Use Supervisor when you want emergent delegation via LLM tool calls: the
// supervisor agent decides at runtime which worker agents to invoke and in
// what order.
//
// # Core abstraction
//
// The central type is Executor — anything that implements
// RunWithContext(ctx, *StageContext) error can participate in a pipeline.
// goagent agents integrate via AgentAdapter or AgentBlocksAdapter.
//
// Pipeline — sequential execution:
//
//	pipeline := orchestration.NewPipeline(
//	    orchestration.WithStages(
//	        orchestration.Stage("research", orchestration.AgentStage(
//	            researcherAgent,
//	            func(sc *orchestration.StageContext) string {
//	                return "Research: " + sc.Goal
//	            },
//	        )),
//	        orchestration.Stage("code", orchestration.AgentStage(
//	            coderAgent,
//	            func(sc *orchestration.StageContext) string {
//	                research, _ := sc.RequireOutput("research")
//	                return fmt.Sprintf("Goal: %s\n\nResearch:\n%s", sc.Goal, research)
//	            },
//	        )),
//	    ),
//	)
//	sc, err := pipeline.Run(ctx, "implement a REST endpoint")
//
// Graph — routing dinámico con branching y loops:
//
//	graph, err := orchestration.NewGraph(
//	    orchestration.WithStart("generate"),
//	    orchestration.WithMaxIterations(20),
//	    orchestration.WithNode("generate", func(ctx context.Context, sc *orchestration.StageContext) (string, error) {
//	        output, err := coderAgent.Run(ctx, sc.Goal)
//	        if err != nil {
//	            return "", err
//	        }
//	        sc.SetOutput("code", output)
//	        return "review", nil
//	    }),
//	    orchestration.WithNode("review", func(ctx context.Context, sc *orchestration.StageContext) (string, error) {
//	        code, _ := sc.RequireOutput("code")
//	        verdict, err := reviewerAgent.Run(ctx, "Review: "+code)
//	        if err != nil {
//	            return "", err
//	        }
//	        if strings.Contains(verdict, "APPROVED") {
//	            return "", nil // END
//	        }
//	        sc.SetArtifact("feedback", verdict)
//	        return "generate", nil // loop
//	    }),
//	)
//	sc, err := graph.Run(ctx, "implement a REST endpoint")
package orchestration
