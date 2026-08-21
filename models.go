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
type ModelInfo struct {
	// Name is the provider-specific model identifier (e.g. "llama3.2").
	Name string
	// Capabilities lists the features the model reports as supported.
	Capabilities []Capability
}

// Supports reports whether the model advertises capability c.
func (m ModelInfo) Supports(c Capability) bool {
	return slices.Contains(m.Capabilities, c)
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
