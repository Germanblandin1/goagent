package session

import (
	"context"
	"errors"
	"strings"
)

type contextKey struct{}

// ErrInvalidSessionID is returned by NewContext when the session ID contains
// the reserved separator character ":". IDs with ":" would silently corrupt
// the "sessionID:baseID:chunkIndex" format used by LongTermMemory and
// InMemoryStore to scope vector entries per session.
var ErrInvalidSessionID = errors.New("session: ID must not contain ':'")

// NewContext returns a new context carrying the given session ID.
// Returns ErrInvalidSessionID if id contains ":".
//
// Agent.Run injects the agent name here when WithName is configured.
// InMemoryStore.Search uses this value to filter results to a single session
// by matching the "sessionID:" prefix of stored entry IDs.
func NewContext(ctx context.Context, id string) (context.Context, error) {
	if strings.Contains(id, ":") {
		return ctx, ErrInvalidSessionID
	}
	return context.WithValue(ctx, contextKey{}, id), nil
}

// IDFromContext extracts the session ID injected by NewContext.
// Returns ("", false) when no session ID is present or when the stored ID is
// the empty string.
//
// Invariant: a session ID returned here never contains ":", because NewContext
// rejects such IDs at injection time. Callers that build entry IDs of the form
// "sessionID:baseID:chunkIndex" can rely on this guarantee — the first ":"
// always marks the boundary between the session prefix and the base ID.
func IDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(contextKey{}).(string)
	return id, ok && id != ""
}
