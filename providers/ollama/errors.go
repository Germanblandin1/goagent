package ollama

import (
	"context"
	"errors"
	"fmt"

	"github.com/Germanblandin1/goagent"
)

// StatusError is returned when the Ollama server responds with a non-200 HTTP
// status. It preserves the status code and the server-reported error body so
// callers can inspect the failure, and it implements goagent.TransientError so
// RetryProvider retries retryable statuses (429, 5xx) but not permanent ones (4xx).
type StatusError struct {
	// StatusCode is the HTTP status returned by the server.
	StatusCode int
	// Body is the error message decoded from the response body's {"error": ...}
	// field, or empty when the server sent no such message.
	Body string
}

// Error renders the same message the provider produced before StatusError
// existed: "ollama: status <code>: <body>", or "ollama: status <code>" when the
// body is empty.
func (e *StatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("ollama: status %d", e.StatusCode)
	}
	return fmt.Sprintf("ollama: status %d: %s", e.StatusCode, e.Body)
}

// IsTransient implements goagent.TransientError: 429 and 5xx are retryable,
// all other statuses (notably 4xx) are permanent.
func (e *StatusError) IsTransient() bool {
	return goagent.HTTPStatusIsTransient(e.StatusCode)
}

// TransportError wraps a low-level failure from the HTTP client (connection
// refused, timeout, reset, EOF) that occurs before any HTTP status is received.
// Such failures are usually transient, so it implements goagent.TransientError
// accordingly — with one exception: context.Canceled reflects caller intent,
// not a fault, and is reported as non-transient so it is never retried.
type TransportError struct {
	// Cause is the underlying transport error.
	Cause error
}

// Error preserves the underlying error's message unchanged (the provider
// returned these raw before TransportError existed).
func (e *TransportError) Error() string { return e.Cause.Error() }

// Unwrap exposes the underlying error so errors.Is / errors.As keep working
// (e.g. errors.Is(err, context.Canceled)).
func (e *TransportError) Unwrap() error { return e.Cause }

// IsTransient implements goagent.TransientError. Caller-initiated cancellation
// (context.Canceled) is not transient; everything else — timeouts
// (context.DeadlineExceeded), net.Error, io.EOF — is treated as transient.
func (e *TransportError) IsTransient() bool {
	return !errors.Is(e.Cause, context.Canceled)
}

// wrapTransport wraps a transport-level error so it self-classifies for retry.
// A nil error is returned unchanged.
func wrapTransport(err error) error {
	if err == nil {
		return nil
	}
	return &TransportError{Cause: err}
}
