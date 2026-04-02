// embedder-bench measures embedding latency across text sizes using a local
// Ollama model. It embeds five texts of increasing length and prints elapsed
// time and vector dimensions for each.
//
// Requires Ollama running locally with an embedding model pulled:
//
//	ollama pull nomic-embed-text
//
// Usage:
//
//	go run ./examples/embedder-bench
//
// Override the model:
//
//	EMBED_MODEL=mxbai-embed-large go run ./examples/embedder-bench
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/providers/ollama"
)

// texts holds samples of increasing size for the benchmark.
// nomic-embed-text uses num_ctx=2048 by default in Ollama (~8000 chars).
// To test beyond that limit, run: ollama run nomic-embed-text --num-ctx 8192
var texts = []struct {
	label string
	text  string
}{
	{
		"tiny   (~10 words)",
		"The quick brown fox jumps over the lazy dog.",
	},
	{
		"small  (~100 words)",
		strings.Repeat("Go is a statically typed, compiled language designed at Google. ", 10),
	},
	{
		"medium (~500 words)",
		strings.Repeat("Go is a statically typed, compiled language designed at Google. ", 50),
	},
	{
		"large  (~1k words)",
		strings.Repeat("Go is a statically typed, compiled language designed at Google. ", 100),
	},
	{
		"near-limit (~1800 words)",
		strings.Repeat("Go is a statically typed, compiled language designed at Google. ", 115),
	},
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	model := "nomic-embed-text:latest"
	if m := os.Getenv("EMBED_MODEL"); m != "" {
		model = m
	}

	embedder := ollama.NewEmbedder(ollama.WithEmbedModel(model))

	fmt.Printf("provider: ollama  model: %s\n\n", model)
	fmt.Printf("%-22s  %8s  %6s  %8s\n", "size", "elapsed", "dims", "chars")
	fmt.Println(strings.Repeat("─", 52))

	for _, tc := range texts {
		blocks := []goagent.ContentBlock{goagent.TextBlock(tc.text)}

		start := time.Now()
		vec, err := embedder.Embed(ctx, blocks)
		elapsed := time.Since(start)

		if err != nil {
			log.Fatalf("%s: %v", tc.label, err)
		}

		fmt.Printf("%-22s  %8s  %6d  %8d\n",
			tc.label,
			elapsed.Round(time.Millisecond),
			len(vec),
			len([]rune(tc.text)),
		)
	}
}
