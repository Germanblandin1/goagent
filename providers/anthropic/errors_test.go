package anthropic_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/providers/anthropic"
)

func TestStatusError_IsTransient(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code int
		want bool
	}{
		{http.StatusTooManyRequests, true},     // 429
		{http.StatusInternalServerError, true}, // 500
		{http.StatusBadGateway, true},          // 502
		{http.StatusServiceUnavailable, true},  // 503
		{http.StatusGatewayTimeout, true},      // 504
		{http.StatusBadRequest, false},         // 400
		{http.StatusNotFound, false},           // 404
		{http.StatusUnauthorized, false},       // 401
	}
	for _, tc := range tests {
		err := &anthropic.StatusError{StatusCode: tc.code, Cause: errors.New("boom")}
		if got := err.IsTransient(); got != tc.want {
			t.Errorf("StatusError{%d}.IsTransient() = %v, want %v", tc.code, got, tc.want)
		}
	}
}

func TestStatusError_UnwrapAndMessage(t *testing.T) {
	t.Parallel()

	cause := errors.New("overloaded_error")
	err := &anthropic.StatusError{StatusCode: 529, Cause: cause}
	// Message is preserved unchanged (no added prefix).
	if got := err.Error(); got != cause.Error() {
		t.Errorf("Error() = %q, want %q", got, cause.Error())
	}
	// errors.Is traverses the wrapper.
	if !errors.Is(err, cause) {
		t.Error("errors.Is(err, cause) = false, want true")
	}
}

func TestTransportError_IsTransient(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		cause error
		want  bool
	}{
		{"context canceled", context.Canceled, false},
		{"deadline exceeded", context.DeadlineExceeded, true},
		{"eof", io.EOF, true},
		{"connection refused", errors.New("dial tcp: connection refused"), true},
		{"wrapped canceled", fmt.Errorf("Post \"...\": %w", context.Canceled), false},
	}
	for _, tc := range tests {
		err := &anthropic.TransportError{Cause: tc.cause}
		if got := err.IsTransient(); got != tc.want {
			t.Errorf("%s: IsTransient() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestComplete_ClassifiesHTTPError verifies Complete returns an error that
// resolves to *StatusError carrying the server's status code. A 404 is used
// because the Anthropic SDK does not retry 4xx, keeping the test fast.
func TestComplete_ClassifiesHTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	p := anthropic.NewWithClient(anthropic.NewClient(anthropic.WithAPIKey("test"), anthropic.WithBaseURL(srv.URL)))
	_, err := p.Complete(context.Background(), goagent.CompletionRequest{
		Model:    "claude-sonnet-4",
		Messages: []goagent.Message{{Role: goagent.RoleUser, Content: []goagent.ContentBlock{goagent.TextBlock("hi")}}},
	})

	var se *anthropic.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("error is not *StatusError: %v", err)
	}
	if se.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want %d", se.StatusCode, http.StatusNotFound)
	}
	if se.IsTransient() {
		t.Error("IsTransient() = true, want false for 404")
	}
}

// countingProvider records how many times Complete is called and always fails
// with err. Implements goagent.Provider.
type countingProvider struct {
	err   error
	calls int
}

func (p *countingProvider) Complete(_ context.Context, _ goagent.CompletionRequest) (goagent.CompletionResponse, error) {
	p.calls++
	return goagent.CompletionResponse{}, p.err
}

// TestRetryProvider_ClassifiesAnthropicStatusErrors is the acceptance case: with
// the default policy, RetryProvider retries a 503 up to the attempt budget but
// stops immediately on a 404.
func TestRetryProvider_ClassifiesAnthropicStatusErrors(t *testing.T) {
	t.Parallel()

	policy := goagent.RetryPolicy{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     2 * time.Millisecond,
	}

	t.Run("retries 503", func(t *testing.T) {
		t.Parallel()
		fp := &countingProvider{err: &anthropic.StatusError{StatusCode: 503, Cause: errors.New("overloaded")}}
		rp := goagent.RetryProvider(fp, policy)
		if _, err := rp.Complete(context.Background(), goagent.CompletionRequest{}); err == nil {
			t.Fatal("expected error, got nil")
		}
		if fp.calls != 3 {
			t.Errorf("calls = %d, want 3 (503 is transient, retried to budget)", fp.calls)
		}
	})

	t.Run("does not retry 404", func(t *testing.T) {
		t.Parallel()
		fp := &countingProvider{err: &anthropic.StatusError{StatusCode: 404, Cause: errors.New("not found")}}
		rp := goagent.RetryProvider(fp, policy)
		if _, err := rp.Complete(context.Background(), goagent.CompletionRequest{}); err == nil {
			t.Fatal("expected error, got nil")
		}
		if fp.calls != 1 {
			t.Errorf("calls = %d, want 1 (404 is permanent, not retried)", fp.calls)
		}
	})
}
