package orchestration_test

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/internal/testutil"
	"github.com/Germanblandin1/goagent/orchestration"
)

// exampleExec is a minimal Executor for documentation examples.
// It writes a fixed string to sc.SetOutput(key) and returns nil.
type exampleExec struct{ key, val string }

func (e exampleExec) RunWithContext(_ context.Context, sc *orchestration.StageContext) error {
	sc.SetOutput(e.key, e.val)
	return nil
}

// exampleFuncExec adapts a plain function to the orchestration.Executor interface.
type exampleFuncExec func(context.Context, *orchestration.StageContext) error

func (f exampleFuncExec) RunWithContext(ctx context.Context, sc *orchestration.StageContext) error {
	return f(ctx, sc)
}

// Example demonstrates a two-stage sequential pipeline where a researcher agent
// produces output that the writer agent consumes. Both use a deterministic mock
// provider so no API key is required.
func Example() {
	researcher, err := goagent.New(goagent.WithProvider(testutil.NewMockProvider(
		goagent.CompletionResponse{
			Message:    goagent.AssistantMessage("Go uses interfaces for polymorphism."),
			StopReason: goagent.StopReasonEndTurn,
		},
	)))
	if err != nil {
		log.Fatal(err)
	}
	writer, err := goagent.New(goagent.WithProvider(testutil.NewMockProvider(
		goagent.CompletionResponse{
			Message:    goagent.AssistantMessage("Article published."),
			StopReason: goagent.StopReasonEndTurn,
		},
	)))
	if err != nil {
		log.Fatal(err)
	}

	pipeline := orchestration.NewPipeline(
		orchestration.WithStages(
			orchestration.Stage("research", orchestration.NewAgentAdapter(researcher, "research", orchestration.GoalOnly)),
			orchestration.Stage("write", orchestration.NewAgentAdapter(writer, "write", orchestration.OutputOf("research"))),
		),
	)

	sc, err := pipeline.Run(context.Background(), "explain Go interfaces")
	if err != nil {
		log.Fatal(err)
	}
	result, _ := sc.RequireOutput("write")
	fmt.Println(result)
	// Output: Article published.
}

// ExampleNewStageContext shows how to build a StageContext for testing custom Executors
// outside of a Pipeline.
func ExampleNewStageContext() {
	sc := orchestration.NewStageContext("build a CLI tool")
	fmt.Println(sc.Goal)
	// Output: build a CLI tool
}

// ExampleNewStrictStageContext shows collision detection intended for development
// and testing. Every duplicate key write records a KeyCollisionError that is
// retrievable after all stages complete via CollisionErrors.
func ExampleNewStrictStageContext() {
	sc := orchestration.NewStrictStageContext("test goal")
	sc.SetOutput("result", "first write")
	sc.SetOutput("result", "second write") // collision — key already written

	errs := sc.CollisionErrors()
	fmt.Println(len(errs))
	fmt.Println(errs[0])
	// Output:
	// 1
	// orchestration: strict key collision in output "result": key already set
}

// ExampleStageContext_SetOutput shows storing and reading back a text output.
func ExampleStageContext_SetOutput() {
	sc := orchestration.NewStageContext("goal")
	sc.SetOutput("summary", "this is the summary")

	v, ok := sc.Output("summary")
	fmt.Println(ok)
	fmt.Println(v)
	// Output:
	// true
	// this is the summary
}

// ExampleStageContext_RequireOutput shows the typed error returned when a key
// has not been written by any previous stage.
func ExampleStageContext_RequireOutput() {
	sc := orchestration.NewStageContext("goal")
	_, err := sc.RequireOutput("missing_key")
	fmt.Println(err)
	// Output: orchestration: output "missing_key" not found in stage context
}

// ExampleGetArtifact demonstrates type-safe retrieval of a structured Go value
// stored by an Executor via SetArtifact.
func ExampleGetArtifact() {
	sc := orchestration.NewStageContext("goal")
	sc.SetArtifact("confidence", 0.95)

	score, err := orchestration.GetArtifact[float64](sc, "confidence")
	fmt.Println(err)
	fmt.Printf("%.2f\n", score)
	// Output:
	// <nil>
	// 0.95
}

// ExampleGoalOnly demonstrates the simplest PromptBuilder: it returns sc.Goal unchanged.
func ExampleGoalOnly() {
	sc := orchestration.NewStageContext("research quantum computing")
	fmt.Println(orchestration.GoalOnly(sc))
	// Output: research quantum computing
}

// ExampleOutputOf demonstrates a PromptBuilder that reads a specific named stage's
// output. Falls back to sc.Goal when the named stage has not yet written an output.
func ExampleOutputOf() {
	sc := orchestration.NewStageContext("build a CLI")
	sc.SetOutput("research", "Go CLI best practices")

	pb := orchestration.OutputOf("research")
	fmt.Println(pb(sc))
	// Output: Go CLI best practices
}

// ExampleLastOutput shows a PromptBuilder that reads the last successful stage output.
// Prefer OutputOf when the pipeline contains a ParallelGroup.
func ExampleLastOutput() {
	sc := orchestration.NewStageContext("goal")
	e := exampleExec{key: "step", val: "step output"}
	p := orchestration.NewPipeline(orchestration.WithStages(orchestration.Stage("step", e)))
	_ = p.RunWithContext(context.Background(), sc)

	fmt.Println(orchestration.LastOutput(sc))
	// Output: step output
}

// ExampleStageNameFromContext shows retrieving the stage name injected by Pipeline
// from inside an Executor.
func ExampleStageNameFromContext() {
	var captured string
	e := exampleFuncExec(func(ctx context.Context, _ *orchestration.StageContext) error {
		captured = orchestration.StageNameFromContext(ctx)
		return nil
	})
	p := orchestration.NewPipeline(orchestration.WithStages(orchestration.Stage("transform", e)))
	_, _ = p.Run(context.Background(), "goal")
	fmt.Println(captured)
	// Output: transform
}

// ExampleNodeNameFromContext shows retrieving the node name injected by Graph
// from inside a NodeFunc.
func ExampleNodeNameFromContext() {
	var captured string
	nodeFunc := func(ctx context.Context, _ *orchestration.StageContext) (string, error) {
		captured = orchestration.NodeNameFromContext(ctx)
		return "", nil
	}
	g, _ := orchestration.NewGraph(
		orchestration.WithStart("process"),
		orchestration.WithNode("process", nodeFunc),
	)
	_, _ = g.Run(context.Background(), "goal")
	fmt.Println(captured)
	// Output: process
}

// ExampleNewPipeline shows a two-stage sequential pipeline with a timeout.
// Each stage reads from and writes to the shared StageContext.
func ExampleNewPipeline() {
	research := exampleExec{key: "research", val: "Go interfaces enable abstraction."}
	write := exampleExec{key: "write", val: "Article ready."}

	p := orchestration.NewPipeline(
		orchestration.WithStages(
			orchestration.Stage("research", research),
			orchestration.Stage("write", write),
		),
		orchestration.WithPipelineTimeout(30*time.Second),
	)
	sc, err := p.Run(context.Background(), "explain Go interfaces")
	if err != nil {
		log.Fatal(err)
	}
	result, _ := sc.RequireOutput("write")
	fmt.Println(result)
	// Output: Article ready.
}

// ExampleNewParallelGroup shows multiple stages running concurrently, each writing
// to a different output key so they never collide.
func ExampleNewParallelGroup() {
	code := exampleExec{key: "code", val: "implementation done"}
	tests := exampleExec{key: "tests", val: "test suite done"}

	group := orchestration.NewParallelGroup(
		orchestration.WithParallelStages(
			orchestration.Stage("code", code),
			orchestration.Stage("tests", tests),
		),
		orchestration.WithMaxConcurrency(2),
	)
	sc, err := group.Run(context.Background(), "build library")
	if err != nil {
		log.Fatal(err)
	}
	c, _ := sc.RequireOutput("code")
	t, _ := sc.RequireOutput("tests")
	fmt.Println(c)
	fmt.Println(t)
	// Output:
	// implementation done
	// test suite done
}

// ExampleNewGraph shows a single-node graph that terminates immediately by
// returning "" from the NodeFunc.
func ExampleNewGraph() {
	step := func(_ context.Context, sc *orchestration.StageContext) (string, error) {
		sc.SetOutput("result", "graph finished")
		return "", nil // "" terminates the graph
	}
	g, err := orchestration.NewGraph(
		orchestration.WithStart("step"),
		orchestration.WithNode("step", step),
	)
	if err != nil {
		log.Fatal(err)
	}
	sc, err := g.Run(context.Background(), "run graph")
	if err != nil {
		log.Fatal(err)
	}
	v, _ := sc.RequireOutput("result")
	fmt.Println(v)
	// Output: graph finished
}

// ExampleGraph_Mermaid shows the flowchart string produced from declared edges.
// Node names must be alphanumeric for valid Mermaid syntax.
func ExampleGraph_Mermaid() {
	noop := func(_ context.Context, _ *orchestration.StageContext) (string, error) { return "", nil }
	g, _ := orchestration.NewGraph(
		orchestration.WithStart("step"),
		orchestration.WithNode("step", noop, orchestration.WithToNodes("")),
	)
	fmt.Print(g.Mermaid())
	// Output:
	// graph TD
	//     step --> END
}

// ExampleExecutorNode shows converting an Executor into a NodeFunc with a fixed
// next-node destination.
func ExampleExecutorNode() {
	e := exampleExec{key: "result", val: "done"}
	nodeFunc := orchestration.ExecutorNode(e, "next_node")

	sc := orchestration.NewStageContext("goal")
	next, err := nodeFunc(context.Background(), sc)
	if err != nil {
		log.Fatal(err)
	}
	v, _ := sc.RequireOutput("result")
	fmt.Println(next)
	fmt.Println(v)
	// Output:
	// next_node
	// done
}

// ExampleHumanApprovalNode shows a graph that pauses for human approval before
// continuing. The graph runs in a goroutine; the main goroutine handles the channels.
func ExampleHumanApprovalNode() {
	requestCh := make(chan orchestration.ApprovalRequest, 1)
	responseCh := make(chan orchestration.ApprovalResponse, 1)

	generate := func(_ context.Context, sc *orchestration.StageContext) (string, error) {
		sc.SetOutput("draft", "initial draft")
		return "approve", nil
	}
	finalize := func(_ context.Context, _ *orchestration.StageContext) (string, error) {
		return "", nil
	}

	g, _ := orchestration.NewGraph(
		orchestration.WithStart("generate"),
		orchestration.WithNode("generate", generate),
		orchestration.WithNode("approve", orchestration.HumanApprovalNode(
			requestCh, responseCh, "finalize", "generate",
		)),
		orchestration.WithNode("finalize", finalize),
	)

	done := make(chan *orchestration.StageContext)
	go func() {
		sc, _ := g.Run(context.Background(), "write a report")
		done <- sc
	}()

	req := <-requestCh
	_ = req // present req.Outputs and req.Artifacts to the user in a real app
	responseCh <- orchestration.ApprovalResponse{Approved: true}
	sc := <-done

	draft, _ := sc.RequireOutput("draft")
	fmt.Println(draft)
	// Output: initial draft
}

// ExampleRetryMiddleware shows a flaky node retried up to MaxAttempts times
// with exponential backoff.
func ExampleRetryMiddleware() {
	attempts := 0
	flaky := func(_ context.Context, sc *orchestration.StageContext) (string, error) {
		attempts++
		if attempts < 3 {
			return "", fmt.Errorf("transient failure")
		}
		sc.SetOutput("result", "success after retries")
		return "", nil
	}
	g, _ := orchestration.NewGraph(
		orchestration.WithStart("call"),
		orchestration.WithNode("call", flaky,
			orchestration.WithNodeMiddleware(orchestration.RetryMiddleware(goagent.RetryPolicy{
				MaxAttempts:  3,
				InitialDelay: time.Millisecond,
				MaxDelay:     time.Millisecond,
			})),
		),
	)
	sc, err := g.Run(context.Background(), "retry demo")
	if err != nil {
		log.Fatal(err)
	}
	v, _ := sc.RequireOutput("result")
	fmt.Println(v)
	fmt.Println(attempts)
	// Output:
	// success after retries
	// 3
}

// ExampleRecoverMiddleware shows a panicking node converted to an error instead
// of crashing the graph.
func ExampleRecoverMiddleware() {
	panicky := func(_ context.Context, _ *orchestration.StageContext) (string, error) {
		panic("unexpected failure")
	}
	g, _ := orchestration.NewGraph(
		orchestration.WithStart("risky"),
		orchestration.WithNode("risky", panicky,
			orchestration.WithNodeMiddleware(orchestration.RecoverMiddleware),
		),
	)
	_, err := g.Run(context.Background(), "recover demo")
	fmt.Println(err != nil)
	// Output: true
}

// ExampleTimeoutMiddleware shows a per-node timeout that cancels a slow NodeFunc.
func ExampleTimeoutMiddleware() {
	blocked := func(ctx context.Context, _ *orchestration.StageContext) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(10 * time.Second):
			return "", nil
		}
	}
	g, _ := orchestration.NewGraph(
		orchestration.WithStart("slow"),
		orchestration.WithNode("slow", blocked,
			orchestration.WithNodeMiddleware(orchestration.TimeoutMiddleware(time.Millisecond)),
		),
	)
	_, err := g.Run(context.Background(), "timeout demo")
	fmt.Println(err != nil)
	// Output: true
}

// ExampleMergeOrchestrationHooks shows composing two hook sets so both fire on
// every stage start. The merged ctx flows from h1 into h2 in declaration order.
func ExampleMergeOrchestrationHooks() {
	var events []string

	h1 := orchestration.OrchestrationHooks{
		OnStageStart: func(ctx context.Context, name string) context.Context {
			events = append(events, "h1:"+name)
			return ctx
		},
	}
	h2 := orchestration.OrchestrationHooks{
		OnStageStart: func(ctx context.Context, name string) context.Context {
			events = append(events, "h2:"+name)
			return ctx
		},
	}

	merged := orchestration.MergeOrchestrationHooks(h1, h2)
	e := exampleExec{key: "result", val: "ok"}
	p := orchestration.NewPipeline(
		orchestration.WithStages(orchestration.Stage("work", e)),
		orchestration.WithPipelineHooks(merged),
	)
	_, _ = p.Run(context.Background(), "goal")
	fmt.Println(events[0])
	fmt.Println(events[1])
	// Output:
	// h1:work
	// h2:work
}

// ExampleNewAgentAdapter shows wrapping a *goagent.Agent as an Executor with an
// explicit output key and a PromptBuilder.
func ExampleNewAgentAdapter() {
	agent, err := goagent.New(goagent.WithProvider(testutil.NewMockProvider(
		goagent.CompletionResponse{
			Message:    goagent.AssistantMessage("research complete"),
			StopReason: goagent.StopReasonEndTurn,
		},
	)))
	if err != nil {
		log.Fatal(err)
	}

	adapter := orchestration.NewAgentAdapter(agent, "research", orchestration.GoalOnly)
	p := orchestration.NewPipeline(
		orchestration.WithStages(orchestration.Stage("research", adapter)),
	)
	sc, err := p.Run(context.Background(), "research quantum computing")
	if err != nil {
		log.Fatal(err)
	}
	result, _ := sc.RequireOutput("research")
	fmt.Println(result)
	// Output: research complete
}

// ExampleAgentStage shows the syntactic sugar that uses the Stage name as the
// output key automatically, eliminating the need to pass it twice.
func ExampleAgentStage() {
	agent, err := goagent.New(goagent.WithProvider(testutil.NewMockProvider(
		goagent.CompletionResponse{
			Message:    goagent.AssistantMessage("article written"),
			StopReason: goagent.StopReasonEndTurn,
		},
	)))
	if err != nil {
		log.Fatal(err)
	}

	// The stage name "write" is used automatically as the output key.
	p := orchestration.NewPipeline(
		orchestration.WithStages(
			orchestration.Stage("write", orchestration.AgentStage(agent, orchestration.GoalOnly)),
		),
	)
	sc, err := p.Run(context.Background(), "write a blog post")
	if err != nil {
		log.Fatal(err)
	}
	result, _ := sc.RequireOutput("write")
	fmt.Println(result)
	// Output: article written
}

// ExampleNewSupervisor shows constructing a supervisor that coordinates worker agents
// via tool calls. Running requires a real provider and live workers.
// No Output: because the supervisor's LLM behaviour is non-deterministic.
func ExampleNewSupervisor() {
	researcher, _ := goagent.New(goagent.WithProvider(testutil.NewMockProvider()))
	coder, _ := goagent.New(goagent.WithProvider(testutil.NewMockProvider()))

	supervisor, err := orchestration.NewSupervisor(
		"result",
		nil, // uses GoalOnly
		[]orchestration.Worker{
			{
				Name:             "researcher",
				Description:      "Researches technical topics in depth.",
				InputDescription: "The topic to research. Include technology and version context.",
				Agent:            researcher,
			},
			{
				Name:             "coder",
				Description:      "Writes idiomatic Go code.",
				InputDescription: "Goal and research context. Include design constraints.",
				Agent:            coder,
			},
		},
		goagent.WithProvider(testutil.NewMockProvider()),
	)
	if err != nil {
		log.Fatal(err)
	}
	_ = supervisor
}

// ExampleNewAgentTool shows wrapping a worker agent as a goagent.Tool so that
// a supervisor agent can invoke it via tool calls.
func ExampleNewAgentTool() {
	worker, _ := goagent.New(goagent.WithProvider(testutil.NewMockProvider()))

	tool := orchestration.NewAgentTool(
		"researcher",
		"Researches technical topics in depth.",
		"The topic and version context to research.",
		worker,
	)
	def := tool.Definition()
	fmt.Println(def.Name)
	fmt.Println(def.Description)
	// Output:
	// researcher
	// Researches technical topics in depth.
}
