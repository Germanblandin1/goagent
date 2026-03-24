// Package ollama provides a [goagent.Provider] that targets a locally running
// Ollama instance via its OpenAI-compatible API.
//
// # Requirements
//
// Ollama must be installed and running before using this provider.
// Start the server with:
//
//	ollama serve
//
// Then pull the model you want to use:
//
//	ollama pull qwen3
//
// # Supported models
//
// Any model available in Ollama can be used (qwen3, llama3, mistral, gemma2,
// phi3, deepseek-r1, etc.). The model is selected at the agent level via
// [goagent.WithModel], not at the provider level.
//
// # Default URL
//
// The provider connects to http://localhost:11434/v1 by default. Use
// [WithBaseURL] to override when Ollama runs on a different host or port.
//
// # Limitations
//
//   - Document content ([goagent.ContentDocument]) is not supported. Sending
//     a message with document blocks returns a [*goagent.UnsupportedContentError].
//   - Image support depends on the model — vision-capable models (e.g. llava,
//     moondream) handle [goagent.ContentImage] blocks; text-only models may
//     ignore or reject them.
//
// # Usage
//
//	provider := ollama.New()
//
//	agent := goagent.New(
//	    goagent.WithProvider(provider),
//	    goagent.WithModel("qwen3"),
//	)
//
//	answer, err := agent.Run(ctx, "Explain the ReAct pattern")
package ollama
