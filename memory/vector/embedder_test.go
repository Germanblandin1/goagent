package vector_test

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/internal/testutil"
	"github.com/Germanblandin1/goagent/memory/vector"
)

// ── MockEmbedder ─────────────────────────────────────────────────────────────

func TestMockEmbedder_Deterministic(t *testing.T) {
	e := &testutil.MockEmbedder{Dim: 8}
	blocks := []goagent.ContentBlock{goagent.TextBlock("hello world")}

	v1, err := e.Embed(context.Background(), blocks)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := e.Embed(context.Background(), blocks)
	if err != nil {
		t.Fatal(err)
	}
	for i := range v1 {
		if v1[i] != v2[i] {
			t.Errorf("MockEmbedder not deterministic at [%d]: %v vs %v", i, v1[i], v2[i])
		}
	}
}

func TestMockEmbedder_Dimension(t *testing.T) {
	tests := []struct {
		dim  int
		want int
	}{
		{0, 16},  // default
		{8, 8},
		{32, 32},
	}
	for _, tc := range tests {
		e := &testutil.MockEmbedder{Dim: tc.dim}
		v, err := e.Embed(context.Background(), []goagent.ContentBlock{goagent.TextBlock("test")})
		if err != nil {
			t.Fatal(err)
		}
		if len(v) != tc.want {
			t.Errorf("Dim=%d: len = %d, want %d", tc.dim, len(v), tc.want)
		}
	}
}

func TestMockEmbedder_UnitLength(t *testing.T) {
	e := &testutil.MockEmbedder{Dim: 16}
	texts := []string{"hello", "world", "test embedding normalization"}
	for _, text := range texts {
		v, err := e.Embed(context.Background(), []goagent.ContentBlock{goagent.TextBlock(text)})
		if err != nil {
			t.Fatal(err)
		}
		var sum float64
		for _, x := range v {
			sum += float64(x) * float64(x)
		}
		if math.Abs(math.Sqrt(sum)-1.0) > 1e-5 {
			t.Errorf("||Embed(%q)|| = %v, want 1.0", text, math.Sqrt(sum))
		}
	}
}

func TestMockEmbedder_DifferentTexts(t *testing.T) {
	e := &testutil.MockEmbedder{Dim: 16}
	v1, _ := e.Embed(context.Background(), []goagent.ContentBlock{goagent.TextBlock("apple")})
	v2, _ := e.Embed(context.Background(), []goagent.ContentBlock{goagent.TextBlock("orange")})

	same := true
	for i := range v1 {
		if v1[i] != v2[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("different texts produced identical vectors")
	}
}

func TestMockEmbedder_EmptyBlocks(t *testing.T) {
	e := &testutil.MockEmbedder{}
	_, err := e.Embed(context.Background(), []goagent.ContentBlock{})
	if !errors.Is(err, vector.ErrNoEmbeddeableContent) {
		t.Errorf("expected ErrNoEmbeddeableContent, got %v", err)
	}
}

func TestMockEmbedder_ImageOnlyBlocks(t *testing.T) {
	e := &testutil.MockEmbedder{}
	_, err := e.Embed(context.Background(), []goagent.ContentBlock{
		goagent.ImageBlock([]byte{0xFF}, "image/png"),
	})
	if !errors.Is(err, vector.ErrNoEmbeddeableContent) {
		t.Errorf("expected ErrNoEmbeddeableContent, got %v", err)
	}
}

// ── FallbackEmbedder ─────────────────────────────────────────────────────────

func TestFallbackEmbedder_PassesSupportedBlocks(t *testing.T) {
	primary := &testutil.MockEmbedder{Dim: 4}
	fe := vector.NewFallbackEmbedder(primary)

	blocks := []goagent.ContentBlock{
		goagent.TextBlock("hello"),
		goagent.ImageBlock([]byte{0xFF}, "image/png"),
	}
	v, err := fe.Embed(context.Background(), blocks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(v) != 4 {
		t.Errorf("vector len = %d, want 4", len(v))
	}
}

func TestFallbackEmbedder_CallsOnSkipped(t *testing.T) {
	primary := &testutil.MockEmbedder{Dim: 4}
	skipped := 0
	fe := vector.NewFallbackEmbedder(primary,
		vector.WithOnSkipped(func(_ goagent.ContentBlock) {
			skipped++
		}),
	)

	blocks := []goagent.ContentBlock{
		goagent.TextBlock("text"),
		goagent.ImageBlock([]byte{0xFF}, "image/png"),
		goagent.DocumentBlock([]byte("%PDF"), "application/pdf", "doc"),
	}
	_, err := fe.Embed(context.Background(), blocks)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 2 {
		t.Errorf("OnSkipped called %d times, want 2", skipped)
	}
}

func TestFallbackEmbedder_AllUnsupported(t *testing.T) {
	primary := &testutil.MockEmbedder{Dim: 4}
	fe := vector.NewFallbackEmbedder(primary)

	blocks := []goagent.ContentBlock{
		goagent.ImageBlock([]byte{0xFF}, "image/png"),
	}
	_, err := fe.Embed(context.Background(), blocks)
	if !errors.Is(err, vector.ErrNoEmbeddeableContent) {
		t.Errorf("expected ErrNoEmbeddeableContent, got %v", err)
	}
}
