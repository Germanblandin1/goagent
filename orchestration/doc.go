// Package orchestration provee primitivas para coordinar múltiples agentes
// goagent en pipelines secuenciales, paralelos y grafos dinámicos.
//
// El tipo central es Executor — cualquier cosa que implemente
// RunWithContext(ctx, *StageContext) error puede participar en un pipeline.
// Los agentes goagent se integran vía AgentAdapter o AgentBlocksAdapter.
//
// Pipeline — ejecución secuencial:
//
//	pipeline := orchestration.NewPipeline(
//	    orchestration.Stage("research", orchestration.AgentStage(
//	        "research", researcherAgent,
//	        func(sc *orchestration.StageContext) string {
//	            return "Investigá: " + sc.Goal
//	        },
//	    )),
//	    orchestration.Stage("code", orchestration.AgentStage(
//	        "code", coderAgent,
//	        func(sc *orchestration.StageContext) string {
//	            research, _ := sc.RequireOutput("research")
//	            return fmt.Sprintf("Objetivo: %s\n\nInvestigación:\n%s", sc.Goal, research)
//	        },
//	    )),
//	)
//	sc, err := pipeline.Run(ctx, "implementar endpoint REST")
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
