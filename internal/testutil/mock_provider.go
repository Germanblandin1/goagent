// Package testutil provides shared test helpers for the goagent module.
package testutil

import (
	"context"
	"fmt"
	"sync"

	"github.com/Germanblandin1/goagent"
)

// MockProvider is a thread-safe Provider that returns pre-configured responses
// in FIFO order. Use it in unit tests to avoid requiring a live LLM.
type MockProvider struct {
	mu        sync.Mutex
	responses []goagent.CompletionResponse
	calls     []goagent.CompletionRequest
}

// NewMockProvider creates a MockProvider that will return the given responses
// in order. If Complete is called more times than there are responses, it
// returns an error.
func NewMockProvider(responses ...goagent.CompletionResponse) *MockProvider {
	return &MockProvider{responses: responses}
}

// Complete returns the next queued response. It is safe for concurrent use.
func (m *MockProvider) Complete(_ context.Context, req goagent.CompletionRequest) (goagent.CompletionResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls = append(m.calls, req)

	if len(m.responses) == 0 {
		return goagent.CompletionResponse{}, fmt.Errorf("MockProvider: no more responses queued (call #%d)", len(m.calls))
	}

	resp := m.responses[0]
	m.responses = m.responses[1:]
	return resp, nil
}

// Calls returns all requests received so far, in order. Safe to call after
// the test has finished.
func (m *MockProvider) Calls() []goagent.CompletionRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]goagent.CompletionRequest, len(m.calls))
	copy(out, m.calls)
	return out
}
