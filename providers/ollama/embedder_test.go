package ollama_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector"
	"github.com/Germanblandin1/goagent/providers/ollama"
)

// embeddingServer creates a test server that handles POST /api/embeddings
// and returns the given embedding JSON response.
func embeddingServer(t *testing.T, responseJSON string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
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

func TestOllamaEmbedder_Embed_OK(t *testing.T) {
	t.Parallel()

	srv := embeddingServer(t, `{"embedding":[0.1,0.2,0.3]}`)
	client := ollama.NewClient(ollama.WithBaseURL(srv.URL))
	e := ollama.NewEmbedderWithClient(client, ollama.WithEmbedModel("nomic-embed-text"))

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

func TestOllamaEmbedder_Embed_EmptyModel(t *testing.T) {
	t.Parallel()

	srv := embeddingServer(t, `{"embedding":[0.1]}`)
	client := ollama.NewClient(ollama.WithBaseURL(srv.URL))
	e := ollama.NewEmbedderWithClient(client) // no WithEmbedModel

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

func TestOllamaEmbedder_Embed_NoTextBlocks(t *testing.T) {
	t.Parallel()

	srv := embeddingServer(t, `{"embedding":[]}`)
	client := ollama.NewClient(ollama.WithBaseURL(srv.URL))
	e := ollama.NewEmbedderWithClient(client, ollama.WithEmbedModel("nomic-embed-text"))

	_, err := e.Embed(context.Background(), []goagent.ContentBlock{
		goagent.ImageBlock([]byte{0xFF}, "image/png"),
	})
	if !errors.Is(err, vector.ErrNoEmbeddeableContent) {
		t.Errorf("expected ErrNoEmbeddeableContent, got %v", err)
	}
}

func TestOllamaEmbedder_Embed_ContextCancelled(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := ollama.NewClient(ollama.WithBaseURL(srv.URL))
	e := ollama.NewEmbedderWithClient(client, ollama.WithEmbedModel("nomic-embed-text"))

	_, err := e.Embed(ctx, []goagent.ContentBlock{goagent.TextBlock("hello")})
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

