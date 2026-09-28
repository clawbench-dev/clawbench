package ai

import (
	"encoding/json"
	"strings"

	acp "github.com/coder/acp-go-sdk"
)

// parsePiToolCallEnd extracts a tool_use ToolCall from a Pi toolcall_end event.
// Pi toolcall_end provides the complete tool arguments (Done=true).
// Tool name is normalized via normalizeToolName; input field names are
// remapped using the provided remap table. The edit tool uses normalizePiEditInput
// for its nested edits array structure.
func parsePiToolCallEnd(evt *PiAssistantMessageEvent, remaps map[string]string) *ToolCall {
	if evt.ToolCall == nil {
		return nil
	}

	tc := evt.ToolCall
	normalizedInput := normalizePiToolInput(tc.Name, tc.Arguments, remaps)

	return &ToolCall{
		Name:  normalizeToolName(tc.Name),
		ID:    tc.ID,
		Input: normalizedInput,
		Done:  true,
	}
}

// parsePiToolExecutionEnd extracts a tool_result ToolCall from a Pi tool_execution_end event.
// Returns nil if toolCallId is empty. Output text is extracted from result.content[].text
// (joined with newline for multiple items) and truncated via truncateToolOutput.
func parsePiToolExecutionEnd(msg *PiStreamMessage) *ToolCall {
	if msg.ToolCallID == "" {
		return nil
	}

	var outputText string
	if msg.Result != nil {
		var parts []string
		for _, c := range msg.Result.Content {
			if c.Type == "text" && c.Text != "" {
				parts = append(parts, c.Text)
			}
		}
		if len(parts) > 0 {
			outputText = strings.Join(parts, "\n")
		}
	}

	status := "success"
	if msg.IsError {
		status = "error"
	}

	return &ToolCall{
		ID:     msg.ToolCallID,
		Output: truncateToolOutput(outputText),
		Status: status,
	}
}

// normalizePiToolInput normalizes tool input for Pi tool calls.
// For the edit tool, it uses normalizePiEditInput for nested edits array handling.
// For read/write tools, it adds path→file_path remapping on top of the base remaps.
// For all other tools, it uses normalizeToolInput with the provided remap table.
func normalizePiToolInput(toolName string, rawInput json.RawMessage, baseRemaps map[string]string) string {
	if len(rawInput) == 0 {
		return "{}"
	}

	// Copy base remaps so we don't mutate the caller's map
	remaps := map[string]string{}
	for k, v := range baseRemaps {
		remaps[k] = v
	}

	switch toolName {
	case "read", "write":
		remaps["path"] = "file_path"
	case "edit":
		remaps["path"] = "file_path"
		return normalizePiEditInput(rawInput, remaps)
	case "bash":
		// No additional remapping needed
	}

	normalized, err := normalizeToolInput([]byte(rawInput), remaps)
	if err != nil {
		return string(rawInput)
	}
	return string(normalized)
}

// normalizePiACPInput normalizes a Pi ACP tool call's rawInput.
//
// It delegates to normalizePiToolInput so the ACP path and the CLI path produce
// byte-identical tool input for the same arguments. pi-acp forwards Pi's native
// tool arguments verbatim, so rawInput has exactly the same shape the CLI parser
// sees (verified against pi-acp 0.0.34):
//
//	read  {"path":"probe-target.txt"}
//	edit  {"path":"edit-target.txt","edits":[{"oldText":"beta","newText":"BETA"}]}
//
// The generic ACP normalizer cannot be used here: it is a flat single-pass remap
// and never recurses into `edits[]`, so the nested oldText/newText would stay
// untranslated and the edit diff would render empty.
//
// The tool name is passed through so the edit branch (nested edits handling) is
// selected. Note `path` is remapped for every tool — matching the CLI, whose
// remap table also applies to all tools (grep/ls included). Whether a directory
// argument should be called file_path is a pre-existing CLI question, not one
// this ACP path should answer differently.
func normalizePiACPInput(toolName string, rawInput []byte) string {
	if len(rawInput) == 0 {
		return "{}"
	}
	return normalizePiToolInput(toolName, json.RawMessage(rawInput), piACPBaseRemaps)
}

// piACPBaseRemaps is the remap table used by the ACP path. It matches the CLI's
// PiInputRemaps so both transports normalize identically. Kept as a separate
// value (rather than importing backends/pi, which would be an import cycle)
// because internal/ai must not depend on backend sub-packages.
var piACPBaseRemaps = map[string]string{
	"path": "file_path",
}

// parsePiACPToolCall extracts a ToolCall from a Pi ACP tool_call event.
func parsePiACPToolCall(tc acp.SessionUpdateToolCall) *ToolCall {
	tool := &ToolCall{
		Name: extractToolName(tc.Title, tc.Kind, "pi", string(tc.ToolCallId)),
		ID:   string(tc.ToolCallId),
		Done: false,
	}
	if tc.RawInput != nil {
		if inputBytes, err := json.Marshal(tc.RawInput); err == nil {
			tool.Input = normalizePiACPInput(piACPToolNameForInput(tc.Title), inputBytes)
		}
	}
	return tool
}

// parsePiACPToolCallUpdate extracts a ToolCall from a Pi ACP tool_call_update.
func parsePiACPToolCallUpdate(tcu acp.SessionToolCallUpdate) *ToolCall {
	tool := &ToolCall{ID: string(tcu.ToolCallId)}
	mapToolCallStatus(tcu.Status, tool)
	if tcu.RawInput != nil {
		if inputBytes, err := json.Marshal(tcu.RawInput); err == nil {
			title := ""
			if tcu.Title != nil {
				title = *tcu.Title
			}
			tool.Input = normalizePiACPInput(piACPToolNameForInput(title), inputBytes)
		}
	} else {
		mapToolCallInput(tcu, tool, "pi")
	}
	mapToolCallName(tcu, tool, "pi")
	if tool.Done {
		mapToolCallOutput(tcu, tool)
	}
	return tool
}

// piACPToolNameForInput returns the raw Pi tool name used to select the
// normalization branch. Pi's ACP title is the bare tool name ("edit", "read"),
// which is what normalizePiToolInput switches on. A decorated title (spaces or
// a slash) is not a bare tool name, so it is ignored — normalizePiToolInput then
// applies only the base remaps, which is the safe default.
func piACPToolNameForInput(title string) string {
	if title != "" && !strings.ContainsAny(title, " /") {
		return title
	}
	return ""
}

// normalizePiEditInput handles the nested edits array in Pi's edit tool input,
// remapping both top-level fields and nested oldText/newText fields, then
// flattening the first edit into top-level old_string/new_string.
func normalizePiEditInput(rawInput json.RawMessage, topRemaps map[string]string) string {
	var input map[string]any
	if err := json.Unmarshal([]byte(rawInput), &input); err != nil {
		return string(rawInput)
	}

	// Apply top-level remaps
	for from, to := range topRemaps {
		if v, ok := input[from]; ok {
			delete(input, from)
			input[to] = v
		}
	}

	// Remap fields inside edits array: oldText→old_string, newText→new_string
	if editsRaw, ok := input["edits"]; ok {
		if edits, ok := editsRaw.([]any); ok {
			for i, editRaw := range edits {
				if edit, ok := editRaw.(map[string]any); ok {
					if v, ok := edit["oldText"]; ok {
						delete(edit, "oldText")
						edit["old_string"] = v
					}
					if v, ok := edit["newText"]; ok {
						delete(edit, "newText")
						edit["new_string"] = v
					}
					edits[i] = edit
				}
			}
			input["edits"] = edits
		}
	}

	// Promote the first edit to top level. The renderer (renderEditDiff) reads
	// only flat old_string/new_string and has no edits[] handling anywhere, so
	// without this promotion an edit renders as a file header with an empty
	// diff. Multi-edit calls show their first edit; the full list stays under
	// `edits` for any consumer that wants it.
	promoteFirstEditToTopLevel(input)

	normalized, err := json.Marshal(input)
	if err != nil {
		return string(rawInput)
	}
	return string(normalized)
}

// promoteFirstEditToTopLevel copies edits[0].old_string/new_string up to the
// top level when the top level does not already carry them. No-op when there is
// no edits array or when the tool supplied flat fields directly.
func promoteFirstEditToTopLevel(input map[string]any) {
	edits, ok := input["edits"].([]any)
	if !ok || len(edits) == 0 {
		return
	}
	first, ok := edits[0].(map[string]any)
	if !ok {
		return
	}
	if _, exists := input["old_string"]; !exists {
		if v, ok := first["old_string"]; ok {
			input["old_string"] = v
		}
	}
	if _, exists := input["new_string"]; !exists {
		if v, ok := first["new_string"]; ok {
			input["new_string"] = v
		}
	}
}
