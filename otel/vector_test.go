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

// ── Minimal VectorStore stubs ────────────────────────────────────────────────

type vectorStub struct {
	searchResults []goagent.ScoredMessage
	err           error
}

func (s *vectorStub) Upsert(_ context.Context, _ string, _ []float32, _ goagent.Message) error {
	return s.err
}

func (s *vectorStub) Search(_ context.Context, _ []float32, _ int, _ ...goagent.SearchOption) ([]goagent.ScoredMessage, error) {
	return s.searchResults, s.err
}

func (s *vectorStub) Delete(_ context.Context, _ string) error {
	return s.err
}

type bulkVectorStub struct {
	vectorStub
	bulkErr error
}

func (s *bulkVectorStub) BulkUpsert(_ context.Context, _ []goagent.UpsertEntry) error {
	return s.bulkErr
}

func (s *bulkVectorStub) BulkDelete(_ context.Context, _ []string) error {
	return s.bulkErr
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func setupVectorOTel(t *testing.T) (*sdktrace.TracerProvider, *sdkmetric.MeterProvider, *tracetest.InMemoryExporter, *sdkmetric.ManualReader) {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		_ = mp.Shutdown(context.Background())
	})
	return tp, mp, exp, reader
}

func findSpan(spans tracetest.SpanStubs, name string) *tracetest.SpanStub {
	for i := range spans {
		if spans[i].Name == name {
			return &spans[i]
		}
	}
	return nil
}

func collectMetricNames(t *testing.T, reader *sdkmetric.ManualReader) map[string]bool {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	names := make(map[string]bool)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			names[m.Name] = true
		}
	}
	return names
}

// ── Tests ────────────────────────────────────────────────────────────────────

func TestNewVectorStoreObserver_SpansEmittedForAllOps(t *testing.T) {
	t.Parallel()

	tp, mp, exp, _ := setupVectorOTel(t)
	obs, err := otelagent.NewVectorStoreObserver(tp.Tracer("test"), mp.Meter("test"))
	if err != nil {
		t.Fatal(err)
	}

	inner := &vectorStub{
		searchResults: []goagent.ScoredMessage{{Score: 0.9}},
	}
	store := goagent.NewObservableStore(inner, obs)
	ctx := context.Background()

	_ = store.Upsert(ctx, "id1", []float32{0.1}, goagent.UserMessage("msg"))
	_, _ = store.Search(ctx, []float32{0.1}, 3)
	_ = store.Delete(ctx, "id1")

	spans := exp.GetSpans()
	for _, name := range []string{
		"goagent.vector.upsert",
		"goagent.vector.search",
		"goagent.vector.delete",
	} {
		if findSpan(spans, name) == nil {
			t.Errorf("span %q not found", name)
		}
	}
}

func TestNewVectorStoreObserver_SearchSpanAttributes(t *testing.T) {
	t.Parallel()

	tp, mp, exp, _ := setupVectorOTel(t)
	obs, err := otelagent.NewVectorStoreObserver(tp.Tracer("test"), mp.Meter("test"))
	if err != nil {
		t.Fatal(err)
	}

	inner := &vectorStub{
		searchResults: []goagent.ScoredMessage{{Score: 0.8}, {Score: 0.7}},
	}
	store := goagent.NewObservableStore(inner, obs)
	_, _ = store.Search(context.Background(), []float32{0.1}, 5)

	span := findSpan(exp.GetSpans(), "goagent.vector.search")
	if span == nil {
		t.Fatal("goagent.vector.search span not found")
	}

	attrMap := make(map[string]any)
	for _, a := range span.Attributes {
		attrMap[string(a.Key)] = a.Value.AsInterface()
	}

	if attrMap["vector.top_k"] != int64(5) {
		t.Errorf("vector.top_k = %v, want 5", attrMap["vector.top_k"])
	}
	if attrMap["vector.results"] != int64(2) {
		t.Errorf("vector.results = %v, want 2", attrMap["vector.results"])
	}
}

func TestNewVectorStoreObserver_ErrorSpanStatus(t *testing.T) {
	t.Parallel()

	tp, mp, exp, _ := setupVectorOTel(t)
	obs, err := otelagent.NewVectorStoreObserver(tp.Tracer("test"), mp.Meter("test"))
	if err != nil {
		t.Fatal(err)
	}

	inner := &vectorStub{err: errors.New("qdrant down")}
	store := goagent.NewObservableStore(inner, obs)
	_, _ = store.Search(context.Background(), nil, 1)

	span := findSpan(exp.GetSpans(), "goagent.vector.search")
	if span == nil {
		t.Fatal("goagent.vector.search span not found")
	}
	if span.Status.Code.String() != "Error" {
		t.Errorf("span status = %q, want Error", span.Status.Code)
	}
}

func TestNewVectorStoreObserver_RetroactiveTimestamp(t *testing.T) {
	t.Parallel()

	tp, mp, exp, _ := setupVectorOTel(t)
	obs, err := otelagent.NewVectorStoreObserver(tp.Tracer("test"), mp.Meter("test"))
	if err != nil {
		t.Fatal(err)
	}

	store := goagent.NewObservableStore(&vectorStub{}, obs)

	before := time.Now()
	_ = store.Upsert(context.Background(), "id1", nil, goagent.UserMessage(""))
	after := time.Now()

	span := findSpan(exp.GetSpans(), "goagent.vector.upsert")
	if span == nil {
		t.Fatal("span not found")
	}
	if span.StartTime.Before(before) || span.StartTime.After(after) {
		t.Errorf("span start %v not in [%v, %v]", span.StartTime, before, after)
	}
}

func TestNewVectorStoreObserver_MetricsRecorded(t *testing.T) {
	t.Parallel()

	tp, mp, _, reader := setupVectorOTel(t)
	obs, err := otelagent.NewVectorStoreObserver(tp.Tracer("test"), mp.Meter("test"))
	if err != nil {
		t.Fatal(err)
	}

	inner := &vectorStub{searchResults: []goagent.ScoredMessage{{Score: 0.9}}}
	store := goagent.NewObservableStore(inner, obs)
	ctx := context.Background()

	_ = store.Upsert(ctx, "id1", nil, goagent.UserMessage(""))
	_, _ = store.Search(ctx, nil, 3)
	_ = store.Delete(ctx, "id1")

	names := collectMetricNames(t, reader)

	for _, want := range []string{
		"goagent.vector.upsert.duration",
		"goagent.vector.search.duration",
		"goagent.vector.delete.duration",
		"goagent.vector.search.results",
	} {
		if !names[want] {
			t.Errorf("metric %q not recorded", want)
		}
	}
}

func TestNewVectorStoreObserver_ErrorMetricRecorded(t *testing.T) {
	t.Parallel()

	tp, mp, _, reader := setupVectorOTel(t)
	obs, err := otelagent.NewVectorStoreObserver(tp.Tracer("test"), mp.Meter("test"))
	if err != nil {
		t.Fatal(err)
	}

	inner := &vectorStub{err: errors.New("timeout")}
	store := goagent.NewObservableStore(inner, obs)
	_, _ = store.Search(context.Background(), nil, 1)

	names := collectMetricNames(t, reader)
	if !names["goagent.vector.errors"] {
		t.Error("goagent.vector.errors metric not recorded on error")
	}
}

func TestNewVectorStoreObserver_BulkSpansEmitted(t *testing.T) {
	t.Parallel()

	tp, mp, exp, _ := setupVectorOTel(t)
	obs, err := otelagent.NewVectorStoreObserver(tp.Tracer("test"), mp.Meter("test"))
	if err != nil {
		t.Fatal(err)
	}

	inner := &bulkVectorStub{}
	store := goagent.NewObservableStore(inner, obs)
	bulk := store.(goagent.BulkVectorStore)
	ctx := context.Background()

	_ = bulk.BulkUpsert(ctx, []goagent.UpsertEntry{
		{ID: "a", Message: goagent.UserMessage("a")},
		{ID: "b", Message: goagent.UserMessage("b")},
	})
	_ = bulk.BulkDelete(ctx, []string{"a", "b"})

	spans := exp.GetSpans()
	for _, name := range []string{
		"goagent.vector.bulk_upsert",
		"goagent.vector.bulk_delete",
	} {
		if findSpan(spans, name) == nil {
			t.Errorf("span %q not found", name)
		}
	}
}

func TestNewVectorStoreObserver_IdempotentRegistration(t *testing.T) {
	t.Parallel()

	tp, mp, _, _ := setupVectorOTel(t)
	tracer := tp.Tracer("test")
	meter := mp.Meter("test")

	_, err1 := otelagent.NewVectorStoreObserver(tracer, meter)
	_, err2 := otelagent.NewVectorStoreObserver(tracer, meter)
	if err1 != nil {
		t.Errorf("first call: %v", err1)
	}
	if err2 != nil {
		t.Errorf("second call: %v", err2)
	}
}
