package ollama

import "context"

// DoRequest exposes OllamaClient.do for white-box tests in client_test.go.
// It exists only in test binaries.
func DoRequest(c *OllamaClient, ctx context.Context, path string, reqBody, out any) error {
	return c.do(ctx, path, reqBody, out)
}
