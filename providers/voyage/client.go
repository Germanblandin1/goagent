package voyage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

const defaultBaseURL = "https://api.voyageai.com/v1"

// VoyageClient is a reusable HTTP client for the Voyage AI REST API.
// It is safe for concurrent use and is shared across Embedder instances.
type VoyageClient struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
}

// ClientOption configures a VoyageClient.
type ClientOption func(*VoyageClient)

// WithAPIKey sets the Voyage AI API key.
// If not set, the client reads VOYAGE_API_KEY from the environment.
func WithAPIKey(key string) ClientOption {
	return func(c *VoyageClient) { c.apiKey = key }
}

// WithBaseURL overrides the Voyage AI base URL.
// Useful for proxies or test servers.
func WithBaseURL(url string) ClientOption {
	return func(c *VoyageClient) { c.baseURL = url }
}

// WithHTTPClient replaces the default *http.Client used for all requests.
func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *VoyageClient) { c.httpClient = hc }
}

// WithTimeout creates an *http.Client with the given timeout and assigns it
// to the VoyageClient. Applied after WithHTTPClient if both are provided.
func WithTimeout(d time.Duration) ClientOption {
	return func(c *VoyageClient) { c.httpClient = &http.Client{Timeout: d} }
}

// NewClient creates a VoyageClient with the given options.
// Default base URL: "https://api.voyageai.com/v1".
// Default API key: value of the VOYAGE_API_KEY environment variable.
func NewClient(opts ...ClientOption) *VoyageClient {
	c := &VoyageClient{
		httpClient: &http.Client{},
		baseURL:    defaultBaseURL,
		apiKey:     os.Getenv("VOYAGE_API_KEY"),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// do marshals reqBody as JSON, POSTs it to baseURL+path with a Bearer
// Authorization header, checks the HTTP status, and decodes the response
// body into out.
func (c *VoyageClient) do(ctx context.Context, path string, reqBody any, out any) error {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("voyage: marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("voyage: creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody struct {
			Detail string `json:"detail"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		if errBody.Detail != "" {
			return fmt.Errorf("voyage: status %d: %s", resp.StatusCode, errBody.Detail)
		}
		return fmt.Errorf("voyage: status %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("voyage: decoding response: %w", err)
	}
	return nil
}
