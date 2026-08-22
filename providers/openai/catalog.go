package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/Germanblandin1/goagent"
)

// rawModelsResponse mirrors the GET {baseURL}/models payload. We decode the raw
// JSON ourselves instead of using go-openai's ListModels because the SDK maps
// the listing onto its vanilla OpenAI Model type (id/created/owned_by) and
// discards the rich metadata that OpenAI-compatible endpoints such as OpenRouter
// return: pricing, context length, supported parameters and modalities.
type rawModelsResponse struct {
	Data []rawModel `json:"data"`
}

// rawModel is one entry of the models listing. Fields absent from a given
// provider (e.g. the official OpenAI API omits pricing and context length)
// simply stay at their zero value / nil, which maps to "unknown".
type rawModel struct {
	ID                  string           `json:"id"`
	Name                string           `json:"name,omitempty"`
	ContextLength       int              `json:"context_length,omitempty"`
	Architecture        *rawArchitecture `json:"architecture,omitempty"`
	Pricing             *rawPricing      `json:"pricing,omitempty"`
	TopProvider         *rawTopProvider  `json:"top_provider,omitempty"`
	SupportedParameters []string         `json:"supported_parameters,omitempty"`
}

type rawArchitecture struct {
	InputModalities []string `json:"input_modalities,omitempty"`
}

// rawPricing holds OpenRouter-style pricing: strings in USD per single token
// (a flat "0" everywhere means the model is free), plus a fixed per-request fee.
type rawPricing struct {
	Prompt     string `json:"prompt,omitempty"`
	Completion string `json:"completion,omitempty"`
	Request    string `json:"request,omitempty"`
}

type rawTopProvider struct {
	ContextLength       int `json:"context_length,omitempty"`
	MaxCompletionTokens int `json:"max_completion_tokens,omitempty"`
}

// Models lists the models exposed by the configured endpoint via GET
// {baseURL}/models, decoding the raw payload so metadata beyond the model id is
// preserved. For OpenAI-compatible endpoints like OpenRouter the listing already
// carries everything (pricing, context length, capabilities), so each returned
// ModelInfo is fully populated in this single call. For the official OpenAI API,
// which reports only ids, the extra fields stay empty/nil and callers degrade.
//
// The result is cached so ModelInfo can resolve a single model without another
// round trip.
//
// Models implements goagent.ModelCatalog.
func (p *Provider) Models(ctx context.Context) ([]goagent.ModelInfo, error) {
	catalog, err := p.fetchCatalog(ctx)
	if err != nil {
		return nil, err
	}

	p.cacheMu.Lock()
	p.modelCache = catalog
	p.cacheMu.Unlock()

	return catalog, nil
}

// ModelInfo returns the metadata for a single model, reusing the catalog fetched
// by Models (or fetching it on first use) rather than making a per-model call.
// If the model is not present in the listing it returns ModelInfo{Name: model}
// so callers still get a usable value.
//
// ModelInfo implements goagent.ModelCatalog.
func (p *Provider) ModelInfo(ctx context.Context, model string) (goagent.ModelInfo, error) {
	p.cacheMu.RLock()
	cached := p.modelCache
	p.cacheMu.RUnlock()

	if cached == nil {
		catalog, err := p.fetchCatalog(ctx)
		if err != nil {
			return goagent.ModelInfo{}, err
		}
		p.cacheMu.Lock()
		p.modelCache = catalog
		p.cacheMu.Unlock()
		cached = catalog
	}

	for _, mi := range cached {
		if mi.Name == model {
			return mi, nil
		}
	}
	return goagent.ModelInfo{Name: model}, nil
}

// fetchCatalog performs the raw GET {baseURL}/models and maps the payload to
// goagent.ModelInfo values. Errors are wrapped so RetryProvider can classify
// them: a non-2xx status becomes a *StatusError (retryable on 429/5xx) and a
// transport failure a *TransportError.
func (p *Provider) fetchCatalog(ctx context.Context) ([]goagent.ModelInfo, error) {
	url := strings.TrimRight(p.baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("openai list models: %w", err)
	}
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai list models: %w", &TransportError{Cause: err})
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("openai list models: %w", &StatusError{
			StatusCode: resp.StatusCode,
			Cause:      fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body))),
		})
	}

	var raw rawModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("openai list models: decoding response: %w", err)
	}

	out := make([]goagent.ModelInfo, 0, len(raw.Data))
	for _, m := range raw.Data {
		out = append(out, m.toModelInfo())
	}
	return out, nil
}

// toModelInfo maps a raw listing entry to a goagent.ModelInfo, filling only what
// the provider reported and leaving the rest at its zero value.
func (m rawModel) toModelInfo() goagent.ModelInfo {
	info := goagent.ModelInfo{
		Name:          m.ID,
		DisplayName:   m.Name,
		ContextLength: m.ContextLength,
		Capabilities:  m.capabilities(),
		Pricing:       m.Pricing.toPricing(),
	}
	if m.TopProvider != nil {
		if info.ContextLength == 0 {
			info.ContextLength = m.TopProvider.ContextLength
		}
		info.MaxOutputTokens = m.TopProvider.MaxCompletionTokens
	}
	return info
}

// capabilities derives goagent capabilities from the provider's
// supported_parameters and input modalities. Only the cross-provider features
// with a named Capability are mapped; unknown parameters are ignored.
func (m rawModel) capabilities() []goagent.Capability {
	var caps []goagent.Capability
	for _, param := range m.SupportedParameters {
		switch param {
		case "tools":
			caps = append(caps, goagent.CapabilityTools)
		case "reasoning":
			caps = append(caps, goagent.CapabilityThinking)
		}
	}
	if m.Architecture != nil && slices.Contains(m.Architecture.InputModalities, "image") {
		caps = append(caps, goagent.CapabilityVision)
	}
	return caps
}

// toPricing converts OpenRouter-style per-token USD strings to goagent.Pricing
// normalized to USD per one million tokens. A nil receiver (pricing block absent)
// yields nil ("unknown"). A price string that fails to parse is ignored, leaving
// that amount at 0 rather than panicking.
func (p *rawPricing) toPricing() *goagent.Pricing {
	if p == nil {
		return nil
	}
	out := &goagent.Pricing{Currency: "USD"}
	if v, err := strconv.ParseFloat(p.Prompt, 64); err == nil {
		out.InputPerMTok = v * 1e6
	}
	if v, err := strconv.ParseFloat(p.Completion, 64); err == nil {
		out.OutputPerMTok = v * 1e6
	}
	if v, err := strconv.ParseFloat(p.Request, 64); err == nil {
		out.RequestUSD = v
	}
	return out
}
