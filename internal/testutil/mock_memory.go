package testutil

import (
	"context"
	"sync"

	"github.com/Germanblandin1/goagent"
)

// MockMemory is a thread-safe ShortTermMemory implementation for tests.
// It records all Append calls and can be configured to return errors.
type MockMemory struct {
	mu          sync.Mutex
	messages    []goagent.Message
	appendErr   error
	messagesErr error
}

// NewMockMemory creates a MockMemory with no pre-loaded history and no errors.
func NewMockMemory() *MockMemory {
	return &MockMemory{}
}

// NewMockMemoryWithErrors creates a MockMemory that returns the given errors
// from Append and Messages respectively. Pass nil to not error on that method.
func NewMockMemoryWithErrors(appendErr, messagesErr error) *MockMemory {
	return &MockMemory{appendErr: appendErr, messagesErr: messagesErr}
}

// NewMockMemoryWithHistory creates a MockMemory pre-loaded with the given messages.
func NewMockMemoryWithHistory(msgs ...goagent.Message) *MockMemory {
	m := &MockMemory{}
	m.messages = append(m.messages, msgs...)
	return m
}

// Messages returns the stored messages. Returns messagesErr if configured.
func (m *MockMemory) Messages(_ context.Context) ([]goagent.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.messagesErr != nil {
		return nil, m.messagesErr
	}
	if len(m.messages) == 0 {
		return nil, nil
	}
	out := make([]goagent.Message, len(m.messages))
	copy(out, m.messages)
	return out, nil
}

// Append records the messages. Returns appendErr if configured.
func (m *MockMemory) Append(_ context.Context, msgs ...goagent.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.appendErr != nil {
		return m.appendErr
	}
	m.messages = append(m.messages, msgs...)
	return nil
}

// All returns all stored messages as a defensive copy.
func (m *MockMemory) All() []goagent.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]goagent.Message, len(m.messages))
	copy(out, m.messages)
	return out
}

// ── Long-term memory mock ────────────────────────────────────────────────────

// MockLongTermMemory is a thread-safe LongTermMemory implementation for tests.
type MockLongTermMemory struct {
	mu          sync.Mutex
	stored      []goagent.Message
	retrieved   []goagent.ScoredMessage // fixed set returned by Retrieve
	storeErr    error
	retrieveErr error
}

// NewMockLongTermMemory creates a MockLongTermMemory that stores all messages
// and returns an empty slice from Retrieve by default.
func NewMockLongTermMemory() *MockLongTermMemory {
	return &MockLongTermMemory{}
}

// NewMockLongTermMemoryWithRetrieve creates a MockLongTermMemory that returns
// the given messages (with Score 0.0) as the fixed response to every Retrieve call.
func NewMockLongTermMemoryWithRetrieve(retrieved ...goagent.Message) *MockLongTermMemory {
	scored := make([]goagent.ScoredMessage, len(retrieved))
	for i, m := range retrieved {
		scored[i] = goagent.ScoredMessage{Message: m}
	}
	return &MockLongTermMemory{retrieved: scored}
}

// NewMockLongTermMemoryWithErrors creates a MockLongTermMemory that returns
// the given errors from Store and Retrieve respectively.
func NewMockLongTermMemoryWithErrors(storeErr, retrieveErr error) *MockLongTermMemory {
	return &MockLongTermMemory{storeErr: storeErr, retrieveErr: retrieveErr}
}

// Store records msgs. Returns storeErr if configured.
func (m *MockLongTermMemory) Store(_ context.Context, msgs ...goagent.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.storeErr != nil {
		return m.storeErr
	}
	m.stored = append(m.stored, msgs...)
	return nil
}

// Retrieve returns the fixed retrieved slice. Returns retrieveErr if configured.
func (m *MockLongTermMemory) Retrieve(_ context.Context, _ []goagent.ContentBlock, _ int, _ ...goagent.SearchOption) ([]goagent.ScoredMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.retrieveErr != nil {
		return nil, m.retrieveErr
	}
	out := make([]goagent.ScoredMessage, len(m.retrieved))
	copy(out, m.retrieved)
	return out, nil
}

// AllStored returns all messages passed to Store as a defensive copy.
func (m *MockLongTermMemory) AllStored() []goagent.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]goagent.Message, len(m.stored))
	copy(out, m.stored)
	return out
}
