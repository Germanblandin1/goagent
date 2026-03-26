package mcp

import "log/slog"

// Router dispatches tool calls to the Client that registered that tool.
// It is constructed once with NewRouter and is immutable — safe for concurrent use.
type Router struct {
	routes map[string]*Client // tool name → owning client
}

// NewRouter builds the tool-name → client mapping.
// If two clients register a tool with the same name, the last one wins.
// Duplicate names are logged as Warning — callers must ensure unique tool names.
func NewRouter(logger *slog.Logger, clients ...*Client) *Router {
	r := &Router{routes: make(map[string]*Client, len(clients)*4)}
	for _, c := range clients {
		for _, tool := range c.Tools() {
			name := tool.Definition().Name
			if _, exists := r.routes[name]; exists {
				logger.Warn("mcp router: duplicate tool name, overwriting",
					"tool", name,
				)
			}
			r.routes[name] = c
		}
	}
	return r
}

// ClientFor returns the Client that has registered the tool with the given name.
// Returns nil, false if no client knows that tool.
func (r *Router) ClientFor(name string) (*Client, bool) {
	c, ok := r.routes[name]
	return c, ok
}
