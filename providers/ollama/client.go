package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// OllamaClient is a reusable HTTP client for Ollama's REST API.
// It is safe for concurrent use and is shared across Provider and OllamaEmbedder.
type OllamaClient struct {
	httpClient *http.Client
	baseURL    string
}

// ClientOption configures an OllamaClient.
type ClientOption func(*OllamaClient)

// WithBaseURL overrides the Ollama server base URL.
// Default: "http://localhost:11434".
func WithBaseURL(url string) ClientOption {
	return func(c *OllamaClient) { c.baseURL = url }
}

// WithHTTPClient replaces the default *http.Client used for all requests.
func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *OllamaClient) { c.httpClient = hc }
}

// WithTimeout creates an *http.Client with the given timeout and assigns it
// to the OllamaClient. Applied after WithHTTPClient if both are provided.
func WithTimeout(d time.Duration) ClientOption {
	return func(c *OllamaClient) { c.httpClient = &http.Client{Timeout: d} }
}

// NewClient creates an OllamaClient with the given options.
// Default base URL: "http://localhost:11434".
func NewClient(opts ...ClientOption) *OllamaClient {
	c := &OllamaClient{
		httpClient: &http.Client{},
		baseURL:    "http://localhost:11434",
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// do marshals reqBody as JSON, POSTs it to baseURL+path, checks the HTTP
// status, and decodes the response body into out.
// On non-200 status it tries to decode {"error": "..."} from the body and
// includes it in the returned error.
func (c *OllamaClient) do(ctx context.Context, path string, reqBody any, out any) error {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("ollama: marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ollama: creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		if errBody.Error != "" {
			return fmt.Errorf("ollama: status %d: %s", resp.StatusCode, errBody.Error)
		}
		return fmt.Errorf("ollama: status %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("ollama: decoding response: %w", err)
	}
	return nil
}
