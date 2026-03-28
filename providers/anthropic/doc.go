// Package anthropic provides a [goagent.Provider] backed by the Anthropic
// Messages API (Claude).
//
// # Authentication
//
// By default the provider reads the API key from the ANTHROPIC_API_KEY
// environment variable. To supply it explicitly, create a client with
// [NewClient] and pass it to [NewWithClient]:
//
//	client := anthropic.NewClient(anthropic.WithAPIKey("sk-..."))
//	provider := anthropic.NewWithClient(client)
//
// # Shared client
//
// [AnthropicClient] holds the underlying Anthropic SDK client. Create one
// with [NewClient] to share transport settings across multiple providers or
// to target a custom base URL (e.g. a proxy or a test server):
//
//	client := anthropic.NewClient(
//	    anthropic.WithAPIKey("sk-..."),
//	    anthropic.WithBaseURL("https://proxy.internal"),
//	)
//	provider := anthropic.NewWithClient(client)
//
// # Supported models
//
// Any model available through the Anthropic Messages API can be used
// (claude-sonnet-4-6, claude-haiku-4-5-20251001, claude-opus-4-6, etc.).
// The model is selected at the agent level via [goagent.WithModel] or via
// [WithModel] on the Provider.
//
// # Multimodal support
//
// This provider supports all goagent content types natively:
//
//   - Text ([goagent.ContentText]) — always supported.
//   - Images ([goagent.ContentImage]) — JPEG, PNG, GIF, WebP. Limit: 5 MB,
//     ~1600x1600 px recommended.
//   - Documents ([goagent.ContentDocument]) — PDF and plain text. Limit: 32 MB.
//     Claude processes both text and visual content (tables, charts, images)
//     page by page.
//
// # Configuration
//
// Use [WithMaxTokens] to control the maximum output length per completion
// (default: 4096).
//
// # Usage
//
//	provider := anthropic.New()
//
//	agent := goagent.New(
//	    goagent.WithProvider(provider),
//	    goagent.WithModel("claude-sonnet-4-6"),
//	)
//
//	answer, err := agent.Run(ctx, "Summarize this document")
package anthropic
