package goagent_test

import (
	"context"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/internal/testutil"
)

// buildReqFromAgent creates an Agent with the given options, runs it against a
// MockProvider, and returns the CompletionRequest the provider received.
func buildReqFromAgent(t *testing.T, opts ...goagent.Option) goagent.CompletionRequest {
	t.Helper()
	mock := testutil.NewMockProvider(goagent.CompletionResponse{
		Message:    goagent.AssistantMessage("ok"),
		StopReason: goagent.StopReasonEndTurn,
	})
	base := []goagent.Option{
		goagent.WithProvider(mock),
		goagent.WithModel("test-model"),
	}
	a, err := goagent.New(append(base, opts...)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, _ = a.Run(context.Background(), "hi")
	calls := mock.Calls()
	if len(calls) == 0 {
		t.Fatal("provider was not called")
	}
	return calls[0]
}

func TestWithMaxTokens(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		n         int
		wantInReq int
	}{
		{"zero propagates as zero (provider uses its default)", 0, 0},
		{"positive value propagates", 2048, 2048},
		{"large value propagates", 100_000, 100_000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := buildReqFromAgent(t, goagent.WithMaxTokens(tc.n))
			if req.MaxTokens != tc.wantInReq {
				t.Errorf("MaxTokens = %d, want %d", req.MaxTokens, tc.wantInReq)
			}
		})
	}
}

func TestWithTemperature(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		temp      float64
		wantValue float64
	}{
		{"zero sets pointer to 0.0 (distinguishable from nil)", 0.0, 0.0},
		{"standard value", 0.7, 0.7},
		{"max anthropic value", 1.0, 1.0},
		{"max openai-compatible value", 2.0, 2.0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := buildReqFromAgent(t, goagent.WithTemperature(tc.temp))
			if req.Temperature == nil {
				t.Fatal("Temperature is nil, want non-nil pointer")
			}
			if *req.Temperature != tc.wantValue {
				t.Errorf("*Temperature = %v, want %v", *req.Temperature, tc.wantValue)
			}
		})
	}
}

func TestWithTemperature_NotSet_NilInRequest(t *testing.T) {
	t.Parallel()
	req := buildReqFromAgent(t) // no WithTemperature
	if req.Temperature != nil {
		t.Errorf("Temperature = %v, want nil when not configured", *req.Temperature)
	}
}

func TestWithMaxTokens_NotSet_ZeroInRequest(t *testing.T) {
	t.Parallel()
	req := buildReqFromAgent(t) // no WithMaxTokens
	if req.MaxTokens != 0 {
		t.Errorf("MaxTokens = %d, want 0 when not configured", req.MaxTokens)
	}
}
