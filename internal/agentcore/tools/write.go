package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WriteTool creates or overwrites a file.
type WriteTool struct{}

func (WriteTool) Name() string { return "write" }

func (WriteTool) Snippet() string { return "Create or overwrite a file" }

func (WriteTool) Guidelines() []string {
	return []string{"Use write only for new files or complete rewrites.", "To change part of an existing file, use edit instead: a rewrite loses everything you did not reproduce and re-sends the whole file."}
}

func (WriteTool) Spec() Spec {
	return Spec{
		Name: "write",
		Description: "Write a file, creating it if it does not exist and replacing it entirely if it does. " +
			"Parent directories are created automatically. To change part of an existing file, use edit instead.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "File path, absolute or relative to the working directory.",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "Full file content. This replaces whatever is there now.",
				},
			},
			"required": []string{"path", "content"},
		},
	}
}

type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (WriteTool) Run(env Env, raw json.RawMessage) Result {
	var args writeArgs
	if err := decodeArgs(raw, &args); err != nil {
		return Result{Text: err.Error(), IsError: true}
	}
	abs, err := env.resolve(args.Path)
	if err != nil {
		return Result{Text: err.Error(), IsError: true}
	}
	if len(args.Content) > MaxWriteBytes {
		return Result{
			Text: fmt.Sprintf("content is %d bytes, larger than the %d-byte limit for one write. "+
				"Split the file into several writes.", len(args.Content), MaxWriteBytes),
			IsError: true,
		}
	}
	if info, statErr := os.Stat(abs); statErr == nil && info.IsDir() {
		return Result{Text: fmt.Sprintf("%s is a directory.", args.Path), IsError: true}
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return Result{Text: fmt.Sprintf("cannot create parent directory: %v", err), IsError: true}
	}
	// 0644 keeps the file readable by the user's other tools; the working
	// directory is the user's own project, not a secret store.
	if err := os.WriteFile(abs, []byte(args.Content), 0o644); err != nil {
		return Result{Text: fmt.Sprintf("cannot write %s: %v", args.Path, err), IsError: true}
	}
	return Result{
		Text:    fmt.Sprintf("Wrote %d bytes to %s.", len(args.Content), args.Path),
		Display: args.Path,
	}
}
