package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// EditTool performs an exact string replacement in a file.
//
// It refuses ambiguous edits instead of guessing. A model that says "replace
// foo with bar" when the file has four foos has not said which one it means,
// and picking one silently corrupts the file in a way the next read will not
// reveal (the model sees its own edit succeeded). Refusing costs one extra
// round; guessing costs a file.
type EditTool struct{}

func (EditTool) Name() string { return toolEditName }

func (EditTool) Snippet() string { return "Replace exact text in a file" }

func (EditTool) Guidelines() []string {
	return []string{"Use edit for targeted changes; keep old_string short but unique.", "Batch several changes to one file into one edit call rather than one call per change."}
}

func (EditTool) Spec() Spec {
	return Spec{
		Name: toolEditName,
		Description: "Replace an exact string in a file. old_string must appear exactly once unless " +
			"replace_all is true. Include enough surrounding text to make it unique. " +
			"Read the file first — you must reproduce its whitespace exactly.",
		Parameters: map[string]any{
			schemaType: schemaObject,
			schemaProperties: map[string]any{
				argPath: map[string]any{
					schemaType:        schemaString,
					schemaDescription: argPathDescription,
				},
				"old_string": map[string]any{
					schemaType:        schemaString,
					schemaDescription: "Exact text to replace, including indentation.",
				},
				"new_string": map[string]any{
					schemaType:        schemaString,
					schemaDescription: "Replacement text. Use an empty string to delete the matched text.",
				},
				"replace_all": map[string]any{
					schemaType:        schemaBoolean,
					schemaDescription: "Replace every occurrence instead of requiring a unique match.",
				},
			},
			schemaRequired: []string{argPath, "old_string", "new_string"},
		},
	}
}

type editArgs struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

func (EditTool) Run(env Env, raw json.RawMessage) Result {
	var args editArgs
	if err := decodeArgs(raw, &args); err != nil {
		return Result{Text: err.Error(), IsError: true}
	}
	if args.OldString == "" {
		return Result{Text: "old_string is empty; use write to create or replace a whole file.", IsError: true}
	}
	if args.OldString == args.NewString {
		return Result{Text: "old_string and new_string are identical; nothing would change.", IsError: true}
	}
	abs, err := env.resolve(args.Path)
	if err != nil {
		return Result{Text: err.Error(), IsError: true}
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Text: fmt.Sprintf("%s does not exist. Use write to create it.", args.Path), IsError: true}
		}
		return Result{Text: fmt.Sprintf("cannot read %s: %v", args.Path, err), IsError: true}
	}

	content := string(data)
	count := strings.Count(content, args.OldString)
	switch {
	case count == 0:
		return Result{
			Text: fmt.Sprintf("old_string was not found in %s. Read the file again and copy the text exactly, "+
				"including indentation.", args.Path),
			IsError: true,
		}
	case count > 1 && !args.ReplaceAll:
		return Result{
			Text: fmt.Sprintf("old_string appears %d times in %s. Add more surrounding text to make it unique, "+
				"or set replace_all to true to change all of them.", count, args.Path),
			IsError: true,
		}
	}

	var updated string
	if args.ReplaceAll {
		updated = strings.ReplaceAll(content, args.OldString, args.NewString)
	} else {
		updated = strings.Replace(content, args.OldString, args.NewString, 1)
	}
	if err := os.WriteFile(abs, []byte(updated), 0o644); err != nil {
		return Result{Text: fmt.Sprintf("cannot write %s: %v", args.Path, err), IsError: true}
	}

	replaced := 1
	if args.ReplaceAll {
		replaced = count
	}
	return Result{
		Text:    fmt.Sprintf("Replaced %d occurrence(s) in %s.", replaced, args.Path),
		Display: args.Path,
	}
}
