package goagent_test

import (
	"context"
	"testing"

	"github.com/Germanblandin1/goagent"
)

// FuzzToolFunc verifies that ToolFunc.Execute never panics when the underlying
// function safely handles its args. The dispatcher wraps panics in
// panicRecoveryMiddleware, but the tool itself should not leak panics.
//
// Run with: go test -fuzz=FuzzToolFunc -fuzztime=60s .
func FuzzToolFunc(f *testing.F) {
	// Seed corpus: various arg key/value combinations.
	f.Add("query", "find me")
	f.Add("", "")
	f.Add("key", `{"nested": "value"}`)
	f.Add("n", "42")

	f.Fuzz(func(t *testing.T, key, value string) {
		args := map[string]any{key: value}

		tool := goagent.ToolFunc("search", "searches for things", nil,
			func(_ context.Context, a map[string]any) (string, error) {
				// Safe extraction — never type-assert without checking.
				v, _ := a[key].(string)
				return "result: " + v, nil
			},
		)

		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Execute panicked with key=%q value=%q: %v", key, value, r)
			}
		}()

		blocks, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(blocks) == 0 {
			t.Error("expected at least one block")
		}
	})
}

// FuzzToolBlocksFunc verifies that ToolBlocksFunc.Execute never panics.
//
// Run with: go test -fuzz=FuzzToolBlocksFunc -fuzztime=60s .
func FuzzToolBlocksFunc(f *testing.F) {
	f.Add("input", "hello world")
	f.Add("", "")

	f.Fuzz(func(t *testing.T, key, value string) {
		tool := goagent.ToolBlocksFunc("multi", "returns blocks", nil,
			func(_ context.Context, a map[string]any) ([]goagent.ContentBlock, error) {
				v, _ := a[key].(string)
				return []goagent.ContentBlock{goagent.TextBlock(v)}, nil
			},
		)

		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Execute panicked with key=%q value=%q: %v", key, value, r)
			}
		}()

		_, _ = tool.Execute(context.Background(), map[string]any{key: value})
	})
}
