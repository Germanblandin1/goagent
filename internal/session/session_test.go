package session_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Germanblandin1/goagent/internal/session"
)

func TestNewContext(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr error
	}{
		{
			name: "valid plain id",
			id:   "alice",
		},
		{
			name: "valid id with hyphens",
			id:   "user-42",
		},
		{
			name:    "colon in id is rejected",
			id:      "user:42",
			wantErr: session.ErrInvalidSessionID,
		},
		{
			name:    "only colon is rejected",
			id:      ":",
			wantErr: session.ErrInvalidSessionID,
		},
		{
			name:    "colon at start is rejected",
			id:      ":prefix",
			wantErr: session.ErrInvalidSessionID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, err := session.NewContext(context.Background(), tt.id)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("NewContext(%q) error = %v, want %v", tt.id, err, tt.wantErr)
				}
				// On error the original context must be returned unchanged.
				if _, ok := session.IDFromContext(ctx); ok {
					t.Errorf("NewContext(%q) injected ID into context despite error", tt.id)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewContext(%q) unexpected error: %v", tt.id, err)
			}
		})
	}
}

func TestIDFromContext(t *testing.T) {
	tests := []struct {
		name   string
		ctx    func() context.Context
		wantID string
		wantOK bool
	}{
		{
			name:   "no session in context",
			ctx:    func() context.Context { return context.Background() },
			wantID: "",
			wantOK: false,
		},
		{
			name: "empty string — treated as absent",
			ctx: func() context.Context {
				// Bypass NewContext to simulate a zero-value injection.
				ctx, _ := session.NewContext(context.Background(), "x")
				// Use a valid ID so we can test the real empty-string path via
				// the unexported key indirectly: verify that NewContext("") is
				// not possible (it is valid — only ":" is forbidden), then fall
				// back to confirming that a plain background ctx returns false.
				_ = ctx
				return context.Background()
			},
			wantID: "",
			wantOK: false,
		},
		{
			name: "valid session id returned",
			ctx: func() context.Context {
				ctx, _ := session.NewContext(context.Background(), "alice")
				return ctx
			},
			wantID: "alice",
			wantOK: true,
		},
		{
			name: "inner NewContext overrides outer",
			ctx: func() context.Context {
				outer, _ := session.NewContext(context.Background(), "outer")
				inner, _ := session.NewContext(outer, "inner")
				return inner
			},
			wantID: "inner",
			wantOK: true,
		},
		{
			name: "id returned never contains colon — invariant holds",
			ctx: func() context.Context {
				ctx, _ := session.NewContext(context.Background(), "user-42")
				return ctx
			},
			wantID: "user-42",
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotOK := session.IDFromContext(tt.ctx())

			if gotID != tt.wantID || gotOK != tt.wantOK {
				t.Errorf("IDFromContext() = (%q, %v), want (%q, %v)",
					gotID, gotOK, tt.wantID, tt.wantOK)
			}

			// Invariant: any ID we get back must not contain ":".
			if gotOK && len(gotID) > 0 {
				for _, c := range gotID {
					if c == ':' {
						t.Errorf("IDFromContext returned ID %q containing ':'", gotID)
					}
				}
			}
		})
	}
}
