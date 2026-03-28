package voyage_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Germanblandin1/goagent/providers/voyage"
)

func TestVoyageClient_Do_OK(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer test-key")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value": 7}`))
	}))
	t.Cleanup(srv.Close)

	client := voyage.NewClient(
		voyage.WithBaseURL(srv.URL),
		voyage.WithAPIKey("test-key"),
	)
	var out struct {
		Value int `json:"value"`
	}
	err := voyage.DoRequest(client, context.Background(), "/test", map[string]string{"k": "v"}, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Value != 7 {
		t.Errorf("Value = %d, want 7", out.Value)
	}
}

func TestVoyageClient_Do_NonOKStatus(t *testing.T) {
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
			name: "status 401 with detail body",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"detail": "Invalid API key"}`))
			},
			wantContain: "Invalid API key",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(tc.handler)
			t.Cleanup(srv.Close)

			client := voyage.NewClient(voyage.WithBaseURL(srv.URL))
			var out struct{}
			err := voyage.DoRequest(client, context.Background(), "/test", nil, &out)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantContain) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tc.wantContain)
			}
		})
	}
}

func TestVoyageClient_Do_InvalidJSON(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{invalid`))
	}))
	t.Cleanup(srv.Close)

	client := voyage.NewClient(voyage.WithBaseURL(srv.URL))
	var out struct{ Value int }
	err := voyage.DoRequest(client, context.Background(), "/test", nil, &out)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "decoding response") {
		t.Errorf("error = %q, want to contain 'decoding response'", err.Error())
	}
}

func TestVoyageClient_Do_ContextCancelled(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := voyage.NewClient(voyage.WithBaseURL(srv.URL))
	var out struct{}
	err := voyage.DoRequest(client, ctx, "/test", nil, &out)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}
