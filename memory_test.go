package goagent_test

import (
	"testing"

	"github.com/Germanblandin1/goagent"
)

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
