package ollama_test

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
	"github.com/Germanblandin1/goagent/providers/ollama"
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
		err := &ollama.StatusError{StatusCode: tc.code}
		if got := err.IsTransient(); got != tc.want {
			t.Errorf("StatusError{%d}.IsTransient() = %v, want %v", tc.code, got, tc.want)
		}
	}
}

func TestStatusError_Error(t *testing.T) {
	t.Parallel()

	withBody := &ollama.StatusError{StatusCode: 400, Body: "model not found"}
	if got, want := withBody.Error(), "ollama: status 400: model not found"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	noBody := &ollama.StatusError{StatusCode: 500}
	if got, want := noBody.Error(), "ollama: status 500"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
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
		{"wrapped canceled", fmt.Errorf("Get \"...\": %w", context.Canceled), false},
	}
	for _, tc := range tests {
		err := &ollama.TransportError{Cause: tc.cause}
		if got := err.IsTransient(); got != tc.want {
			t.Errorf("%s: IsTransient() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestTransportError_UnwrapAndMessage(t *testing.T) {
	t.Parallel()

	err := &ollama.TransportError{Cause: context.Canceled}
	// Message is preserved unchanged (no added prefix).
	if got := err.Error(); got != context.Canceled.Error() {
		t.Errorf("Error() = %q, want %q", got, context.Canceled.Error())
	}
	// errors.Is traverses the wrapper.
	if !errors.Is(err, context.Canceled) {
		t.Error("errors.Is(err, context.Canceled) = false, want true")
	}
}

// TestClient_StatusErrorType verifies the client returns a *StatusError that
// self-classifies, so RetryProvider can act on it.
func TestClient_StatusErrorType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code          int
		wantTransient bool
	}{
		{http.StatusServiceUnavailable, true}, // 503
		{http.StatusNotFound, false},          // 404
	}
	for _, tc := range tests {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.code)
		}))
		t.Cleanup(srv.Close)

		client := ollama.NewClient(ollama.WithBaseURL(srv.URL))
		var out struct{}
		err := ollama.DoRequest(client, context.Background(), "/test", nil, &out)

		var se *ollama.StatusError
		if !errors.As(err, &se) {
			t.Fatalf("code %d: error is not *StatusError: %v", tc.code, err)
		}
		if se.StatusCode != tc.code {
			t.Errorf("StatusCode = %d, want %d", se.StatusCode, tc.code)
		}
		if se.IsTransient() != tc.wantTransient {
			t.Errorf("code %d: IsTransient() = %v, want %v", tc.code, se.IsTransient(), tc.wantTransient)
		}
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

// TestRetryProvider_ClassifiesOllamaStatusErrors is the acceptance case: with
// the default policy (no Retryable hook), RetryProvider retries a 500 up to the
// attempt budget but stops immediately on a 404.
func TestRetryProvider_ClassifiesOllamaStatusErrors(t *testing.T) {
	t.Parallel()

	policy := goagent.RetryPolicy{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     2 * time.Millisecond,
	}

	t.Run("retries 500", func(t *testing.T) {
		t.Parallel()
		fp := &countingProvider{err: &ollama.StatusError{StatusCode: 500}}
		rp := goagent.RetryProvider(fp, policy)
		_, err := rp.Complete(context.Background(), goagent.CompletionRequest{})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if fp.calls != 3 {
			t.Errorf("calls = %d, want 3 (500 is transient, retried to budget)", fp.calls)
		}
	})

	t.Run("does not retry 404", func(t *testing.T) {
		t.Parallel()
		fp := &countingProvider{err: &ollama.StatusError{StatusCode: 404}}
		rp := goagent.RetryProvider(fp, policy)
		_, err := rp.Complete(context.Background(), goagent.CompletionRequest{})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if fp.calls != 1 {
			t.Errorf("calls = %d, want 1 (404 is permanent, not retried)", fp.calls)
		}
	})
}
