package goagent

import (
	"context"
	"fmt"
	"io"
)

// StreamEventType identifies the kind of each stream event.
type StreamEventType int

const (
	// StreamEventText is a text delta from the model response.
	StreamEventText StreamEventType = iota

	// StreamEventToolStart indicates the model is initiating a tool call.
	StreamEventToolStart

	// StreamEventToolDelta is a cumulative JSON fragment of tool arguments.
	// The provider accumulates input; the handler always receives the total so far.
	StreamEventToolDelta

	// StreamEventDone is the final event, carrying Usage and StopReason.
	StreamEventDone
)

// StreamEvent is the atomic unit delivered to a StreamHandler.
// Only fields relevant to the Type are populated.
type StreamEvent struct {
	Type StreamEventType

	// StreamEventText
	Text string

	// StreamEventToolStart — the model declared it will call a tool
	ToolName string
	ToolID   string

	// StreamEventToolDelta — cumulative JSON of tool args up to this point
	InputDelta string

	// StreamEventDone
	StopReason StopReason
	Usage      Usage
}

// Stream is an iterator over the events of a streaming completion.
// The caller MUST call Close() to release the underlying HTTP connection,
// even if Next() returned false or Err() has an error.
//
// Usage:
//
//	stream, err := sp.CompleteStream(ctx, req)
//	if err != nil {
//	    return err
//	}
//	defer stream.Close()
//
//	for stream.Next(ctx) {
//	    ev := stream.Event()
//	    // handle ev
//	}
//	if err := stream.Err(); err != nil {
//	    return err
//	}
type Stream interface {
	// Next advances to the next event. Returns false when the stream ends
	// normally or on error. Always check Err() after Next() returns false.
	Next(ctx context.Context) bool

	// Event returns the current event. Only valid after Next() == true.
	Event() StreamEvent

	// Err returns the first error encountered. Returns nil on clean end.
	// Only meaningful after Next() returned false.
	Err() error

	// Close releases the HTTP connection and stream resources.
	// Safe to call multiple times.
	Close() error
}

// StreamingProvider is optionally implemented by providers that support
// token-by-token text generation.
//
// The agent detects support via type assertion:
//
//	if sp, ok := agent.provider.(goagent.StreamingProvider); ok {
//	    // use streaming
//	}
//
// Not implementing this interface is not an error — the agent automatically
// falls back to Provider.Complete().
type StreamingProvider interface {
	// CompleteStream initiates a streaming completion.
	// Returns a Stream ready for iteration, or an error if the connection failed.
	// The caller is responsible for calling Stream.Close().
	CompleteStream(ctx context.Context, req CompletionRequest) (Stream, error)
}

// StreamHandler is the callback RunStream invokes for each StreamEvent.
// If it returns a non-nil error, RunStream cancels the stream and propagates it.
//
// The handler is invoked synchronously. For heavy work, launch an internal goroutine.
type StreamHandler func(event StreamEvent) error

// TextHandler returns a StreamHandler that writes text deltas to w.
// It is the helper for the most common use case: printing tokens as they arrive.
//
//	result, err := agent.RunStream(ctx, prompt, goagent.TextHandler(os.Stdout))
func TextHandler(w io.Writer) StreamHandler {
	return func(e StreamEvent) error {
		if e.Type != StreamEventText {
			return nil
		}
		_, err := fmt.Fprint(w, e.Text)
		return err
	}
}

// StreamOptions controls the behavior of RunStream.
// Construct it by passing WithStream* functions as variadic arguments.
// Zero-arg calls use the defaults.
type StreamOptions struct {
	// showThinkingText controls whether text the model emits before a tool call
	// ("thinking text") is delivered to the handler.
	//
	// true (default): text is emitted to the handler as normal. It also fires
	// OnThinkingText if configured.
	// false: text is suppressed. See note in RunStream docs on guarantee limits.
	showThinkingText bool
}

// defaultStreamOptions returns the default StreamOptions.
func defaultStreamOptions() StreamOptions {
	return StreamOptions{showThinkingText: true}
}

// StreamOption is a function that modifies StreamOptions.
// It follows the functional-options pattern used throughout goagent.
type StreamOption func(*StreamOptions)

// WithShowThinkingText controls whether text emitted by the model before tool
// calls ("thinking text") is delivered to the handler.
//
//	// Suppress thinking text — handler only receives the final answer
//	agent.RunStream(ctx, prompt, handler, goagent.WithShowThinkingText(false))
//
//	// Show thinking text (default)
//	agent.RunStream(ctx, prompt, handler, goagent.WithShowThinkingText(true))
//	agent.RunStream(ctx, prompt, handler) // equivalent — default=true
func WithShowThinkingText(show bool) StreamOption {
	return func(o *StreamOptions) {
		o.showThinkingText = show
	}
}
