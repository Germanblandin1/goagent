package goagent

import "context"

// runContexts bundles the two contexts used throughout a single Run call.
//
// io is the original caller context — the one that carries cancellation
// semantics. All blocking I/O (provider.Complete, tool dispatch, memory
// reads/writes) uses this context so that caller-initiated cancellation is
// respected.
//
// hook is the context returned by OnRunStart. It may carry additional values
// set by hook consumers (e.g. an OTel trace span embedded by the otel sub-
// package). It is passed exclusively to hook callbacks and never to I/O
// operations, so that a hook cannot inadvertently affect the agent's I/O
// flow by returning a context it controls or that is already cancelled.
type runContexts struct {
	io   context.Context // cancellation + I/O
	hook context.Context // hook callbacks only (OTel spans, etc.)
}

// hookCtxKey is the unexported key used to embed the hook context as a value
// inside the io context before entering the dispatch chain.
type hookCtxKey struct{}

// withHookEmbedded returns rctx.io with rctx.hook stored as a context.Value.
// This allows middlewares inside the dispatch chain (e.g. circuitBreakerMiddleware)
// to extract the hook context for their hook callbacks without ever receiving
// it as a cancellation parent — the io cancellation semantics are preserved.
func (rctx runContexts) withHookEmbedded() context.Context {
	return context.WithValue(rctx.io, hookCtxKey{}, rctx.hook)
}

// hookFromCtx extracts the hook context embedded by withHookEmbedded.
// The returned context carries OTel spans and other hook values set by OnRunStart.
//
// For use only within hook callback invocations — never pass the returned
// context to I/O operations (provider.Complete, tool.Execute, memory
// reads/writes). For I/O, always use the original ctx from the function
// parameter.
//
// Falls back to ctx itself when called outside of a Run or when OnRunStart
// was not configured — hooks receive a valid context in both cases.
func hookFromCtx(ctx context.Context) context.Context {
	if h, ok := ctx.Value(hookCtxKey{}).(context.Context); ok {
		return h
	}
	return ctx
}
