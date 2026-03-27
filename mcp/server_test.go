package mcp_test

import (
	"context"
	"testing"

	"github.com/Germanblandin1/goagent/mcp"
)

func TestServer_AddTool_ValidSchema(t *testing.T) {
	t.Parallel()

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string"},
		},
		"required": []string{"query"},
	}
	s := mcp.NewServer("test-server", "0.0.1")
	err := s.AddTool("search", "Searches files", schema,
		func(_ context.Context, _ map[string]any) (string, error) { return "ok", nil },
	)
	if err != nil {
		t.Fatalf("AddTool: unexpected error: %v", err)
	}

	client := mcp.NewTestClient(t, s)
	tools := client.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool after AddTool, got %d", len(tools))
	}
	if tools[0].Definition().Name != "search" {
		t.Errorf("tool name = %q, want %q", tools[0].Definition().Name, "search")
	}
	if tools[0].Definition().Parameters == nil {
		t.Error("expected Parameters to be non-nil for registered schema")
	}
}

func TestServer_AddTool_InvalidSchema(t *testing.T) {
	t.Parallel()

	s := mcp.NewServer("test-server", "0.0.1")
	// Channels cannot be marshalled to JSON.
	err := s.AddTool("bad", "Bad schema", make(chan int), nil)
	if err == nil {
		t.Error("expected error for invalid schema, got nil")
	}
}

func TestServer_MustAddTool_Panics_OnInvalidSchema(t *testing.T) {
	t.Parallel()

	s := mcp.NewServer("test-server", "0.0.1")
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for invalid schema, got none")
		}
	}()
	// Channels cannot be marshalled to JSON — MustAddTool should panic.
	s.MustAddTool("bad", "Bad schema", make(chan int), nil)
}

func TestServer_AddTool_NilSchema_NoError(t *testing.T) {
	t.Parallel()

	s := mcp.NewServer("test-server", "0.0.1")
	err := s.AddTool("noop", "No arguments", nil,
		func(_ context.Context, _ map[string]any) (string, error) { return "", nil },
	)
	if err != nil {
		t.Errorf("AddTool with nil schema: unexpected error: %v", err)
	}
}
