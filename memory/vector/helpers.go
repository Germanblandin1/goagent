package vector

import (
	"context"
	"strings"

	"github.com/Germanblandin1/goagent"
)

// extractText concatenates all ContentText blocks separated by a single space.
// Blocks of other types are silently ignored.
// Returns an empty string when no text blocks are present.
func extractText(blocks []goagent.ContentBlock) string {
	var parts []string
	for _, b := range blocks {
		if b.Type == goagent.ContentText && strings.TrimSpace(b.Text) != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, " ")
}

// splitWithOverlap divides text into sub-strings where each chunk is at most
// maxSize units (as measured by est) with overlap units of shared context
// between adjacent chunks. Chunks never split in the middle of a word.
// ctx is forwarded to every est.Measure call so I/O-based estimators can be
// cancelled. Returns nil when text is empty.
func splitWithOverlap(ctx context.Context, text string, maxSize, overlap int, est SizeEstimator) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}

	var chunks []string
	start := 0

	for start < len(words) {
		end := start
		size := 0

		for end < len(words) {
			wordSize := est.Measure(ctx, words[end])
			if size+wordSize > maxSize && end > start {
				break
			}
			size += wordSize
			end++
		}

		chunks = append(chunks, strings.Join(words[start:end], " "))

		// Compute how many words to roll back for the overlap.
		overlapWords := 0
		overlapSize := 0
		for i := end - 1; i >= start && overlapSize < overlap; i-- {
			overlapSize += est.Measure(ctx, words[i])
			overlapWords++
		}
		next := end - overlapWords
		if next <= start {
			// Guard: always advance at least one word to prevent infinite loop.
			next = end
		}
		start = next
	}
	return chunks
}

// copyMeta returns a shallow copy of m.
// Returns an empty map when m is nil.
func copyMeta(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// ExtractText is the exported counterpart of extractText.
// It concatenates all ContentText blocks from blocks separated by spaces.
func ExtractText(blocks []goagent.ContentBlock) string {
	return extractText(blocks)
}

// ChunkToMessage builds a Message from an original message and a ChunkResult.
// The returned message preserves the original Role and ToolCallID but
// replaces Content with the chunk's blocks.
func ChunkToMessage(orig goagent.Message, chunk ChunkResult) goagent.Message {
	return goagent.Message{
		Role:       orig.Role,
		ToolCallID: orig.ToolCallID,
		Content:    chunk.Blocks,
	}
}
