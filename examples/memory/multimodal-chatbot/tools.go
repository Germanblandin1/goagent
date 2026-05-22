package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ledongthuc/pdf"

	"github.com/Germanblandin1/goagent"
)

// fileKind distinguishes between image and document content blocks.
type fileKind int

const (
	kindImage    fileKind = iota
	kindDocument fileKind = iota
)

type fileType struct {
	kind      fileKind
	mediaType string
}

// detectFileType returns the kind and MIME type for the given path based on
// its extension. Returns an error for unsupported extensions.
func detectFileType(path string) (fileType, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return fileType{kindImage, "image/jpeg"}, nil
	case ".png":
		return fileType{kindImage, "image/png"}, nil
	case ".gif":
		return fileType{kindImage, "image/gif"}, nil
	case ".webp":
		return fileType{kindImage, "image/webp"}, nil
	case ".pdf":
		return fileType{kindDocument, "application/pdf"}, nil
	case ".txt":
		return fileType{kindDocument, "text/plain"}, nil
	default:
		return fileType{}, fmt.Errorf(
			"load_file: unsupported extension %q — supported: jpg, png, gif, webp, pdf, txt",
			strings.ToLower(filepath.Ext(path)),
		)
	}
}

// isSupportedFile reports whether the file name has a supported extension
// (images or documents).
func isSupportedFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".pdf", ".txt":
		return true
	}
	return false
}

// NewLoadFileTool returns a Tool that reads an image or document from the local
// filesystem and returns the appropriate content block for the model to process.
//
// Images are returned as ImageBlock (base64 data URL).
// Documents are extracted as plain text and returned as TextBlock:
//   - PDF (.pdf): text extracted via ledongthuc/pdf
//   - Plain text (.txt): content read directly
func NewLoadFileTool() goagent.Tool {
	return goagent.ToolBlocksFunc(
		"load_file",
		"Load an image or document from the local filesystem so it can be analyzed. "+
			"Images: jpg, png, gif, webp. Documents: pdf, txt. "+
			"Call this tool when the user mentions a file path or asks to analyze a file.",
		goagent.SchemaFrom(struct {
			Path string `json:"path" jsonschema_description:"Absolute or relative path to the file to load. Supported images: jpg, png, gif, webp. Supported documents: pdf, txt."`
		}{}),
		func(ctx context.Context, args map[string]any) ([]goagent.ContentBlock, error) {
			path, ok := args["path"].(string)
			if !ok || path == "" {
				return nil, fmt.Errorf("load_file: 'path' argument is required")
			}

			ft, err := detectFileType(path)
			if err != nil {
				return nil, err
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("load_file: reading %q: %w", path, err)
			}

			switch ft.kind {
			case kindImage:
				return []goagent.ContentBlock{
					goagent.TextBlock(fmt.Sprintf("Image loaded: %s (%s, %d bytes)", path, ft.mediaType, len(data))),
					goagent.ImageBlock(data, ft.mediaType),
				}, nil
			default: // kindDocument
				text, err := extractDocumentText(path, ft.mediaType)
				if err != nil {
					return nil, err
				}
				return []goagent.ContentBlock{
					goagent.TextBlock(fmt.Sprintf("Document: %s (%s, %d bytes)\n\n%s", path, ft.mediaType, len(data), text)),
				}, nil
			}
		},
	)
}

// extractDocumentText returns the plain text content of a document.
// PDF files are parsed with ledongthuc/pdf; text files are read directly.
func extractDocumentText(path, mediaType string) (string, error) {
	switch mediaType {
	case "text/plain":
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("load_file: reading %q: %w", path, err)
		}
		return string(data), nil
	case "application/pdf":
		text, err := extractPDFText(path)
		if err != nil {
			return "", fmt.Errorf("load_file: extracting text from %q: %w", path, err)
		}
		if strings.TrimSpace(text) == "" {
			return "", fmt.Errorf("load_file: no extractable text in %q (may be a scanned/image-only PDF)", path)
		}
		return text, nil
	default:
		return "", fmt.Errorf("load_file: unsupported document type %q", mediaType)
	}
}

// extractPDFText uses ledongthuc/pdf to extract all text from a PDF file.
func extractPDFText(path string) (string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var buf bytes.Buffer
	for i := 1; i <= r.NumPage(); i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			continue // skip unreadable pages rather than aborting
		}
		buf.WriteString(text)
	}
	return buf.String(), nil
}

// NewScanDirTool returns a Tool that lists supported files (images and documents)
// inside a directory. By default it scans only the top level; set recursive=true
// to walk sub-directories.
func NewScanDirTool() goagent.Tool {
	return goagent.ToolFunc(
		"scan_dir",
		"List supported files (images: jpg, png, gif, webp; documents: pdf, txt) in a directory. "+
			"Use this to discover available files before loading one with load_file.",
		goagent.SchemaFrom(struct {
			Path      string `json:"path" jsonschema_description:"Absolute or relative path to the directory to scan."`
			Recursive bool   `json:"recursive,omitempty" jsonschema_description:"If true, walk all sub-directories. Defaults to false."`
		}{}),
		func(ctx context.Context, args map[string]any) (string, error) {
			dir, ok := args["path"].(string)
			if !ok || dir == "" {
				return "", fmt.Errorf("scan_dir: 'path' argument is required")
			}

			recursive, _ := args["recursive"].(bool)

			info, err := os.Stat(dir)
			if err != nil {
				return "", fmt.Errorf("scan_dir: %w", err)
			}
			if !info.IsDir() {
				return "", fmt.Errorf("scan_dir: %q is not a directory", dir)
			}

			var found []string

			if recursive {
				err = filepath.WalkDir(dir, func(p string, d os.DirEntry, walkErr error) error {
					if walkErr != nil {
						return nil // skip unreadable entries
					}
					if !d.IsDir() && isSupportedFile(p) {
						found = append(found, p)
					}
					return nil
				})
				if err != nil {
					return "", fmt.Errorf("scan_dir: walking directory: %w", err)
				}
			} else {
				entries, err2 := os.ReadDir(dir)
				if err2 != nil {
					return "", fmt.Errorf("scan_dir: reading directory: %w", err2)
				}
				for _, e := range entries {
					if !e.IsDir() && isSupportedFile(e.Name()) {
						found = append(found, filepath.Join(dir, e.Name()))
					}
				}
			}

			if len(found) == 0 {
				return fmt.Sprintf("No supported files found in %q.", dir), nil
			}

			sort.Strings(found)

			var sb strings.Builder
			fmt.Fprintf(&sb, "Found %d file(s) in %q:\n", len(found), dir)
			for _, p := range found {
				fi, err := os.Stat(p)
				if err != nil {
					fmt.Fprintf(&sb, "  %s\n", p)
					continue
				}
				ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(p)), ".")
				fmt.Fprintf(&sb, "  %s  (%s, %d bytes)\n", p, ext, fi.Size())
			}
			return sb.String(), nil
		},
	)
}
