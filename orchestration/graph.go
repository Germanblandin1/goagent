package orchestration

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// NodeFunc is the function that executes a graph node.
// It reads from and writes to the StageContext, then returns the name of
// the next node to execute. Returning "" terminates the graph.
//
// A NodeFunc can execute any number of Executors internally — including
// a dynamically constructed ParallelGroup — before returning the next node.
// The Graph treats the node as a black box.
//
// Example — conditional parallelism decided at runtime:
//
//	func analyzeNode(ctx context.Context, sc *StageContext) (string, error) {
//	    needsResearch, _ := GetArtifact[bool](sc, "needs_research")
//
//	    if needsResearch {
//	        group := orchestration.NewParallelGroup(
//	            orchestration.Stage("research", researcherAdapter),
//	            orchestration.Stage("code",     coderAdapter),
//	        )
//	        if err := group.RunWithContext(ctx, sc); err != nil {
//	            return "", err
//	        }
//	        return "synthesize", nil
//	    }
//
//	    if err := coderAdapter.RunWithContext(ctx, sc); err != nil {
//	        return "", err
//	    }
//	    return "review", nil
//	}
type NodeFunc func(ctx context.Context, sc *StageContext) (next string, err error)

// nodeEntry holds a NodeFunc together with its per-node options.
type nodeEntry struct {
	fn        NodeFunc
	maxCycles int      // 0 = no individual limit
	toNodes   []string // declared edges — optional, used by Mermaid()
}

// NodeOption configures a single node registered via WithNode.
type NodeOption func(*nodeEntry)

// WithMaxCycles sets the maximum number of times this specific node may execute
// within a single graph run. Protects against individual nodes that loop more
// than expected while allowing other nodes to run freely.
// A value of 0 (the default) means no per-node limit.
func WithMaxCycles(n int) NodeOption {
	return func(e *nodeEntry) {
		e.maxCycles = n
	}
}

// WithToNodes declares the possible destination nodes from this node.
// This is optional — the graph routes dynamically via NodeFunc return values
// regardless of whether edges are declared here.
// Used by Mermaid() to generate the flow diagram.
// Pass "" to represent the END terminal node.
func WithToNodes(names ...string) NodeOption {
	return func(e *nodeEntry) {
		e.toNodes = names
	}
}

// Graph executes a set of NodeFuncs connected by dynamic routing.
// Each node decides at runtime which node runs next by returning its name.
// Returning "" from a node terminates the graph.
//
// Graph implements Executor — it can be nested inside a Pipeline or
// ParallelGroup.
//
// Parallelism is achieved by constructing a ParallelGroup inside a NodeFunc
// and calling RunWithContext directly. The Graph does not need to know about
// parallelism — the node is the unit of encapsulation.
type Graph struct {
	nodes         map[string]nodeEntry
	start         string
	maxIterations int
	hooks         OrchestrationHooks
}

// GraphOption configures a Graph.
type GraphOption func(*Graph)

// WithGraphHooks configures observability hooks for the graph.
// Hooks are called around each node execution and around the graph itself.
// The zero value of OrchestrationHooks is safe — unset fields are no-ops.
func WithGraphHooks(h OrchestrationHooks) GraphOption {
	return func(g *Graph) {
		g.hooks = h
	}
}

// WithNode registers a node under name.
// The NodeFunc is called when the graph reaches this node.
// Optional NodeOptions (e.g. WithMaxCycles) constrain this node individually.
func WithNode(name string, fn NodeFunc, opts ...NodeOption) GraphOption {
	return func(g *Graph) {
		e := nodeEntry{fn: fn}
		for _, opt := range opts {
			opt(&e)
		}
		g.nodes[name] = e
	}
}

// WithStart sets the entry point of the graph.
// Required — NewGraph returns an error if not set.
func WithStart(name string) GraphOption {
	return func(g *Graph) {
		g.start = name
	}
}

// WithMaxIterations sets the maximum number of node executions before
// the graph returns an error. Protects against infinite loops.
// Default: 100.
func WithMaxIterations(n int) GraphOption {
	return func(g *Graph) {
		g.maxIterations = n
	}
}

// NewGraph constructs a Graph from the given options.
// Returns an error if:
//   - WithStart was not called
//   - the start node was not registered with WithNode
func NewGraph(opts ...GraphOption) (*Graph, error) {
	g := &Graph{
		nodes:         make(map[string]nodeEntry),
		maxIterations: 100,
	}
	for _, opt := range opts {
		opt(g)
	}
	if g.start == "" {
		return nil, fmt.Errorf("graph: start node not set, use WithStart")
	}
	if _, ok := g.nodes[g.start]; !ok {
		return nil, fmt.Errorf("graph: start node %q not registered", g.start)
	}
	return g, nil
}

// Run is the main entry point. Constructs a StageContext with the given goal
// and executes the graph from the start node.
// Returns the complete StageContext so the caller can inspect all outputs
// and artifacts produced during execution.
func (g *Graph) Run(ctx context.Context, goal string) (*StageContext, error) {
	sc := NewStageContext(goal)
	return sc, g.RunWithContext(ctx, sc)
}

// RunWithContext implements Executor.
// Allows nesting this Graph inside a Pipeline or ParallelGroup.
// Executes from the start node using the provided StageContext.
func (g *Graph) RunWithContext(ctx context.Context, sc *StageContext) (err error) {
	graphCtx := invokeStart(g.hooks.OnGraphStart, ctx, sc.Goal)

	defer func() {
		if fn := g.hooks.OnGraphEnd; fn != nil {
			fn(graphCtx, sc, err)
		}
	}()

	current := g.start
	iterations := 0
	cycles := make(map[string]int)

	for current != "" {
		select {
		case <-graphCtx.Done():
			return graphCtx.Err()
		default:
		}

		if iterations >= g.maxIterations {
			return fmt.Errorf("graph: exceeded max iterations (%d), possible infinite loop at node %q",
				g.maxIterations, current)
		}

		entry, ok := g.nodes[current]
		if !ok {
			return fmt.Errorf("graph: node %q not found", current)
		}

		cycles[current]++
		if entry.maxCycles > 0 && cycles[current] > entry.maxCycles {
			return fmt.Errorf("graph: node %q exceeded max cycles (%d)", current, entry.maxCycles)
		}

		nodeCtx := invokeStart(g.hooks.OnNodeEnter, graphCtx, current)

		start := time.Now()
		next, nodeErr := entry.fn(nodeCtx, sc)
		dur := time.Since(start)
		sc.appendTrace(current, dur, nodeErr)

		if fn := g.hooks.OnNodeExit; fn != nil {
			fn(nodeCtx, current, next, dur, nodeErr)
		}

		if nodeErr != nil {
			return fmt.Errorf("graph: node %q: %w", current, nodeErr)
		}

		iterations++
		current = next
	}

	return nil
}

// Mermaid returns a Mermaid flowchart string representing the graph structure.
// Only nodes registered with WithToNodes produce edges in the diagram.
// Nodes without WithToNodes appear as isolated nodes.
// "" in WithToNodes is rendered as END.
//
// Example output:
//
//	graph TD
//	    generate --> review
//	    review --> generate
//	    review --> END
//	    synthesize
func (g *Graph) Mermaid() string {
	names := make([]string, 0, len(g.nodes))
	for name := range g.nodes {
		names = append(names, name)
	}
	sort.Strings(names)

	var sb strings.Builder
	sb.WriteString("graph TD\n")

	for _, name := range names {
		entry := g.nodes[name]
		if len(entry.toNodes) == 0 {
			fmt.Fprintf(&sb, "    %s\n", name)
			continue
		}
		for _, dest := range entry.toNodes {
			if dest == "" {
				fmt.Fprintf(&sb, "    %s --> END\n", name)
			} else {
				fmt.Fprintf(&sb, "    %s --> %s\n", name, dest)
			}
		}
	}

	return sb.String()
}
