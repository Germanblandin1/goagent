package policy_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/policy"
)

// encodePNG encodes an image of the given dimensions as a PNG byte slice.
func encodePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.White)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encodePNG: %v", err)
	}
	return buf.Bytes()
}

func TestTokenWindow_PanicsOnZero(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for maxTokens=0")
		}
	}()
	policy.NewTokenWindow(0)
}

func TestTokenWindow_Apply(t *testing.T) {
	t.Parallel()

	user := func(c string) goagent.Message { return goagent.UserMessage(c) }
	asst := func(c string) goagent.Message { return goagent.AssistantMessage(c) }
	tool := func(c string) goagent.Message { return goagent.TextMessage(goagent.RoleTool, c) }
	asstTool := func() goagent.Message {
		return goagent.Message{Role: goagent.RoleAssistant, ToolCalls: []goagent.ToolCall{{ID: "c1"}}}
	}

	// estimateTokens = len(text)/4 + 4
	// 4-char content → 1 + 4 = 5 tokens
	// empty content  → 0 + 4 = 4 tokens

	tests := []struct {
		name      string
		maxTokens int
		input     []goagent.Message
		wantLen   int
	}{
		{
			name:      "empty input",
			maxTokens: 100,
			input:     nil,
			wantLen:   0,
		},
		{
			name:      "budget fits all messages",
			maxTokens: 1000,
			input:     []goagent.Message{user("hi"), asst("hello")},
			wantLen:   2,
		},
		{
			name:      "budget fits only the last message",
			maxTokens: 5, // exactly one 4-char message (1+4=5 tokens)
			input:     []goagent.Message{user("aaaa"), user("bbbb")},
			wantLen:   1,
		},
		{
			name:      "budget too small for any message",
			maxTokens: 3, // less than minimum cost (4 tokens overhead)
			input:     []goagent.Message{user("a")},
			wantLen:   0,
		},
		{
			name:      "large message cut, tool pair intact",
			maxTokens: 20,
			// user(400 chars) = 100+4 = 104 tokens — exceeds budget alone.
			// asstTool(4) + tool(4) + asst(5) = 13 tokens ≤ 20 → all three fit.
			// start lands on asstTool (Role=assistant), adjustStart is a no-op.
			input: []goagent.Message{
				user(strings.Repeat("x", 400)),
				asstTool(),
				tool("42"),
				asst("answer"),
			},
			wantLen: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := policy.NewTokenWindow(tt.maxTokens)
			got, err := p.Apply(context.Background(), tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tt.wantLen {
				t.Errorf("len(got) = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestTokenWindow_Apply_ToolCallInvariant(t *testing.T) {
	t.Parallel()

	asstTool := goagent.Message{Role: goagent.RoleAssistant, ToolCalls: []goagent.ToolCall{{ID: "c1"}}}
	toolResult := goagent.Message{
		Role:       goagent.RoleTool,
		Content:    []goagent.ContentBlock{goagent.TextBlock("42")},
		ToolCallID: "c1",
	}
	finalAsst := goagent.AssistantMessage("answer")

	// Budget of 9 fits: toolResult(4) + finalAsst(5+4=... wait:
	// len("answer") = 6, 6/4 = 1, +4 = 5. finalAsst = 5 tokens.
	// toolResult: len("42")=2, 2/4=0, +4=4. = 4 tokens.
	// asstTool: no text content, +4 = 4 tokens.
	// With budget=9: fits finalAsst(5) and toolResult(4) = 9 tokens exactly.
	// start = index of toolResult → Role == RoleTool → adjustStart → skip → start = finalAsst.
	// Result: [finalAsst] (1 message, not orphaned).

	p := policy.NewTokenWindow(9)
	input := []goagent.Message{asstTool, toolResult, finalAsst}
	got, err := p.Apply(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The tool result was skipped because its tool_use pair would be missing.
	for _, msg := range got {
		if msg.Role == goagent.RoleTool {
			t.Error("got an orphaned RoleTool message — tool call invariant violated")
		}
	}
}

func TestTokenWindow_WithTokenizer(t *testing.T) {
	t.Parallel()

	// Custom tokenizer: every message costs exactly 10 tokens.
	fixed := policy.TokenizerFunc(func(_ goagent.Message) int { return 10 })

	p := policy.NewTokenWindow(25, policy.WithTokenizer(fixed))
	msgs := []goagent.Message{
		goagent.UserMessage("a"),
		goagent.AssistantMessage("b"),
		goagent.UserMessage("c"),
	}
	// Budget 25 / 10 per message = 2 messages fit.
	got, err := p.Apply(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len(got) = %d, want 2", len(got))
	}
	if got[0].TextContent() != "b" {
		t.Errorf("got[0].TextContent() = %q, want %q", got[0].TextContent(), "b")
	}
}

// TestTokenWindow_ImageTokenHeuristic verifies the Anthropic token formula
// ceil(w/64)×ceil(h/64)×170 for known image dimensions.
func TestTokenWindow_ImageTokenHeuristic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		w, h       int
		wantTokens int // image tokens only (no per-message overhead)
	}{
		// ceil(1/64)=1, ceil(1/64)=1 → 1×1×170 = 170
		{"1x1", 1, 1, 170},
		// ceil(64/64)=1, ceil(64/64)=1 → 1×1×170 = 170
		{"64x64", 64, 64, 170},
		// ceil(65/64)=2, ceil(64/64)=1 → 2×1×170 = 340
		{"65x64", 65, 64, 340},
		// ceil(128/64)=2, ceil(128/64)=2 → 2×2×170 = 680
		{"128x128", 128, 128, 680},
		// ceil(1000/64)=16, ceil(1000/64)=16 → 16×16×170 = 43520
		// (previous fixed heuristic was 1600 — 27× underestimate)
		{"1000x1000", 1000, 1000, 43520},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			img := encodePNG(t, tt.w, tt.h)
			msg := goagent.Message{
				Role: goagent.RoleUser,
				Content: []goagent.ContentBlock{
					goagent.ImageBlock(img, "image/png"),
				},
			}
			// Budget = wantTokens + 4 overhead → exactly one message fits.
			budget := tt.wantTokens + 4
			p := policy.NewTokenWindow(budget)
			got, err := p.Apply(context.Background(), []goagent.Message{msg})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != 1 {
				t.Errorf("budget=%d: expected 1 message to fit, got %d (image tokens likely wrong)", budget, len(got))
			}

			// One token less → message must not fit.
			pTight := policy.NewTokenWindow(budget - 1)
			gotTight, err := pTight.Apply(context.Background(), []goagent.Message{msg})
			if err != nil {
				t.Fatalf("unexpected error (tight): %v", err)
			}
			if len(gotTight) != 0 {
				t.Errorf("budget=%d: expected 0 messages, got %d", budget-1, len(gotTight))
			}
		})
	}
}

// TestTokenWindow_ImageTokenFallback verifies that corrupt or unsupported image
// data (e.g. WebP) falls back to 1600 tokens.
func TestTokenWindow_ImageTokenFallback(t *testing.T) {
	t.Parallel()

	msg := goagent.Message{
		Role: goagent.RoleUser,
		Content: []goagent.ContentBlock{
			goagent.ImageBlock([]byte("not-a-real-image"), "image/webp"),
		},
	}
	// 2500 (fallback) + 4 overhead = 2504 → fits; 2503 → doesn't fit.
	p := policy.NewTokenWindow(2504)
	got, err := p.Apply(context.Background(), []goagent.Message{msg})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 message with fallback budget, got %d", len(got))
	}

	pTight := policy.NewTokenWindow(2503)
	gotTight, _ := pTight.Apply(context.Background(), []goagent.Message{msg})
	if len(gotTight) != 0 {
		t.Errorf("expected 0 messages below fallback budget, got %d", len(gotTight))
	}
}

func TestTokenWindow_Apply_DefensiveCopy(t *testing.T) {
	t.Parallel()
	p := policy.NewTokenWindow(1000)
	input := []goagent.Message{goagent.UserMessage("original")}
	got, _ := p.Apply(context.Background(), input)
	got[0].Content = []goagent.ContentBlock{goagent.TextBlock("mutated")}
	if input[0].TextContent() != "original" {
		t.Error("Apply modified the input slice")
	}
}
