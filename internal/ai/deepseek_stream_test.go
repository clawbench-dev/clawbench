package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDeepSeekStreamParserContent(t *testing.T) {
	parser := &DeepSeekStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"content","content":"hello world"}`, ch)

	select {
	case evt := <-ch:
		if evt.Type != "content" {
			t.Errorf("expected content event, got %s", evt.Type)
		}
		if evt.Content != "hello world" {
			t.Errorf("expected 'hello world', got '%s'", evt.Content)
		}
	default:
		t.Error("expected event on channel")
	}
}

func TestDeepSeekStreamParserThinking(t *testing.T) {
	parser := &DeepSeekStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"thinking","content":"reasoning about the problem..."}`, ch)

	select {
	case evt := <-ch:
		if evt.Type != "thinking" {
			t.Errorf("expected thinking event, got %s", evt.Type)
		}
		if evt.Content != "reasoning about the problem..." {
			t.Errorf("unexpected content: %s", evt.Content)
		}
	default:
		t.Error("expected event on channel")
	}
}

func TestDeepSeekStreamParserToolUse(t *testing.T) {
	parser := &DeepSeekStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"tool_use","name":"read_file","id":"call_001","input":{"path":"/tmp/test.go"},"done":true}`, ch)

	select {
	case evt := <-ch:
		if evt.Type != "tool_use" {
			t.Errorf("expected tool_use event, got %s", evt.Type)
		}
		if evt.Tool == nil {
			t.Fatal("expected Tool to be non-nil")
		}
		if evt.Tool.Name != "Read" {
			t.Errorf("expected canonical tool name 'Read', got '%s'", evt.Tool.Name)
		}
		if evt.Tool.ID != "call_001" {
			t.Errorf("expected tool ID 'call_001', got '%s'", evt.Tool.ID)
		}
		if !evt.Tool.Done {
			t.Error("expected Done=true")
		}
		// DeepSeek read_file uses "path" → should be normalized to "file_path"
		if !strings.Contains(evt.Tool.Input, `"file_path"`) {
			t.Errorf("expected input field 'file_path', got '%s'", evt.Tool.Input)
		}
		if !strings.Contains(evt.Tool.Input, "/tmp/test.go") {
			t.Errorf("expected path value '/tmp/test.go' in input, got '%s'", evt.Tool.Input)
		}
	default:
		t.Error("expected event on channel")
	}
}

func TestDeepSeekStreamParserToolResult(t *testing.T) {
	parser := &DeepSeekStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"tool_result","id":"call_001","output":"file contents here","status":"success"}`, ch)

	select {
	case evt := <-ch:
		if evt.Type != "tool_result" {
			t.Errorf("expected tool_result event, got %s", evt.Type)
		}
		if evt.Tool.ID != "call_001" {
			t.Errorf("expected tool ID 'call_001', got '%s'", evt.Tool.ID)
		}
		if evt.Tool.Status != "success" {
			t.Errorf("expected status 'success', got '%s'", evt.Tool.Status)
		}
	default:
		t.Error("expected event on channel")
	}
}

func TestDeepSeekStreamParserSessionCapture(t *testing.T) {
	parser := &DeepSeekStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"session_capture","content":"4bf83f0f-a9b6-47b4-bcde-68af7354cd9f"}`, ch)

	select {
	case evt := <-ch:
		if evt.Type != "session_capture" {
			t.Errorf("expected session_capture event, got %s", evt.Type)
		}
		if evt.Content != "4bf83f0f-a9b6-47b4-bcde-68af7354cd9f" {
			t.Errorf("unexpected session ID: %s", evt.Content)
		}
	default:
		t.Error("expected event on channel")
	}

	// Verify GetCapturedSessionID
	if id := parser.GetCapturedSessionID(); id != "4bf83f0f-a9b6-47b4-bcde-68af7354cd9f" {
		t.Errorf("expected captured session ID, got '%s'", id)
	}
}

func TestDeepSeekStreamParserMetadata(t *testing.T) {
	parser := &DeepSeekStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"metadata","meta":{"model":"deepseek-v4-flash","input_tokens":100,"output_tokens":50,"session_id":"abc-123"}}`, ch)

	select {
	case evt := <-ch:
		if evt.Type != "metadata" {
			t.Errorf("expected metadata event, got %s", evt.Type)
		}
		if evt.Meta == nil {
			t.Fatal("expected Meta to be non-nil")
		}
		if evt.Meta.Model != "deepseek-v4-flash" {
			t.Errorf("expected model 'deepseek-v4-flash', got '%s'", evt.Meta.Model)
		}
		if evt.Meta.InputTokens != 100 {
			t.Errorf("expected 100 input tokens, got %d", evt.Meta.InputTokens)
		}
		if evt.Meta.OutputTokens != 50 {
			t.Errorf("expected 50 output tokens, got %d", evt.Meta.OutputTokens)
		}
		if evt.Meta.SessionID != "abc-123" {
			t.Errorf("expected session ID 'abc-123', got '%s'", evt.Meta.SessionID)
		}
	default:
		t.Error("expected event on channel")
	}
}

func TestDeepSeekStreamParserDone(t *testing.T) {
	parser := &DeepSeekStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"done"}`, ch)

	select {
	case evt := <-ch:
		if evt.Type != "done" {
			t.Errorf("expected done event, got %s", evt.Type)
		}
	default:
		t.Error("expected event on channel")
	}
}

func TestDeepSeekStreamParserError(t *testing.T) {
	parser := &DeepSeekStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"error","error":"API rate limit exceeded"}`, ch)

	select {
	case evt := <-ch:
		if evt.Type != "error" {
			t.Errorf("expected error event, got %s", evt.Type)
		}
		if evt.Error != "API rate limit exceeded" {
			t.Errorf("unexpected error message: %s", evt.Error)
		}
	default:
		t.Error("expected event on channel")
	}
}

func TestDeepSeekStreamParserSkipInvalidJSON(t *testing.T) {
	parser := &DeepSeekStreamParser{}
	ch := make(chan StreamEvent, 10)
	// Should not panic or send events for invalid JSON
	parser.ParseLine("not json", ch)
	parser.ParseLine("", ch)

	select {
	case evt := <-ch:
		t.Errorf("expected no events for invalid JSON, got %+v", evt)
	default:
		// expected
	}
}

func TestDeepSeekStreamParserSkipUnknownType(t *testing.T) {
	parser := &DeepSeekStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"unknown_type","data":"something"}`, ch)

	select {
	case evt := <-ch:
		t.Errorf("expected no events for unknown type, got %+v", evt)
	default:
		// expected
	}
}

func TestDeepSeekToolNameNormalization(t *testing.T) {
	parser := &DeepSeekStreamParser{}
	ch := make(chan StreamEvent, 10)

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
		parser.ParseLine(`{"type":"tool_use","name":"`+input+`","id":"t1","input":{},"done":true}`, ch)
		evt := <-ch
		if evt.Tool.Name != expected {
			t.Errorf("normalizeToolName(%q) = %q, want %q", input, evt.Tool.Name, expected)
		}
	}
}

func TestDeepSeekInputFieldNormalization(t *testing.T) {
	parser := &DeepSeekStreamParser{}
	ch := make(chan StreamEvent, 10)

	// edit_file: {path, search, replace} → {file_path, old_string, new_string}
	parser.ParseLine(`{"type":"tool_use","name":"edit_file","id":"e1","input":{"path":"/tmp/a.txt","search":"hello","replace":"world"},"done":true}`, ch)
	evt := <-ch
	if !strings.Contains(evt.Tool.Input, `"file_path"`) {
		t.Errorf("edit_file: expected 'file_path' in input, got '%s'", evt.Tool.Input)
	}
	if !strings.Contains(evt.Tool.Input, `"old_string"`) {
		t.Errorf("edit_file: expected 'old_string' in input, got '%s'", evt.Tool.Input)
	}
	if !strings.Contains(evt.Tool.Input, `"new_string"`) {
		t.Errorf("edit_file: expected 'new_string' in input, got '%s'", evt.Tool.Input)
	}
	if strings.Contains(evt.Tool.Input, `"path"`) {
		t.Errorf("edit_file: 'path' should be remapped to 'file_path', got '%s'", evt.Tool.Input)
	}
	if strings.Contains(evt.Tool.Input, `"search"`) {
		t.Errorf("edit_file: 'search' should be remapped to 'old_string', got '%s'", evt.Tool.Input)
	}
	if strings.Contains(evt.Tool.Input, `"replace"`) {
		t.Errorf("edit_file: 'replace' should be remapped to 'new_string', got '%s'", evt.Tool.Input)
	}

	// read_file: {path} → {file_path}
	parser.ParseLine(`{"type":"tool_use","name":"read_file","id":"r1","input":{"path":"/tmp/b.txt"},"done":true}`, ch)
	evt = <-ch
	if !strings.Contains(evt.Tool.Input, `"file_path"`) {
		t.Errorf("read_file: expected 'file_path' in input, got '%s'", evt.Tool.Input)
	}
	if strings.Contains(evt.Tool.Input, `"path"`) {
		t.Errorf("read_file: 'path' should be remapped to 'file_path', got '%s'", evt.Tool.Input)
	}

	// exec_shell: {command} stays as {command}
	parser.ParseLine(`{"type":"tool_use","name":"exec_shell","id":"s1","input":{"command":"ls -la"},"done":true}`, ch)
	evt = <-ch
	if !strings.Contains(evt.Tool.Input, `"command"`) {
		t.Errorf("exec_shell: expected 'command' in input, got '%s'", evt.Tool.Input)
	}

	// list_dir: {path} → {file_path}
	parser.ParseLine(`{"type":"tool_use","name":"list_dir","id":"d1","input":{"path":"/tmp"},"done":true}`, ch)
	evt = <-ch
	if !strings.Contains(evt.Tool.Input, `"file_path"`) {
		t.Errorf("list_dir: expected 'file_path' in input, got '%s'", evt.Tool.Input)
	}
}

func TestDeepSeekStreamParser_EmptyContentSkipped(t *testing.T) {
	parser := &DeepSeekStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"content","content":""}`, ch)

	select {
	case evt := <-ch:
		t.Errorf("empty content should be skipped, got %+v", evt)
	default:
		// expected
	}
}

func TestDeepSeekStreamParser_EmptyThinkingSkipped(t *testing.T) {
	parser := &DeepSeekStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"thinking","content":""}`, ch)

	select {
	case evt := <-ch:
		t.Errorf("empty thinking should be skipped, got %+v", evt)
	default:
		// expected
	}
}

func TestDeepSeekStreamParser_ToolResultEmptyIDSkipped(t *testing.T) {
	parser := &DeepSeekStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"tool_result","id":"","output":"some output","status":"success"}`, ch)

	select {
	case evt := <-ch:
		t.Errorf("tool_result with empty ID should be skipped, got %+v", evt)
	default:
		// expected
	}
}

func TestDeepSeekStreamParser_MetadataNilMetaSkipped(t *testing.T) {
	parser := &DeepSeekStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"metadata"}`, ch)

	select {
	case evt := <-ch:
		t.Errorf("metadata with nil Meta should be skipped, got %+v", evt)
	default:
		// expected
	}
}

func TestDeepSeekStreamParser_ErrorEmptyMessageSkipped(t *testing.T) {
	parser := &DeepSeekStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"error","error":""}`, ch)

	select {
	case evt := <-ch:
		t.Errorf("error with empty message should be skipped, got %+v", evt)
	default:
		// expected
	}
}

func TestDeepSeekInputFieldNormalization_GrepFilesPathNotRemapped(t *testing.T) {
	// grep_files: 'path' field should NOT be remapped to 'file_path'
	// because Grep's canonical field is 'path', not 'file_path'
	parser := &DeepSeekStreamParser{}
	ch := make(chan StreamEvent, 10)

	parser.ParseLine(`{"type":"tool_use","name":"grep_files","id":"g1","input":{"path":"/tmp","pattern":"TODO"},"done":true}`, ch)
	evt := <-ch
	if !strings.Contains(evt.Tool.Input, `"path"`) {
		t.Errorf("grep_files: 'path' should NOT be remapped for Grep, got '%s'", evt.Tool.Input)
	}
	if strings.Contains(evt.Tool.Input, `"file_path"`) {
		t.Errorf("grep_files: 'path' should not become 'file_path' for Grep, got '%s'", evt.Tool.Input)
	}
}

func TestDeepSeekInputFieldNormalization_InvalidJSON(t *testing.T) {
	// When tool input is invalid JSON, parseDeepSeekToolUse returns the raw string.
	msg := DeepSeekStreamMessage{Name: "read_file", Input: json.RawMessage(`{invalid}`)}
	tc := parseDeepSeekToolUse(&msg, nil)
	if tc.Input == "" {
		t.Error("expected non-empty result for invalid JSON input")
	}
	if tc.Input != "{invalid}" {
		t.Errorf("expected raw input returned on parse error, got '%s'", tc.Input)
	}
}

func TestDeepSeekInputFieldNormalization_EmptyInput(t *testing.T) {
	// Empty raw input should produce empty result
	msg := DeepSeekStreamMessage{Name: "read_file", Input: json.RawMessage(``)}
	tc := parseDeepSeekToolUse(&msg, nil)
	if tc.Input != "" {
		t.Errorf("expected empty string for empty input, got '%s'", tc.Input)
	}
}
