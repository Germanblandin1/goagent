// Package ollama provides a [goagent.Provider] and a [goagent.Embedder] that
// target a locally running Ollama instance.
//
// # Requirements
//
// Ollama must be installed and running before using this package.
// Start the server with:
//
//	ollama serve
//
// Then pull the model you want to use:
//
//	ollama pull qwen3
//
// # Shared client
//
// All communication with Ollama goes through [OllamaClient]. Create one with
// [NewClient] and share it across a Provider and an Embedder via
// [NewWithClient] and [NewEmbedderWithClient]:
//
//	client := ollama.NewClient()                             // http://localhost:11434
//	provider := ollama.NewWithClient(client)
//	embedder := ollama.NewEmbedderWithClient(client,
//	    ollama.WithEmbedModel("nomic-embed-text"),
//	)
//
// For the default client, [New] and [NewEmbedder] take options directly.
//
// # Supported models
//
// Any model available in Ollama can be used (qwen3, llama3, mistral, gemma2,
// phi3, deepseek-r1, etc.). The chat model is selected at the agent level via
// [goagent.WithModel]. The embedding model
// is set via [WithEmbedModel].
//
// # Default URL
//
// The client connects to http://localhost:11434 by default. Use
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
//	agent, err := goagent.New(
//	    goagent.WithProvider(provider),
//	    goagent.WithModel("qwen3"),
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	answer, err := agent.Run(ctx, "Explain the ReAct pattern")
package ollama
