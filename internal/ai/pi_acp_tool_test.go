package ai

import (
	"encoding/json"
	"testing"

	acp "github.com/coder/acp-go-sdk"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin the Pi ACP tool-input path against the rawInput shapes
// pi-acp 0.0.34 actually forwards (Pi's native arguments, not canonical ACP
// names). The generic ACP normalizer is a flat single-pass remap, so it cannot
// translate the nested edits[] array; Pi therefore has its own parser.

func mustParsePiACPInput(t *testing.T, raw string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("input is not valid JSON: %v (%s)", err, raw)
	}
	return m
}

func TestParsePiACPToolCall_ReadRemapsPath(t *testing.T) {
	tc := parsePiACPToolCall(acp.SessionUpdateToolCall{
		ToolCallId: "call_1",
		Title:      "read",
		Kind:       acp.ToolKindRead,
		RawInput:   map[string]any{"path": "probe-target.txt"},
	})
	if tc.Name != "Read" {
		t.Errorf("tool name = %q, want Read", tc.Name)
	}
	m := mustParsePiACPInput(t, tc.Input)
	if m["file_path"] != "probe-target.txt" {
		t.Errorf("path not remapped to file_path: %v", m)
	}
	if _, still := m["path"]; still {
		t.Errorf("raw path survived: %v", m)
	}
}

// TestParsePiACPToolCall_EditFlattensNestedEdits is the regression guard for the
// empty-diff bug: pi-acp nests the pair under edits[], and the renderer reads
// only flat old_string/new_string.
func TestParsePiACPToolCall_EditFlattensNestedEdits(t *testing.T) {
	tc := parsePiACPToolCall(acp.SessionUpdateToolCall{
		ToolCallId: "call_2",
		Title:      "edit",
		Kind:       acp.ToolKindEdit,
		RawInput: map[string]any{
			"path":  "edit-target.txt",
			"edits": []any{map[string]any{"oldText": "beta", "newText": "BETA"}},
		},
	})
	if tc.Name != "Edit" {
		t.Errorf("tool name = %q, want Edit", tc.Name)
	}
	m := mustParsePiACPInput(t, tc.Input)
	if m["file_path"] != "edit-target.txt" {
		t.Errorf("path not remapped: %v", m)
	}
	// Without the promotion these are absent and renderEditDiff draws no body.
	if m["old_string"] != "beta" || m["new_string"] != "BETA" {
		t.Errorf("nested edit not flattened to top level: old=%v new=%v (%s)",
			m["old_string"], m["new_string"], tc.Input)
	}
}

// TestParsePiACPToolCall_GrepKeepsPathRemap documents a deliberate choice: Pi
// ACP remaps `path` for every tool, exactly like the Pi CLI (whose remap table
// also applies to all tools — see pi_tool_test.go TestParsePiToolCallEnd_GrepTool).
// ACP and CLI must not diverge on the same arguments.
func TestParsePiACPToolCall_GrepKeepsPathRemap(t *testing.T) {
	tc := parsePiACPToolCall(acp.SessionUpdateToolCall{
		ToolCallId: "call_3",
		Title:      "grep",
		Kind:       acp.ToolKindSearch,
		RawInput:   map[string]any{"path": "/home/u/proj", "pattern": "func main"},
	})
	m := mustParsePiACPInput(t, tc.Input)
	if m["file_path"] != "/home/u/proj" {
		t.Errorf("grep path should be remapped to match the CLI path: %v", m)
	}
	if m["pattern"] != "func main" {
		t.Errorf("pattern must be preserved: %v", m)
	}
}

func TestParsePiACPToolCallUpdate_EditFlattensNestedEdits(t *testing.T) {
	title := "edit"
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "call_4",
		Title:      &title,
		RawInput: map[string]any{
			"path":  "f.txt",
			"edits": []any{map[string]any{"oldText": "a", "newText": "b"}},
		},
	}
	tc := parsePiACPToolCallUpdate(tcu)
	m := mustParsePiACPInput(t, tc.Input)
	if m["file_path"] != "f.txt" {
		t.Errorf("path not remapped: %v", m)
	}
	if m["old_string"] != "a" || m["new_string"] != "b" {
		t.Errorf("nested edit not flattened on the update path: %v", m)
	}
}

// TestPiACPRemapMatchesCLIPath is the cross-transport parity guard: the same
// arguments through the ACP normalizer and the CLI normalizer must produce
// identical output, or Pi behaves differently depending on transport.
func TestPiACPRemapMatchesCLIPath(t *testing.T) {
	cases := map[string]string{
		"read":  `{"path":"probe-target.txt"}`,
		"write": `{"path":"out.txt","content":"hi"}`,
		"edit":  `{"path":"f.txt","edits":[{"oldText":"a","newText":"b"},{"oldText":"c","newText":"d"}]}`,
		"grep":  `{"path":"/home/u/proj","pattern":"func main"}`,
		"ls":    `{"path":"/home/u/proj"}`,
	}
	for name, raw := range cases {
		acpOut := normalizePiACPInput(name, []byte(raw))
		cliOut := normalizePiToolInput(name, json.RawMessage(raw), piACPBaseRemaps)
		if acpOut != cliOut {
			t.Errorf("%s: ACP and CLI normalizers diverged\n  acp=%s\n  cli=%s", name, acpOut, cliOut)
		}
	}
}

// ── Dispatch routing ─────────────────────────────────────────────────────────

// TestParseACPToolCall_RoutesPiToPiParser guards the `case "pi"` branch. The
// discriminating assertion is the nested-edits flattening: only the Pi parser
// does it, so if the branch fell through to the generic parser the input would
// keep `edits[]` and lose old_string/new_string.
func TestParseACPToolCall_RoutesPiToPiParser(t *testing.T) {
	tc := parseACPToolCall("pi", acp.SessionUpdateToolCall{
		ToolCallId: "call_dispatch_1",
		Title:      "edit",
		Kind:       acp.ToolKindEdit,
		RawInput: map[string]any{
			"path":  "f.txt",
			"edits": []any{map[string]any{"oldText": "a", "newText": "b"}},
		},
	})
	m := mustParsePiACPInput(t, tc.Input)
	if m["old_string"] != "a" || m["new_string"] != "b" {
		t.Errorf("dispatch did not reach the Pi parser (nested edits not flattened): %v", m)
	}
	if m["file_path"] != "f.txt" {
		t.Errorf("path not remapped: %v", m)
	}
}

// TestParseACPToolCall_NonPiBackendStaysGeneric is the control for the test
// above: an unlisted backend must NOT get Pi's nested-edits handling, or the
// `case "pi"` assertion would pass even if every backend routed to Pi.
func TestParseACPToolCall_NonPiBackendStaysGeneric(t *testing.T) {
	tc := parseACPToolCall("grok", acp.SessionUpdateToolCall{
		ToolCallId: "call_dispatch_2",
		Title:      "edit",
		Kind:       acp.ToolKindEdit,
		RawInput: map[string]any{
			"path":  "f.txt",
			"edits": []any{map[string]any{"oldText": "a", "newText": "b"}},
		},
	})
	if tc.Input == "" {
		t.Fatal("generic parser produced no input")
	}
	m := mustParsePiACPInput(t, tc.Input)
	// The generic flat remap does not recurse, so the flattened pair is absent.
	if _, flattened := m["old_string"]; flattened {
		t.Errorf("generic backend unexpectedly got Pi's nested-edits flattening: %v", m)
	}
}

// TestParseACPToolCallUpdate_RoutesPiToPiParser covers the update-path branch,
// which is a separate switch and can drift from the initial-call one.
func TestParseACPToolCallUpdate_RoutesPiToPiParser(t *testing.T) {
	title := "edit"
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "call_dispatch_3",
		Title:      &title,
		RawInput: map[string]any{
			"path":  "f.txt",
			"edits": []any{map[string]any{"oldText": "a", "newText": "b"}},
		},
	}
	tc := parseACPToolCallUpdate("pi", tcu)
	m := mustParsePiACPInput(t, tc.Input)
	if m["old_string"] != "a" || m["new_string"] != "b" {
		t.Errorf("update dispatch did not reach the Pi parser: %v", m)
	}
}

// TestParseACPToolCallUpdate_NonPiBackendStaysGeneric is the control for the
// update-path dispatch.
func TestParseACPToolCallUpdate_NonPiBackendStaysGeneric(t *testing.T) {
	title := "edit"
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "call_dispatch_4",
		Title:      &title,
		RawInput: map[string]any{
			"path":  "f.txt",
			"edits": []any{map[string]any{"oldText": "a", "newText": "b"}},
		},
	}
	tc := parseACPToolCallUpdate("grok", tcu)
	m := mustParsePiACPInput(t, tc.Input)
	if _, flattened := m["old_string"]; flattened {
		t.Errorf("generic update backend unexpectedly got Pi's flattening: %v", m)
	}
}

// ── normalizePiACPInput / parsePiACPToolCallUpdate branches ──────────────────

func TestNormalizePiACPInput_EmptyInputReturnsEmptyObject(t *testing.T) {
	// An empty rawInput must serialize as "{}" rather than "" — the renderer
	// parses this string, and "" is not valid JSON.
	assert.Equal(t, "{}", normalizePiACPInput("read", nil))
	assert.Equal(t, "{}", normalizePiACPInput("read", []byte{}))
}

// TestParsePiACPToolCallUpdate_NilRawInputFallsBackToLocations covers the else
// branch: pi-acp omits rawInput on updates that only carry locations, and the
// shared helper is what turns those into an input. The Kind must be present —
// `extractInputFromLocationsAndTitle` keys off it to decide whether a location
// is a path worth surfacing.
func TestParsePiACPToolCallUpdate_NilRawInputFallsBackToLocations(t *testing.T) {
	title := "read"
	kind := acp.ToolKindRead
	inProgress := acp.ToolCallStatusInProgress
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "call_loc_1",
		Title:      &title,
		Kind:       &kind,
		Status:     &inProgress,
		Locations:  []acp.ToolCallLocation{{Path: "/proj/main.go"}},
	}
	tc := parsePiACPToolCallUpdate(tcu)
	if tc.Input == "" {
		t.Fatal("no input produced from locations-only update")
	}
	assert.Contains(t, tc.Input, "main.go",
		"the location path must survive into the input")
}

// TestParsePiACPToolCallUpdate_CompletedMapsOutput covers the `tool.Done`
// branch that attaches raw output.
func TestParsePiACPToolCallUpdate_CompletedMapsOutput(t *testing.T) {
	completed := acp.ToolCallStatusCompleted
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "call_done_1",
		Status:     &completed,
		RawOutput:  "the tool output",
	}
	tc := parsePiACPToolCallUpdate(tcu)
	require.True(t, tc.Done, "completed status must mark the call done")
	assert.Equal(t, "the tool output", tc.Output,
		"a done call must carry its raw output")
}

// TestPiACPToolNameForInput_RejectsDecoratedTitle covers the empty-return
// branch: a decorated title is not a bare Pi tool name, so the caller must fall
// back to base remaps rather than selecting a wrong branch.
func TestPiACPToolNameForInput_RejectsDecoratedTitle(t *testing.T) {
	assert.Equal(t, "edit", piACPToolNameForInput("edit"), "bare name passes through")
	assert.Equal(t, "", piACPToolNameForInput(""), "empty title has no name")
	assert.Equal(t, "", piACPToolNameForInput("Edit File"),
		"a title with a space is not a bare tool name")
	assert.Equal(t, "", piACPToolNameForInput("tools/edit"),
		"a title with a slash is not a bare tool name")
}
