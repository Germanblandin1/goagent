package otel_test

import (
	"context"
	"fmt"
	"log"

	goagent "github.com/Germanblandin1/goagent"
	otelagent "github.com/Germanblandin1/goagent/otel"
	"go.opentelemetry.io/otel/metric/noop"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
)

// ExampleNewHooks demonstrates wiring OTel hooks into a goagent Agent.
// In production, replace the noop provider and meter with real SDK instances
// (e.g. from go.opentelemetry.io/otel/sdk/trace and sdk/metric).
func ExampleNewHooks() {
	tracer := nooptrace.NewTracerProvider().Tracer("example")
	meter := noop.NewMeterProvider().Meter("example")

	hooks, err := otelagent.NewHooks(tracer, meter)
	if err != nil {
		log.Fatal(err)
	}

	// In production the ctx would come from your HTTP handler or gRPC
	// interceptor, and may already carry an active trace:
	//   ctx, span := tracer.Start(r.Context(), "handle_user_request")
	//   defer span.End()
	ctx := context.Background()

	agent, err := goagent.New(
		goagent.WithProvider(&concurrentProvider{}),
		goagent.WithHooks(hooks),
	)
	if err != nil {
		log.Fatal(err)
	}

	result, err := agent.Run(ctx, "hello")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result)
	// Output: ok
}
