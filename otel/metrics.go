package otel

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// instruments holds all OTel metric instruments registered for goagent.
// Each field maps to a single metric in the RED pattern.
type instruments struct {
	// Run
	runDuration metric.Float64Histogram
	runErrors   metric.Int64Counter

	// Provider
	providerDuration  metric.Float64Histogram
	providerTokensIn  metric.Int64Counter
	providerTokensOut metric.Int64Counter

	// Tool
	toolDuration metric.Float64Histogram
	toolErrors   metric.Int64Counter

	// Memory
	memoryLoadDuration   metric.Float64Histogram
	memoryAppendDuration metric.Float64Histogram
}

// vectorInstruments holds OTel metric instruments for [VectorStore] operations.
type vectorInstruments struct {
	// Latency histograms per operation
	upsertDuration metric.Float64Histogram
	searchDuration metric.Float64Histogram
	deleteDuration metric.Float64Histogram

	// Distribution of result counts returned by Search
	searchResults metric.Int64Histogram

	// Error counter shared across operations; "operation" attribute distinguishes them
	errors metric.Int64Counter

	// Distribution of batch sizes for BulkUpsert / BulkDelete;
	// "operation" attribute distinguishes upsert vs delete
	bulkSize metric.Int64Histogram
}

// newInstruments registers all metric instruments with the given meter.
// Returns an error if any instrument registration fails.
func newInstruments(meter metric.Meter) (instruments, error) {
	var inst instruments
	var err error

	inst.runDuration, err = meter.Float64Histogram(
		"goagent.run.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Wall-clock duration of each Run call."),
	)
	if err != nil {
		return instruments{}, fmt.Errorf("otel: register goagent.run.duration: %w", err)
	}

	inst.runErrors, err = meter.Int64Counter(
		"goagent.run.errors",
		metric.WithUnit("{error}"),
		metric.WithDescription("Number of Run calls that returned an error."),
	)
	if err != nil {
		return instruments{}, fmt.Errorf("otel: register goagent.run.errors: %w", err)
	}

	inst.providerDuration, err = meter.Float64Histogram(
		"goagent.provider.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Wall-clock duration of each Provider.Complete call."),
	)
	if err != nil {
		return instruments{}, fmt.Errorf("otel: register goagent.provider.duration: %w", err)
	}

	inst.providerTokensIn, err = meter.Int64Counter(
		"goagent.provider.tokens.input",
		metric.WithUnit("{token}"),
		metric.WithDescription("Cumulative input tokens sent to the provider."),
	)
	if err != nil {
		return instruments{}, fmt.Errorf("otel: register goagent.provider.tokens.input: %w", err)
	}

	inst.providerTokensOut, err = meter.Int64Counter(
		"goagent.provider.tokens.output",
		metric.WithUnit("{token}"),
		metric.WithDescription("Cumulative output tokens received from the provider."),
	)
	if err != nil {
		return instruments{}, fmt.Errorf("otel: register goagent.provider.tokens.output: %w", err)
	}

	inst.toolDuration, err = meter.Float64Histogram(
		"goagent.tool.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Wall-clock duration of each tool execution."),
	)
	if err != nil {
		return instruments{}, fmt.Errorf("otel: register goagent.tool.duration: %w", err)
	}

	inst.toolErrors, err = meter.Int64Counter(
		"goagent.tool.errors",
		metric.WithUnit("{error}"),
		metric.WithDescription("Number of tool executions that returned an error."),
	)
	if err != nil {
		return instruments{}, fmt.Errorf("otel: register goagent.tool.errors: %w", err)
	}

	inst.memoryLoadDuration, err = meter.Float64Histogram(
		"goagent.memory.load.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Wall-clock duration of memory load operations."),
	)
	if err != nil {
		return instruments{}, fmt.Errorf("otel: register goagent.memory.load.duration: %w", err)
	}

	inst.memoryAppendDuration, err = meter.Float64Histogram(
		"goagent.memory.append.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Wall-clock duration of memory append/store operations."),
	)
	if err != nil {
		return instruments{}, fmt.Errorf("otel: register goagent.memory.append.duration: %w", err)
	}

	return inst, nil
}

// newVectorInstruments registers all VectorStore metric instruments with the
// given meter. Returns an error if any registration fails.
func newVectorInstruments(meter metric.Meter) (vectorInstruments, error) {
	var inst vectorInstruments
	var err error

	inst.upsertDuration, err = meter.Float64Histogram(
		"goagent.vector.upsert.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Wall-clock duration of each VectorStore.Upsert call."),
	)
	if err != nil {
		return vectorInstruments{}, fmt.Errorf("otel: register goagent.vector.upsert.duration: %w", err)
	}

	inst.searchDuration, err = meter.Float64Histogram(
		"goagent.vector.search.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Wall-clock duration of each VectorStore.Search call."),
	)
	if err != nil {
		return vectorInstruments{}, fmt.Errorf("otel: register goagent.vector.search.duration: %w", err)
	}

	inst.deleteDuration, err = meter.Float64Histogram(
		"goagent.vector.delete.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Wall-clock duration of each VectorStore.Delete call."),
	)
	if err != nil {
		return vectorInstruments{}, fmt.Errorf("otel: register goagent.vector.delete.duration: %w", err)
	}

	inst.searchResults, err = meter.Int64Histogram(
		"goagent.vector.search.results",
		metric.WithUnit("{message}"),
		metric.WithDescription("Number of results returned by each VectorStore.Search call."),
	)
	if err != nil {
		return vectorInstruments{}, fmt.Errorf("otel: register goagent.vector.search.results: %w", err)
	}

	inst.errors, err = meter.Int64Counter(
		"goagent.vector.errors",
		metric.WithUnit("{error}"),
		metric.WithDescription("Number of VectorStore operations that returned an error."),
	)
	if err != nil {
		return vectorInstruments{}, fmt.Errorf("otel: register goagent.vector.errors: %w", err)
	}

	inst.bulkSize, err = meter.Int64Histogram(
		"goagent.vector.bulk.size",
		metric.WithUnit("{entry}"),
		metric.WithDescription("Number of entries per BulkUpsert or BulkDelete call."),
	)
	if err != nil {
		return vectorInstruments{}, fmt.Errorf("otel: register goagent.vector.bulk.size: %w", err)
	}

	return inst, nil
}
