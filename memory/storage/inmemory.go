package storage

import (
	"context"
	"sync"

	"github.com/Germanblandin1/goagent"
)

// InMemory is a thread-safe, in-process Storage backed by a slice.
// It retains all messages without any limit or eviction policy.
// Use InMemory when the Policy layer is responsible for deciding which
// messages to surface (e.g. FixedWindow, TokenWindow).
//
// # Copy semantics
//
// Both Load and Save perform a shallow copy of each Message: the Content
// []ContentBlock slice is copied so that callers may freely append to or
// reassign the returned Content field without affecting the stored state.
// However, the binary payloads inside ImageData.Data and DocumentData.Data
// are NOT deep-copied — those []byte values point to the same underlying
// memory. Callers must not mutate the bytes of images or documents.
//
// InMemory is safe for concurrent use from multiple goroutines.
type InMemory struct {
	mu       sync.RWMutex
	messages []goagent.Message
}

// NewInMemory returns an empty InMemory storage.
func NewInMemory() *InMemory {
	return &InMemory{}
}

// Load returns all stored messages in chronological order.
// The returned slice is a defensive copy; see the type-level doc for the
// exact copy semantics (Content slice is copied; binary bytes are shared).
func (s *InMemory) Load(_ context.Context) ([]goagent.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.messages) == 0 {
		return nil, nil
	}
	return copyMessages(s.messages), nil
}

// Save replaces the stored message history with msgs.
// It stores a defensive copy of the provided slice; see the type-level doc
// for the exact copy semantics (Content slice is copied; binary bytes are shared).
func (s *InMemory) Save(_ context.Context, msgs []goagent.Message) error {
	fresh := copyMessages(msgs)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = fresh
	return nil
}

// Append adds msgs to the end of the stored history without loading or
// replacing the existing state. This is O(len(msgs)) regardless of how
// many messages are already stored.
// The same copy semantics as Save apply — Content slices are copied,
// binary payloads are shared.
func (s *InMemory) Append(_ context.Context, msgs ...goagent.Message) error {
	if len(msgs) == 0 {
		return nil
	}
	fresh := copyMessages(msgs)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, fresh...)
	return nil
}

// copyMessages returns a new slice where each Message has its own independent
// Content []ContentBlock slice. This prevents a caller that appends to or
// reassigns Content from affecting the stored copy.
//
// Binary payloads (ImageData.Data, DocumentData.Data) are NOT deep-copied;
// those []byte values share the same underlying memory. Do not mutate the
// bytes of images or documents.
func copyMessages(msgs []goagent.Message) []goagent.Message {
	out := make([]goagent.Message, len(msgs))
	for i, msg := range msgs {
		out[i] = msg
		if len(msg.Content) > 0 {
			out[i].Content = make([]goagent.ContentBlock, len(msg.Content))
			copy(out[i].Content, msg.Content)
		}
	}
	return out
}
