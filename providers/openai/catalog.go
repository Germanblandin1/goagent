package openai

import (
	"context"
	"fmt"

	"github.com/Germanblandin1/goagent"
)

// Models lists the models exposed by the configured endpoint via GET /models.
//
// The returned ModelInfo values carry Name only; Capabilities is left empty
// because the OpenAI /models listing does not report capabilities in a
// standard, cross-provider way. This also covers OpenAI-compatible services
// that mirror the same /v1/models endpoint (e.g. OpenRouter).
//
// Models implements goagent.ModelCatalog.
func (p *Provider) Models(ctx context.Context) ([]goagent.ModelInfo, error) {
	list, err := p.client.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("openai list models: %w", classifyError(err))
	}

	out := make([]goagent.ModelInfo, 0, len(list.Models))
	for _, m := range list.Models {
		out = append(out, goagent.ModelInfo{Name: m.ID})
	}
	return out, nil
}

// ModelInfo returns information about a single model.
//
// The OpenAI (and OpenAI-compatible) /models endpoint does not report
// per-model capabilities in a standard way, so the result carries Name only
// with empty Capabilities, mirroring Models. No network call is made.
//
// ModelInfo implements goagent.ModelCatalog.
func (p *Provider) ModelInfo(_ context.Context, model string) (goagent.ModelInfo, error) {
	return goagent.ModelInfo{Name: model}, nil
}
