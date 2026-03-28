package vector_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Germanblandin1/goagent/memory/vector"
)

func TestCharEstimator(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{"empty", "", 0},
		{"ascii", "hello", 5},
		{"unicode multibyte", "héllo", 5}, // é is 2 bytes but 1 rune
		{"emoji", "hi 🙂", 4},             // emoji is 1 rune
		{"chinese", "你好", 2},
	}
	est := &vector.CharEstimator{}
	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := est.Measure(ctx, tc.text); got != tc.want {
				t.Errorf("CharEstimator.Measure(%q) = %d, want %d", tc.text, got, tc.want)
			}
		})
	}
}

func TestHeuristicTokenEstimator(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{"empty", "", 4},           // 0/4 + 4
		{"short", "hi", 4},         // 2/4 + 4 = 4
		{"longer", "hello world", 6}, // 11/4 + 4 = 6
	}
	est := &vector.HeuristicTokenEstimator{}
	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := est.Measure(ctx, tc.text); got != tc.want {
				t.Errorf("HeuristicTokenEstimator.Measure(%q) = %d, want %d", tc.text, got, tc.want)
			}
		})
	}
}

func TestOllamaTokenEstimator_FallbackOnError(t *testing.T) {
	// Server returns 500 — estimator must not panic and must return a positive value.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	est := vector.NewOllamaTokenEstimator("nomic-embed-text")
	est.BaseURL = srv.URL

	ctx := context.Background()
	text := "hello world this is a test"
	got := est.Measure(ctx, text)
	want := (&vector.HeuristicTokenEstimator{}).Measure(ctx, text)
	if got != want {
		t.Errorf("OllamaTokenEstimator fallback = %d, want %d (heuristic)", got, want)
	}
}

func TestOllamaTokenEstimator_FallbackOnInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	est := vector.NewOllamaTokenEstimator("nomic-embed-text")
	est.BaseURL = srv.URL

	got := est.Measure(context.Background(), "some text")
	if got <= 0 {
		t.Errorf("OllamaTokenEstimator fallback returned %d, want > 0", got)
	}
}

func TestOllamaTokenEstimator_Success(t *testing.T) {
	// Fake server that returns 5 tokens.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"tokens":[1,2,3,4,5]}`))
	}))
	defer srv.Close()

	est := vector.NewOllamaTokenEstimator("nomic-embed-text")
	est.BaseURL = srv.URL

	if got := est.Measure(context.Background(), "any text"); got != 5 {
		t.Errorf("OllamaTokenEstimator.Measure = %d, want 5", got)
	}
}

func TestOllamaTokenEstimator_ContextCancellation(t *testing.T) {
	// Use a pre-cancelled context: http.NewRequestWithContext fails immediately
	// before the request reaches the server. srv never receives a connection,
	// so srv.Close() does not block.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be reached with a pre-cancelled context")
	}))
	defer srv.Close()

	est := vector.NewOllamaTokenEstimator("nomic-embed-text")
	est.BaseURL = srv.URL

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the call

	got := est.Measure(ctx, "hello")
	if got <= 0 {
		t.Errorf("expected positive fallback after cancellation, got %d", got)
	}
}
