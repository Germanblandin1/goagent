package ollama_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/providers/ollama"
)

// Compile-time check: the Ollama provider implements the optional ModelCatalog.
var _ goagent.ModelCatalog = (*ollama.Provider)(nil)

func TestProvider_Models(t *testing.T) {
	t.Parallel()

	tags := `{"models":[
	  {"name":"llama3.2:latest","model":"llama3.2:latest"},
	  {"name":"gpt-oss:120b-cloud","model":"gpt-oss:120b-cloud"}
	]}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(tags))
	}))
	t.Cleanup(srv.Close)

	p := ollama.NewWithClient(ollama.NewClient(ollama.WithBaseURL(srv.URL)))
	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatalf("Models error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("got %d models, want 2", len(models))
	}
	if models[0].Name != "llama3.2:latest" || models[1].Name != "gpt-oss:120b-cloud" {
		t.Errorf("names = %q/%q", models[0].Name, models[1].Name)
	}
	// Models is the cheap listing — capabilities are not populated here.
	if len(models[0].Capabilities) != 0 {
		t.Errorf("Models should not populate capabilities, got %v", models[0].Capabilities)
	}
}

func TestProvider_ModelInfoCapabilities(t *testing.T) {
	t.Parallel()

	var showCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/show" {
			http.NotFound(w, r)
			return
		}
		showCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"capabilities":["completion","tools","thinking"]}`))
	}))
	t.Cleanup(srv.Close)

	p := ollama.NewWithClient(ollama.NewClient(ollama.WithBaseURL(srv.URL)))

	info, err := p.ModelInfo(context.Background(), "gpt-oss")
	if err != nil {
		t.Fatalf("ModelInfo error: %v", err)
	}
	if info.Name != "gpt-oss" {
		t.Errorf("Name = %q, want gpt-oss", info.Name)
	}
	if !info.Supports(goagent.CapabilityThinking) {
		t.Error("expected Supports(CapabilityThinking) = true")
	}
	if !info.Supports(goagent.CapabilityTools) {
		t.Error("expected Supports(CapabilityTools) = true")
	}
	if info.Supports(goagent.CapabilityVision) {
		t.Error("did not expect Supports(CapabilityVision) = true")
	}

	// Second lookup for the same model must be served from cache.
	if _, err := p.ModelInfo(context.Background(), "gpt-oss"); err != nil {
		t.Fatalf("second ModelInfo error: %v", err)
	}
	if got := showCalls.Load(); got != 1 {
		t.Errorf("/api/show called %d times, want 1 (cached)", got)
	}
}

func TestModelInfo_Supports(t *testing.T) {
	t.Parallel()

	info := goagent.ModelInfo{
		Name:         "m",
		Capabilities: []goagent.Capability{goagent.CapabilityCompletion, goagent.CapabilityThinking},
	}
	if !info.Supports(goagent.CapabilityThinking) {
		t.Error("Supports(CapabilityThinking) = false, want true")
	}
	if info.Supports(goagent.CapabilityEmbedding) {
		t.Error("Supports(CapabilityEmbedding) = true, want false")
	}
}
