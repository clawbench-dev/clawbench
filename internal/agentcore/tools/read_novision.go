package tools

import (
	"encoding/json"
	"fmt"
	"os"
)

// ReadToolNoVision is ReadTool for a model that cannot accept image input.
//
// It exists as a separate type rather than a flag so the tool list can be built
// without mutating shared state, and so the spec's description can honestly say
// images are not available. A model told "images come back as an image" that
// then receives a text note concludes the read failed.
type ReadToolNoVision struct{}

func (ReadToolNoVision) Name() string { return toolReadName }

func (ReadToolNoVision) Snippet() string {
	return "Read a text file (with line numbers)"
}

func (ReadToolNoVision) Guidelines() []string {
	return []string{
		"Use read to examine files instead of cat, head, tail or sed.",
	}
}

func (ReadToolNoVision) Spec() Spec {
	return Spec{
		Name: toolReadName,
		Description: "Read a text file from the working directory, with line numbers. " +
			"Binary files cannot be read. Use offset and limit to read a long file in pieces.",
		Parameters: map[string]any{
			schemaType: schemaObject,
			schemaProperties: map[string]any{
				argPath: map[string]any{
					schemaType:        schemaString,
					schemaDescription: argPathDescription,
				},
				argOffset: map[string]any{
					schemaType:        schemaInteger,
					schemaDescription: "1-based line to start at.",
				},
				argLimit: map[string]any{
					schemaType:        schemaInteger,
					schemaDescription: fmt.Sprintf("Maximum lines to return (default and maximum %d).", MaxReadLines),
				},
			},
			schemaRequired: []string{argPath},
		},
	}
}

func (ReadToolNoVision) Run(env Env, raw json.RawMessage) Result {
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
		// Naming the reason is what lets the model route around it (ask the
		// user, read a neighboring text file) instead of retrying.
		return Result{
			Text: fmt.Sprintf("%s is an image (%s) and the current model cannot read images. "+
				"Ask the user to describe it, or work from text files instead.", args.Path, mt),
			IsError: true,
		}
	}
	if isLikelyBinary(abs) {
		return Result{
			Text:    fmt.Sprintf("%s is a binary file and cannot be read as text.", args.Path),
			IsError: true,
		}
	}
	return readText(abs, args.Path, args.Offset, args.Limit)
}
