package goagent_test

import (
	"context"
	"testing"

	"github.com/Germanblandin1/goagent"
)

func TestToolBlocksFunc_Definition(t *testing.T) {
	t.Parallel()

	params := map[string]any{"type": "object"}
	tool := goagent.ToolBlocksFunc("screenshot", "captures the screen", params,
		func(_ context.Context, _ map[string]any) ([]goagent.ContentBlock, error) {
			return nil, nil
		},
	)

	def := tool.Definition()
	if def.Name != "screenshot" {
		t.Errorf("Name = %q, want %q", def.Name, "screenshot")
	}
	if def.Description != "captures the screen" {
		t.Errorf("Description = %q, want %q", def.Description, "captures the screen")
	}
	if def.Parameters == nil {
		t.Error("Parameters should not be nil")
	}
}

func TestToolBlocksFunc_Execute(t *testing.T) {
	t.Parallel()

	want := []goagent.ContentBlock{
		goagent.TextBlock("result"),
		goagent.ImageBlock([]byte{0xFF}, "image/png"),
	}

	tool := goagent.ToolBlocksFunc("multi", "multimodal tool", nil,
		func(_ context.Context, args map[string]any) ([]goagent.ContentBlock, error) {
			return want, nil
		},
	)

	got, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d blocks, want %d", len(got), len(want))
	}
	if got[0].Type != goagent.ContentText || got[0].Text != "result" {
		t.Errorf("block[0] = %+v, want text 'result'", got[0])
	}
	if got[1].Type != goagent.ContentImage {
		t.Errorf("block[1].Type = %q, want %q", got[1].Type, goagent.ContentImage)
	}
}
