package vector

import (
	"context"
	"strings"

	"github.com/Germanblandin1/goagent"
)

// defaultSeparators defines the hierarchy of separators used by RecursiveChunker.
// They are tried in order from coarsest (paragraph) to finest (space).
var defaultSeparators = []string{"\n\n", "\n", ". ", "! ", "? ", " "}

var _ Chunker = (*RecursiveChunker)(nil)

// RecursiveChunkerOption configures a RecursiveChunker.
type RecursiveChunkerOption func(*RecursiveChunker)

// WithRCSeparators sets the ordered list of separators the chunker tries when
// splitting text. The first separator that produces multiple fragments is used;
// shorter separators are only tried when larger ones produce no split.
func WithRCSeparators(seps []string) RecursiveChunkerOption {
	return func(c *RecursiveChunker) { c.separators = seps }
}

// WithRCMaxSize sets the maximum chunk size in estimator units. Default: 500.
func WithRCMaxSize(n int) RecursiveChunkerOption {
	return func(c *RecursiveChunker) { c.maxSize = n }
}

// WithRCOverlap sets the overlap budget in estimator units appended from the
// tail of chunk[i] to the head of chunk[i+1]. Default: 50.
func WithRCOverlap(n int) RecursiveChunkerOption {
	return func(c *RecursiveChunker) { c.overlap = n }
}

// WithRCEstimator sets the SizeEstimator used to measure text length.
// Default: HeuristicTokenEstimator.
func WithRCEstimator(e SizeEstimator) RecursiveChunkerOption {
	return func(c *RecursiveChunker) { c.estimator = e }
}

// RecursiveChunker divides text by respecting a hierarchy of separators.
// It tries high-level separators first (\n\n, \n) and falls back to sentences
// and words only when necessary. Ideal for Markdown, documentation, and
// paragraph-structured text.
//
// Overlap is applied post-split: the tail of chunk[i] is prepended to
// chunk[i+1] to preserve context at boundaries.
type RecursiveChunker struct {
	separators []string
	maxSize    int
	overlap    int
	estimator  SizeEstimator
}

// NewRecursiveChunker creates a RecursiveChunker with sensible defaults:
// separators=defaultSeparators, maxSize=500, overlap=50,
// estimator=HeuristicTokenEstimator.
func NewRecursiveChunker(opts ...RecursiveChunkerOption) *RecursiveChunker {
	c := &RecursiveChunker{
		separators: defaultSeparators,
		maxSize:    500,
		overlap:    50,
		estimator:  &HeuristicTokenEstimator{},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Chunk splits content into hierarchically-aware chunks. ContentImage and
// ContentDocument blocks are silently ignored. Returns nil when there is no
// text. When all text fits in a single chunk, chunk_index and chunk_total are
// not added to the metadata.
func (c *RecursiveChunker) Chunk(ctx context.Context, content ChunkContent) ([]ChunkResult, error) {
	var parts []string
	for _, b := range content.Blocks {
		if b.Type == goagent.ContentText && strings.TrimSpace(b.Text) != "" {
			parts = append(parts, b.Text)
		}
	}
	if len(parts) == 0 {
		return nil, nil
	}
	// Join with "\n\n" to preserve hierarchical separators between blocks.
	text := strings.Join(parts, "\n\n")

	if c.estimator.Measure(ctx, text) <= c.maxSize {
		return []ChunkResult{{
			Blocks:   []goagent.ContentBlock{goagent.TextBlock(text)},
			Metadata: copyMeta(content.Metadata),
		}}, nil
	}

	fragments := c.splitRecursive(ctx, text, c.separators)
	chunks := c.applyOverlap(ctx, fragments)

	total := len(chunks)
	results := make([]ChunkResult, total)
	for i, ch := range chunks {
		meta := copyMeta(content.Metadata)
		meta["chunk_index"] = i
		meta["chunk_total"] = total
		results[i] = ChunkResult{
			Blocks:   []goagent.ContentBlock{goagent.TextBlock(ch)},
			Metadata: meta,
		}
	}
	return results, nil
}

// splitRecursive recursively divides text using the separator hierarchy.
func (c *RecursiveChunker) splitRecursive(ctx context.Context, text string, seps []string) []string {
	if c.estimator.Measure(ctx, text) <= c.maxSize {
		return []string{text}
	}
	if len(seps) == 0 {
		// No separator left: fall back to word-boundary split (overlap=0 because
		// applyOverlap handles context sharing between the final chunks).
		return splitWithOverlap(ctx, text, c.maxSize, 0, c.estimator)
	}

	fragments := strings.Split(text, seps[0])
	if len(fragments) == 1 {
		// Separator not present: try the next finer separator.
		return c.splitRecursive(ctx, text, seps[1:])
	}
	return c.mergeFragments(ctx, fragments, seps[0], seps[1:])
}

// mergeFragments groups split fragments back into chunks that respect maxSize.
// Fragments that individually exceed maxSize are recursively split with restSeps.
func (c *RecursiveChunker) mergeFragments(ctx context.Context, fragments []string, sep string, restSeps []string) []string {
	var result []string
	var current []string
	currentSize := 0
	sepSize := c.estimator.Measure(ctx, sep)

	flush := func() {
		if len(current) > 0 {
			result = append(result, strings.Join(current, sep))
			current = current[:0]
			currentSize = 0
		}
	}

	for _, frag := range fragments {
		fragSize := c.estimator.Measure(ctx, frag)

		if fragSize > c.maxSize {
			flush()
			result = append(result, c.splitRecursive(ctx, frag, restSeps)...)
			continue
		}

		addSize := fragSize
		if len(current) > 0 {
			addSize += sepSize
		}

		if len(current) > 0 && currentSize+addSize > c.maxSize {
			flush()
		}

		if len(current) > 0 {
			currentSize += sepSize
		}
		current = append(current, frag)
		currentSize += fragSize
	}
	flush()
	return result
}

// applyOverlap prepends the tail of chunk[i] to chunk[i+1] to preserve
// context at boundaries. chunk[0] is never modified.
func (c *RecursiveChunker) applyOverlap(ctx context.Context, chunks []string) []string {
	if c.overlap == 0 || len(chunks) <= 1 {
		return chunks
	}
	result := make([]string, len(chunks))
	result[0] = chunks[0]
	for i := 1; i < len(chunks); i++ {
		tail := c.extractTail(ctx, chunks[i-1], c.overlap)
		if tail != "" {
			result[i] = tail + "\n\n" + chunks[i]
		} else {
			result[i] = chunks[i]
		}
	}
	return result
}

// extractTail returns up to targetSize estimator units from the end of text,
// never splitting in the middle of a word.
func (c *RecursiveChunker) extractTail(ctx context.Context, text string, targetSize int) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}

	var selected []string
	size := 0
	for i := len(words) - 1; i >= 0; i-- {
		wordSize := c.estimator.Measure(ctx, words[i])
		if size+wordSize > targetSize {
			break
		}
		size += wordSize
		selected = append(selected, words[i])
	}

	if len(selected) == 0 {
		return ""
	}

	// Reverse: we collected words from the end backwards.
	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}
	return strings.Join(selected, " ")
}
