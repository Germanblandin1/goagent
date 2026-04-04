package rag

import (
	"strings"

	"github.com/Germanblandin1/goagent"
)

// extractSource reads the "source" key from msg.Metadata.
// Returns "" when the key is absent or not a string.
func extractSource(msg goagent.Message) string {
	if msg.Metadata == nil {
		return ""
	}
	s, _ := msg.Metadata["source"].(string)
	return s
}

// extractText concatenates all ContentText blocks, separated by a single space.
// Non-text blocks are silently ignored.
func extractText(blocks []goagent.ContentBlock) string {
	var parts []string
	for _, b := range blocks {
		if b.Type == goagent.ContentText && strings.TrimSpace(b.Text) != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, " ")
}
