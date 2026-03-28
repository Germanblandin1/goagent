package ollama_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Germanblandin1/goagent/providers/ollama"
)

func TestOllamaClient_Do_OK(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value": 42}`))
	}))
	t.Cleanup(srv.Close)

	client := ollama.NewClient(ollama.WithBaseURL(srv.URL))
	var out struct {
		Value int `json:"value"`
	}
	err := ollama.DoRequest(client, context.Background(), "/test", map[string]string{"k": "v"}, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Value != 42 {
		t.Errorf("Value = %d, want 42", out.Value)
	}
}

func TestOllamaClient_Do_NonOKStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		handler     http.HandlerFunc
		wantContain string
	}{
		{
			name: "status 500 without error body",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("not json"))
			},
			wantContain: "status 500",
		},
		{
			name: "status 400 with error body",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error": "model not found"}`))
			},
			wantContain: "model not found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(tc.handler)
			t.Cleanup(srv.Close)

			client := ollama.NewClient(ollama.WithBaseURL(srv.URL))
			var out struct{}
			err := ollama.DoRequest(client, context.Background(), "/test", nil, &out)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if got := err.Error(); !strings.Contains(got, tc.wantContain) {
				t.Errorf("error = %q, want to contain %q", got, tc.wantContain)
			}
		})
	}
}

func TestOllamaClient_Do_InvalidJSON(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{invalid json`))
	}))
	t.Cleanup(srv.Close)

	client := ollama.NewClient(ollama.WithBaseURL(srv.URL))
	var out struct{ Value int }
	err := ollama.DoRequest(client, context.Background(), "/test", nil, &out)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "decoding response") {
		t.Errorf("error = %q, want to contain 'decoding response'", err.Error())
	}
}

func TestOllamaClient_Do_ContextCancelled(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the request context is cancelled.
		<-r.Context().Done()
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	client := ollama.NewClient(ollama.WithBaseURL(srv.URL))
	var out struct{}
	err := ollama.DoRequest(client, ctx, "/test", nil, &out)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

