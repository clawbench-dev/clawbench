package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// --- parsePiToolCallEnd ---

func TestParsePiToolCallEnd_ReadTool(t *testing.T) {
	evt := &PiAssistantMessageEvent{
		Type: "toolcall_end",
		ToolCall: &PiToolCallEnd{
			Type:      "toolCall",
			ID:        "call_bbd96f43dd2140138ca453fb",
			Name:      "read",
			Arguments: json.RawMessage(`{"path":"/home/user/project/go.mod","limit":5}`),
		},
	}

	tc := parsePiToolCallEnd(evt)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	if tc.Name != "Read" {
		t.Errorf("expected name 'Read', got '%s'", tc.Name)
	}
	if tc.ID != "call_bbd96f43dd2140138ca453fb" {
		t.Errorf("expected ID 'call_bbd96f43dd2140138ca453fb', got '%s'", tc.ID)
	}
	if !tc.Done {
		t.Error("expected Done=true")
	}
	// Pi read uses "path" -> should be normalized to "file_path"
	if !strings.Contains(tc.Input, `"file_path"`) {
		t.Errorf("expected 'file_path' in input, got '%s'", tc.Input)
	}
	if strings.Contains(tc.Input, `"path"`) {
		t.Errorf("expected 'path' to be remapped, but it still exists in input '%s'", tc.Input)
	}
}

func TestParsePiToolCallEnd_WriteTool(t *testing.T) {
	evt := &PiAssistantMessageEvent{
		Type: "toolcall_end",
		ToolCall: &PiToolCallEnd{
			Type:      "toolCall",
			ID:        "call_write1",
			Name:      "write",
			Arguments: json.RawMessage(`{"path":"/tmp/test.txt","content":"hello"}`),
		},
	}

	tc := parsePiToolCallEnd(evt)
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

func TestParsePiToolCallEnd_EditTool(t *testing.T) {
	evt := &PiAssistantMessageEvent{
		Type: "toolcall_end",
		ToolCall: &PiToolCallEnd{
			Type:      "toolCall",
			ID:        "call_edit1",
			Name:      "edit",
			Arguments: json.RawMessage(`{"path":"/tmp/test.go","edits":[{"oldText":"foo","newText":"bar"}]}`),
		},
	}

	tc := parsePiToolCallEnd(evt)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	if tc.Name != "Edit" {
		t.Errorf("expected name 'Edit', got '%s'", tc.Name)
	}
	// Pi edit: path -> file_path, oldText -> old_string, newText -> new_string
	if !strings.Contains(tc.Input, `"file_path"`) {
		t.Errorf("expected 'file_path' in input, got '%s'", tc.Input)
	}
	if !strings.Contains(tc.Input, `"old_string"`) {
		t.Errorf("expected 'old_string' in input, got '%s'", tc.Input)
	}
	if !strings.Contains(tc.Input, `"new_string"`) {
		t.Errorf("expected 'new_string' in input, got '%s'", tc.Input)
	}
	// Original field names should be gone
	if strings.Contains(tc.Input, `"oldText"`) {
		t.Errorf("expected 'oldText' to be remapped, but it still exists in input '%s'", tc.Input)
	}
	if strings.Contains(tc.Input, `"newText"`) {
		t.Errorf("expected 'newText' to be remapped, but it still exists in input '%s'", tc.Input)
	}
}

func TestParsePiToolCallEnd_BashTool(t *testing.T) {
	evt := &PiAssistantMessageEvent{
		Type: "toolcall_end",
		ToolCall: &PiToolCallEnd{
			Type:      "toolCall",
			ID:        "call_bash1",
			Name:      "bash",
			Arguments: json.RawMessage(`{"command":"echo hello"}`),
		},
	}

	tc := parsePiToolCallEnd(evt)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	if tc.Name != "Bash" {
		t.Errorf("expected name 'Bash', got '%s'", tc.Name)
	}
	// bash command field should remain unchanged
	if !strings.Contains(tc.Input, `"command"`) {
		t.Errorf("expected 'command' in input, got '%s'", tc.Input)
	}
}

func TestParsePiToolCallEnd_GrepTool(t *testing.T) {
	evt := &PiAssistantMessageEvent{
		Type: "toolcall_end",
		ToolCall: &PiToolCallEnd{
			Type:      "toolCall",
			ID:        "call_grep1",
			Name:      "grep",
			Arguments: json.RawMessage(`{"pattern":"func main","path":"/home/user/project/cmd/"}`),
		},
	}

	tc := parsePiToolCallEnd(evt)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	if tc.Name != "Grep" {
		t.Errorf("expected name 'Grep', got '%s'", tc.Name)
	}
	// grep also uses "path" -> "file_path" via pi_cli remaps
	if !strings.Contains(tc.Input, `"file_path"`) {
		t.Errorf("expected 'file_path' in input, got '%s'", tc.Input)
	}
}

func TestParsePiToolCallEnd_NilToolCall(t *testing.T) {
	evt := &PiAssistantMessageEvent{
		Type:     "toolcall_end",
		ToolCall: nil,
	}

	tc := parsePiToolCallEnd(evt)
	if tc != nil {
		t.Errorf("expected nil for nil ToolCall, got %+v", tc)
	}
}

func TestParsePiToolCallEnd_EmptyArguments(t *testing.T) {
	evt := &PiAssistantMessageEvent{
		Type: "toolcall_end",
		ToolCall: &PiToolCallEnd{
			Type:      "toolCall",
			ID:        "call_empty",
			Name:      "bash",
			Arguments: json.RawMessage(`{}`),
		},
	}

	tc := parsePiToolCallEnd(evt)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	if tc.Input != `{}` {
		t.Errorf("expected '{}', got '%s'", tc.Input)
	}
}

func TestParsePiToolCallEnd_NilArguments(t *testing.T) {
	evt := &PiAssistantMessageEvent{
		Type: "toolcall_end",
		ToolCall: &PiToolCallEnd{
			Type:      "toolCall",
			ID:        "call_nil_args",
			Name:      "bash",
			Arguments: nil,
		},
	}

	tc := parsePiToolCallEnd(evt)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	// nil arguments should produce "{}"
	if tc.Input != `{}` {
		t.Errorf("expected '{}', got '%s'", tc.Input)
	}
}

func TestParsePiToolCallEnd_EditToolMultipleEdits(t *testing.T) {
	evt := &PiAssistantMessageEvent{
		Type: "toolcall_end",
		ToolCall: &PiToolCallEnd{
			Type:      "toolCall",
			ID:        "call_multi_edit",
			Name:      "edit",
			Arguments: json.RawMessage(`{"path":"/tmp/test.go","edits":[{"oldText":"foo","newText":"bar"},{"oldText":"baz","newText":"qux"}]}`),
		},
	}

	tc := parsePiToolCallEnd(evt)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	// All edits should have oldText/newText remapped
	if strings.Count(tc.Input, `"old_string"`) != 2 {
		t.Errorf("expected 2 'old_string' occurrences, got input: '%s'", tc.Input)
	}
	if strings.Count(tc.Input, `"new_string"`) != 2 {
		t.Errorf("expected 2 'new_string' occurrences, got input: '%s'", tc.Input)
	}
}

func TestParsePiToolCallEnd_UnknownTool(t *testing.T) {
	evt := &PiAssistantMessageEvent{
		Type: "toolcall_end",
		ToolCall: &PiToolCallEnd{
			Type:      "toolCall",
			ID:        "call_unknown",
			Name:      "custom_tool",
			Arguments: json.RawMessage(`{"key":"value"}`),
		},
	}

	tc := parsePiToolCallEnd(evt)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	// Unknown tool name is passed through as-is
	if tc.Name != "custom_tool" {
		t.Errorf("expected name 'custom_tool', got '%s'", tc.Name)
	}
}

func TestNormalizePiToolInput_InvalidJSONFallback(t *testing.T) {
	// normalizePiToolInput should return raw input when normalizeToolInput fails
	result := normalizePiToolInput("bash", json.RawMessage(`not valid json`))
	if result != "not valid json" {
		t.Errorf("expected invalid JSON returned as-is, got '%s'", result)
	}
}

func TestNormalizePiToolInput_EmptyInput(t *testing.T) {
	result := normalizePiToolInput("bash", nil)
	if result != "{}" {
		t.Errorf("expected '{}', got '%s'", result)
	}
}

// --- parsePiToolExecutionEnd ---

func TestParsePiToolExecutionEnd_BashSuccess(t *testing.T) {
	msg := &PiStreamMessage{
		Type:       "tool_execution_end",
		ToolCallID: "call_bash1",
		ToolName:   "bash",
		Result: &PiToolResult{
			Content: []PiToolResultContent{
				{Type: "text", Text: "hello from pi"},
			},
		},
		IsError: false,
	}

	tc := parsePiToolExecutionEnd(msg)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	if tc.ID != "call_bash1" {
		t.Errorf("expected ID 'call_bash1', got '%s'", tc.ID)
	}
	if tc.Output != "hello from pi" {
		t.Errorf("expected output 'hello from pi', got '%s'", tc.Output)
	}
	if tc.Status != "success" {
		t.Errorf("expected status 'success', got '%s'", tc.Status)
	}
}

func TestParsePiToolExecutionEnd_ErrorResult(t *testing.T) {
	msg := &PiStreamMessage{
		Type:       "tool_execution_end",
		ToolCallID: "call_err1",
		ToolName:   "bash",
		Result: &PiToolResult{
			Content: []PiToolResultContent{
				{Type: "text", Text: "permission denied"},
			},
		},
		IsError: true,
	}

	tc := parsePiToolExecutionEnd(msg)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	if tc.Status != "error" {
		t.Errorf("expected status 'error', got '%s'", tc.Status)
	}
}

func TestParsePiToolExecutionEnd_MultiContent(t *testing.T) {
	msg := &PiStreamMessage{
		Type:       "tool_execution_end",
		ToolCallID: "call_multi",
		ToolName:   "bash",
		Result: &PiToolResult{
			Content: []PiToolResultContent{
				{Type: "text", Text: "line1"},
				{Type: "text", Text: "line2"},
			},
		},
		IsError: false,
	}

	tc := parsePiToolExecutionEnd(msg)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	// Multiple text content items should be joined with newline
	if tc.Output != "line1\nline2" {
		t.Errorf("expected 'line1\\nline2', got '%s'", tc.Output)
	}
}

func TestParsePiToolExecutionEnd_EmptyToolCallID(t *testing.T) {
	msg := &PiStreamMessage{
		Type:       "tool_execution_end",
		ToolCallID: "",
		ToolName:   "bash",
		Result: &PiToolResult{
			Content: []PiToolResultContent{
				{Type: "text", Text: "output"},
			},
		},
		IsError: false,
	}

	tc := parsePiToolExecutionEnd(msg)
	if tc != nil {
		t.Errorf("expected nil for empty toolCallId, got %+v", tc)
	}
}

func TestParsePiToolExecutionEnd_NilResult(t *testing.T) {
	msg := &PiStreamMessage{
		Type:       "tool_execution_end",
		ToolCallID: "call_nil_result",
		ToolName:   "bash",
		Result:     nil,
		IsError:    false,
	}

	tc := parsePiToolExecutionEnd(msg)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	if tc.Output != "" {
		t.Errorf("expected empty output, got '%s'", tc.Output)
	}
	if tc.Status != "success" {
		t.Errorf("expected status 'success', got '%s'", tc.Status)
	}
}

func TestParsePiToolExecutionEnd_EmptyContentArray(t *testing.T) {
	msg := &PiStreamMessage{
		Type:       "tool_execution_end",
		ToolCallID: "call_empty_content",
		ToolName:   "bash",
		Result: &PiToolResult{
			Content: []PiToolResultContent{},
		},
		IsError: false,
	}

	tc := parsePiToolExecutionEnd(msg)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	if tc.Output != "" {
		t.Errorf("expected empty output, got '%s'", tc.Output)
	}
}

func TestParsePiToolExecutionEnd_NonTextContentIgnored(t *testing.T) {
	msg := &PiStreamMessage{
		Type:       "tool_execution_end",
		ToolCallID: "call_mixed",
		ToolName:   "bash",
		Result: &PiToolResult{
			Content: []PiToolResultContent{
				{Type: "image", Text: "should-be-ignored"},
				{Type: "text", Text: "actual text"},
			},
		},
		IsError: false,
	}

	tc := parsePiToolExecutionEnd(msg)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	if tc.Output != "actual text" {
		t.Errorf("expected 'actual text', got '%s'", tc.Output)
	}
}

func TestParsePiToolExecutionEnd_EmptyTextIgnored(t *testing.T) {
	msg := &PiStreamMessage{
		Type:       "tool_execution_end",
		ToolCallID: "call_empty_text",
		ToolName:   "bash",
		Result: &PiToolResult{
			Content: []PiToolResultContent{
				{Type: "text", Text: ""},
				{Type: "text", Text: "visible"},
			},
		},
		IsError: false,
	}

	tc := parsePiToolExecutionEnd(msg)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	// Empty text items should be skipped; only non-empty text is joined
	if tc.Output != "visible" {
		t.Errorf("expected 'visible', got '%s'", tc.Output)
	}
}

func TestParsePiToolExecutionEnd_ErrorWithNilResult(t *testing.T) {
	msg := &PiStreamMessage{
		Type:       "tool_execution_end",
		ToolCallID: "call_err_nil",
		ToolName:   "bash",
		Result:     nil,
		IsError:    true,
	}

	tc := parsePiToolExecutionEnd(msg)
	if tc == nil {
		t.Fatal("expected non-nil ToolCall")
		return
	}
	if tc.Status != "error" {
		t.Errorf("expected status 'error', got '%s'", tc.Status)
	}
}

// --- normalizePiEditInput ---

func TestNormalizePiEditInput_SingleEdit(t *testing.T) {
	input := json.RawMessage(`{"path":"/tmp/test.go","edits":[{"oldText":"foo","newText":"bar"}]}`)
	topRemaps := map[string]string{"path": "file_path"}

	result := normalizePiEditInput(input, topRemaps)

	if !strings.Contains(result, `"file_path"`) {
		t.Errorf("expected 'file_path' in result, got '%s'", result)
	}
	if !strings.Contains(result, `"old_string"`) {
		t.Errorf("expected 'old_string' in result, got '%s'", result)
	}
	if !strings.Contains(result, `"new_string"`) {
		t.Errorf("expected 'new_string' in result, got '%s'", result)
	}
	if strings.Contains(result, `"oldText"`) {
		t.Errorf("expected 'oldText' to be remapped, still present in '%s'", result)
	}
	if strings.Contains(result, `"newText"`) {
		t.Errorf("expected 'newText' to be remapped, still present in '%s'", result)
	}
	if strings.Contains(result, `"path"`) {
		t.Errorf("expected 'path' to be remapped, still present in '%s'", result)
	}
}

func TestNormalizePiEditInput_MultipleEdits(t *testing.T) {
	input := json.RawMessage(`{"path":"/tmp/test.go","edits":[{"oldText":"a","newText":"b"},{"oldText":"c","newText":"d"}]}`)
	topRemaps := map[string]string{"path": "file_path"}

	result := normalizePiEditInput(input, topRemaps)

	if strings.Count(result, `"old_string"`) != 2 {
		t.Errorf("expected 2 'old_string' occurrences, got: '%s'", result)
	}
	if strings.Count(result, `"new_string"`) != 2 {
		t.Errorf("expected 2 'new_string' occurrences, got: '%s'", result)
	}
}

func TestNormalizePiEditInput_NoEditsArray(t *testing.T) {
	input := json.RawMessage(`{"path":"/tmp/test.go"}`)
	topRemaps := map[string]string{"path": "file_path"}

	result := normalizePiEditInput(input, topRemaps)

	if !strings.Contains(result, `"file_path"`) {
		t.Errorf("expected 'file_path' in result, got '%s'", result)
	}
	if strings.Contains(result, `"path"`) {
		t.Errorf("expected 'path' to be remapped, still present in '%s'", result)
	}
}

func TestNormalizePiEditInput_EmptyTopRemaps(t *testing.T) {
	input := json.RawMessage(`{"edits":[{"oldText":"foo","newText":"bar"}]}`)
	topRemaps := map[string]string{}

	result := normalizePiEditInput(input, topRemaps)

	// Even with no top-level remaps, nested edit fields should still be remapped
	if !strings.Contains(result, `"old_string"`) {
		t.Errorf("expected 'old_string' in result, got '%s'", result)
	}
	if !strings.Contains(result, `"new_string"`) {
		t.Errorf("expected 'new_string' in result, got '%s'", result)
	}
}

func TestNormalizePiEditInput_InvalidJSON(t *testing.T) {
	input := json.RawMessage(`not valid json`)
	topRemaps := map[string]string{"path": "file_path"}

	result := normalizePiEditInput(input, topRemaps)
	// Invalid JSON should be returned as-is
	if result != string(input) {
		t.Errorf("expected invalid JSON returned as-is, got '%s'", result)
	}
}

func TestNormalizePiEditInput_EditsNotArray(t *testing.T) {
	input := json.RawMessage(`{"path":"/tmp/test.go","edits":"not_an_array"}`)
	topRemaps := map[string]string{"path": "file_path"}

	result := normalizePiEditInput(input, topRemaps)
	// When edits is not an array, top-level remaps should still be applied
	if !strings.Contains(result, `"file_path"`) {
		t.Errorf("expected 'file_path' in result, got '%s'", result)
	}
}

func TestNormalizePiEditInput_EditItemNotObject(t *testing.T) {
	input := json.RawMessage(`{"path":"/tmp/test.go","edits":["string_item",42]}`)
	topRemaps := map[string]string{"path": "file_path"}

	result := normalizePiEditInput(input, topRemaps)
	// Non-object items in edits array should be left as-is
	if !strings.Contains(result, `"file_path"`) {
		t.Errorf("expected 'file_path' in result, got '%s'", result)
	}
}

func TestNormalizePiEditInput_PreservesOtherFields(t *testing.T) {
	input := json.RawMessage(`{"path":"/tmp/test.go","edits":[{"oldText":"foo","newText":"bar","dryRun":true}],"verbose":true}`)
	topRemaps := map[string]string{"path": "file_path"}

	result := normalizePiEditInput(input, topRemaps)

	// Non-remapped fields should be preserved
	if !strings.Contains(result, `"dryRun"`) {
		t.Errorf("expected 'dryRun' preserved in edits, got '%s'", result)
	}
	if !strings.Contains(result, `"verbose"`) {
		t.Errorf("expected 'verbose' preserved at top level, got '%s'", result)
	}
}
