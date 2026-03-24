package goagent_test

import (
	"testing"

	"github.com/Germanblandin1/goagent"
)

func TestValidImageMediaType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mediaType string
		want      bool
	}{
		{"image/jpeg", true},
		{"image/png", true},
		{"image/gif", true},
		{"image/webp", true},
		{"image/bmp", false},
		{"image/svg+xml", false},
		{"", false},
		{"text/plain", false},
	}

	for _, tt := range tests {
		t.Run(tt.mediaType, func(t *testing.T) {
			t.Parallel()
			if got := goagent.ValidImageMediaType(tt.mediaType); got != tt.want {
				t.Errorf("ValidImageMediaType(%q) = %v, want %v", tt.mediaType, got, tt.want)
			}
		})
	}
}

func TestValidDocumentMediaType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mediaType string
		want      bool
	}{
		{"application/pdf", true},
		{"text/plain", true},
		{"text/html", false},
		{"application/json", false},
		{"", false},
		{"image/png", false},
	}

	for _, tt := range tests {
		t.Run(tt.mediaType, func(t *testing.T) {
			t.Parallel()
			if got := goagent.ValidDocumentMediaType(tt.mediaType); got != tt.want {
				t.Errorf("ValidDocumentMediaType(%q) = %v, want %v", tt.mediaType, got, tt.want)
			}
		})
	}
}

func TestImageBlock(t *testing.T) {
	t.Parallel()

	data := []byte("fake-png-data")
	b := goagent.ImageBlock(data, "image/png")

	if b.Type != goagent.ContentImage {
		t.Errorf("Type = %q, want %q", b.Type, goagent.ContentImage)
	}
	if b.Image == nil {
		t.Fatal("Image is nil")
	}
	if b.Image.MediaType != "image/png" {
		t.Errorf("MediaType = %q, want %q", b.Image.MediaType, "image/png")
	}
	if string(b.Image.Data) != "fake-png-data" {
		t.Errorf("Data = %q, want %q", b.Image.Data, "fake-png-data")
	}
}

func TestDocumentBlock(t *testing.T) {
	t.Parallel()

	data := []byte("pdf-bytes")
	b := goagent.DocumentBlock(data, "application/pdf", "report.pdf")

	if b.Type != goagent.ContentDocument {
		t.Errorf("Type = %q, want %q", b.Type, goagent.ContentDocument)
	}
	if b.Document == nil {
		t.Fatal("Document is nil")
	}
	if b.Document.MediaType != "application/pdf" {
		t.Errorf("MediaType = %q, want %q", b.Document.MediaType, "application/pdf")
	}
	if string(b.Document.Data) != "pdf-bytes" {
		t.Errorf("Data = %q, want %q", b.Document.Data, "pdf-bytes")
	}
	if b.Document.Title != "report.pdf" {
		t.Errorf("Title = %q, want %q", b.Document.Title, "report.pdf")
	}
}

func TestHasContentType(t *testing.T) {
	t.Parallel()

	msg := goagent.Message{
		Role: goagent.RoleUser,
		Content: []goagent.ContentBlock{
			goagent.TextBlock("hello"),
			goagent.ImageBlock([]byte{0xFF}, "image/png"),
		},
	}

	tests := []struct {
		name string
		ct   goagent.ContentType
		want bool
	}{
		{"has text", goagent.ContentText, true},
		{"has image", goagent.ContentImage, true},
		{"no document", goagent.ContentDocument, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := msg.HasContentType(tt.ct); got != tt.want {
				t.Errorf("HasContentType(%q) = %v, want %v", tt.ct, got, tt.want)
			}
		})
	}

	t.Run("empty message", func(t *testing.T) {
		t.Parallel()
		empty := goagent.Message{}
		if empty.HasContentType(goagent.ContentText) {
			t.Error("empty message should not have any content type")
		}
	})
}
