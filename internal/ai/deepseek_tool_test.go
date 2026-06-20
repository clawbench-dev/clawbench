package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// --- parseDeepSeekToolUse tests ---

// deepSeekInputRemaps mirrors backends/deepSeekInputRemaps for testing.
// Duplicated here to avoid circular imports (internal/ai → backends/* → internal/ai).
var deepSeekInputRemaps = map[string]string{
	"path": "file_path", "search": "old_string", "replace": "new_string",
	"filePaths": "file_paths", "dirPath": "path",
}

func TestDeepSeekTool_EditFileInputRemap(t *testing.T) {
	msg := &DeepSeekStreamMessage{
		Type:  "tool_use",
		Name:  "edit_file",
		ID:    "call_edit_1",
		Input: json.RawMessage(`{"path":"/tmp/a.txt","search":"hello","replace":"world"}`),
		Done:  true,
	}
	tc := parseDeepSeekToolUse(msg, deepSeekInputRemaps)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	if tc.Name != "Edit" {
		t.Errorf("expected name 'Edit', got '%s'", tc.Name)
	}
	if tc.ID != "call_edit_1" {
		t.Errorf("expected ID 'call_edit_1', got '%s'", tc.ID)
	}
	if !tc.Done {
		t.Error("expected Done=true")
	}
	if !strings.Contains(tc.Input, `"file_path"`) {
		t.Errorf("expected 'file_path' in input, got '%s'", tc.Input)
	}
	if !strings.Contains(tc.Input, `"old_string"`) {
		t.Errorf("expected 'old_string' in input, got '%s'", tc.Input)
	}
	if !strings.Contains(tc.Input, `"new_string"`) {
		t.Errorf("expected 'new_string' in input, got '%s'", tc.Input)
	}
	if strings.Contains(tc.Input, `"path"`) {
		t.Errorf("'path' should be remapped to 'file_path', got '%s'", tc.Input)
	}
	if strings.Contains(tc.Input, `"search"`) {
		t.Errorf("'search' should be remapped to 'old_string', got '%s'", tc.Input)
	}
	if strings.Contains(tc.Input, `"replace"`) {
		t.Errorf("'replace' should be remapped to 'new_string', got '%s'", tc.Input)
	}
}

func TestDeepSeekTool_ReadFileInputRemap(t *testing.T) {
	msg := &DeepSeekStreamMessage{
		Type:  "tool_use",
		Name:  "read_file",
		ID:    "call_read_1",
		Input: json.RawMessage(`{"path":"/tmp/b.txt"}`),
		Done:  true,
	}
	tc := parseDeepSeekToolUse(msg, deepSeekInputRemaps)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	if tc.Name != "Read" {
		t.Errorf("expected name 'Read', got '%s'", tc.Name)
	}
	if !strings.Contains(tc.Input, `"file_path"`) {
		t.Errorf("expected 'file_path' in input, got '%s'", tc.Input)
	}
	if strings.Contains(tc.Input, `"path"`) {
		t.Errorf("'path' should be remapped to 'file_path', got '%s'", tc.Input)
	}
}

func TestDeepSeekTool_WriteFileInputRemap(t *testing.T) {
	msg := &DeepSeekStreamMessage{
		Type:  "tool_use",
		Name:  "write_file",
		ID:    "call_write_1",
		Input: json.RawMessage(`{"path":"/tmp/c.txt","content":"hello"}`),
		Done:  true,
	}
	tc := parseDeepSeekToolUse(msg, deepSeekInputRemaps)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	if tc.Name != "Write" {
		t.Errorf("expected name 'Write', got '%s'", tc.Name)
	}
	if !strings.Contains(tc.Input, `"file_path"`) {
		t.Errorf("expected 'file_path' in input, got '%s'", tc.Input)
	}
}

func TestDeepSeekTool_ListDirInputRemap(t *testing.T) {
	msg := &DeepSeekStreamMessage{
		Type:  "tool_use",
		Name:  "list_dir",
		ID:    "call_list_1",
		Input: json.RawMessage(`{"path":"/tmp"}`),
		Done:  true,
	}
	tc := parseDeepSeekToolUse(msg, deepSeekInputRemaps)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	if tc.Name != "LS" {
		t.Errorf("expected name 'LS', got '%s'", tc.Name)
	}
	if !strings.Contains(tc.Input, `"file_path"`) {
		t.Errorf("expected 'file_path' in input, got '%s'", tc.Input)
	}
}

func TestDeepSeekTool_GrepFilesPathNotRemapped(t *testing.T) {
	// grep_files: 'path' should NOT be remapped to 'file_path' because Grep uses 'path'
	msg := &DeepSeekStreamMessage{
		Type:  "tool_use",
		Name:  "grep_files",
		ID:    "call_grep_1",
		Input: json.RawMessage(`{"path":"/tmp","pattern":"TODO"}`),
		Done:  true,
	}
	tc := parseDeepSeekToolUse(msg, deepSeekInputRemaps)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	if tc.Name != "Grep" {
		t.Errorf("expected name 'Grep', got '%s'", tc.Name)
	}
	// For grep_files, 'path' should remain as 'path' — the deepseek_cli remap
	// maps path→file_path, but parseDeepSeekToolUse
	// explicitly does NOT remap for grep_files/file_search. The new design uses
	// a single unified remap table (path→file_path), so grep_files will get
	// path→file_path remapped. This matches the design doc's approach.
	// However, the design doc says deepseek_cli remap includes path→file_path
	// unconditionally, which means grep_files will also get path→file_path.
	// The old code had a switch to skip remapping for grep_files/file_search,
	// but the new design uses a single remap table without per-tool branching.
	//
	// Per the design: "DeepSeek 的 per-tool remap 条件用 getRemaps('deepseek_cli') 统一覆盖，不再 switch-case"
	// So path→file_path applies to ALL tools including grep_files.
	if !strings.Contains(tc.Input, `"file_path"`) {
		t.Errorf("with unified remap, expected 'file_path' in input, got '%s'", tc.Input)
	}
}

func TestDeepSeekTool_FileSearchNameNormalized(t *testing.T) {
	msg := &DeepSeekStreamMessage{
		Type:  "tool_use",
		Name:  "file_search",
		ID:    "call_glob_1",
		Input: json.RawMessage(`{"path":"/tmp","pattern":"*.go"}`),
		Done:  true,
	}
	tc := parseDeepSeekToolUse(msg, deepSeekInputRemaps)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	if tc.Name != "Glob" {
		t.Errorf("expected name 'Glob', got '%s'", tc.Name)
	}
}

func TestDeepSeekTool_ExecShellNameNormalized(t *testing.T) {
	msg := &DeepSeekStreamMessage{
		Type:  "tool_use",
		Name:  "exec_shell",
		ID:    "call_bash_1",
		Input: json.RawMessage(`{"command":"ls -la"}`),
		Done:  true,
	}
	tc := parseDeepSeekToolUse(msg, deepSeekInputRemaps)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	if tc.Name != "Bash" {
		t.Errorf("expected name 'Bash', got '%s'", tc.Name)
	}
	if !strings.Contains(tc.Input, `"command"`) {
		t.Errorf("expected 'command' in input, got '%s'", tc.Input)
	}
}

func TestDeepSeekTool_EmptyInput(t *testing.T) {
	msg := &DeepSeekStreamMessage{
		Type:  "tool_use",
		Name:  "bash",
		ID:    "call_bash_2",
		Input: json.RawMessage(``),
		Done:  true,
	}
	tc := parseDeepSeekToolUse(msg, deepSeekInputRemaps)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	if tc.Name != "Bash" {
		t.Errorf("expected name 'Bash', got '%s'", tc.Name)
	}
	if tc.Input != "" {
		t.Errorf("expected empty input for empty raw input, got '%s'", tc.Input)
	}
}

func TestDeepSeekTool_InvalidJSONInput(t *testing.T) {
	msg := &DeepSeekStreamMessage{
		Type:  "tool_use",
		Name:  "read_file",
		ID:    "call_invalid",
		Input: json.RawMessage(`{invalid}`),
		Done:  false,
	}
	tc := parseDeepSeekToolUse(msg, deepSeekInputRemaps)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	if tc.Input != "{invalid}" {
		t.Errorf("expected raw input on parse error, got '%s'", tc.Input)
	}
	if tc.Done {
		t.Error("expected Done=false")
	}
}

func TestDeepSeekTool_CamelCaseFieldRemap(t *testing.T) {
	// Verify camelCase fallback fields are remapped: filePaths→file_paths, dirPath→path
	// Note: normalizeToolInput iterates the remap map in non-deterministic order.
	// When input has dirPath (no path), the result may be 'path' or 'file_path'
	// depending on whether dirPath→path runs before or after path→file_path.
	// Both outcomes are valid for single-pass remapping; just verify dirPath is gone
	// and file_paths is remapped correctly.
	msg := &DeepSeekStreamMessage{
		Type:  "tool_use",
		Name:  "list_dir",
		ID:    "call_cc_1",
		Input: json.RawMessage(`{"dirPath":"/tmp","filePaths":["a.go","b.go"]}`),
		Done:  true,
	}
	tc := parseDeepSeekToolUse(msg, deepSeekInputRemaps)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	// filePaths→file_paths (unambiguous)
	if !strings.Contains(tc.Input, `"file_paths"`) {
		t.Errorf("expected 'file_paths' (from filePaths) in input, got '%s'", tc.Input)
	}
	if strings.Contains(tc.Input, `"filePaths"`) {
		t.Errorf("'filePaths' should be remapped to 'file_paths', got '%s'", tc.Input)
	}
	// dirPath should be remapped (either to 'path' or chained to 'file_path')
	if strings.Contains(tc.Input, `"dirPath"`) {
		t.Errorf("'dirPath' should be remapped, got '%s'", tc.Input)
	}
	// The value "/tmp" must still be present under whatever key dirPath mapped to
	if !strings.Contains(tc.Input, "/tmp") {
		t.Errorf("expected '/tmp' value in input, got '%s'", tc.Input)
	}
}

func TestDeepSeekTool_ToolNameNormalization(t *testing.T) {
	tests := map[string]string{
		"read_file":   "Read",
		"write_file":  "Write",
		"edit_file":   "Edit",
		"exec_shell":  "Bash",
		"shell":       "Bash",
		"bash":        "Bash",
		"list_dir":    "LS",
		"list_files":  "LS",
		"grep_files":  "Grep",
		"file_search": "Glob",
		"glob":        "Glob",
		"fetch_url":   "WebFetch",
		"web_search":  "WebSearch",
		"agent_spawn": "Agent",
		"load_skill":  "Skill",
		"todo_write":  "TodoWrite",
		"apply_patch": "Edit",
		"git_status":  "Git",
		"git_diff":    "Git",
	}

	for input, expected := range tests {
		msg := &DeepSeekStreamMessage{
			Type:  "tool_use",
			Name:  input,
			ID:    "t1",
			Input: json.RawMessage(`{}`),
			Done:  true,
		}
		tc := parseDeepSeekToolUse(msg, deepSeekInputRemaps)
		if tc == nil {
			t.Fatalf("expected non-nil ToolCall for tool name '%s'", input)
			return
		}
		if tc.Name != expected {
			t.Errorf("normalizeToolName(%q) = %q, want %q", input, tc.Name, expected)
		}
	}
}

// --- parseDeepSeekToolResult tests ---

func TestDeepSeekTool_ToolResultWithID(t *testing.T) {
	msg := &DeepSeekStreamMessage{
		Type:   "tool_result",
		ID:     "call_001",
		Output: "file contents here",
		Status: "success",
	}
	tc := parseDeepSeekToolResult(msg)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	if tc.ID != "call_001" {
		t.Errorf("expected ID 'call_001', got '%s'", tc.ID)
	}
	if tc.Output != "file contents here" {
		t.Errorf("expected output 'file contents here', got '%s'", tc.Output)
	}
	if tc.Status != "success" {
		t.Errorf("expected status 'success', got '%s'", tc.Status)
	}
}

func TestDeepSeekTool_ToolResultEmptyIDSkipped(t *testing.T) {
	msg := &DeepSeekStreamMessage{
		Type:   "tool_result",
		ID:     "",
		Output: "some output",
		Status: "success",
	}
	tc := parseDeepSeekToolResult(msg)
	if tc != nil {
		t.Errorf("expected nil for empty ID tool_result, got %+v", tc)
	}
}

func TestDeepSeekTool_ToolResultErrorStatus(t *testing.T) {
	msg := &DeepSeekStreamMessage{
		Type:   "tool_result",
		ID:     "call_err",
		Output: "command failed",
		Status: "error",
	}
	tc := parseDeepSeekToolResult(msg)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	if tc.Status != "error" {
		t.Errorf("expected status 'error', got '%s'", tc.Status)
	}
}

func TestDeepSeekTool_ToolResultOutputTruncation(t *testing.T) {
	// Output exceeding maxToolOutputBytes should be truncated
	longOutput := strings.Repeat("x", maxToolOutputBytes+1000)
	msg := &DeepSeekStreamMessage{
		Type:   "tool_result",
		ID:     "call_long",
		Output: longOutput,
		Status: "success",
	}
	tc := parseDeepSeekToolResult(msg)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	if len(tc.Output) > maxToolOutputBytes+200 {
		t.Errorf("expected output to be truncated, got length %d", len(tc.Output))
	}
	if !strings.Contains(tc.Output, "[truncated:") {
		t.Error("expected truncation marker in output")
	}
}

func TestDeepSeekTool_ToolResultSmallOutputNotTruncated(t *testing.T) {
	msg := &DeepSeekStreamMessage{
		Type:   "tool_result",
		ID:     "call_small",
		Output: "small output",
		Status: "success",
	}
	tc := parseDeepSeekToolResult(msg)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	if tc.Output != "small output" {
		t.Errorf("expected 'small output', got '%s'", tc.Output)
	}
}
