package goagent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// dispatcher executes tool calls, running them in parallel via goroutines.
// Each dispatcher is owned by a single Run call and is not shared.
type dispatcher struct {
	tools  map[string]Tool
	logger *slog.Logger
}

// newDispatcher builds a dispatcher indexed by tool name.
func newDispatcher(tools []Tool, logger *slog.Logger) *dispatcher {
	m := make(map[string]Tool, len(tools))
	for _, t := range tools {
		m[t.Definition().Name] = t
	}
	return &dispatcher{tools: m, logger: logger}
}

// dispatch executes all tool calls in parallel.
// Each goroutine writes to its own index in results, so no mutex is needed.
// A missing or failing tool is recorded as an error result and does not abort
// the remaining calls.
func (d *dispatcher) dispatch(ctx context.Context, calls []ToolCall) []ToolResult {
	results := make([]ToolResult, len(calls))

	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Add(1)
		go func(idx int, tc ToolCall) {
			defer wg.Done()
			results[idx] = d.execute(ctx, tc)
		}(i, call)
	}
	wg.Wait()

	return results
}

// execute runs a single tool call and returns its result.
func (d *dispatcher) execute(ctx context.Context, tc ToolCall) ToolResult {
	t, ok := d.tools[tc.Name]
	if !ok {
		d.logger.Debug("tool not found", "tool", tc.Name)
		return ToolResult{
			ToolCallID: tc.ID,
			Name:       tc.Name,
			Err:        fmt.Errorf("%w: %s", ErrToolNotFound, tc.Name),
		}
	}

	content, err := t.Execute(ctx, tc.Arguments)
	if err != nil {
		d.logger.Debug("tool execution failed", "tool", tc.Name, "error", err)
		return ToolResult{
			ToolCallID: tc.ID,
			Name:       tc.Name,
			Err:        &ToolExecutionError{ToolName: tc.Name, Args: tc.Arguments, Cause: err},
		}
	}

	d.logger.Debug("tool executed", "tool", tc.Name)
	return ToolResult{
		ToolCallID: tc.ID,
		Name:       tc.Name,
		Content:    content,
	}
}
