package goagent

import (
	"errors"
	"fmt"
)

// ErrToolNotFound is returned when the model requests a tool that was not
// registered with the agent.
var ErrToolNotFound = errors.New("tool not found")

// MaxIterationsError is returned by Run when the agent exhausts its iteration
// budget without producing a final answer.
type MaxIterationsError struct {
	// Iterations is the budget that was exhausted (set by WithMaxIterations).
	Iterations int

	// LastThought is the text content of the model's last assistant message
	// before the budget ran out. It is empty if the model's last turn
	// produced only tool calls with no accompanying text. Useful for
	// debugging runaway loops or surfacing a partial answer to the user.
	LastThought string
}

func (e *MaxIterationsError) Error() string {
	return fmt.Sprintf("agent reached max iterations (%d): last thought: %s",
		e.Iterations, e.LastThought)
}

// ToolExecutionError wraps an error returned by a tool's Execute method,
// adding the tool name and arguments for diagnosis.
type ToolExecutionError struct {
	ToolName string
	Args     map[string]any
	Cause    error
}

func (e *ToolExecutionError) Error() string {
	return fmt.Sprintf("tool %q failed: %v", e.ToolName, e.Cause)
}

// Unwrap enables errors.Is and errors.As to inspect the underlying cause.
func (e *ToolExecutionError) Unwrap() error { return e.Cause }

// ErrUnsupportedContent is returned when the provider does not support a
// content type present in the request (e.g. documents on an OpenAI-compatible
// provider, or images on a text-only model).
var ErrUnsupportedContent = errors.New("unsupported content type")

// UnsupportedContentError provides detail about which content type is not
// supported and by which provider. It wraps ErrUnsupportedContent so callers
// can match with errors.Is(err, ErrUnsupportedContent).
type UnsupportedContentError struct {
	ContentType ContentType
	Provider    string
	Reason      string
}

func (e *UnsupportedContentError) Error() string {
	return fmt.Sprintf("provider %q does not support %s content: %s",
		e.Provider, e.ContentType, e.Reason)
}

// Unwrap returns ErrUnsupportedContent so errors.Is works.
func (e *UnsupportedContentError) Unwrap() error { return ErrUnsupportedContent }

// ErrInvalidMediaType is returned when a content block has a MIME type that
// is not in the set of supported types for its content kind.
var ErrInvalidMediaType = errors.New("invalid media type")

// ProviderError wraps an error returned by a Provider's Complete method.
type ProviderError struct {
	Provider string
	Cause    error
}

func (e *ProviderError) Error() string {
	if e.Provider != "" {
		return fmt.Sprintf("provider %q: %v", e.Provider, e.Cause)
	}
	return fmt.Sprintf("provider: %v", e.Cause)
}

// Unwrap enables errors.Is and errors.As to inspect the underlying cause.
func (e *ProviderError) Unwrap() error { return e.Cause }
