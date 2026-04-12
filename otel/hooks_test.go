package otel_test

import (
	"context"
	"errors"
	"testing"
	"time"

	goagent "github.com/Germanblandin1/goagent"
	otelagent "github.com/Germanblandin1/goagent/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// setupOTel creates an in-memory tracer and meter suitable for testing.
func setupOTel(t *testing.T) (*sdktrace.TracerProvider, *sdkmetric.MeterProvider, *tracetest.InMemoryExporter) {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		_ = mp.Shutdown(context.Background())
	})
	return tp, mp, exp
}

// mockProvider is a minimal goagent.Provider for tests.
type mockProvider struct {
	responses []goagent.CompletionResponse
	idx       int
}

func (m *mockProvider) Complete(_ context.Context, _ goagent.CompletionRequest) (goagent.CompletionResponse, error) {
	if m.idx >= len(m.responses) {
		return goagent.CompletionResponse{}, errors.New("no more responses")
	}
	r := m.responses[m.idx]
	m.idx++
	return r, nil
}

func endTurnResp(text string) goagent.CompletionResponse {
	return goagent.CompletionResponse{
		Message:    goagent.AssistantMessage(text),
		StopReason: goagent.StopReasonEndTurn,
		Usage:      goagent.Usage{InputTokens: 10, OutputTokens: 5},
	}
}

func toolUseResp(id, name string) goagent.CompletionResponse {
	return goagent.CompletionResponse{
		Message: goagent.Message{
			Role: goagent.RoleAssistant,
			ToolCalls: []goagent.ToolCall{
				{ID: id, Name: name, Arguments: map[string]any{}},
			},
		},
		StopReason: goagent.StopReasonToolUse,
		Usage:      goagent.Usage{InputTokens: 20, OutputTokens: 10},
	}
}

func errResp(err error) goagent.CompletionResponse {
	return goagent.CompletionResponse{}
}

// noopTool is a trivial Tool for tests.
type noopTool struct{ name string }

func (t *noopTool) Definition() goagent.ToolDefinition {
	return goagent.ToolDefinition{Name: t.name, Description: "noop"}
}
func (t *noopTool) Execute(_ context.Context, _ map[string]any) ([]goagent.ContentBlock, error) {
	return []goagent.ContentBlock{goagent.TextBlock("ok")}, nil
}

func TestNewHooks_SpanHierarchy(t *testing.T) {
	t.Parallel()

	tp, mp, exp := setupOTel(t)
	hooks, err := otelagent.NewHooks(tp.Tracer("test"), mp.Meter("test"))
	if err != nil {
		t.Fatal(err)
	}

	provider := &mockProvider{responses: []goagent.CompletionResponse{
		toolUseResp("t1", "calc"),
		endTurnResp("done"),
	}}
	agent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithTool(&noopTool{name: "calc"}),
		goagent.WithHooks(hooks),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = agent.Run(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}

	spans := exp.GetSpans()
	if len(spans) == 0 {
		t.Fatal("no spans recorded")
	}

	// Find root span.
	var rootSpan *tracetest.SpanStub
	for i := range spans {
		if spans[i].Name == "goagent.run" {
			rootSpan = &spans[i]
			break
		}
	}
	if rootSpan == nil {
		t.Fatal("goagent.run span not found")
	}

	// Verify child spans share the same trace ID.
	for _, s := range spans {
		if s.SpanContext.TraceID() != rootSpan.SpanContext.TraceID() {
			t.Errorf("span %q has different trace ID than root", s.Name)
		}
	}

	// The tool span should be a child of root.
	var toolSpan *tracetest.SpanStub
	for i := range spans {
		if spans[i].Name == "goagent.tool.calc" {
			toolSpan = &spans[i]
			break
		}
	}
	if toolSpan == nil {
		t.Fatal("goagent.tool.calc span not found")
	}
	if toolSpan.Parent.SpanID() != rootSpan.SpanContext.SpanID() {
		t.Errorf("tool span parent = %v, want root span ID %v",
			toolSpan.Parent.SpanID(), rootSpan.SpanContext.SpanID())
	}
}

func TestNewHooks_RunSpanAttributes(t *testing.T) {
	t.Parallel()

	tp, mp, exp := setupOTel(t)
	hooks, err := otelagent.NewHooks(tp.Tracer("test"), mp.Meter("test"))
	if err != nil {
		t.Fatal(err)
	}

	provider := &mockProvider{responses: []goagent.CompletionResponse{
		endTurnResp("hi"),
	}}
	agent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithHooks(hooks),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = agent.Run(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}

	var runSpan *tracetest.SpanStub
	for i := range exp.GetSpans() {
		s := exp.GetSpans()[i]
		if s.Name == "goagent.run" {
			runSpan = &s
			break
		}
	}
	if runSpan == nil {
		t.Fatal("goagent.run span not found")
	}

	attrMap := make(map[string]any)
	for _, a := range runSpan.Attributes {
		attrMap[string(a.Key)] = a.Value.AsInterface()
	}
	if _, ok := attrMap["iterations"]; !ok {
		t.Error("missing attribute: iterations")
	}
	if _, ok := attrMap["tool_calls"]; !ok {
		t.Error("missing attribute: tool_calls")
	}
}

func TestNewHooks_ErrorPropagation(t *testing.T) {
	t.Parallel()

	tp, mp, exp := setupOTel(t)
	hooks, err := otelagent.NewHooks(tp.Tracer("test"), mp.Meter("test"))
	if err != nil {
		t.Fatal(err)
	}

	provErr := errors.New("provider down")
	type errProvider struct{ err error }
	provider := &struct{ goagent.Provider }{
		Provider: &mockProvider{},
	}
	_ = provider
	// Use a provider that always errors.
	alwaysErr := &alwaysErrProvider{err: provErr}

	agent, err := goagent.New(
		goagent.WithProvider(alwaysErr),
		goagent.WithHooks(hooks),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, runErr := agent.Run(context.Background(), "hello")
	if runErr == nil {
		t.Fatal("expected error, got nil")
	}

	var runSpan *tracetest.SpanStub
	for i := range exp.GetSpans() {
		s := exp.GetSpans()[i]
		if s.Name == "goagent.run" {
			runSpan = &s
			break
		}
	}
	if runSpan == nil {
		t.Fatal("goagent.run span not found")
	}
	// Status should be Error.
	if runSpan.Status.Code.String() != "Error" {
		t.Errorf("span status = %q, want Error", runSpan.Status.Code)
	}
}

// alwaysErrProvider is a Provider that always returns an error.
type alwaysErrProvider struct{ err error }

func (p *alwaysErrProvider) Complete(_ context.Context, _ goagent.CompletionRequest) (goagent.CompletionResponse, error) {
	return goagent.CompletionResponse{}, p.err
}

func TestNewHooks_ConcurrentRuns(t *testing.T) {
	t.Parallel()

	tp, mp, _ := setupOTel(t)
	hooks, err := otelagent.NewHooks(tp.Tracer("test"), mp.Meter("test"))
	if err != nil {
		t.Fatal(err)
	}

	// Use a provider that is safe for concurrent use.
	provider := &concurrentProvider{}
	agent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithHooks(hooks),
	)
	if err != nil {
		t.Fatal(err)
	}

	const n = 3
	errs := make(chan error, n)
	for range n {
		go func() {
			_, err := agent.Run(context.Background(), "concurrent")
			errs <- err
		}()
	}
	for range n {
		if err := <-errs; err != nil {
			t.Errorf("concurrent Run error: %v", err)
		}
	}
}

// concurrentProvider is safe for concurrent use.
type concurrentProvider struct{}

func (p *concurrentProvider) Complete(_ context.Context, _ goagent.CompletionRequest) (goagent.CompletionResponse, error) {
	return endTurnResp("ok"), nil
}

func TestNewHooks_MetricsRED(t *testing.T) {
	t.Parallel()

	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		_ = mp.Shutdown(context.Background())
	})

	hooks, err := otelagent.NewHooks(tp.Tracer("test"), mp.Meter("test"))
	if err != nil {
		t.Fatal(err)
	}

	provider := &mockProvider{responses: []goagent.CompletionResponse{
		endTurnResp("answer"),
	}}
	agent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithHooks(hooks),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = agent.Run(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}

	// Collect metrics via the manual reader.
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("could not collect metrics: %v", err)
	}

	// Verify at least one metric was recorded for the run.
	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "goagent.run.duration" {
				found = true
			}
		}
	}
	if !found {
		t.Error("goagent.run.duration metric not recorded")
	}
}

func TestNewHooks_NoOnRunStart(t *testing.T) {
	t.Parallel()

	tp, mp, _ := setupOTel(t)
	hooks, err := otelagent.NewHooks(tp.Tracer("test"), mp.Meter("test"))
	if err != nil {
		t.Fatal(err)
	}

	// If the caller does NOT wire OnRunStart, inner spans use context.Background()
	// as root. Verify this does not panic.
	innerHooks := goagent.Hooks{
		OnProviderResponse: hooks.OnProviderResponse,
		OnToolResult:       hooks.OnToolResult,
		// OnRunStart intentionally omitted
	}

	provider := &mockProvider{responses: []goagent.CompletionResponse{
		endTurnResp("hi"),
	}}
	agent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithHooks(innerHooks),
	)
	if err != nil {
		t.Fatal(err)
	}

	// Must not panic.
	if _, err := agent.Run(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
}

func TestNewHooks_ProviderSpanAttributes(t *testing.T) {
	t.Parallel()

	tp, mp, exp := setupOTel(t)
	hooks, err := otelagent.NewHooks(tp.Tracer("test"), mp.Meter("test"))
	if err != nil {
		t.Fatal(err)
	}

	provider := &mockProvider{responses: []goagent.CompletionResponse{
		{
			Message:    goagent.AssistantMessage("done"),
			StopReason: goagent.StopReasonEndTurn,
			Usage:      goagent.Usage{InputTokens: 100, OutputTokens: 50},
		},
	}}
	agent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithHooks(hooks),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = agent.Run(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}

	var provSpan *tracetest.SpanStub
	for i := range exp.GetSpans() {
		s := exp.GetSpans()[i]
		if s.Name == "goagent.provider.complete" {
			provSpan = &s
			break
		}
	}
	if provSpan == nil {
		t.Fatal("goagent.provider.complete span not found")
	}

	attrMap := make(map[string]any)
	for _, a := range provSpan.Attributes {
		attrMap[string(a.Key)] = a.Value.AsInterface()
	}
	if attrMap["input_tokens"] != int64(100) {
		t.Errorf("input_tokens = %v, want 100", attrMap["input_tokens"])
	}
	if attrMap["output_tokens"] != int64(50) {
		t.Errorf("output_tokens = %v, want 50", attrMap["output_tokens"])
	}
}

func TestNewHooks_ToolSpanAttributes(t *testing.T) {
	t.Parallel()

	tp, mp, exp := setupOTel(t)
	hooks, err := otelagent.NewHooks(tp.Tracer("test"), mp.Meter("test"))
	if err != nil {
		t.Fatal(err)
	}

	provider := &mockProvider{responses: []goagent.CompletionResponse{
		toolUseResp("t1", "my_tool"),
		endTurnResp("done"),
	}}
	agent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithTool(&noopTool{name: "my_tool"}),
		goagent.WithHooks(hooks),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = agent.Run(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}

	var toolSpan *tracetest.SpanStub
	for i := range exp.GetSpans() {
		s := exp.GetSpans()[i]
		if s.Name == "goagent.tool.my_tool" {
			toolSpan = &s
			break
		}
	}
	if toolSpan == nil {
		t.Fatal("goagent.tool.my_tool span not found")
	}

	attrMap := make(map[string]any)
	for _, a := range toolSpan.Attributes {
		attrMap[string(a.Key)] = a.Value.AsInterface()
	}
	if attrMap["tool.name"] != "my_tool" {
		t.Errorf("tool.name = %v, want my_tool", attrMap["tool.name"])
	}

	// Span duration should be >= 0.
	dur := toolSpan.EndTime.Sub(toolSpan.StartTime)
	if dur < 0 {
		t.Errorf("tool span duration = %v, want >= 0", dur)
	}
}

// Verify that NewHooks returns an error if meter registration fails.
// This test uses a real meter so it should never fail, but the call must not
// panic when called multiple times (idempotent instrument registration).
func TestNewHooks_IdempotentRegistration(t *testing.T) {
	t.Parallel()

	tp, mp, _ := setupOTel(t)
	tracer := tp.Tracer("test")
	meter := mp.Meter("test")

	// Two calls with the same meter should both succeed.
	_, err1 := otelagent.NewHooks(tracer, meter)
	_, err2 := otelagent.NewHooks(tracer, meter)
	if err1 != nil {
		t.Errorf("first call: %v", err1)
	}
	if err2 != nil {
		t.Errorf("second call: %v", err2)
	}
}

// TestNewHooks_MemoryHooksEmitSpans verifies that the four memory hooks
// (OnShortTermLoad, OnShortTermAppend, OnLongTermRetrieve, OnLongTermStore)
// each produce an OTel span with the expected name.
func TestNewHooks_MemoryHooksEmitSpans(t *testing.T) {
	t.Parallel()

	tp, mp, exp := setupOTel(t)
	hooks, err := otelagent.NewHooks(tp.Tracer("test"), mp.Meter("test"))
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Invoke each memory hook directly — they are plain functions stored in the struct.
	hooks.OnShortTermLoad(ctx, 3, time.Millisecond, nil)
	hooks.OnShortTermAppend(ctx, 2, time.Millisecond, nil)
	hooks.OnLongTermRetrieve(ctx, []goagent.ScoredMessage{{Score: 0.9}}, time.Millisecond, nil)
	hooks.OnLongTermStore(ctx, 5, time.Millisecond, nil)

	want := []string{
		"goagent.memory.short_term.load",
		"goagent.memory.short_term.append",
		"goagent.memory.long_term.retrieve",
		"goagent.memory.long_term.store",
	}

	spans := exp.GetSpans()
	recorded := make(map[string]bool, len(spans))
	for _, s := range spans {
		recorded[s.Name] = true
	}

	for _, name := range want {
		if !recorded[name] {
			t.Errorf("span %q not found; recorded: %v", name, spans)
		}
	}
}

// TestNewHooks_MemoryHooks_ErrorSpanStatus verifies that the memory hooks set
// span status to Error when the operation fails.
func TestNewHooks_MemoryHooks_ErrorSpanStatus(t *testing.T) {
	t.Parallel()

	tp, mp, exp := setupOTel(t)
	hooks, err := otelagent.NewHooks(tp.Tracer("test"), mp.Meter("test"))
	if err != nil {
		t.Fatal(err)
	}

	opErr := errors.New("storage unavailable")
	ctx := context.Background()

	hooks.OnShortTermLoad(ctx, 0, time.Millisecond, opErr)
	hooks.OnShortTermAppend(ctx, 0, time.Millisecond, opErr)
	hooks.OnLongTermRetrieve(ctx, nil, time.Millisecond, opErr)
	hooks.OnLongTermStore(ctx, 0, time.Millisecond, opErr)

	for _, s := range exp.GetSpans() {
		if s.Status.Code.String() != "Error" {
			t.Errorf("span %q: status = %q, want Error", s.Name, s.Status.Code)
		}
	}
}

// TestNewHooks_ToolResult_ErrorEmitsMetric verifies that a failed tool call
// increments the tool errors counter.
func TestNewHooks_ToolResult_ErrorEmitsMetric(t *testing.T) {
	t.Parallel()

	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		_ = mp.Shutdown(context.Background())
	})

	hooks, err := otelagent.NewHooks(tp.Tracer("test"), mp.Meter("test"))
	if err != nil {
		t.Fatal(err)
	}

	toolErr := errors.New("tool failed")
	hooks.OnToolResult(context.Background(), "my_tool", nil, time.Millisecond, toolErr)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "goagent.tool.errors" {
				found = true
			}
		}
	}
	if !found {
		t.Error("goagent.tool.errors metric not recorded on tool error")
	}
}

// Ensure the errResp helper compiles (suppress unused warning).
var _ = errResp
var _ = time.Second
