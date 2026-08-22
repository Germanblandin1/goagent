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
// shared stream events. The OpenAI API does not expose reasoning in the response,
// so no [goagent.StreamEventThinking] events are emitted.
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
//   - [goagent.ThinkingConfig] is ignored — the OpenAI API does not expose
//     thinking/reasoning in the response.
//   - For o-series models, the correct field is MaxCompletionTokens, but this
//     provider uses MaxTokens for now. This will be addressed in a future option.
package openai
