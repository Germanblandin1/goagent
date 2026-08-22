package openai_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// modelsServer starts a server that responds to GET /models with responseJSON.
// go-openai builds the URL as baseURL+"/models".
func modelsServer(t *testing.T, responseJSON string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
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

func TestProvider_Models_ListsIDs(t *testing.T) {
	t.Parallel()

	body := `{
	  "object":"list",
	  "data":[
	    {"id":"gpt-4o","object":"model"},
	    {"id":"gpt-3.5-turbo","object":"model"}
	  ]
	}`
	srv := modelsServer(t, body)
	p := newProvider(t, srv)

	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("got %d models, want 2", len(models))
	}
	if models[0].Name != "gpt-4o" || models[1].Name != "gpt-3.5-turbo" {
		t.Errorf("model names = %q, %q; want gpt-4o, gpt-3.5-turbo", models[0].Name, models[1].Name)
	}
	if len(models[0].Capabilities) != 0 {
		t.Errorf("capabilities = %v, want empty (listing does not report them)", models[0].Capabilities)
	}
}

func TestProvider_Models_Empty(t *testing.T) {
	t.Parallel()

	srv := modelsServer(t, `{"object":"list","data":[]}`)
	p := newProvider(t, srv)

	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("got %d models, want 0", len(models))
	}
}

func TestProvider_Models_ErrorClassified(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":{"message":"boom"}}`, http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	p := newProvider(t, srv)

	if _, err := p.Models(context.Background()); err == nil {
		t.Fatal("expected error from failing /models, got nil")
	}
}

func TestProvider_ModelInfo_ReturnsName(t *testing.T) {
	t.Parallel()

	// ModelInfo makes no network call; any server works.
	srv := modelsServer(t, `{"object":"list","data":[]}`)
	p := newProvider(t, srv)

	info, err := p.ModelInfo(context.Background(), "gpt-4o")
	if err != nil {
		t.Fatalf("ModelInfo: %v", err)
	}
	if info.Name != "gpt-4o" {
		t.Errorf("name = %q, want gpt-4o", info.Name)
	}
	if len(info.Capabilities) != 0 {
		t.Errorf("capabilities = %v, want empty", info.Capabilities)
	}
}
