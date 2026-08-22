package goagent

import (
	"context"
	"slices"
)

// Capability names a feature a model may support. Providers report the
// capabilities of a model through ModelInfo so callers can adapt behaviour
// (for example, only enabling extended thinking on models that support it)
// instead of hard-coding per-model assumptions.
//
// The named constants cover the common, cross-provider features. Providers may
// also report provider-specific capabilities as Capability values that have no
// named constant; compare against the raw string in that case.
type Capability string

const (
	// CapabilityCompletion indicates the model can generate text completions.
	CapabilityCompletion Capability = "completion"
	// CapabilityTools indicates the model can call tools (function calling).
	CapabilityTools Capability = "tools"
	// CapabilityThinking indicates the model supports extended thinking /
	// reasoning (goagent's WithThinking and WithEffort).
	CapabilityThinking Capability = "thinking"
	// CapabilityVision indicates the model can accept image input.
	CapabilityVision Capability = "vision"
	// CapabilityEmbedding indicates the model can produce embeddings.
	CapabilityEmbedding Capability = "embedding"
)

// ModelInfo describes a model exposed by a provider and the capabilities it
// reports. Capabilities may be empty when the information was obtained from a
// cheap listing (see ModelCatalog.Models); use ModelCatalog.ModelInfo to
// resolve the capabilities of a specific model.
//
// Beyond Name and Capabilities, the remaining fields carry best-effort model
// metadata: each provider fills what it can report and leaves the rest at its
// zero value. A zero value therefore means "unknown", not a factual zero — a
// consumer must degrade gracefully (e.g. hide a column, skip a cost estimate)
// rather than treat 0 as a real limit or price. The one exception is Pricing,
// where nil means "unknown" but a non-nil block with all-zero amounts means the
// model is genuinely free (see IsFree).
type ModelInfo struct {
	// Name is the provider-specific model identifier (e.g. "llama3.2").
	Name string
	// Capabilities lists the features the model reports as supported.
	Capabilities []Capability

	// DisplayName is a human-readable label for the model as reported by the
	// provider (e.g. "GPT-4o"). Empty when the provider does not report one;
	// callers should fall back to Name.
	DisplayName string
	// ContextLength is the model's context window in tokens. 0 means unknown.
	ContextLength int
	// MaxOutputTokens is the maximum number of tokens the model can emit in a
	// single response. 0 means unknown.
	MaxOutputTokens int
	// Pricing describes the model's cost. nil means unknown; it does NOT mean
	// free — use IsFree to detect a genuinely free model.
	Pricing *Pricing
}

// Pricing describes the cost of using a model, normalized to USD per one
// million tokens so a consumer can compute cost = usage × pricing against a
// usage ledger without per-provider unit conversion. Providers that report
// costs in other units (e.g. OpenRouter reports USD per token) convert to this
// representation.
type Pricing struct {
	// InputPerMTok is the cost in Currency per 1,000,000 input (prompt) tokens.
	InputPerMTok float64
	// OutputPerMTok is the cost in Currency per 1,000,000 output (completion)
	// tokens.
	OutputPerMTok float64
	// RequestUSD is a fixed cost charged per request, when the provider applies
	// one; 0 when there is none.
	RequestUSD float64
	// Currency is the ISO currency code for the amounts above. Empty is treated
	// as "USD".
	Currency string
}

// Supports reports whether the model advertises capability c.
func (m ModelInfo) Supports(c Capability) bool {
	return slices.Contains(m.Capabilities, c)
}

// IsFree reports whether the model is known to be free of charge: its Pricing
// is populated (non-nil) and every amount is zero. It returns false when
// Pricing is nil, because unknown pricing must not be reported as free — that
// distinction is what keeps a cost ledger from booking a false zero.
func (m ModelInfo) IsFree() bool {
	return m.Pricing != nil &&
		m.Pricing.InputPerMTok == 0 &&
		m.Pricing.OutputPerMTok == 0 &&
		m.Pricing.RequestUSD == 0
}

// ModelCatalog is optionally implemented by providers that can enumerate their
// models and report per-model capabilities at runtime.
//
// Support is detected via type assertion, the same pattern as StreamingProvider:
//
//	if cat, ok := provider.(goagent.ModelCatalog); ok {
//	    models, err := cat.Models(ctx)
//	    // ...
//	}
//
// Providers that cannot introspect their models simply do not implement this
// interface; that is not an error.
type ModelCatalog interface {
	// Models lists the models available to the provider. The returned
	// ModelInfo values carry at least Name; Capabilities may be empty when
	// filling them would require a per-model round trip. Call ModelInfo to
	// resolve the capabilities of a specific model.
	Models(ctx context.Context) ([]ModelInfo, error)

	// ModelInfo returns the capabilities of a single model. Implementations
	// should cache the result so repeated calls for the same model do not
	// repeatedly hit the network.
	ModelInfo(ctx context.Context, model string) (ModelInfo, error)
}
