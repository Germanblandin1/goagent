package orchestration_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/internal/testutil"
	"github.com/Germanblandin1/goagent/orchestration"
)

// mockAgentRunner implements agentRunner for tests.
// Returns a fixed response or err.
type mockAgentRunner struct {
	response  string
	err       error
	lastInput string
}

func (m *mockAgentRunner) Run(_ context.Context, prompt string) (string, error) {
	m.lastInput = prompt
	return m.response, m.err
}

func (m *mockAgentRunner) RunBlocks(_ context.Context, _ ...goagent.ContentBlock) (string, error) {
	return m.response, m.err
}

// --- AgentTool tests ---

func TestAgentTool_Definition_hasCorrectSchema(t *testing.T) {
	tool := orchestration.NewAgentTool(
		"researcher",
		"Researches topics",
		"The topic to research",
		&mockAgentRunner{},
	)

	def := tool.Definition()

	if def.Name != "researcher" {
		t.Errorf("Name: got %q, want %q", def.Name, "researcher")
	}
	if def.Description != "Researches topics" {
		t.Errorf("Description: got %q", def.Description)
	}

	params := def.Parameters
	if params["type"] != "object" {
		t.Errorf("Parameters[type]: got %v, want %q", params["type"], "object")
	}

	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatal("Parameters[properties] missing or wrong type")
	}
	inputProp, ok := props["input"].(map[string]any)
	if !ok {
		t.Fatal("properties[input] missing or wrong type")
	}
	if inputProp["type"] != "string" {
		t.Errorf("input.type: got %v, want %q", inputProp["type"], "string")
	}
	if inputProp["description"] != "The topic to research" {
		t.Errorf("input.description: got %v", inputProp["description"])
	}

	required, ok := params["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "input" {
		t.Errorf("required: got %v, want [input]", params["required"])
	}
}

func TestAgentTool_Execute_callsAgentWithInput(t *testing.T) {
	worker := &mockAgentRunner{response: "investigación sobre embeddings"}
	tool := orchestration.NewAgentTool("researcher", "desc", "input desc", worker)

	blocks, err := tool.Execute(context.Background(), map[string]any{
		"input": "investigá embeddings de Voyage AI",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if worker.lastInput != "investigá embeddings de Voyage AI" {
		t.Errorf("worker received wrong input: %q", worker.lastInput)
	}
}

func TestAgentTool_Execute_outputIsTextBlock(t *testing.T) {
	worker := &mockAgentRunner{response: "resultado del worker"}
	tool := orchestration.NewAgentTool("worker", "desc", "input desc", worker)

	blocks, err := tool.Execute(context.Background(), map[string]any{"input": "hacé algo"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Text != "resultado del worker" {
		t.Errorf("block text: got %q, want %q", blocks[0].Text, "resultado del worker")
	}
}

func TestAgentTool_Execute_missingInput_returnsError(t *testing.T) {
	tool := orchestration.NewAgentTool("researcher", "desc", "input desc", &mockAgentRunner{})

	_, err := tool.Execute(context.Background(), map[string]any{})

	if err == nil {
		t.Fatal("expected error for missing input, got nil")
	}
	if !strings.Contains(err.Error(), "researcher") {
		t.Errorf("error should mention tool name, got: %v", err)
	}
}

func TestAgentTool_Execute_emptyInput_returnsError(t *testing.T) {
	tool := orchestration.NewAgentTool("researcher", "desc", "input desc", &mockAgentRunner{})

	_, err := tool.Execute(context.Background(), map[string]any{"input": ""})

	if err == nil {
		t.Fatal("expected error for empty input, got nil")
	}
}

func TestAgentTool_Execute_workerError_wrapsWithToolName(t *testing.T) {
	errWorker := errors.New("worker exploded")
	tool := orchestration.NewAgentTool(
		"coder", "desc", "input desc",
		&mockAgentRunner{err: errWorker},
	)

	_, err := tool.Execute(context.Background(), map[string]any{"input": "escribí algo"})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, errWorker) {
		t.Errorf("expected errWorker in chain, got: %v", err)
	}
	if !strings.Contains(err.Error(), "coder") {
		t.Errorf("error should mention tool name, got: %v", err)
	}
}

// --- NewSupervisor tests ---

func TestNewSupervisor_noWorkers_returnsError(t *testing.T) {
	_, err := orchestration.NewSupervisor("result", nil, nil)

	if err == nil {
		t.Fatal("expected error for empty workers, got nil")
	}
}

func TestNewSupervisor_emptyWorkers_returnsError(t *testing.T) {
	_, err := orchestration.NewSupervisor("result", nil, []orchestration.Worker{})

	if err == nil {
		t.Fatal("expected error for empty workers, got nil")
	}
}

// TestNewSupervisor_callsWorkerAndStoresOutput is an integration test that
// wires a MockProvider with two scripted responses:
//  1. a tool call that delegates to "researcher"
//  2. a final answer that synthesizes the result
//
// It verifies the full flow: supervisor agent → AgentTool → worker → output.
func TestNewSupervisor_callsWorkerAndStoresOutput(t *testing.T) {
	worker := &mockAgentRunner{response: "detailed research result"}

	provider := testutil.NewMockProvider(
		// iteration 1: supervisor calls the "researcher" tool
		goagent.CompletionResponse{
			Message: goagent.Message{
				Role: goagent.RoleAssistant,
				ToolCalls: []goagent.ToolCall{
					{ID: "tc1", Name: "researcher", Arguments: map[string]any{"input": "research this topic"}},
				},
			},
			StopReason: goagent.StopReasonToolUse,
		},
		// iteration 2: supervisor synthesizes the final answer
		goagent.CompletionResponse{
			Message:    goagent.AssistantMessage("final synthesis"),
			StopReason: goagent.StopReasonEndTurn,
		},
	)

	supervisor, err := orchestration.NewSupervisor(
		"result",
		nil,
		[]orchestration.Worker{
			{
				Name:             "researcher",
				Description:      "Researches topics",
				InputDescription: "The topic to research",
				Agent:            worker,
			},
		},
		goagent.WithProvider(provider),
		goagent.WithModel("test-model"),
	)
	if err != nil {
		t.Fatalf("NewSupervisor: unexpected error: %v", err)
	}

	sc := orchestration.NewStageContext("research this topic")
	if err := supervisor.RunWithContext(context.Background(), sc); err != nil {
		t.Fatalf("RunWithContext: unexpected error: %v", err)
	}

	got, ok := sc.Output("result")
	if !ok {
		t.Fatal("output key \"result\" not set in StageContext")
	}
	if got != "final synthesis" {
		t.Errorf("output: got %q, want %q", got, "final synthesis")
	}
	if worker.lastInput != "research this topic" {
		t.Errorf("worker received wrong input: %q", worker.lastInput)
	}
}
