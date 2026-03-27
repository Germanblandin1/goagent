package goagent_test

import (
	"testing"

	"github.com/Germanblandin1/goagent"
)

func TestMinLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		n        int
		prompt   string
		response string
		wantNil  bool
	}{
		{
			name:     "combined length exceeds n — store pair",
			n:        5,
			prompt:   "hello",
			response: "world!",
			wantNil:  false,
		},
		{
			name:     "combined length equals n — discard",
			n:        10,
			prompt:   "hello",
			response: "world",
			wantNil:  true,
		},
		{
			name:     "combined length below n — discard",
			n:        20,
			prompt:   "hi",
			response: "ok",
			wantNil:  true,
		},
		{
			name:     "n=0 with non-empty messages — always store",
			n:        0,
			prompt:   "a",
			response: "b",
			wantNil:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			policy := goagent.MinLength(tt.n)
			p := goagent.UserMessage(tt.prompt)
			r := goagent.AssistantMessage(tt.response)
			got := policy(p, r)
			if tt.wantNil && got != nil {
				t.Errorf("expected nil (discard), got %v", got)
			}
			if !tt.wantNil && got == nil {
				t.Error("expected non-nil (store), got nil")
			}
			if got != nil && len(got) != 2 {
				t.Errorf("expected 2 messages, got %d", len(got))
			}
		})
	}
}

func TestTextFrom(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		blocks []goagent.ContentBlock
		want   string
	}{
		{
			name:   "nil blocks",
			blocks: nil,
			want:   "",
		},
		{
			name:   "empty slice",
			blocks: []goagent.ContentBlock{},
			want:   "",
		},
		{
			name: "single text block",
			blocks: []goagent.ContentBlock{
				goagent.TextBlock("hello"),
			},
			want: "hello",
		},
		{
			name: "multiple text blocks",
			blocks: []goagent.ContentBlock{
				goagent.TextBlock("hello"),
				goagent.TextBlock("world"),
			},
			want: "hello world",
		},
		{
			name: "only images",
			blocks: []goagent.ContentBlock{
				goagent.ImageBlock([]byte{0xFF}, "image/png"),
			},
			want: "",
		},
		{
			name: "mixed text and image",
			blocks: []goagent.ContentBlock{
				goagent.TextBlock("hello"),
				goagent.ImageBlock([]byte{0xFF}, "image/png"),
				goagent.TextBlock("world"),
			},
			want: "hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := goagent.TextFrom(tt.blocks); got != tt.want {
				t.Errorf("TextFrom() = %q, want %q", got, tt.want)
			}
		})
	}
}
