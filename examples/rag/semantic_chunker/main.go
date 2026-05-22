// Semantic-chunker demonstrates how to implement the vector.Chunker interface
// using a goagent.Agent as the splitting engine.
//
// SemanticChunker satisfies vector.Chunker. Its Chunk method sends the text
// content to a dedicated agent configured with a strict system prompt, then
// parses the JSON response into []vector.ChunkResult. Non-text blocks pass
// through unchanged without calling the agent.
//
// This pattern is useful for contracts, technical articles, or any document
// where topical coherence matters more than chunk size uniformity.
// For cost-sensitive pipelines, prefer vector.NewTextChunker instead.
//
// Requires Ollama running locally with the qwen3 model: https://ollama.com
//
// Usage:
//
//	go run ./examples/semantic_chunker
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector"
	"github.com/Germanblandin1/goagent/providers/ollama"
)

// systemPrompt instructs the agent to act purely as a semantic splitter.
// No reasoning, no commentary — only the JSON array is accepted as output.
const systemPrompt = `You are a semantic text splitter.
Your only job is to divide the user's text into semantically independent sections.
Rules:
- Each section must cover exactly one coherent topic and stand alone.
- Do not merge sections that cover different topics.
- Do not add, remove, or rephrase any words — preserve the original text exactly.
- Reply ONLY with a valid JSON array. No explanation, no markdown fences.
Format: [{"text": "..."}, {"text": "..."}]`

// SemanticChunker implements vector.Chunker using a goagent.Agent.
// Text blocks are divided by the agent into semantically coherent sections.
// Non-text blocks (images, documents) pass through as single chunks unchanged.
type SemanticChunker struct {
	agent *goagent.Agent
}

// NewSemanticChunker builds a SemanticChunker backed by the given provider and model.
func NewSemanticChunker(provider goagent.Provider, model string) (*SemanticChunker, error) {
	agent, err := goagent.New(
		goagent.WithProvider(provider),
		goagent.WithModel(model),
		goagent.WithSystemPrompt(systemPrompt),
		goagent.WithMaxIterations(1), // single-shot: no tool calls needed
	)
	if err != nil {
		return nil, fmt.Errorf("semantic chunker: %w", err)
	}
	return &SemanticChunker{agent: agent}, nil
}

// Chunk implements vector.Chunker.
// Text blocks are joined and sent to the agent; the JSON response is parsed
// into individual ChunkResults. Non-text blocks are returned unchanged.
// If the agent returns unparseable JSON, the full text is returned as one chunk.
func (c *SemanticChunker) Chunk(ctx context.Context, content vector.ChunkContent) ([]vector.ChunkResult, error) {
	var textBlocks, otherBlocks []goagent.ContentBlock
	for _, b := range content.Blocks {
		if b.Type == goagent.ContentText {
			textBlocks = append(textBlocks, b)
		} else {
			otherBlocks = append(otherBlocks, b)
		}
	}

	// Non-text blocks pass through as individual chunks without calling the agent.
	var results []vector.ChunkResult
	for _, b := range otherBlocks {
		results = append(results, vector.ChunkResult{
			Blocks:   []goagent.ContentBlock{b},
			Metadata: content.Metadata,
		})
	}

	if len(textBlocks) == 0 {
		return results, nil
	}

	// Join all text blocks into a single string for the agent.
	var parts []string
	for _, b := range textBlocks {
		parts = append(parts, b.Text)
	}
	fullText := strings.Join(parts, "\n")

	raw, err := c.agent.Run(ctx, fullText)
	if err != nil {
		return nil, fmt.Errorf("semantic chunker agent: %w", err)
	}

	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var parsed []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		// Graceful fallback: return the full text as a single chunk.
		fmt.Fprintf(os.Stderr, "warn: agent returned unparseable JSON, falling back to single chunk: %v\n", err)
		return append(results, vector.ChunkResult{
			Blocks:   []goagent.ContentBlock{goagent.TextBlock(fullText)},
			Metadata: content.Metadata,
		}), nil
	}

	for i, p := range parsed {
		meta := make(map[string]any, len(content.Metadata)+2)
		for k, v := range content.Metadata {
			meta[k] = v
		}
		meta["chunk_index"] = i
		meta["chunk_total"] = len(parsed)
		results = append(results, vector.ChunkResult{
			Blocks:   []goagent.ContentBlock{goagent.TextBlock(strings.TrimSpace(p.Text))},
			Metadata: meta,
		})
	}
	return results, nil
}

// ── demo ─────────────────────────────────────────────────────────────────────

const document = `Go is a statically typed, compiled programming language designed at Google.
It was created by Robert Griesemer, Rob Pike, and Ken Thompson, and first
appeared in 2009. The language is known for its simplicity and strong support
for concurrent programming through goroutines and channels.

One of Go's most notable features is its garbage collector, which provides
automatic memory management without the overhead typically associated with
managed runtimes. The runtime is lightweight, making Go suitable for both
systems programming and large-scale distributed services.

Go includes a built-in testing framework accessed via the "go test" command.
Tests live in files ending with _test.go and follow a simple convention:
functions named Test* receive a *testing.T argument. The toolchain also
supports benchmarks (Benchmark*) and example functions (Example*) that serve
as live documentation on pkg.go.dev.

The Go module system, introduced in Go 1.11 and stabilized in Go 1.16,
replaced GOPATH-based workflows. A module is a collection of packages versioned
together. The go.mod file declares the module path and its dependencies.
Semantic versioning (semver) is used for all public modules.`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	chunker, err := NewSemanticChunker(ollama.New(), "gpt-oss:120b-cloud")
	if err != nil {
		log.Fatal(err)
	}

	content := vector.ChunkContent{
		Blocks:   []goagent.ContentBlock{goagent.TextBlock(document)},
		Metadata: map[string]any{"source": "go-overview.txt"},
	}

	chunks, err := chunker.Chunk(ctx, content)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Document split into %d semantic chunks:\n\n", len(chunks))
	for _, ch := range chunks {
		fmt.Printf("── chunk %d/%d  (source: %v) ──────────────────\n",
			ch.Metadata["chunk_index"],
			ch.Metadata["chunk_total"],
			ch.Metadata["source"],
		)
		fmt.Println(vector.ExtractText(ch.Blocks))
		fmt.Println()
	}
}
