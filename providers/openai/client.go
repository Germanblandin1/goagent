package openai

import (
	"net/http"
	"os"
	"sync"

	openaiSDK "github.com/sashabaranov/go-openai"

	"github.com/Germanblandin1/goagent"
)

// Provider implements goagent.Provider and goagent.StreamingProvider using
// the OpenAI Chat Completions API.
type Provider struct {
	client *openaiSDK.Client

	// baseURL, apiKey and httpClient are retained so the model catalog can issue
	// a raw GET {baseURL}/models: the go-openai SDK decodes the listing into its
	// vanilla Model type and discards the rich metadata (pricing, context
	// length, supported parameters) that OpenAI-compatible endpoints such as
	// OpenRouter return. See catalog.go.
	baseURL    string
	apiKey     string
	httpClient *http.Client

	// modelCache holds the metadata-rich catalog once fetched, so ModelInfo can
	// resolve a single model without another network round trip.
	modelCache []goagent.ModelInfo
	cacheMu    sync.RWMutex
}

// providerConfig accumulates options before building the openai.Client.
type providerConfig struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// ProviderOption configures a Provider.
type ProviderOption func(*providerConfig)

// WithAPIKey sets the OpenAI API key.
// If not set, the key is read from the OPENAI_API_KEY environment variable.
func WithAPIKey(key string) ProviderOption {
	return func(c *providerConfig) { c.apiKey = key }
}

// WithBaseURL overrides the OpenAI API base URL.
// Useful for proxies, Azure OpenAI, or OpenAI-compatible services.
// Default: "https://api.openai.com/v1".
func WithBaseURL(url string) ProviderOption {
	return func(c *providerConfig) { c.baseURL = url }
}

// WithHTTPClient sets a custom *http.Client for all requests.
// Useful for configuring timeouts, transport, or test round-trippers.
func WithHTTPClient(hc *http.Client) ProviderOption {
	return func(c *providerConfig) { c.httpClient = hc }
}

// New creates a Provider targeting the OpenAI Chat Completions API.
// If no WithAPIKey option is given, the OPENAI_API_KEY environment variable
// is used. The API key is not validated at construction time.
func New(opts ...ProviderOption) *Provider {
	cfg := &providerConfig{}
	for _, o := range opts {
		o(cfg)
	}

	apiKey := cfg.apiKey
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}

	sdkCfg := openaiSDK.DefaultConfig(apiKey)
	if cfg.baseURL != "" {
		sdkCfg.BaseURL = cfg.baseURL
	}
	httpClient := cfg.httpClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	sdkCfg.HTTPClient = httpClient

	return &Provider{
		client:     openaiSDK.NewClientWithConfig(sdkCfg),
		baseURL:    sdkCfg.BaseURL,
		apiKey:     apiKey,
		httpClient: httpClient,
	}
}
