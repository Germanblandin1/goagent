package voyage_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector"
	"github.com/Germanblandin1/goagent/providers/voyage"
)

// voyageEmbeddingServer creates a test server that handles POST /embeddings
// and returns the given embedding JSON response.
func voyageEmbeddingServer(t *testing.T, responseJSON string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(responseJSON)); err != nil {
			t.Errorf("writing response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestVoyageEmbedder_Embed_OK(t *testing.T) {
	t.Parallel()

	srv := voyageEmbeddingServer(t, `{"data":[{"embedding":[0.1,0.2,0.3],"index":0}]}`)
	client := voyage.NewClient(voyage.WithBaseURL(srv.URL))
	e := voyage.NewEmbedderWithClient(client, voyage.WithEmbedModel("voyage-3"))

	vec, err := e.Embed(context.Background(), []goagent.ContentBlock{
		goagent.TextBlock("hello world"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vec) != 3 {
		t.Fatalf("vector length = %d, want 3", len(vec))
	}
	if vec[0] != 0.1 || vec[1] != 0.2 || vec[2] != 0.3 {
		t.Errorf("vector = %v, want [0.1 0.2 0.3]", vec)
	}
}

func TestVoyageEmbedder_Embed_EmptyModel(t *testing.T) {
	t.Parallel()

	srv := voyageEmbeddingServer(t, `{"data":[{"embedding":[0.1],"index":0}]}`)
	client := voyage.NewClient(voyage.WithBaseURL(srv.URL))
	e := voyage.NewEmbedderWithClient(client) // no WithEmbedModel

	_, err := e.Embed(context.Background(), []goagent.ContentBlock{
		goagent.TextBlock("hello"),
	})
	if err == nil {
		t.Fatal("expected error for empty model, got nil")
	}
	if !strings.Contains(err.Error(), "model not set") {
		t.Errorf("error = %q, want to contain 'model not set'", err.Error())
	}
}

func TestVoyageEmbedder_Embed_NoTextBlocks(t *testing.T) {
	t.Parallel()

	srv := voyageEmbeddingServer(t, `{"data":[]}`)
	client := voyage.NewClient(voyage.WithBaseURL(srv.URL))
	e := voyage.NewEmbedderWithClient(client, voyage.WithEmbedModel("voyage-3"))

	_, err := e.Embed(context.Background(), []goagent.ContentBlock{
		goagent.ImageBlock([]byte{0xFF}, "image/png"),
	})
	if !errors.Is(err, vector.ErrNoEmbeddeableContent) {
		t.Errorf("expected ErrNoEmbeddeableContent, got %v", err)
	}
}

func TestVoyageEmbedder_Embed_InputTypeSent(t *testing.T) {
	t.Parallel()

	var gotInputType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			InputType string `json:"input_type"`
		}
		_ = json.Unmarshal(body, &req)
		gotInputType = req.InputType
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.5],"index":0}]}`))
	}))
	t.Cleanup(srv.Close)

	client := voyage.NewClient(voyage.WithBaseURL(srv.URL))
	e := voyage.NewEmbedderWithClient(client,
		voyage.WithEmbedModel("voyage-3"),
		voyage.WithInputType("document"),
	)

	_, err := e.Embed(context.Background(), []goagent.ContentBlock{
		goagent.TextBlock("some document"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotInputType != "document" {
		t.Errorf("input_type = %q, want %q", gotInputType, "document")
	}
}

func TestVoyageEmbedder_Embed_ContextCancelled(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := voyage.NewClient(voyage.WithBaseURL(srv.URL))
	e := voyage.NewEmbedderWithClient(client, voyage.WithEmbedModel("voyage-3"))

	_, err := e.Embed(ctx, []goagent.ContentBlock{goagent.TextBlock("hello")})
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}
