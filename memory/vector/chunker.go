package vector

import (
	"context"
	"fmt"
	"strings"

	"github.com/Germanblandin1/goagent"
)

// ChunkContent is the input to a Chunker.
// Blocks holds the content to be divided.
// Metadata carries source information that is propagated to every ChunkResult.
type ChunkContent struct {
	Blocks   []goagent.ContentBlock
	Metadata map[string]any
}

// ChunkResult is the output of a Chunker — a piece ready for embedding.
// Metadata inherits from ChunkContent and may include chunk_index, chunk_total,
// page, source, etc., depending on the Chunker implementation.
type ChunkResult struct {
	Blocks   []goagent.ContentBlock
	Metadata map[string]any
}

// Chunker splits a ChunkContent into pieces that can be embedded individually.
// Implementations decide how to handle each ContentBlock type.
type Chunker interface {
	Chunk(ctx context.Context, content ChunkContent) ([]ChunkResult, error)
}

// PDFExtractor extracts page content from a PDF document.
// Implementations may return text (PDFs with embedded text), images (scanned
// PDFs or pages with diagrams), or both.
type PDFExtractor interface {
	ExtractPages(ctx context.Context, data []byte) ([]PDFPage, error)
}

// PDFPage represents the content of a single extracted page.
// Text is empty when the page contains only images.
// Image is nil when the page has extractable text.
type PDFPage struct {
	Text  string
	Image []byte
}

// ── NoOpChunker ──────────────────────────────────────────────────────────────

// NoOpChunker returns its input as a single unchanged chunk.
// Default for conversational messages whose size rarely exceeds model limits.
type NoOpChunker struct{}

// NewNoOpChunker creates a NoOpChunker.
func NewNoOpChunker() *NoOpChunker { return &NoOpChunker{} }

// Chunk returns content as a single ChunkResult without modification.
func (c *NoOpChunker) Chunk(_ context.Context, content ChunkContent) ([]ChunkResult, error) {
	return []ChunkResult{{
		Blocks:   content.Blocks,
		Metadata: content.Metadata,
	}}, nil
}

// ── TextChunker ──────────────────────────────────────────────────────────────

// TextChunkerOption configures a TextChunker.
type TextChunkerOption func(*TextChunker)

// WithMaxSize sets the maximum chunk size in estimator units. Default: 500.
func WithMaxSize(n int) TextChunkerOption { return func(c *TextChunker) { c.MaxSize = n } }

// WithOverlap sets the number of overlapping units between adjacent chunks. Default: 50.
func WithOverlap(n int) TextChunkerOption { return func(c *TextChunker) { c.Overlap = n } }

// WithEstimator sets the SizeEstimator used to measure chunk size. Default: HeuristicTokenEstimator.
func WithEstimator(e SizeEstimator) TextChunkerOption {
	return func(c *TextChunker) { c.Estimator = e }
}

// TextChunker extracts ContentText blocks and splits them when they exceed MaxSize.
// ContentImage and ContentDocument blocks are silently ignored.
// Compatible with any text-only Embedder.
type TextChunker struct {
	MaxSize   int           // maximum chunk size in Estimator units
	Overlap   int           // overlap between adjacent chunks in Estimator units
	Estimator SizeEstimator // default: HeuristicTokenEstimator
}

// NewTextChunker creates a TextChunker with defaults: MaxSize=500, Overlap=50,
// Estimator=HeuristicTokenEstimator.
func NewTextChunker(opts ...TextChunkerOption) *TextChunker {
	c := &TextChunker{
		MaxSize:   500,
		Overlap:   50,
		Estimator: &HeuristicTokenEstimator{},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Chunk splits content into text chunks. Returns nil when there is no text.
func (c *TextChunker) Chunk(ctx context.Context, content ChunkContent) ([]ChunkResult, error) {
	var parts []string
	for _, block := range content.Blocks {
		if block.Type == goagent.ContentText {
			parts = append(parts, block.Text)
		}
	}

	fullText := strings.Join(parts, " ")
	if fullText == "" {
		return nil, nil
	}

	if c.Estimator.Measure(ctx, fullText) <= c.MaxSize {
		return []ChunkResult{{
			Blocks:   []goagent.ContentBlock{{Type: goagent.ContentText, Text: fullText}},
			Metadata: content.Metadata,
		}}, nil
	}

	subTexts := splitWithOverlap(ctx, fullText, c.MaxSize, c.Overlap, c.Estimator)
	results := make([]ChunkResult, len(subTexts))
	for i, t := range subTexts {
		meta := copyMeta(content.Metadata)
		meta["chunk_index"] = i
		meta["chunk_total"] = len(subTexts)
		results[i] = ChunkResult{
			Blocks:   []goagent.ContentBlock{{Type: goagent.ContentText, Text: t}},
			Metadata: meta,
		}
	}
	return results, nil
}

// ── BlockChunker ─────────────────────────────────────────────────────────────

// BlockChunkerOption configures a BlockChunker.
type BlockChunkerOption func(*BlockChunker)

// WithBlockMaxSize sets the maximum text chunk size in estimator units. Default: 500.
func WithBlockMaxSize(n int) BlockChunkerOption { return func(c *BlockChunker) { c.TextMaxSize = n } }

// WithBlockOverlap sets the text overlap in estimator units. Default: 50.
func WithBlockOverlap(n int) BlockChunkerOption { return func(c *BlockChunker) { c.TextOverlap = n } }

// WithBlockEstimator sets the SizeEstimator for text blocks. Default: HeuristicTokenEstimator.
func WithBlockEstimator(e SizeEstimator) BlockChunkerOption {
	return func(c *BlockChunker) { c.TextEstimator = e }
}

// WithPDFExtractor sets the PDFExtractor for document blocks.
// Without an extractor, document blocks pass through as single chunks.
func WithPDFExtractor(e PDFExtractor) BlockChunkerOption {
	return func(c *BlockChunker) { c.PDFExtractor = e }
}

// BlockChunker generates chunks per ContentBlock, preserving block types.
// Long text blocks are sub-divided using TextChunker logic.
// Images pass through unchanged — they are never split.
// PDFs are split per page when a PDFExtractor is configured.
// Requires a multimodal Embedder capable of processing images (Phase 3+).
type BlockChunker struct {
	TextMaxSize   int
	TextOverlap   int
	TextEstimator SizeEstimator
	PDFExtractor  PDFExtractor // nil disables page extraction
}

// NewBlockChunker creates a BlockChunker with defaults: MaxSize=500, Overlap=50,
// Estimator=HeuristicTokenEstimator.
func NewBlockChunker(opts ...BlockChunkerOption) *BlockChunker {
	c := &BlockChunker{
		TextMaxSize:   500,
		TextOverlap:   50,
		TextEstimator: &HeuristicTokenEstimator{},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Chunk splits content into typed chunks.
func (c *BlockChunker) Chunk(ctx context.Context, content ChunkContent) ([]ChunkResult, error) {
	var results []ChunkResult

	for _, block := range content.Blocks {
		switch block.Type {
		case goagent.ContentText:
			tc := &TextChunker{
				MaxSize:   c.TextMaxSize,
				Overlap:   c.TextOverlap,
				Estimator: c.TextEstimator,
			}
			sub, err := tc.Chunk(ctx, ChunkContent{
				Blocks:   []goagent.ContentBlock{block},
				Metadata: content.Metadata,
			})
			if err != nil {
				return nil, err
			}
			results = append(results, sub...)

		case goagent.ContentImage:
			// Images are not split — they pass through as a single chunk.
			results = append(results, ChunkResult{
				Blocks:   []goagent.ContentBlock{block},
				Metadata: content.Metadata,
			})

		case goagent.ContentDocument:
			if c.PDFExtractor == nil {
				results = append(results, ChunkResult{
					Blocks:   []goagent.ContentBlock{block},
					Metadata: content.Metadata,
				})
				continue
			}
			pages, err := c.PDFExtractor.ExtractPages(ctx, block.Document.Data)
			if err != nil {
				return nil, fmt.Errorf("extracting PDF pages: %w", err)
			}
			for i, page := range pages {
				meta := copyMeta(content.Metadata)
				meta["page"] = i + 1
				meta["total_pages"] = len(pages)
				if block.Document != nil {
					meta["source"] = block.Document.Title
				}

				var pageBlocks []goagent.ContentBlock
				if page.Text != "" {
					pageBlocks = append(pageBlocks,
						goagent.ContentBlock{Type: goagent.ContentText, Text: page.Text})
				}
				if page.Image != nil {
					pageBlocks = append(pageBlocks,
						goagent.ContentBlock{
							Type:  goagent.ContentImage,
							Image: &goagent.ImageData{Data: page.Image, MediaType: "image/png"},
						})
				}
				results = append(results, ChunkResult{
					Blocks:   pageBlocks,
					Metadata: meta,
				})
			}
		}
	}
	return results, nil
}

// ── PageChunker ──────────────────────────────────────────────────────────────

// PageChunkerOption configures a PageChunker.
type PageChunkerOption func(*PageChunker)

// WithPagePDFExtractor sets the PDFExtractor for the PageChunker.
func WithPagePDFExtractor(e PDFExtractor) PageChunkerOption {
	return func(c *PageChunker) { c.PDFExtractor = e }
}

// PageChunker splits PDF documents into per-page chunks.
// Non-document blocks pass through unchanged.
// Each chunk carries metadata with "page", "total_pages", and "source".
// For v0.4 use only when processing PDFs with extractable text.
type PageChunker struct {
	PDFExtractor PDFExtractor
}

// NewPageChunker creates a PageChunker with the given options.
func NewPageChunker(opts ...PageChunkerOption) *PageChunker {
	c := &PageChunker{}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Chunk splits document blocks by page; non-document blocks pass through.
func (c *PageChunker) Chunk(ctx context.Context, content ChunkContent) ([]ChunkResult, error) {
	var results []ChunkResult

	for _, block := range content.Blocks {
		if block.Type != goagent.ContentDocument || c.PDFExtractor == nil {
			results = append(results, ChunkResult{
				Blocks:   []goagent.ContentBlock{block},
				Metadata: content.Metadata,
			})
			continue
		}

		pages, err := c.PDFExtractor.ExtractPages(ctx, block.Document.Data)
		if err != nil {
			return nil, fmt.Errorf("extracting PDF pages: %w", err)
		}

		for i, page := range pages {
			meta := copyMeta(content.Metadata)
			meta["page"] = i + 1
			meta["total_pages"] = len(pages)
			if block.Document != nil {
				meta["source"] = block.Document.Title
			}
			results = append(results, ChunkResult{
				Blocks: []goagent.ContentBlock{{
					Type: goagent.ContentText,
					Text: page.Text,
				}},
				Metadata: meta,
			})
		}
	}
	return results, nil
}

