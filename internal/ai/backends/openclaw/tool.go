package openclaw

import (
	"encoding/json"
	"strings"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends/pi"
)

// ---------------------------------------------------------------------------
// OpenClaw ACP tool parsing
// ---------------------------------------------------------------------------
//
// `openclaw acp` forwards OpenClaw's native tool arguments verbatim, and its
// titles are "<tool>: <key>: <value>, <key>: <value>" with the tool name first.
// The flat ACP remap table cannot normalize it, for two reasons:
//
//  1. `read` / `write` / `edit` carry the path under "path", but their cards read
//     "file_path". `ls` ALSO uses "path" and its card reads "path", so a global
//     path→file_path remap would blank the LS card. path→file_path therefore
//     lives in the BASE table (so tools the list does not know yet still render
//     a path) and `ls` overrides it back.
//  2. `edit` carries a nested `edits: [{oldText, newText}]` array and no flat
//     old_string/new_string. The diff renderer only reads the flat fields, so the
//     first edit has to be promoted (exactly what pi needs — see
//     normalizePiEditInput, which is reused rather than duplicated).
//
// The base table is keyed by FIELD, not by tool, so it must hold only remaps
// that are right for every tool. `path` is the one exception: it is the field
// most file tools use and the cards that read it (Read/Write/Edit) all want
// file_path, while the single tool that disagrees (ls) overrides it explicitly.
// That keeps a tool this list has never seen (e.g. a future `apply_patch`-style
// call carrying "path") rendering a path instead of a blank file header.
//
// Measured shapes (TestOpenClawACPProbe_L4_ToolFrameShapes):
//
//	read    {"path": "/a/b.go"}                              → file_path
//	ls      {"path": "/x"}                                   → path (unchanged)
//	exec    {"command": "…", "workdir": "…"}                 → command
//	write   {"path": "/x", "content": "…"}                   → file_path
//	edit    {"path": "/x", "edits": [{"oldText","newText"}]} → file_path + old_string/new_string
//	web_fetch {"url": "…"}                                   → url

// openClawACPToolName returns the bare tool name encoded at the start of an
// OpenClaw title ("read: path: /a" → "read"). Returns "" when the title does not
// start with a known tool name, which makes the normalizer fall back to the base
// remaps — the safe default for an unrecognized shape.
func openClawACPToolName(title string) string {
	if title == "" {
		return ""
	}
	name := title
	if idx := strings.Index(title, ":"); idx > 0 {
		name = title[:idx]
	}
	// A title with spaces before the colon is a phrase, not a tool name.
	if strings.ContainsAny(name, " /") {
		return ""
	}
	return name
}

// normalizeOpenClawACPInput normalizes an OpenClaw tool call's rawInput.
func normalizeOpenClawACPInput(toolName string, rawInput []byte) string {
	if len(rawInput) == 0 {
		return "{}"
	}

	// Copy so the shared table is never mutated.
	remaps := map[string]string{}
	for k, v := range openClawACPBaseRemaps {
		remaps[k] = v
	}

	switch toolName {
	case "edit":
		// path→file_path comes from the base table; the nested edits[] array
		// still needs the pi normalizer.
		return pi.NormalizePiEditInput(json.RawMessage(rawInput), remaps)
	case "ls":
		// LS's card reads "path", so the base path→file_path remap is dropped
		// for this tool. Deleted rather than reversed (file_path→path): the
		// remap pass iterates a map, so a pair of opposite remaps would apply
		// in a random order and make the result nondeterministic.
		delete(remaps, "path")
	}

	normalized, err := ai.NormalizeToolInput(rawInput, remaps)
	if err != nil {
		return string(rawInput)
	}
	return string(normalized)
}

// openClawACPBaseRemaps is the remap table applied for every OpenClaw tool.
// Kept here (not imported from backends/openclaw, which would be an import
// cycle) and kept in sync by register_test.go in that package — that test
// asserts the shared keys explicitly, so a drift is a test failure rather than
// a silent divergence.
//
// "path" is included because it is the field every file tool uses and the
// Read/Write/Edit cards all read "file_path". The one tool that disagrees (ls)
// removes it (see normalizeOpenClawACPInput).
//
// filePath→file_path is NOT here: normalizeToolInput applies it to every
// backend already (defaultMappings).
var openClawACPBaseRemaps = map[string]string{
	"path":       "file_path",
	"oldString":  "old_string",
	"newString":  "new_string",
	"replaceAll": "replace_all",
}

// The call/update sequence is shared with pi (see
// ai.NewNormalizingToolCallParser); openclaw supplies only the tool-name reader
// and the per-tool normalizer.
var (
	parseOpenClawACPToolCall, parseOpenClawACPToolCallUpdate = ai.NewNormalizingToolCallParser(
		"openclaw", openClawACPToolName, normalizeOpenClawACPInput,
	)
)

// ---------------------------------------------------------------------------
// Test-only exports
//
// The backend sub-package (backends/openclaw) cannot import internal/ai's
// unexported helpers, and internal/ai must not import the sub-package (cycle).
// These thin wrappers let that package's tests exercise the real parsing paths
// instead of re-implementing them.
// ---------------------------------------------------------------------------

// NormalizeOpenClawACPInputForTest exports normalizeOpenClawACPInput.
func NormalizeOpenClawACPInputForTest(toolName, rawInput string) string {
	return normalizeOpenClawACPInput(toolName, []byte(rawInput))
}

// OpenClawACPBaseRemapsForTest exports a copy of openClawACPBaseRemaps so the
// backend sub-package's register_test.go can pin it against
// OpenClawACPInputRemaps — the two tables live in different packages (this one
// must not import backends/openclaw: cycle) and are kept in sync only by
// convention. A copy, not the map itself, so a test cannot mutate production
// state.
func OpenClawACPBaseRemapsForTest() map[string]string {
	out := make(map[string]string, len(openClawACPBaseRemaps))
	for k, v := range openClawACPBaseRemaps {
		out[k] = v
	}
	return out
}

// ParseOpenClawToolCallUpdateForTest exports parseOpenClawACPToolCallUpdate.
func ParseOpenClawToolCallUpdateForTest(tcu acp.SessionToolCallUpdate) *ai.ToolCall {
	return parseOpenClawACPToolCallUpdate(tcu)
}
