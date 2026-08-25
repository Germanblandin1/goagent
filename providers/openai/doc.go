// Package openai provides a [goagent.Provider] backed by the OpenAI
// Chat Completions API.
//
// # Authentication
//
// By default the provider reads the API key from the OPENAI_API_KEY
// environment variable. Supply it explicitly via [WithAPIKey]:
//
//	p := openai.New(openai.WithAPIKey("sk-..."))
//
// # Base URL
//
// The default base URL is https://api.openai.com/v1.
// Override it with [WithBaseURL] to target a proxy, Azure OpenAI, or any
// OpenAI-compatible endpoint:
//
//	p := openai.New(openai.WithBaseURL("https://my-proxy.internal/v1"))
//
// # Supported models
//
// Any model available through the Chat Completions API can be used
// (gpt-4o, gpt-4o-mini, o1, o3, o3-mini, o4-mini, etc.).
// The model is selected at the agent level via [goagent.WithModel].
//
// # Reasoning effort
//
// For o-series reasoning models, map the effort level to
// [goagent.CompletionRequest.Effort] ("low", "medium", "high").
// It is forwarded as reasoning_effort in the request:
//
//	agent, _ := goagent.New(
//	    goagent.WithProvider(openai.New()),
//	    goagent.WithModel("o3-mini"),
//	    goagent.WithEffort("high"),
//	)
//
// # Streaming
//
// The provider implements [goagent.StreamingProvider]: [Provider.CompleteStream]
// delivers text tokens over SSE as they arrive and translates tool calls to the
// shared stream events.
//
// When the upstream is an OpenAI-compatible API that surfaces reasoning, those
// tokens are delivered as [goagent.StreamEventThinking] events, kept separate
// from the final text. Two conventions are supported: DeepSeek's
// reasoning_content and OpenRouter's reasoning field. The official OpenAI API
// exposes neither, so it emits no thinking events.
//
// Streaming requests set stream_options.include_usage, so the terminal
// StreamEventDone reports prompt and completion tokens as [goagent.Usage]
// (InputTokens/OutputTokens), matching the non-streaming [Provider.Complete].
//
// # Model catalog
//
// The provider implements [goagent.ModelCatalog]: [Provider.Models] and
// [Provider.ModelInfo] list the models exposed by the configured endpoint via
// GET {baseURL}/models, and enrich each [goagent.ModelInfo] with best-effort
// metadata (DisplayName, ContextLength, MaxOutputTokens, Pricing, Capabilities).
//
// Note on portability: the /models envelope ({"data": [...]}) is shared across
// OpenAI-compatible backends, but the rich metadata is NOT part of the OpenAI
// standard — the fields decoded here (context_length, pricing, top_provider,
// supported_parameters, architecture.input_modalities) follow OpenRouter's
// listing convention. Consequently:
//
//   - Against OpenRouter, every ModelInfo is fully populated in a single call.
//   - Against the official OpenAI API, which reports only model ids, the extra
//     fields stay empty/nil ("unknown") and callers degrade — pricing and
//     capabilities cannot be discovered from OpenAI's /models at all.
//   - Against other OpenAI-compatible backends, whatever fields match this
//     convention are filled; the rest degrade to Name only.
//
// The capability mapping (supported_parameters "tools"->CapabilityTools,
// "reasoning"->CapabilityThinking, and an "image" input modality->CapabilityVision)
// likewise reflects OpenRouter's semantics, not an OpenAI-wide contract. If a
// future backend reports metadata under a different schema, it may warrant a
// dedicated provider rather than extending this decoder.
//
// # Retry classification
//
// API failures are returned as typed errors that implement
// [goagent.TransientError]: [StatusError] (a non-2xx response, keyed on status)
// and [TransportError] (a pre-status transport failure). A [goagent.RetryProvider]
// wrapping this provider then retries only transient failures (429, 5xx, network).
//
// # Limitations
//
//   - Document content ([goagent.ContentDocument]) is not supported.
//     Sending a message with document blocks returns [*goagent.UnsupportedContentError].
//   - [goagent.ThinkingConfig] on the request is ignored — reasoning is
//     controlled via [goagent.CompletionRequest.Effort] (reasoning_effort), not
//     a thinking budget. Reasoning that an OpenAI-compatible upstream returns is
//     still surfaced in streaming (see Streaming above).
//   - For o-series models, the correct field is MaxCompletionTokens, but this
//     provider uses MaxTokens for now. This will be addressed in a future option.
package openai
