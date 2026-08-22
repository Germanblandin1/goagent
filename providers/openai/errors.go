package openai

import (
	"context"
	"errors"

	openaiSDK "github.com/sashabaranov/go-openai"

	"github.com/Germanblandin1/goagent"
)

// StatusError wraps an error returned by the OpenAI API that carries an HTTP
// status code. It implements goagent.TransientError so RetryProvider retries
// retryable statuses (429, 5xx) but fails fast on permanent ones (4xx).
type StatusError struct {
	// StatusCode is the HTTP status returned by the API.
	StatusCode int
	// Cause is the underlying SDK error (an *openai.APIError or
	// *openai.RequestError); its message is preserved unchanged.
	Cause error
}

// Error preserves the underlying SDK error message unchanged.
func (e *StatusError) Error() string { return e.Cause.Error() }

// Unwrap exposes the underlying SDK error so errors.Is / errors.As keep working.
func (e *StatusError) Unwrap() error { return e.Cause }

// IsTransient implements goagent.TransientError: 429 and 5xx are retryable,
// all other statuses (notably 4xx) are permanent.
func (e *StatusError) IsTransient() bool {
	return goagent.HTTPStatusIsTransient(e.StatusCode)
}

// TransportError wraps a low-level failure that occurs before any HTTP status is
// received (connection refused, timeout, reset, EOF). Such failures are usually
// transient, so it implements goagent.TransientError accordingly — with one
// exception: context.Canceled reflects caller intent, not a fault, and is
// reported as non-transient so it is never retried.
type TransportError struct {
	// Cause is the underlying transport error.
	Cause error
}

// Error preserves the underlying error's message unchanged.
func (e *TransportError) Error() string { return e.Cause.Error() }

// Unwrap exposes the underlying error so errors.Is / errors.As keep working
// (e.g. errors.Is(err, context.Canceled)).
func (e *TransportError) Unwrap() error { return e.Cause }

// IsTransient implements goagent.TransientError. Caller-initiated cancellation
// (context.Canceled) is not transient; everything else is treated as transient.
func (e *TransportError) IsTransient() bool {
	return !errors.Is(e.Cause, context.Canceled)
}

// classifyError wraps an error returned by the go-openai SDK so it self-classifies
// for RetryProvider. Errors carrying an HTTP status (*openai.APIError,
// *openai.RequestError) become a *StatusError keyed on that status; anything else
// is treated as a transport-level failure. A nil error is returned unchanged.
func classifyError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *openaiSDK.APIError
	if errors.As(err, &apiErr) {
		return &StatusError{StatusCode: apiErr.HTTPStatusCode, Cause: err}
	}
	var reqErr *openaiSDK.RequestError
	if errors.As(err, &reqErr) {
		return &StatusError{StatusCode: reqErr.HTTPStatusCode, Cause: err}
	}
	return &TransportError{Cause: err}
}
