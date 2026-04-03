// Package otel translates goagent hook events into OpenTelemetry spans and
// metrics, following the RED pattern (Rate, Errors, Duration).
//
// # Usage
//
// Create a tracer and meter from your OTel provider, call [NewHooks], and
// pass the result to [goagent.WithHooks]:
//
//	hooks, err := otel.NewHooks(tracer, meter)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	agent, err := goagent.New(
//	    goagent.WithProvider(provider),
//	    goagent.WithHooks(hooks),
//	)
//
// If the caller's ctx already carries an active trace (e.g. from an HTTP
// handler), the spans created by goagent will be children of it automatically:
//
//	ctx, span := tracer.Start(r.Context(), "handle_request")
//	defer span.End()
//	result, err := agent.Run(ctx, prompt)
//
// # Span hierarchy
//
// For each Run call the package emits the following span tree:
//
//	goagent.run  (root, or child of caller's span)
//	  ├── goagent.provider.complete  (one per LLM call)
//	  ├── goagent.tool.<name>        (one per tool execution)
//	  ├── goagent.memory.short_term.load
//	  ├── goagent.memory.short_term.append
//	  ├── goagent.memory.long_term.retrieve
//	  └── goagent.memory.long_term.store
//
// # RED metrics
//
// | Instrument | Name | Unit |
// |---|---|---|
// | Run duration | goagent.run.duration | s |
// | Run errors | goagent.run.errors | {error} |
// | Provider duration | goagent.provider.duration | s |
// | Provider input tokens | goagent.provider.tokens.input | {token} |
// | Provider output tokens | goagent.provider.tokens.output | {token} |
// | Tool duration | goagent.tool.duration | s |
// | Tool errors | goagent.tool.errors | {error} |
// | Memory load duration | goagent.memory.load.duration | s |
// | Memory append duration | goagent.memory.append.duration | s |
//
// # Concurrency
//
// [NewHooks] is safe for concurrent use. Multiple simultaneous Run calls on
// the same agent each produce an independent span tree — there is no shared
// mutable state between calls. Metric instruments are thread-safe by OTel SDK
// design.
package otel
