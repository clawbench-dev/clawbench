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

func (ReadTool) Name() string { return toolReadName }

func (ReadTool) Snippet() string { return "Read a file (text with line numbers, or an image)" }

func (ReadTool) Guidelines() []string {
	return []string{"Use read to examine files instead of cat, head, tail or sed."}
}

func (ReadTool) Spec() Spec {
	return Spec{
		Name: toolReadName,
		Description: "Read a file from the working directory. Text files come back with line numbers. " +
			"Image files (PNG, JPEG, GIF, WebP) come back as an image you can look at. " +
			"Use offset and limit to read a long file in pieces.",
		Parameters: map[string]any{
			schemaType: schemaObject,
			schemaProperties: map[string]any{
				argPath: map[string]any{
					schemaType:        schemaString,
					schemaDescription: argPathDescription,
				},
				argOffset: map[string]any{
					schemaType:        schemaInteger,
					schemaDescription: "1-based line to start at. Omit to start from the beginning.",
				},
				argLimit: map[string]any{
					schemaType:        schemaInteger,
					schemaDescription: fmt.Sprintf("Maximum number of lines to return (default and maximum %d).", MaxReadLines),
				},
			},
			schemaRequired: []string{argPath},
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
	abs, err := env.resolve(args.Path)
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

	// The byte cap bounds ONE read. The cut is made on a line boundary: half a
	// line handed to the model is a line it will try to address, and continuing
	// from it would either duplicate or drop content.
	truncatedByBytes := false
	if len(data) > MaxReadBytes {
		data = data[:MaxReadBytes]
		truncatedByBytes = true
	}

	lines := splitReadLines(string(data), truncatedByBytes)
	completeLines := len(lines)

	start := 0
	if offset > 0 {
		start = offset - 1
	}
	// One line past the last complete line is legal: that is exactly the offset
	// this tool's own note hands out when the cut left the last line incomplete,
	// and answering it with "has only N lines" sent the model in a circle.
	if start > completeLines {
		return Result{
			Text:    fmt.Sprintf("%s has only %d lines; offset %d is past the end.", display, completeLines, offset),
			IsError: true,
		}
	}

	want := MaxReadLines
	if limit > 0 && limit < want {
		want = limit
	}
	end := start + want
	if end > completeLines {
		end = completeLines
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		fmt.Fprintf(&b, "%d: %s\n", i+1, lines[i])
	}

	switch {
	case truncatedByBytes:
		// The BYTE cap stopped this read, so more of the file exists after what
		// was shown. Naming the next offset is the whole point: without it the
		// model guessed one and got "has only N lines" — N being the fiction the
		// cut had just created. When the cut left the LAST line incomplete the
		// next offset points at the same line, which is by then complete.
		fmt.Fprintf(&b, "\n[Stopped at the %d-byte read limit. More of the file follows; "+
			"call read again with offset=%d to continue.]", MaxReadBytes, end+1)
	case end < completeLines:
		fmt.Fprintf(&b, "\n[%d more lines. Call read again with offset=%d to continue.]", completeLines-end, end+1)
	}
	return Result{Text: b.String(), Display: display}
}

// splitReadLines splits the bytes already read into addressable lines, dropping
// the element a trailing newline leaves behind and — when the byte cap cut the
// buffer — the final INCOMPLETE line.
//
// The incomplete line is neither shown (the model would get half a line) nor
// counted (the note would point at content that was never shown). It is dropped
// rather than kept-but-hidden so the count the note quotes is the count the
// model can address.
func splitReadLines(text string, truncatedByBytes bool) []string {
	lines := strings.Split(text, "\n")
	// A trailing newline produces a final empty element that is not a line.
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	// The cap cut the buffer at an arbitrary byte, so the last element is a
	// partial line.
	if truncatedByBytes && len(lines) > 0 {
		lines = lines[:len(lines)-1]
	}
	return lines
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
