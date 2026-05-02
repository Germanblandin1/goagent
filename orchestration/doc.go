// Package orchestration provee primitivas para coordinar múltiples agentes
// goagent en pipelines secuenciales y paralelos.
//
// El tipo central es Executor — cualquier cosa que implemente
// RunWithContext(ctx, *StageContext) error puede participar en un pipeline.
// Los agentes goagent se integran vía AgentAdapter o AgentBlocksAdapter.
//
// Ejemplo de uso:
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
//
//	result, err := pipeline.Run(ctx, "implementar endpoint REST")
package orchestration
