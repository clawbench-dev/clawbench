package ai

import (
	"encoding/json"
	"testing"

	acp "github.com/coder/acp-go-sdk"
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
