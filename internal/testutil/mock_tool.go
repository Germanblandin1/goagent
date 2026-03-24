package testutil

import (
	"context"

	"github.com/Germanblandin1/goagent"
)

// MockTool is a Tool implementation for use in tests.
type MockTool struct {
	name        string
	description string
	result      []goagent.ContentBlock
	err         error
}

// NewMockTool creates a tool that always returns result as a text block with no error.
func NewMockTool(name, description, result string) *MockTool {
	return &MockTool{
		name:        name,
		description: description,
		result:      []goagent.ContentBlock{goagent.TextBlock(result)},
	}
}

// NewMockToolWithError creates a tool that always returns the given error.
func NewMockToolWithError(name, description string, err error) *MockTool {
	return &MockTool{name: name, description: description, err: err}
}

func (m *MockTool) Definition() goagent.ToolDefinition {
	return goagent.ToolDefinition{
		Name:        m.name,
		Description: m.description,
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}
}

func (m *MockTool) Execute(_ context.Context, _ map[string]any) ([]goagent.ContentBlock, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}
