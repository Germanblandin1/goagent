package session

import "context"

type contextKey struct{}

// WithID returns a new context carrying the given session ID.
// Agent.Run injects the agent's name here when WithName is configured;
// InMemoryStore.Search uses this value to filter results to a single session.
func WithID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// IDFromContext extracts the session ID injected by WithID.
// Returns ("", false) when no session ID is present.
func IDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(contextKey{}).(string)
	return id, ok && id != ""
}
