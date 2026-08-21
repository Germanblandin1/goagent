package ollama

import (
	"context"

	"github.com/Germanblandin1/goagent"
)

// ollamaTagsResponse is the payload of GET /api/tags.
type ollamaTagsResponse struct {
	Models []struct {
		Name  string `json:"name"`
		Model string `json:"model"`
	} `json:"models"`
}

// ollamaShowResponse is the subset of POST /api/show we consume. Ollama returns
// much more (modelfile, parameters, template, details); we only need the
// capabilities array, e.g. ["completion","tools","thinking"].
type ollamaShowResponse struct {
	Capabilities []string `json:"capabilities"`
}

// Models lists the models installed on the Ollama server via GET /api/tags.
//
// The returned ModelInfo values carry Name only; Capabilities is left empty
// because /api/tags does not report capabilities. Call ModelInfo to resolve the
// capabilities of a specific model (one /api/show round trip, cached).
//
// Models implements goagent.ModelCatalog.
func (p *Provider) Models(ctx context.Context) ([]goagent.ModelInfo, error) {
	var resp ollamaTagsResponse
	if err := p.client.doGet(ctx, "/api/tags", &resp); err != nil {
		return nil, err
	}

	out := make([]goagent.ModelInfo, 0, len(resp.Models))
	for _, m := range resp.Models {
		out = append(out, goagent.ModelInfo{Name: m.Name})
	}
	return out, nil
}

// ModelInfo returns the capabilities of a single model via POST /api/show.
//
// Results are cached per model: the first call for a given model hits the
// network, subsequent calls reuse the cached value. The cache is unbounded but
// keyed by model name, so it is bounded by the number of distinct models used.
//
// ModelInfo implements goagent.ModelCatalog.
func (p *Provider) ModelInfo(ctx context.Context, model string) (goagent.ModelInfo, error) {
	if info, ok := p.cachedModelInfo(model); ok {
		return info, nil
	}

	var resp ollamaShowResponse
	if err := p.client.do(ctx, "/api/show", map[string]string{"model": model}, &resp); err != nil {
		return goagent.ModelInfo{}, err
	}

	caps := make([]goagent.Capability, 0, len(resp.Capabilities))
	for _, c := range resp.Capabilities {
		caps = append(caps, goagent.Capability(c))
	}
	info := goagent.ModelInfo{Name: model, Capabilities: caps}

	p.capsMu.Lock()
	p.capsCache[model] = info
	p.capsMu.Unlock()

	return info, nil
}

// cachedModelInfo returns the cached ModelInfo for model, if present.
func (p *Provider) cachedModelInfo(model string) (goagent.ModelInfo, bool) {
	p.capsMu.RLock()
	defer p.capsMu.RUnlock()
	info, ok := p.capsCache[model]
	return info, ok
}

// supportsThinking reports whether model advertises the thinking capability.
// On any error resolving capabilities (network failure, unknown model) it
// returns false so callers degrade to not requesting thinking rather than
// failing the request — Ollama returns HTTP 400 if `think` is sent to a model
// that does not support it.
func (p *Provider) supportsThinking(ctx context.Context, model string) bool {
	info, err := p.ModelInfo(ctx, model)
	if err != nil {
		return false
	}
	return info.Supports(goagent.CapabilityThinking)
}
