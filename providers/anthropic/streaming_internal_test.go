package anthropic

import (
	"context"
	"encoding/json"
	"testing"

	sdk "github.com/anthropics/anthropic-sdk-go"

	"github.com/Germanblandin1/goagent"
)

// deltaEvent builds a ContentBlockDeltaEvent from a raw delta JSON payload.
// The SDK's As*Delta accessors read the union's internal raw JSON, so the delta
// must be unmarshaled rather than struct-initialized for AsAny() to resolve it.
func deltaEvent(t *testing.T, rawJSON string) sdk.ContentBlockDeltaEvent {
	t.Helper()
	var delta sdk.RawContentBlockDeltaUnion
	if err := json.Unmarshal([]byte(rawJSON), &delta); err != nil {
		t.Fatalf("unmarshal delta %q: %v", rawJSON, err)
	}
	return sdk.ContentBlockDeltaEvent{Delta: delta}
}

// newMockStream builds an anthropicStream that replays the given SDK event
// variants in order, mimicking what CompleteStream wires from the live SDK
// stream (currentFn returns the AsAny() variant of each event).
func newMockStream(events []any) *anthropicStream {
	i := -1
	return &anthropicStream{
		nextFn:    func() bool { i++; return i < len(events) },
		currentFn: func() any { return events[i] },
		errFn:     func() error { return nil },
		closeFn:   func() error { return nil },
	}
}

// TestAnthropicStream_ThinkingDelta verifies that an extended-thinking
// thinking_delta is surfaced as a StreamEventThinking (not StreamEventText),
// so RunStream routes it to OnThinkingText without polluting the final answer.
func TestAnthropicStream_ThinkingDelta(t *testing.T) {
	t.Parallel()

	events := []any{
		deltaEvent(t, `{"type":"thinking_delta","thinking":"let me reason"}`),
		deltaEvent(t, `{"type":"signature_delta","signature":"sig-abc"}`),
		deltaEvent(t, `{"type":"text_delta","text":"the answer"}`),
	}
	s := newMockStream(events)

	// First event: the thinking token (Text set, no signature).
	if !s.Next(context.Background()) {
		t.Fatal("Next() = false, want a thinking event")
	}
	if ev := s.Event(); ev.Type != goagent.StreamEventThinking || ev.Text != "let me reason" || ev.Signature != "" {
		t.Fatalf("event = {Type:%v Text:%q Sig:%q}, want {StreamEventThinking %q \"\"}", ev.Type, ev.Text, ev.Signature, "let me reason")
	}

	// Second event: the signature seal (Signature set, no Text), so RunStream can
	// rebuild a signed thinking block.
	if !s.Next(context.Background()) {
		t.Fatal("Next() = false, want a signature event")
	}
	if ev := s.Event(); ev.Type != goagent.StreamEventThinking || ev.Text != "" || ev.Signature != "sig-abc" {
		t.Fatalf("event = {Type:%v Text:%q Sig:%q}, want {StreamEventThinking \"\" %q}", ev.Type, ev.Text, ev.Signature, "sig-abc")
	}

	// Third event: the answer text.
	if !s.Next(context.Background()) {
		t.Fatal("Next() = false, want a text event")
	}
	if ev := s.Event(); ev.Type != goagent.StreamEventText || ev.Text != "the answer" {
		t.Fatalf("event = {Type:%v Text:%q}, want {StreamEventText %q}", ev.Type, ev.Text, "the answer")
	}

	if s.Next(context.Background()) {
		t.Fatalf("Next() = true after stream end, event = %+v", s.Event())
	}
}
