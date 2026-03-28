package anthropic

import (
	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AnthropicClient wraps the Anthropic SDK client for shared use across
// multiple Provider instances (e.g. behind a proxy, a shared transport,
// or a test server).
type AnthropicClient struct {
	cl sdk.Client
}

// ClientOption configures an AnthropicClient.
type ClientOption func(*clientConfig)

type clientConfig struct {
	sdkOpts []option.RequestOption
}

// WithAPIKey sets the Anthropic API key.
// If not set, the SDK reads ANTHROPIC_API_KEY from the environment.
func WithAPIKey(key string) ClientOption {
	return func(c *clientConfig) {
		c.sdkOpts = append(c.sdkOpts, option.WithAPIKey(key))
	}
}

// WithBaseURL overrides the Anthropic API base URL.
// Useful for proxies, API-compatible services, or test servers.
func WithBaseURL(url string) ClientOption {
	return func(c *clientConfig) {
		c.sdkOpts = append(c.sdkOpts, option.WithBaseURL(url))
	}
}

// NewClient creates an AnthropicClient with the given options.
// Default base URL and API key resolution follow the Anthropic SDK defaults.
func NewClient(opts ...ClientOption) *AnthropicClient {
	cfg := &clientConfig{}
	for _, o := range opts {
		o(cfg)
	}
	return &AnthropicClient{cl: sdk.NewClient(cfg.sdkOpts...)}
}
