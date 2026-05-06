package testutil

import (
	"context"

	"github.com/Germanblandin1/goagent"
)

// MockStreamingProvider is a Provider that also implements goagent.StreamingProvider.
// It embeds MockProvider for the non-streaming Complete path.
type MockStreamingProvider struct {
	*MockProvider
	streamEvents []goagent.StreamEvent
	streamErr    error
}

// NewMockStreamingProvider creates a MockStreamingProvider.
// events are returned by CompleteStream; responses are returned by Complete.
func NewMockStreamingProvider(
	events []goagent.StreamEvent,
	responses ...goagent.CompletionResponse,
) *MockStreamingProvider {
	return &MockStreamingProvider{
		MockProvider: NewMockProvider(responses...),
		streamEvents: events,
	}
}

// WithStreamError configures CompleteStream to return err instead of a stream.
func (m *MockStreamingProvider) WithStreamError(err error) {
	m.streamErr = err
}

// CompleteStream implements goagent.StreamingProvider.
func (m *MockStreamingProvider) CompleteStream(
	_ context.Context, _ goagent.CompletionRequest,
) (goagent.Stream, error) {
	if m.streamErr != nil {
		return nil, m.streamErr
	}
	return &MockStream{Events: m.streamEvents}, nil
}

// MockStream is an exported Stream backed by a fixed slice of events.
// Use it when a test needs fine-grained control over stream contents.
type MockStream struct {
	Events  []goagent.StreamEvent
	pos     int
	current goagent.StreamEvent
}

func (s *MockStream) Next(_ context.Context) bool {
	if s.pos >= len(s.Events) {
		return false
	}
	s.current = s.Events[s.pos]
	s.pos++
	return true
}

func (s *MockStream) Event() goagent.StreamEvent { return s.current }
func (s *MockStream) Err() error                 { return nil }
func (s *MockStream) Close() error               { return nil }
