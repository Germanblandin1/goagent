package openai_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Germanblandin1/goagent"
)

// modelsServer starts a server that responds to GET /models with responseJSON.
// The provider builds the URL as baseURL+"/models".
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

func TestProvider_ModelInfo_UnknownModel_ReturnsName(t *testing.T) {
	t.Parallel()

	// The model is absent from the listing, so ModelInfo falls back to Name.
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

// openRouterModels mimics the shape of OpenRouter's /api/v1/models listing:
// one paid model with rich metadata and one free model (all pricing "0").
const openRouterModels = `{
  "data": [
    {
      "id": "anthropic/claude-3.5-sonnet",
      "name": "Anthropic: Claude 3.5 Sonnet",
      "context_length": 200000,
      "architecture": {"input_modalities": ["text", "image"]},
      "pricing": {"prompt": "0.000003", "completion": "0.000015", "request": "0"},
      "top_provider": {"context_length": 200000, "max_completion_tokens": 8192},
      "supported_parameters": ["tools", "reasoning", "temperature"]
    },
    {
      "id": "meta-llama/llama-3.1-8b-instruct:free",
      "name": "Meta: Llama 3.1 8B Instruct (free)",
      "context_length": 131072,
      "architecture": {"input_modalities": ["text"]},
      "pricing": {"prompt": "0", "completion": "0", "request": "0"},
      "top_provider": {"context_length": 131072, "max_completion_tokens": 0},
      "supported_parameters": ["tools"]
    }
  ]
}`

func TestProvider_Models_OpenRouterMetadata(t *testing.T) {
	t.Parallel()

	srv := modelsServer(t, openRouterModels)
	p := newProvider(t, srv)

	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("got %d models, want 2", len(models))
	}

	paid := models[0]
	if paid.Name != "anthropic/claude-3.5-sonnet" {
		t.Errorf("name = %q", paid.Name)
	}
	if paid.DisplayName != "Anthropic: Claude 3.5 Sonnet" {
		t.Errorf("display name = %q", paid.DisplayName)
	}
	if paid.ContextLength != 200000 {
		t.Errorf("context length = %d, want 200000", paid.ContextLength)
	}
	if paid.MaxOutputTokens != 8192 {
		t.Errorf("max output tokens = %d, want 8192", paid.MaxOutputTokens)
	}
	if paid.Pricing == nil {
		t.Fatal("paid model pricing is nil, want populated")
	}
	// 0.000003 USD/token * 1e6 = 3.0 USD per million tokens.
	if paid.Pricing.InputPerMTok != 3.0 {
		t.Errorf("input per MTok = %v, want 3.0", paid.Pricing.InputPerMTok)
	}
	if paid.Pricing.OutputPerMTok != 15.0 {
		t.Errorf("output per MTok = %v, want 15.0", paid.Pricing.OutputPerMTok)
	}
	if paid.Pricing.Currency != "USD" {
		t.Errorf("currency = %q, want USD", paid.Pricing.Currency)
	}
	if paid.IsFree() {
		t.Error("paid model reported as free")
	}
	if !paid.Supports(goagent.CapabilityTools) ||
		!paid.Supports(goagent.CapabilityThinking) ||
		!paid.Supports(goagent.CapabilityVision) {
		t.Errorf("capabilities = %v, want tools+thinking+vision", paid.Capabilities)
	}

	free := models[1]
	if free.Pricing == nil {
		t.Fatal("free model pricing is nil, want populated (all-zero)")
	}
	if !free.IsFree() {
		t.Error("free model not reported as free")
	}
	if free.Supports(goagent.CapabilityVision) {
		t.Error("text-only free model must not report vision")
	}
	if !free.Supports(goagent.CapabilityTools) {
		t.Error("free model should report tools")
	}
}

func TestProvider_Models_OpenAIStyle_NoPricing(t *testing.T) {
	t.Parallel()

	// The official OpenAI API reports only ids: no pricing, no context length.
	srv := modelsServer(t, `{"object":"list","data":[{"id":"gpt-4o","object":"model"}]}`)
	p := newProvider(t, srv)

	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("got %d models, want 1", len(models))
	}
	m := models[0]
	if m.Name != "gpt-4o" {
		t.Errorf("name = %q, want gpt-4o", m.Name)
	}
	if m.Pricing != nil {
		t.Errorf("pricing = %+v, want nil (unknown)", m.Pricing)
	}
	if m.IsFree() {
		t.Error("model with unknown pricing must not report as free")
	}
	if m.ContextLength != 0 || m.MaxOutputTokens != 0 {
		t.Errorf("context/output = %d/%d, want 0/0 (unknown)", m.ContextLength, m.MaxOutputTokens)
	}
	if m.DisplayName != "" {
		t.Errorf("display name = %q, want empty", m.DisplayName)
	}
	if len(m.Capabilities) != 0 {
		t.Errorf("capabilities = %v, want empty", m.Capabilities)
	}
}

func TestProvider_ModelInfo_PopulatedFromListing(t *testing.T) {
	t.Parallel()

	srv := modelsServer(t, openRouterModels)
	p := newProvider(t, srv)

	info, err := p.ModelInfo(context.Background(), "anthropic/claude-3.5-sonnet")
	if err != nil {
		t.Fatalf("ModelInfo: %v", err)
	}
	if info.ContextLength != 200000 || info.Pricing == nil || info.Pricing.InputPerMTok != 3.0 {
		t.Errorf("ModelInfo did not reuse the populated listing: %+v", info)
	}
}
