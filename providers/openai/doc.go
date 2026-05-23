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
// # Limitations
//
//   - Document content ([goagent.ContentDocument]) is not supported.
//     Sending a message with document blocks returns [*goagent.UnsupportedContentError].
//   - [goagent.ThinkingConfig] is ignored — the OpenAI API does not expose
//     thinking/reasoning in the response.
//   - For o-series models, the correct field is MaxCompletionTokens, but this
//     provider uses MaxTokens for now. This will be addressed in a future option.
package openai
