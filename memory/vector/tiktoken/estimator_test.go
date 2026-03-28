package tiktoken_test

import (
	"context"
	"testing"

	vektiktoken "github.com/Germanblandin1/goagent/memory/vector/tiktoken"
)

func TestNewEstimator_UnknownModel(t *testing.T) {
	_, err := vektiktoken.NewEstimator("not-a-real-model-xyz")
	if err == nil {
		t.Fatal("expected error for unknown model, got nil")
	}
}

func TestEstimator_Measure_KnownOutput(t *testing.T) {
	// "hello world" tokenizes to exactly 2 tokens with cl100k_base (gpt-4o).
	est, err := vektiktoken.NewEstimator("gpt-4o")
	if err != nil {
		t.Fatalf("NewEstimator: %v", err)
	}

	ctx := context.Background()
	tests := []struct {
		text string
		want int
	}{
		{"hello world", 2},
		{"", 0},
		{"tiktoken", 3}, // "tik" + "tok" + "en"
	}
	for _, tc := range tests {
		got := est.Measure(ctx, tc.text)
		if got != tc.want {
			t.Errorf("Measure(%q) = %d, want %d", tc.text, got, tc.want)
		}
	}
}

func TestEstimator_Measure_Positive(t *testing.T) {
	est, err := vektiktoken.NewEstimator("gpt-4o")
	if err != nil {
		t.Fatalf("NewEstimator: %v", err)
	}
	ctx := context.Background()
	texts := []string{
		"The quick brown fox",
		"Una frase en español con acentos: café, niño, corazón.",
		"日本語のテキスト",
	}
	for _, text := range texts {
		got := est.Measure(ctx, text)
		if got <= 0 {
			t.Errorf("Measure(%q) = %d, want > 0", text, got)
		}
	}
}
