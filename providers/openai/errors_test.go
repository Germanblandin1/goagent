package openai_test

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
	"github.com/Germanblandin1/goagent/providers/openai"
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
		err := &openai.StatusError{StatusCode: tc.code, Cause: errors.New("boom")}
		if got := err.IsTransient(); got != tc.want {
			t.Errorf("StatusError{%d}.IsTransient() = %v, want %v", tc.code, got, tc.want)
		}
	}
}

func TestStatusError_UnwrapAndMessage(t *testing.T) {
	t.Parallel()

	cause := errors.New("rate limit reached")
	err := &openai.StatusError{StatusCode: 429, Cause: cause}
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
		err := &openai.TransportError{Cause: tc.cause}
		if got := err.IsTransient(); got != tc.want {
			t.Errorf("%s: IsTransient() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestComplete_ClassifiesHTTPErrors verifies Complete returns an error that
// resolves to *StatusError carrying the server's status code, so RetryProvider
// can distinguish transient (5xx/429) from permanent (4xx) failures.
func TestComplete_ClassifiesHTTPErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code          int
		wantTransient bool
	}{
		{http.StatusTooManyRequests, true}, // 429
		{http.StatusNotFound, false},       // 404
	}
	for _, tc := range tests {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.code)
		}))
		t.Cleanup(srv.Close)

		p := openai.New(openai.WithAPIKey("test"), openai.WithBaseURL(srv.URL))
		_, err := p.Complete(context.Background(), goagent.CompletionRequest{
			Model:    "gpt-4o",
			Messages: []goagent.Message{{Role: goagent.RoleUser, Content: []goagent.ContentBlock{goagent.TextBlock("hi")}}},
		})

		var se *openai.StatusError
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

// TestRetryProvider_ClassifiesOpenAIStatusErrors is the acceptance case: with the
// default policy, RetryProvider retries a 500 up to the attempt budget but stops
// immediately on a 400.
func TestRetryProvider_ClassifiesOpenAIStatusErrors(t *testing.T) {
	t.Parallel()

	policy := goagent.RetryPolicy{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     2 * time.Millisecond,
	}

	t.Run("retries 500", func(t *testing.T) {
		t.Parallel()
		fp := &countingProvider{err: &openai.StatusError{StatusCode: 500, Cause: errors.New("server error")}}
		rp := goagent.RetryProvider(fp, policy)
		if _, err := rp.Complete(context.Background(), goagent.CompletionRequest{}); err == nil {
			t.Fatal("expected error, got nil")
		}
		if fp.calls != 3 {
			t.Errorf("calls = %d, want 3 (500 is transient, retried to budget)", fp.calls)
		}
	})

	t.Run("does not retry 400", func(t *testing.T) {
		t.Parallel()
		fp := &countingProvider{err: &openai.StatusError{StatusCode: 400, Cause: errors.New("bad request")}}
		rp := goagent.RetryProvider(fp, policy)
		if _, err := rp.Complete(context.Background(), goagent.CompletionRequest{}); err == nil {
			t.Fatal("expected error, got nil")
		}
		if fp.calls != 1 {
			t.Errorf("calls = %d, want 1 (400 is permanent, not retried)", fp.calls)
		}
	})
}
