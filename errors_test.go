package goagent_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Germanblandin1/goagent"
)

func TestMaxIterationsError_Error(t *testing.T) {
	t.Parallel()

	err := &goagent.MaxIterationsError{Iterations: 5, LastThought: "thinking..."}
	got := err.Error()

	if !strings.Contains(got, "5") {
		t.Errorf("Error() = %q, want to contain iteration count 5", got)
	}
	if !strings.Contains(got, "thinking...") {
		t.Errorf("Error() = %q, want to contain last thought", got)
	}
}

func TestToolExecutionError_ErrorAndUnwrap(t *testing.T) {
	t.Parallel()

	cause := errors.New("division by zero")
	err := &goagent.ToolExecutionError{
		ToolName: "calc",
		Args:     map[string]any{"a": 1},
		Cause:    cause,
	}

	got := err.Error()
	if !strings.Contains(got, "calc") {
		t.Errorf("Error() = %q, want to contain tool name", got)
	}
	if !strings.Contains(got, "division by zero") {
		t.Errorf("Error() = %q, want to contain cause", got)
	}
	if err.Unwrap() != cause {
		t.Errorf("Unwrap() = %v, want %v", err.Unwrap(), cause)
	}
	if !errors.Is(err, cause) {
		t.Error("errors.Is should match the cause")
	}
}

func TestUnsupportedContentError_ErrorAndUnwrap(t *testing.T) {
	t.Parallel()

	err := &goagent.UnsupportedContentError{
		ContentType: goagent.ContentDocument,
		Provider:    "ollama",
		Reason:      "not supported",
	}

	got := err.Error()
	if !strings.Contains(got, "ollama") {
		t.Errorf("Error() = %q, want to contain provider name", got)
	}
	if !strings.Contains(got, string(goagent.ContentDocument)) {
		t.Errorf("Error() = %q, want to contain content type", got)
	}

	if !errors.Is(err, goagent.ErrUnsupportedContent) {
		t.Error("errors.Is(err, ErrUnsupportedContent) should be true")
	}

	var target *goagent.UnsupportedContentError
	if !errors.As(err, &target) {
		t.Error("errors.As should match *UnsupportedContentError")
	}
}

func TestProviderError_Error(t *testing.T) {
	t.Parallel()

	cause := errors.New("connection refused")

	t.Run("with provider name", func(t *testing.T) {
		t.Parallel()
		err := &goagent.ProviderError{Provider: "ollama", Cause: cause}
		got := err.Error()
		if !strings.Contains(got, "ollama") {
			t.Errorf("Error() = %q, want to contain provider name", got)
		}
		if !strings.Contains(got, "connection refused") {
			t.Errorf("Error() = %q, want to contain cause", got)
		}
	})

	t.Run("without provider name", func(t *testing.T) {
		t.Parallel()
		err := &goagent.ProviderError{Provider: "", Cause: cause}
		got := err.Error()
		if strings.Contains(got, `""`) {
			t.Errorf("Error() = %q, should not contain empty quoted provider", got)
		}
		if !strings.Contains(got, "connection refused") {
			t.Errorf("Error() = %q, want to contain cause", got)
		}
	})
}

func TestProviderError_Unwrap(t *testing.T) {
	t.Parallel()

	cause := errors.New("timeout")
	err := &goagent.ProviderError{Provider: "anthropic", Cause: cause}

	if err.Unwrap() != cause {
		t.Errorf("Unwrap() = %v, want %v", err.Unwrap(), cause)
	}
	if !errors.Is(err, cause) {
		t.Error("errors.Is should match the cause")
	}
}
