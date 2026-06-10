package ai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func parseOpenCodeLine(line string) []StreamEvent {
	ch := make(chan StreamEvent, 64)
	parser := &OpenCodeStreamParser{}
	parser.ParseLine(line, ch)
	close(ch)
	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	return events
}

func TestOpenCodeStream_ParseLine_Text(t *testing.T) {
	line := `{"type":"text","timestamp":1777038590233,"sessionID":"ses_abc123","part":{"type":"text","text":"\n\nHello, world!"}}`
	events := parseOpenCodeLine(line)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
		return
	}
	if events[0].Type != "content" {
		t.Errorf("expected content event, got %s", events[0].Type)
	}
	if events[0].Content != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", events[0].Content)
	}
}

func TestOpenCodeStream_ParseLine_TextNoPrefix(t *testing.T) {
	line := `{"type":"text","timestamp":1,"sessionID":"ses_abc","part":{"type":"text","text":"No prefix here"}}`
	events := parseOpenCodeLine(line)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
		return
	}
	if events[0].Content != "No prefix here" {
		t.Errorf("expected 'No prefix here', got %q", events[0].Content)
	}
}

func TestOpenCodeStream_ParseLine_Reasoning(t *testing.T) {
	line := `{"type":"reasoning","timestamp":1,"sessionID":"ses_abc","part":{"type":"reasoning","text":"I need to think about this."}}`
	events := parseOpenCodeLine(line)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
		return
	}
	if events[0].Type != "thinking" {
		t.Errorf("expected thinking event, got %s", events[0].Type)
	}
	if events[0].Content != "I need to think about this." {
		t.Errorf("unexpected content: %q", events[0].Content)
	}
}

func TestOpenCodeStream_ParseLine_ToolUse(t *testing.T) {
	line := `{"type":"tool_use","timestamp":1,"sessionID":"ses_abc","part":{"type":"tool","tool":"read","callID":"call_123","state":{"status":"completed","input":{"filePath":"/tmp/test.go"},"output":"file content here"}}}`
	events := parseOpenCodeLine(line)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
		return
	}
	if events[0].Type != "tool_use" {
		t.Errorf("expected tool_use event, got %s", events[0].Type)
	}
	tool := events[0].Tool
	if tool == nil {
		t.Fatal("expected tool call, got nil")
		return
	}
	if tool.Name != "Read" {
		t.Errorf("expected normalized tool name 'Read', got %q", tool.Name)
	}
	if tool.ID != "call_123" {
		t.Errorf("expected call ID 'call_123', got %q", tool.ID)
	}
	if !tool.Done {
		t.Error("expected Done=true for completed tool")
	}
	// Verify input is normalized: filePath → file_path
	var input map[string]any
	if err := json.Unmarshal([]byte(tool.Input), &input); err != nil {
		t.Fatalf("failed to parse tool input: %v", err)
		return
	}
	if input["file_path"] != "/tmp/test.go" {
		t.Errorf("unexpected input: %v", input)
	}
}

func TestOpenCodeStream_ParseLine_ToolUseNonObjectInput(t *testing.T) {
	// When tool input is valid JSON but not an object (e.g., an array),
	// normalizeToolInput should fail and fall back to raw input string
	line := `{"type":"tool_use","timestamp":1,"sessionID":"ses_abc","part":{"type":"tool","tool":"bash","callID":"call_arr","state":{"status":"completed","input":[1,2,3],"output":"done"}}}`
	events := parseOpenCodeLine(line)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
		return
	}
	tool := events[0].Tool
	if tool == nil {
		t.Fatal("expected tool call, got nil")
		return
	}
	// Should fall back to raw input string
	assert.Equal(t, "[1,2,3]", tool.Input)
}

func TestOpenCodeStream_ParseLine_ToolUseRunning(t *testing.T) {
	line := `{"type":"tool_use","timestamp":1,"sessionID":"ses_abc","part":{"type":"tool","tool":"bash","callID":"call_456","state":{"status":"running","input":{"command":"ls"}}}}`
	events := parseOpenCodeLine(line)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
		return
	}
	tool := events[0].Tool
	if tool.Done {
		t.Error("expected Done=false for running tool")
	}
}

func TestOpenCodeStream_ParseLine_StepFinishStop(t *testing.T) {
	line := `{"type":"step_finish","timestamp":1,"sessionID":"ses_abc","part":{"reason":"stop","tokens":{"total":36690,"input":36635,"output":55,"reasoning":0},"cost":0}}`
	events := parseOpenCodeLine(line)

	if len(events) != 2 {
		t.Fatalf("expected 2 events (metadata + done), got %d", len(events))
		return
	}
	if events[0].Type != "metadata" {
		t.Errorf("expected metadata event first, got %s", events[0].Type)
	}
	meta := events[0].Meta
	if meta == nil {
		t.Fatal("expected metadata, got nil")
		return
	}
	if meta.SessionID != "ses_abc" {
		t.Errorf("expected sessionID 'ses_abc', got %q", meta.SessionID)
	}
	if meta.InputTokens != 36635 {
		t.Errorf("expected input tokens 36635, got %d", meta.InputTokens)
	}
	if meta.OutputTokens != 55 {
		t.Errorf("expected output tokens 55, got %d", meta.OutputTokens)
	}
	if meta.StopReason != "stop" {
		t.Errorf("expected stopReason 'stop', got %q", meta.StopReason)
	}
	if events[1].Type != "done" {
		t.Errorf("expected done event second, got %s", events[1].Type)
	}
}

func TestOpenCodeStream_ParseLine_StepFinishToolCalls(t *testing.T) {
	line := `{"type":"step_finish","timestamp":1,"sessionID":"ses_abc","part":{"reason":"tool-calls","tokens":{"total":36715,"input":326,"output":77},"cost":0}}`
	events := parseOpenCodeLine(line)

	if len(events) != 0 {
		t.Fatalf("expected 0 events for tool-calls step_finish, got %d: %+v", len(events), events)
		return
	}
}

func TestOpenCodeStream_ParseLine_StepStart(t *testing.T) {
	line := `{"type":"step_start","timestamp":1,"sessionID":"ses_abc","part":{"type":"step-start"}}`
	events := parseOpenCodeLine(line)

	if len(events) != 0 {
		t.Fatalf("expected 0 events for step_start, got %d", len(events))
		return
	}
}

func TestOpenCodeStream_GetCapturedSessionID(t *testing.T) {
	parser := &OpenCodeStreamParser{}

	// Before any parsing, session ID is empty
	if id := parser.GetCapturedSessionID(); id != "" {
		t.Errorf("expected empty session ID before parsing, got %q", id)
	}

	// step_start captures session ID
	ch := make(chan StreamEvent, 64)
	parser.ParseLine(`{"type":"step_start","timestamp":1,"sessionID":"ses_test123","part":{"type":"step-start"}}`, ch)
	if id := parser.GetCapturedSessionID(); id != "ses_test123" {
		t.Errorf("expected session ID ses_test123 after step_start, got %q", id)
	}

	// text message updates to new session ID
	parser.ParseLine(`{"type":"text","timestamp":2,"sessionID":"ses_updated456","part":{"type":"text","text":"\n\nhello"}}`, ch)
	if id := parser.GetCapturedSessionID(); id != "ses_updated456" {
		t.Errorf("expected session ID ses_updated456 after text, got %q", id)
	}
}

func TestOpenCodeStream_ParseLine_EmptyText(t *testing.T) {
	line := `{"type":"text","timestamp":1,"sessionID":"ses_abc","part":{"type":"text","text":"\n\n"}}`
	events := parseOpenCodeLine(line)

	// After stripping \n\n prefix, text is empty → no event
	if len(events) != 0 {
		t.Fatalf("expected 0 events for empty text after prefix strip, got %d", len(events))
		return
	}
}

func TestOpenCodeStream_ParseLine_UnparseableLine(t *testing.T) {
	events := parseOpenCodeLine("not json at all")
	if len(events) != 0 {
		t.Fatalf("expected 0 events for unparseable line, got %d", len(events))
		return
	}
}

func TestOpenCodeStream_ParseLine_UnknownType(t *testing.T) {
	line := `{"type":"custom_event","timestamp":1,"sessionID":"ses_abc","part":{}}`
	events := parseOpenCodeLine(line)
	if len(events) != 0 {
		t.Fatalf("expected 0 events for unknown type, got %d", len(events))
		return
	}
}

func TestOpenCodeStream_SessionIDCapture(t *testing.T) {
	parser := &OpenCodeStreamParser{}
	ch := make(chan StreamEvent, 64)

	// First message captures session ID
	line1 := `{"type":"step_start","timestamp":1,"sessionID":"ses_captured123","part":{"type":"step-start"}}`
	parser.ParseLine(line1, ch)

	// Second message uses the captured session ID in metadata
	line2 := `{"type":"step_finish","timestamp":2,"part":{"reason":"stop","tokens":{"total":100,"input":80,"output":20},"cost":0}}`
	parser.ParseLine(line2, ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
		return
	}
	if events[0].Meta.SessionID != "ses_captured123" {
		t.Errorf("expected sessionID 'ses_captured123' in metadata, got %q", events[0].Meta.SessionID)
	}
}

func TestOpenCodeStream_MultiStepFlow(t *testing.T) {
	lines := []string{
		`{"type":"step_start","timestamp":1,"sessionID":"ses_multi","part":{"type":"step-start"}}`,
		`{"type":"tool_use","timestamp":2,"sessionID":"ses_multi","part":{"type":"tool","tool":"read","callID":"call_1","state":{"status":"completed","input":{"filePath":"main.go"},"output":"package main"}}}`,
		`{"type":"step_finish","timestamp":3,"sessionID":"ses_multi","part":{"reason":"tool-calls","tokens":{"total":500,"input":400,"output":100},"cost":0}}`,
		`{"type":"step_start","timestamp":4,"sessionID":"ses_multi","part":{"type":"step-start"}}`,
		`{"type":"text","timestamp":5,"sessionID":"ses_multi","part":{"type":"text","text":"\n\nThe file uses package main."}}`,
		`{"type":"step_finish","timestamp":6,"sessionID":"ses_multi","part":{"reason":"stop","tokens":{"total":600,"input":500,"output":100},"cost":0}}`,
	}

	ch := make(chan StreamEvent, 64)
	parser := &OpenCodeStreamParser{}
	for _, line := range lines {
		parser.ParseLine(line, ch)
	}
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Expected: tool_use, content, metadata, done
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
		return
	}

	// Event 1: tool_use
	if events[0].Type != "tool_use" {
		t.Errorf("event 0: expected tool_use, got %s", events[0].Type)
	}
	if events[0].Tool.Name != "Read" {
		t.Errorf("event 0: expected normalized tool name 'Read', got %q", events[0].Tool.Name)
	}

	// Event 2: content
	if events[1].Type != "content" {
		t.Errorf("event 1: expected content, got %s", events[1].Type)
	}
	if events[1].Content != "The file uses package main." {
		t.Errorf("event 1: unexpected content %q", events[1].Content)
	}

	// Event 3: metadata
	if events[2].Type != "metadata" {
		t.Errorf("event 2: expected metadata, got %s", events[2].Type)
	}
	if events[2].Meta.SessionID != "ses_multi" {
		t.Errorf("event 2: expected sessionID 'ses_multi', got %q", events[2].Meta.SessionID)
	}

	// Event 4: done
	if events[3].Type != "done" {
		t.Errorf("event 3: expected done, got %s", events[3].Type)
	}
}

func TestBuildOpenCodeStreamArgs_NewSession(t *testing.T) {
	req := ChatRequest{
		Prompt:  "say hello",
		WorkDir: "/home/user/project",
		Model:   "minimax-cn-coding-plan/MiniMax-M2.7",
	}
	args := buildOpenCodeStreamArgs(req)

	expected := []string{"run", "say hello", "--format", "json", "--dangerously-skip-permissions", "--dir", "/home/user/project", "--model", "minimax-cn-coding-plan/MiniMax-M2.7"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
		return
	}
	for i, v := range expected {
		if args[i] != v {
			t.Errorf("arg %d: expected %q, got %q", i, v, args[i])
		}
	}

	// Should NOT contain --session (new session, Resume=false)
	for _, a := range args {
		if a == "--session" {
			t.Error("should not contain --session for new session")
		}
	}
}

func TestBuildOpenCodeStreamArgs_NewSessionWithClawBenchUUID(t *testing.T) {
	// When ClawBench sends a UUID session ID but Resume=false, should NOT pass --session
	req := ChatRequest{
		Prompt:    "hello",
		SessionID: "c8abd620-87e9-43d7-a031-fa20674667d0", // ClawBench UUID
		Resume:    false,
	}
	args := buildOpenCodeStreamArgs(req)

	for _, a := range args {
		if a == "--session" {
			t.Error("should not contain --session when Resume=false even with UUID SessionID")
		}
	}
}

func TestBuildOpenCodeStreamArgs_ResumeSession(t *testing.T) {
	req := ChatRequest{
		Prompt:    "continue",
		SessionID: "ses_abc123", // OpenCode session ID
		Resume:    true,
		WorkDir:   "/home/user/project",
	}
	args := buildOpenCodeStreamArgs(req)

	// Should contain --session ses_abc123 because Resume=true
	found := false
	for i, a := range args {
		if a == "--session" && i+1 < len(args) && args[i+1] == "ses_abc123" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected --session ses_abc123 in args when Resume=true")
	}
}

func TestNormalizeOpenCodeToolName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Existing mappings
		{"read", "Read"},
		{"write", "Write"},
		{"edit", "Edit"},
		{"bash", "Bash"},
		{"glob", "Glob"},
		{"grep", "Grep"},
		{"ls", "LS"},
		// New mappings
		{"webfetch", "WebFetch"},
		{"websearch", "WebSearch"},
		{"skill", "Skill"},
		{"task", "Agent"},
		{"todowrite", "TodoWrite"},
		{"look_at", "Read"},
		// Unknown tool → passthrough
		{"custom_tool", "custom_tool"},
		{"", ""},
	}
	for _, tt := range tests {
		got := normalizeToolName(tt.input)
		if got != tt.expected {
			t.Errorf("normalizeToolName(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestNormalizeOpenCodeInput_FieldRemapping(t *testing.T) {
	// filePath → file_path
	input1 := json.RawMessage(`{"filePath":"/tmp/test.go"}`)
	norm1, err := normalizeToolInput(input1, map[string]string{"oldString": "old_string", "newString": "new_string"})
	if err != nil {
		t.Fatalf("normalizeToolInput failed: %v", err)
		return
	}
	result1 := string(norm1)
	var parsed1 map[string]any
	if unmarshalErr := json.Unmarshal([]byte(result1), &parsed1); unmarshalErr != nil {
		t.Fatalf("failed to parse result: %v", unmarshalErr)
		return
	}
	if _, exists := parsed1["filePath"]; exists {
		t.Error("filePath should be removed")
	}
	if parsed1["file_path"] != "/tmp/test.go" {
		t.Errorf("expected file_path=/tmp/test.go, got %v", parsed1["file_path"])
	}

	// oldString → old_string
	input2 := json.RawMessage(`{"oldString":"foo","newString":"bar"}`)
	norm2, err := normalizeToolInput(input2, map[string]string{"oldString": "old_string", "newString": "new_string"})
	if err != nil {
		t.Fatalf("normalizeToolInput failed: %v", err)
		return
	}
	result2 := string(norm2)
	var parsed2 map[string]any
	if unmarshalErr := json.Unmarshal([]byte(result2), &parsed2); unmarshalErr != nil {
		t.Fatalf("failed to parse result: %v", unmarshalErr)
		return
	}
	if _, exists := parsed2["oldString"]; exists {
		t.Error("oldString should be removed")
	}
	if _, exists := parsed2["newString"]; exists {
		t.Error("newString should be removed")
	}
	if parsed2["old_string"] != "foo" {
		t.Errorf("expected old_string=foo, got %v", parsed2["old_string"])
	}
	if parsed2["new_string"] != "bar" {
		t.Errorf("expected new_string=bar, got %v", parsed2["new_string"])
	}

	// Combined: filePath + oldString + newString
	input3 := json.RawMessage(`{"filePath":"main.go","oldString":"hello","newString":"world","replace_all":true}`)
	norm3, err := normalizeToolInput(input3, map[string]string{"oldString": "old_string", "newString": "new_string"})
	if err != nil {
		t.Fatalf("normalizeToolInput failed: %v", err)
		return
	}
	result3 := string(norm3)
	var parsed3 map[string]any
	if err := json.Unmarshal([]byte(result3), &parsed3); err != nil {
		t.Fatalf("failed to parse result: %v", err)
		return
	}
	if parsed3["file_path"] != "main.go" {
		t.Errorf("expected file_path=main.go, got %v", parsed3["file_path"])
	}
	if parsed3["old_string"] != "hello" {
		t.Errorf("expected old_string=hello, got %v", parsed3["old_string"])
	}
	if parsed3["new_string"] != "world" {
		t.Errorf("expected new_string=world, got %v", parsed3["new_string"])
	}
	if parsed3["replace_all"] != true {
		t.Errorf("expected replace_all=true, got %v", parsed3["replace_all"])
	}
}

func TestNormalizeOpenCodeInput_UnparseableJSON(t *testing.T) {
	bad := json.RawMessage(`not valid json`)
	_, err := normalizeToolInput(bad, nil)
	if err == nil {
		t.Error("expected error for unparseable JSON")
	}
}

func TestNormalizeOpenCodeInput_AlreadyCanonical(t *testing.T) {
	// If input already uses snake_case, no remapping needed
	input := json.RawMessage(`{"file_path":"/tmp/test.go","old_string":"foo","new_string":"bar"}`)
	norm, err := normalizeToolInput(input, map[string]string{"oldString": "old_string", "newString": "new_string"})
	if err != nil {
		t.Fatalf("normalizeToolInput failed: %v", err)
		return
	}
	result := string(norm)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
		return
	}
	if parsed["file_path"] != "/tmp/test.go" {
		t.Errorf("expected file_path=/tmp/test.go, got %v", parsed["file_path"])
	}
	if parsed["old_string"] != "foo" {
		t.Errorf("expected old_string=foo, got %v", parsed["old_string"])
	}
}

func TestOpenCodeStream_ParseLine_ToolUse_NewTools(t *testing.T) {
	tests := []struct {
		name         string
		line         string
		expectedTool string
		checkInput   func(t *testing.T, input map[string]any)
	}{
		{
			name:         "websearch",
			line:         `{"type":"tool_use","timestamp":1,"sessionID":"ses_abc","part":{"type":"tool","tool":"websearch","callID":"call_ws","state":{"status":"completed","input":{"query":"golang testing"},"output":"results"}}}`,
			expectedTool: "WebSearch",
			checkInput: func(t *testing.T, input map[string]any) {
				if input["query"] != "golang testing" {
					t.Errorf("expected query='golang testing', got %v", input["query"])
				}
			},
		},
		{
			name:         "webfetch",
			line:         `{"type":"tool_use","timestamp":1,"sessionID":"ses_abc","part":{"type":"tool","tool":"webfetch","callID":"call_wf","state":{"status":"completed","input":{"url":"https://example.com"},"output":"page content"}}}`,
			expectedTool: "WebFetch",
			checkInput: func(t *testing.T, input map[string]any) {
				if input["url"] != "https://example.com" {
					t.Errorf("expected url='https://example.com', got %v", input["url"])
				}
			},
		},
		{
			name:         "skill",
			line:         `{"type":"tool_use","timestamp":1,"sessionID":"ses_abc","part":{"type":"tool","tool":"skill","callID":"call_sk","state":{"status":"completed","input":{"skill":"commit"},"output":"done"}}}`,
			expectedTool: "Skill",
			checkInput: func(t *testing.T, input map[string]any) {
				if input["skill"] != "commit" {
					t.Errorf("expected skill='commit', got %v", input["skill"])
				}
			},
		},
		{
			name:         "task_as_agent",
			line:         `{"type":"tool_use","timestamp":1,"sessionID":"ses_abc","part":{"type":"tool","tool":"task","callID":"call_task","state":{"status":"completed","input":{"description":"research task"},"output":"result"}}}`,
			expectedTool: "Agent",
			checkInput: func(t *testing.T, input map[string]any) {
				if input["description"] != "research task" {
					t.Errorf("expected description='research task', got %v", input["description"])
				}
			},
		},
		{
			name:         "grep_with_camelCase",
			line:         `{"type":"tool_use","timestamp":1,"sessionID":"ses_abc","part":{"type":"tool","tool":"grep","callID":"call_grep","state":{"status":"completed","input":{"pattern":"TODO","path":"./src"},"output":"matches"}}}`,
			expectedTool: "Grep",
			checkInput: func(t *testing.T, input map[string]any) {
				if input["pattern"] != "TODO" {
					t.Errorf("expected pattern='TODO', got %v", input["pattern"])
				}
			},
		},
		{
			name:         "edit_with_camelCase_fields",
			line:         `{"type":"tool_use","timestamp":1,"sessionID":"ses_abc","part":{"type":"tool","tool":"edit","callID":"call_edit","state":{"status":"completed","input":{"filePath":"main.go","oldString":"old","newString":"new"},"output":"ok"}}}`,
			expectedTool: "Edit",
			checkInput: func(t *testing.T, input map[string]any) {
				if input["file_path"] != "main.go" {
					t.Errorf("expected file_path='main.go', got %v", input["file_path"])
				}
				if input["old_string"] != "old" {
					t.Errorf("expected old_string='old', got %v", input["old_string"])
				}
				if input["new_string"] != "new" {
					t.Errorf("expected new_string='new', got %v", input["new_string"])
				}
				// camelCase keys should not exist
				if _, ok := input["filePath"]; ok {
					t.Error("filePath should not exist after normalization")
				}
				if _, ok := input["oldString"]; ok {
					t.Error("oldString should not exist after normalization")
				}
				if _, ok := input["newString"]; ok {
					t.Error("newString should not exist after normalization")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := parseOpenCodeLine(tt.line)
			if len(events) != 1 {
				t.Fatalf("expected 1 event, got %d", len(events))
				return
			}
			if events[0].Tool == nil {
				t.Fatal("expected tool call, got nil")
				return
			}
			if events[0].Tool.Name != tt.expectedTool {
				t.Errorf("expected tool name %q, got %q", tt.expectedTool, events[0].Tool.Name)
			}
			var input map[string]any
			if err := json.Unmarshal([]byte(events[0].Tool.Input), &input); err != nil {
				t.Fatalf("failed to parse tool input: %v", err)
				return
			}
			tt.checkInput(t, input)
		})
	}
}

func TestBuildOpenCodeStreamArgs_Minimal(t *testing.T) {
	req := ChatRequest{
		Prompt: "hello",
	}
	args := buildOpenCodeStreamArgs(req)

	// Minimal args: run, prompt, --format json, --thinking, --dangerously-skip-permissions
	expected := []string{"run", "hello", "--format", "json", "--dangerously-skip-permissions"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
		return
	}
	for i, v := range expected {
		if args[i] != v {
			t.Errorf("arg %d: expected %q, got %q", i, v, args[i])
		}
	}
}
