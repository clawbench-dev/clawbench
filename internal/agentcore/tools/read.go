package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReadTool reads a text file, or returns an image for a supported binary.
type ReadTool struct{}

func (ReadTool) Name() string { return "read" }

func (ReadTool) Spec() Spec {
	return Spec{
		Name: "read",
		Description: "Read a file from the working directory. Text files come back with line numbers. " +
			"Image files (PNG, JPEG, GIF, WebP) come back as an image you can look at. " +
			"Use offset and limit to read a long file in pieces.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "File path, absolute or relative to the working directory.",
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "1-based line to start at. Omit to start from the beginning.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": fmt.Sprintf("Maximum number of lines to return (default and maximum %d).", MaxReadLines),
				},
			},
			"required": []string{"path"},
		},
	}
}

type readArgs struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

func (ReadTool) Run(env Env, raw json.RawMessage) Result {
	var args readArgs
	if err := decodeArgs(raw, &args); err != nil {
		return Result{Text: err.Error(), IsError: true}
	}
	abs, err := resolvePath(env.Root, args.Path)
	if err != nil {
		return Result{Text: err.Error(), IsError: true}
	}

	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Text: fmt.Sprintf("%s does not exist.", args.Path), IsError: true}
		}
		return Result{Text: fmt.Sprintf("cannot stat %s: %v", args.Path, err), IsError: true}
	}
	if info.IsDir() {
		return Result{
			Text:    fmt.Sprintf("%s is a directory, not a file. Use glob to list its contents.", args.Path),
			IsError: true,
		}
	}

	if mt, ok := imageMediaType(abs); ok {
		return readImage(abs, args.Path, mt, info.Size())
	}
	if isLikelyBinary(abs) {
		return Result{
			Text:    fmt.Sprintf("%s looks like a binary file and cannot be read as text.", args.Path),
			IsError: true,
		}
	}
	return readText(abs, args.Path, args.Offset, args.Limit)
}

// readImage returns the bytes as an image part.
//
// The size cap is a hard rejection rather than a truncation: half a PNG decodes
// to nothing, so a truncated image would reach the model as a corrupt picture
// and produce a confidently wrong description.
func readImage(abs, display, mediaType string, size int64) Result {
	if size > MaxImageBytes {
		return Result{
			Text: fmt.Sprintf("%s is %d bytes, larger than the %d-byte limit for images.",
				display, size, MaxImageBytes),
			IsError: true,
		}
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return Result{Text: fmt.Sprintf("cannot read %s: %v", display, err), IsError: true}
	}
	return Result{
		Text:    fmt.Sprintf("[image: %s, %s, %d bytes]", display, mediaType, len(data)),
		Image:   &Image{MediaType: mediaType, Data: data},
		Display: display,
	}
}

func readText(abs, display string, offset, limit int) Result {
	data, err := os.ReadFile(abs)
	if err != nil {
		return Result{Text: fmt.Sprintf("cannot read %s: %v", display, err), IsError: true}
	}
	if len(data) == 0 {
		return Result{Text: fmt.Sprintf("%s is empty.", display), Display: display}
	}

	// A byte cap is applied before line handling so a single enormous line
	// cannot bypass the line limit.
	truncatedByBytes := false
	if len(data) > MaxReadBytes {
		data = data[:MaxReadBytes]
		truncatedByBytes = true
	}

	lines := strings.Split(string(data), "\n")
	// A trailing newline produces a final empty element that is not a line.
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}

	start := 0
	if offset > 0 {
		start = offset - 1
	}
	if start > len(lines) {
		return Result{
			Text:    fmt.Sprintf("%s has only %d lines; offset %d is past the end.", display, len(lines), offset),
			IsError: true,
		}
	}

	want := MaxReadLines
	if limit > 0 && limit < want {
		want = limit
	}
	end := start + want
	if end > len(lines) {
		end = len(lines)
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		fmt.Fprintf(&b, "%d: %s\n", i+1, lines[i])
	}

	remaining := len(lines) - end
	if remaining > 0 {
		fmt.Fprintf(&b, "\n[%d more lines. Call read again with offset=%d to continue.]", remaining, end+1)
	} else if truncatedByBytes {
		b.WriteString(fmt.Sprintf("\n[stopped at the %d-byte read limit; the file continues beyond this point.]", MaxReadBytes))
	}
	return Result{Text: b.String(), Display: display}
}

// imageMediaType maps an extension to the media type the model APIs accept.
func imageMediaType(path string) (string, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png", true
	case ".jpg", ".jpeg":
		return "image/jpeg", true
	case ".gif":
		return "image/gif", true
	case ".webp":
		return "image/webp", true
	}
	return "", false
}

// isLikelyBinary sniffs the first block for a NUL byte, which is what
// `file(1)` effectively does for "text vs binary" and is enough to keep a
// binary blob out of the context window.
func isLikelyBinary(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false // let the read report the real error
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, 8000)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return false
	}
	return strings.ContainsRune(string(buf[:n]), 0)
}
