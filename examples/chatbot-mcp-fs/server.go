package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Germanblandin1/goagent/mcp"
)

// runServer starts an MCP stdio server that exposes two filesystem tools:
//
//   - list_dir: lists the contents of a directory
//   - read_file: returns the text content of a file
//
// All paths are resolved relative to root and validated to prevent directory
// traversal — no path can escape above root.
func runServer(root string) error {
	s := mcp.NewServer("fs", "1.0.0")

	s.MustAddTool("list_dir",
		"Lists the files and sub-directories inside a directory. "+
			`Use "." to list the working directory.`,
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": `Relative path to the directory (e.g. ".", "src", "src/util"). Defaults to ".".`,
				},
			},
		},
		func(_ context.Context, args map[string]any) (string, error) {
			rel, _ := args["path"].(string)
			if rel == "" {
				rel = "."
			}
			abs, err := safePath(root, rel)
			if err != nil {
				// Business error: model receives the message and can self-correct.
				return err.Error(), nil
			}
			return listDir(abs, root)
		},
	)

	s.MustAddTool("read_file",
		"Returns the text content of a file. "+
			fmt.Sprintf("Files larger than %d KB are rejected.", maxFileKB),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": `Relative path to the file (e.g. "README.md", "src/main.go").`,
				},
			},
			"required": []string{"path"},
		},
		func(_ context.Context, args map[string]any) (string, error) {
			rel, _ := args["path"].(string)
			abs, err := safePath(root, rel)
			if err != nil {
				return err.Error(), nil
			}
			return readFile(abs)
		},
	)

	return s.ServeStdio()
}

// safePath resolves userPath relative to root and verifies the result is
// still inside root, preventing directory traversal attacks.
//
// Rejected inputs:
//   - Absolute paths (e.g. "/etc/passwd", "C:\Windows")
//   - Paths that escape root after cleaning (e.g. "../../etc/passwd", "../..")
func safePath(root, userPath string) (string, error) {
	// filepath.Clean normalises ".", "..", double slashes, and similar.
	clean := filepath.Clean(userPath)

	// Reject absolute paths outright — callers must use relative paths.
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("absolute paths are not allowed: use a path relative to the working directory")
	}

	// Join with root and resolve the final absolute path.
	abs := filepath.Join(root, clean)

	// Verify the result is still under root.
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path %q escapes the working directory — only paths inside it are allowed", userPath)
	}

	return abs, nil
}

const maxFileKB = 100
const maxFileBytes = maxFileKB * 1024

func listDir(abs, root string) (string, error) {
	entries, err := os.ReadDir(abs)
	if err != nil {
		return fmt.Sprintf("cannot read directory: %v", err), nil
	}

	relBase, _ := filepath.Rel(root, abs)

	var sb strings.Builder
	fmt.Fprintf(&sb, "Contents of %s:\n", relBase)

	for _, e := range entries {
		info, _ := e.Info()
		if e.IsDir() {
			fmt.Fprintf(&sb, "  [dir]  %s/\n", e.Name())
		} else if info != nil {
			fmt.Fprintf(&sb, "  [file] %-40s  %d bytes\n", e.Name(), info.Size())
		} else {
			fmt.Fprintf(&sb, "  [file] %s\n", e.Name())
		}
	}

	if len(entries) == 0 {
		sb.WriteString("  (empty)\n")
	}

	return sb.String(), nil
}

func readFile(abs string) (string, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Sprintf("cannot access path: %v", err), nil
	}
	if info.IsDir() {
		return "that path is a directory — use list_dir to explore it", nil
	}
	if info.Size() > maxFileBytes {
		return fmt.Sprintf("file is too large (%d bytes); maximum allowed is %d KB",
			info.Size(), maxFileKB), nil
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Sprintf("cannot read file: %v", err), nil
	}
	return string(data), nil
}
