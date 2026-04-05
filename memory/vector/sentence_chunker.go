package vector

import (
	"context"
	"regexp"
	"strings"

	"github.com/Germanblandin1/goagent"
)

// sentenceSplit matches the end of a sentence: one or more punctuation marks
// followed by whitespace, or a paragraph break (two or more newlines).
var sentenceSplit = regexp.MustCompile(`[.!?]+\s+|\n\n+`)

// splitIntoSentences divides text at sentence boundaries.
// Each returned sentence retains its terminating punctuation.
// Empty pieces and leading/trailing whitespace are discarded.
func splitIntoSentences(text string) []string {
	locs := sentenceSplit.FindAllStringIndex(text, -1)
	var sentences []string
	start := 0
	for _, loc := range locs {
		s := strings.TrimSpace(text[start:loc[1]])
		if s != "" {
			sentences = append(sentences, s)
		}
		start = loc[1]
	}
	if start < len(text) {
		s := strings.TrimSpace(text[start:])
		if s != "" {
			sentences = append(sentences, s)
		}
	}
	return sentences
}

// ── SentenceChunker ──────────────────────────────────────────────────────────

// SentenceChunkerOption configures a SentenceChunker.
type SentenceChunkerOption func(*SentenceChunker)

// WithSentenceMaxSize sets the maximum chunk size in estimator units. Default: 500.
func WithSentenceMaxSize(n int) SentenceChunkerOption {
	return func(c *SentenceChunker) { c.MaxSize = n }
}

// WithSentenceOverlap sets the number of complete sentences carried from one
// chunk into the next. Unlike TextChunker, overlap is counted in sentences, not
// tokens, which preserves semantic coherence at boundaries. Default: 1.
func WithSentenceOverlap(n int) SentenceChunkerOption {
	return func(c *SentenceChunker) { c.Overlap = n }
}

// WithSentenceEstimator sets the SizeEstimator used to measure chunk size.
// Default: HeuristicTokenEstimator.
func WithSentenceEstimator(e SizeEstimator) SentenceChunkerOption {
	return func(c *SentenceChunker) { c.Estimator = e }
}

// SentenceChunker splits text at sentence boundaries and groups sentences into
// chunks that do not exceed MaxSize. Adjacent chunks share the last Overlap
// complete sentences for context, preserving semantic coherence better than
// token-level overlap.
//
// ContentImage and ContentDocument blocks are silently ignored.
// Compatible with any text-only Embedder.
type SentenceChunker struct {
	MaxSize   int           // maximum chunk size in Estimator units
	Overlap   int           // number of sentences shared between adjacent chunks
	Estimator SizeEstimator // default: HeuristicTokenEstimator
}

// NewSentenceChunker creates a SentenceChunker with defaults: MaxSize=500,
// Overlap=1, Estimator=HeuristicTokenEstimator.
func NewSentenceChunker(opts ...SentenceChunkerOption) *SentenceChunker {
	c := &SentenceChunker{
		MaxSize:   500,
		Overlap:   1,
		Estimator: &HeuristicTokenEstimator{},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Chunk splits content into sentence-aware chunks. Returns nil when there is no
// text. When all sentences fit in a single chunk, chunk_index and chunk_total
// are not added to the metadata.
func (c *SentenceChunker) Chunk(ctx context.Context, content ChunkContent) ([]ChunkResult, error) {
	text := extractText(content.Blocks)
	sentences := splitIntoSentences(text)
	if len(sentences) == 0 {
		return nil, nil
	}

	var results []ChunkResult
	var current []string
	currentSize := 0

	flush := func() {
		meta := copyMeta(content.Metadata)
		results = append(results, ChunkResult{
			Blocks: []goagent.ContentBlock{{
				Type: goagent.ContentText,
				Text: strings.Join(current, " "),
			}},
			Metadata: meta,
		})
	}

	for _, s := range sentences {
		size := c.Estimator.Measure(ctx, s)
		if currentSize+size > c.MaxSize && len(current) > 0 {
			flush()
			// Carry the last Overlap sentences into the next chunk.
			current = current[max(0, len(current)-c.Overlap):]
			currentSize = 0
			for _, os := range current {
				currentSize += c.Estimator.Measure(ctx, os)
			}
		}
		current = append(current, s)
		currentSize += size
	}
	if len(current) > 0 {
		flush()
	}

	// Backfill chunk_index and chunk_total only when there are multiple chunks,
	// consistent with TextChunker behaviour.
	if len(results) > 1 {
		total := len(results)
		for i := range results {
			results[i].Metadata["chunk_index"] = i
			results[i].Metadata["chunk_total"] = total
		}
	}

	return results, nil
}
