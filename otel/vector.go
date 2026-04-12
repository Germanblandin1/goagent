package otel

import (
	"context"
	"time"

	goagent "github.com/Germanblandin1/goagent"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// NewVectorStoreObserver returns a [goagent.VectorStoreObserver] that emits
// OpenTelemetry spans and records RED metrics for every [goagent.VectorStore]
// operation.
//
// Each operation creates a child span with a retroactive start timestamp so
// the span window matches the actual wall-clock time of the store call.
// Metrics include per-operation latency histograms, a result-count histogram
// for searches, an error counter, and a batch-size histogram for bulk
// operations.
//
// The returned observer is safe for concurrent use.
//
// Typical usage — compose with a logging observer and wrap any store:
//
//	otelObs, err := otel.NewVectorStoreObserver(tracer, meter)
//	store := goagent.NewObservableStore(rawStore,
//	    goagent.MergeVectorStoreObservers(logObs, otelObs),
//	)
func NewVectorStoreObserver(tracer trace.Tracer, meter metric.Meter) (goagent.VectorStoreObserver, error) {
	inst, err := newVectorInstruments(meter)
	if err != nil {
		return goagent.VectorStoreObserver{}, err
	}

	return goagent.VectorStoreObserver{
		OnUpsert: func(ctx context.Context, id string, dur time.Duration, err error) {
			start := time.Now().Add(-dur)
			_, span := tracer.Start(ctx, "goagent.vector.upsert",
				trace.WithTimestamp(start),
			)
			span.SetAttributes(attribute.String("vector.id", id))
			endSpan(span, err)
			inst.upsertDuration.Record(ctx, dur.Seconds())
			if err != nil {
				inst.errors.Add(ctx, 1, metric.WithAttributes(
					attribute.String("operation", "upsert"),
				))
			}
		},

		OnSearch: func(ctx context.Context, topK int, results int, dur time.Duration, err error) {
			start := time.Now().Add(-dur)
			_, span := tracer.Start(ctx, "goagent.vector.search",
				trace.WithTimestamp(start),
			)
			span.SetAttributes(
				attribute.Int("vector.top_k", topK),
				attribute.Int("vector.results", results),
			)
			endSpan(span, err)
			inst.searchDuration.Record(ctx, dur.Seconds())
			inst.searchResults.Record(ctx, int64(results))
			if err != nil {
				inst.errors.Add(ctx, 1, metric.WithAttributes(
					attribute.String("operation", "search"),
				))
			}
		},

		OnDelete: func(ctx context.Context, id string, dur time.Duration, err error) {
			start := time.Now().Add(-dur)
			_, span := tracer.Start(ctx, "goagent.vector.delete",
				trace.WithTimestamp(start),
			)
			span.SetAttributes(attribute.String("vector.id", id))
			endSpan(span, err)
			inst.deleteDuration.Record(ctx, dur.Seconds())
			if err != nil {
				inst.errors.Add(ctx, 1, metric.WithAttributes(
					attribute.String("operation", "delete"),
				))
			}
		},

		OnBulkUpsert: func(ctx context.Context, count int, dur time.Duration, err error) {
			start := time.Now().Add(-dur)
			_, span := tracer.Start(ctx, "goagent.vector.bulk_upsert",
				trace.WithTimestamp(start),
			)
			span.SetAttributes(attribute.Int("vector.count", count))
			endSpan(span, err)
			inst.upsertDuration.Record(ctx, dur.Seconds())
			inst.bulkSize.Record(ctx, int64(count), metric.WithAttributes(
				attribute.String("operation", "upsert"),
			))
			if err != nil {
				inst.errors.Add(ctx, 1, metric.WithAttributes(
					attribute.String("operation", "bulk_upsert"),
				))
			}
		},

		OnBulkDelete: func(ctx context.Context, count int, dur time.Duration, err error) {
			start := time.Now().Add(-dur)
			_, span := tracer.Start(ctx, "goagent.vector.bulk_delete",
				trace.WithTimestamp(start),
			)
			span.SetAttributes(attribute.Int("vector.count", count))
			endSpan(span, err)
			inst.deleteDuration.Record(ctx, dur.Seconds())
			inst.bulkSize.Record(ctx, int64(count), metric.WithAttributes(
				attribute.String("operation", "delete"),
			))
			if err != nil {
				inst.errors.Add(ctx, 1, metric.WithAttributes(
					attribute.String("operation", "bulk_delete"),
				))
			}
		},
	}, nil
}

// endSpan sets the span status and ends it at the current time.
func endSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}
	span.End()
}
