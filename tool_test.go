package goagent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Germanblandin1/goagent"
)

func TestToolBlocksFunc_Definition(t *testing.T) {
	t.Parallel()

	tool := goagent.ToolBlocksFunc("screenshot", "captures the screen", goagent.SchemaFrom(struct{}{}),
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

func TestToolFunc_Definition(t *testing.T) {
	t.Parallel()

	tool := goagent.ToolFunc("greet", "says hello", goagent.SchemaFrom(struct{}{}),
		func(_ context.Context, _ map[string]any) (string, error) { return "", nil },
	)

	def := tool.Definition()
	if def.Name != "greet" {
		t.Errorf("Name = %q, want %q", def.Name, "greet")
	}
	if def.Description != "says hello" {
		t.Errorf("Description = %q, want %q", def.Description, "says hello")
	}
	if def.Parameters == nil {
		t.Error("Parameters should not be nil")
	}
}

func TestToolFunc_Execute_WrapsStringInTextBlock(t *testing.T) {
	t.Parallel()

	tool := goagent.ToolFunc("echo", "echoes input", nil,
		func(_ context.Context, args map[string]any) (string, error) {
			return args["text"].(string), nil
		},
	)

	blocks, err := tool.Execute(context.Background(), map[string]any{"text": "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Type != goagent.ContentText {
		t.Errorf("block type = %v, want ContentText", blocks[0].Type)
	}
	if blocks[0].Text != "hello" {
		t.Errorf("block text = %q, want %q", blocks[0].Text, "hello")
	}
}

func TestToolFunc_Execute_PropagatesError(t *testing.T) {
	t.Parallel()

	want := errors.New("fn error")
	tool := goagent.ToolFunc("fail", "always fails", nil,
		func(_ context.Context, _ map[string]any) (string, error) { return "", want },
	)

	_, err := tool.Execute(context.Background(), nil)
	if !errors.Is(err, want) {
		t.Errorf("want wrapped fn error, got: %v", err)
	}
}
