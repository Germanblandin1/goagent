package rag

import (
	"fmt"
	"strings"

	"github.com/Germanblandin1/goagent"
)

// defaultFormat serialises results as plain text readable by the model.
// Works with any provider — no vision required.
// This is the formatter used when [WithFormatter] is not specified.
func defaultFormat(results []SearchResult) []goagent.ContentBlock {
	if len(results) == 0 {
		return []goagent.ContentBlock{goagent.TextBlock("No relevant information found.")}
	}
	var sb strings.Builder
	sb.WriteString("Relevant information found:\n\n")
	for i, r := range results {
		if r.Source != "" {
			fmt.Fprintf(&sb, "--- Source %d: %s (score: %.2f) ---\n", i+1, r.Source, r.Score)
		} else {
			fmt.Fprintf(&sb, "--- Source %d ---\n", i+1)
		}
		sb.WriteString(extractText(r.Message.Content))
		sb.WriteString("\n\n")
	}
	return []goagent.ContentBlock{goagent.TextBlock(sb.String())}
}

// MultimodalFormat returns the ContentBlocks of each result directly, without
// converting to text. Use when the corpus contains images indexed with a
// multimodal embedder (voyage-multimodal-3, CLIP) and the provider has vision
// capabilities.
//
// With a text-only embedder, the blocks returned will be text, identical in
// effect to [defaultFormat]. MultimodalFormat adds no value in that case.
func MultimodalFormat(results []SearchResult) []goagent.ContentBlock {
	if len(results) == 0 {
		return []goagent.ContentBlock{goagent.TextBlock("No relevant information found.")}
	}
	var blocks []goagent.ContentBlock
	for _, r := range results {
		blocks = append(blocks, r.Message.Content...)
	}
	return blocks
}
