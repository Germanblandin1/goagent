package goagent

import "strings"

// ContentType identifies the kind of content in a ContentBlock.
type ContentType string

const (
	// ContentText indicates the block contains plain text.
	ContentText ContentType = "text"

	// ContentImage indicates the block contains an image.
	// Supported formats: JPEG, PNG, GIF, WebP.
	ContentImage ContentType = "image"

	// ContentDocument indicates the block contains a document.
	// Supported formats: PDF, plain text.
	ContentDocument ContentType = "document"

	// ContentThinking indicates the block contains the model's internal
	// reasoning produced before the final response or a tool call.
	// The text may be a summary (Claude 4+) or the full chain-of-thought
	// (Claude Sonnet 3.7, local models).
	//
	// Thinking blocks are produced by the model and must not be constructed
	// by callers except when echoing them back to the provider (which the
	// Agent does automatically). Use ThinkingBlock to build one if needed.
	ContentThinking ContentType = "thinking"
)

// ContentBlock represents a unit of content within a message.
// Exactly one of Text, Image, Document, or Thinking is valid depending on
// the value of Type. The others are zero value.
//
// Use the helpers TextBlock, ImageBlock, DocumentBlock, and ThinkingBlock
// to construct content blocks instead of building the struct directly.
type ContentBlock struct {
	Type     ContentType
	Text     string
	Image    *ImageData
	Document *DocumentData
	Thinking *ThinkingData
}

// ImageData holds an image to send to the model.
// Data is the raw image content — the provider layer encodes it to base64.
//
// Supported formats: image/jpeg, image/png, image/gif, image/webp.
// Anthropic limit: 5 MB per image, ~1600x1600 px recommended.
type ImageData struct {
	MediaType string // MIME type: "image/jpeg", "image/png", "image/gif", "image/webp"
	Data      []byte // raw image content
}

// DocumentData holds a document to send to the model.
// For PDFs, Claude processes both text and visual content (tables, charts,
// embedded images) page by page.
//
// Supported formats: application/pdf, text/plain.
// Anthropic limit: 32 MB per document.
type DocumentData struct {
	MediaType string // MIME type: "application/pdf", "text/plain"
	Data      []byte // raw document content
	Title     string // optional — gives the model context about the document
}

// ThinkingData holds the model's internal reasoning produced during extended
// thinking. The Agent preserves this data within a turn so the provider can
// echo it back to the API (required by Anthropic for thinking continuity).
//
// Thinking is the reasoning text — may be a summary in Claude 4+ or the
// full chain-of-thought in Claude Sonnet 3.7 and local models.
//
// Signature is the opaque cryptographic token issued by the Anthropic API to
// verify the block's authenticity. It must be echoed back unchanged and must
// never be logged, inspected, or modified. For local models (Ollama) that do
// not use this mechanism, Signature is an empty string.
type ThinkingData struct {
	Thinking  string
	Signature string
}

// validImageTypes lists the MIME types accepted for image content.
var validImageTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

// validDocumentTypes lists the MIME types accepted for document content.
var validDocumentTypes = map[string]bool{
	"application/pdf": true,
	"text/plain":      true,
}

// ValidImageMediaType reports whether mediaType is a supported image MIME type.
func ValidImageMediaType(mediaType string) bool {
	return validImageTypes[mediaType]
}

// ValidDocumentMediaType reports whether mediaType is a supported document MIME type.
func ValidDocumentMediaType(mediaType string) bool {
	return validDocumentTypes[mediaType]
}

// ── ContentBlock helpers ────────────────────────────────────────────────────

// TextBlock creates a text ContentBlock.
func TextBlock(s string) ContentBlock {
	return ContentBlock{Type: ContentText, Text: s}
}

// ImageBlock creates an image ContentBlock from raw bytes.
// mediaType must be one of: "image/jpeg", "image/png", "image/gif", "image/webp".
func ImageBlock(data []byte, mediaType string) ContentBlock {
	return ContentBlock{
		Type:  ContentImage,
		Image: &ImageData{MediaType: mediaType, Data: data},
	}
}

// DocumentBlock creates a document ContentBlock from raw bytes.
// mediaType must be one of: "application/pdf", "text/plain".
// title is optional — if non-empty, it gives the model context about the document.
func DocumentBlock(data []byte, mediaType, title string) ContentBlock {
	return ContentBlock{
		Type:     ContentDocument,
		Document: &DocumentData{MediaType: mediaType, Data: data, Title: title},
	}
}

// ThinkingBlock creates a ContentBlock that carries the model's internal
// reasoning. signature is the opaque cryptographic token from the Anthropic
// API; pass an empty string for local models that do not use this mechanism.
func ThinkingBlock(thinking, signature string) ContentBlock {
	return ContentBlock{
		Type:     ContentThinking,
		Thinking: &ThinkingData{Thinking: thinking, Signature: signature},
	}
}

// ── Message helpers ─────────────────────────────────────────────────────────

// TextMessage creates a Message with a single text content block.
func TextMessage(role Role, text string) Message {
	return Message{
		Role:    role,
		Content: []ContentBlock{{Type: ContentText, Text: text}},
	}
}

// UserMessage creates a user-role Message with text content.
// Shorthand for TextMessage(RoleUser, text).
func UserMessage(text string) Message {
	return TextMessage(RoleUser, text)
}

// AssistantMessage creates an assistant-role Message with text content.
// Shorthand for TextMessage(RoleAssistant, text).
func AssistantMessage(text string) Message {
	return TextMessage(RoleAssistant, text)
}

// ── Message text extraction ─────────────────────────────────────────────────

// TextContent returns the concatenation of all ContentText blocks in the
// message, separated by newlines. Returns an empty string if the message
// contains no text blocks.
func (m Message) TextContent() string {
	var parts []string
	for _, b := range m.Content {
		if b.Type == ContentText && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// HasContentType reports whether the message contains at least one
// ContentBlock of the given type.
func (m Message) HasContentType(ct ContentType) bool {
	for _, b := range m.Content {
		if b.Type == ct {
			return true
		}
	}
	return false
}
