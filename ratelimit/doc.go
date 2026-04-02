// Package ratelimit provides token-bucket rate limiters for goagent tool
// dispatch. It wraps [golang.org/x/time/rate] and plugs into the agent via
// [goagent.WithDispatchMiddleware].
//
// Two flavours are provided:
//
//   - [Middleware]: a single shared limiter across all tool calls.
//   - [PerTool]: independent limiters per tool name.
//
// Both return (DispatchMiddleware, error). Use the Must* variants when
// panicking on invalid parameters is acceptable (e.g. top-level var blocks).
//
// # Usage
//
//	// Global: 5 tool calls/sec, burst 10.
//	agent, _ := goagent.New(
//	    goagent.WithProvider(provider),
//	    goagent.WithDispatchMiddleware(ratelimit.MustMiddleware(5, 10)),
//	)
//
//	// Per-tool: each tool gets its own 2 rps / burst 5 limiter.
//	agent, _ := goagent.New(
//	    goagent.WithProvider(provider),
//	    goagent.WithDispatchMiddleware(ratelimit.MustPerTool(2, 5)),
//	)
//
// The middleware blocks until a token is available or the context is
// cancelled. This prevents runaway agents from saturating external APIs
// while still respecting context deadlines and cancellation.
package ratelimit
