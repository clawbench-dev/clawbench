package ai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func parseOpenCodeLine(line string) []StreamEvent {
	ch := make(chan StreamEvent, 64)
	parser := &OpenCodeStreamParser{InputRemaps: openCodeInputRemaps}
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

// A non-terminal step emits nothing by itself (the turn has not ended), but its
// usage MUST be accumulated — that is the whole fix. Emitting nothing while
// also discarding the counters was the defect.
func TestOpenCodeStream_ParseLine_StepFinishToolCalls(t *testing.T) {
	line := `{"type":"step_finish","timestamp":1,"sessionID":"ses_abc","part":{"reason":"tool-calls","tokens":{"total":36715,"input":326,"output":77},"cost":0}}`
	events := parseOpenCodeLine(line)

	if len(events) != 0 {
		t.Fatalf("expected 0 events for tool-calls step_finish, got %d: %+v", len(events), events)
	}
}

// Every step of a tool-using turn contributes to the turn total. Only the
// terminal "stop" step emits, and it must report the SUM over all steps.
//
// Measured against OpenCode's own storage: one 812-step session reports
// 267,293,125 tokens across its steps but only 11,705,915 in its stop steps —
// recording the stop step alone under-counted 22.8x.
func TestOpenCodeStream_MultiStepAccumulatesUsage(t *testing.T) {
	lines := []string{
		`{"type":"step_finish","timestamp":1,"sessionID":"ses_acc","part":{"reason":"tool-calls","tokens":{"total":1000,"input":100,"output":10,"cache":{"read":800,"write":90}},"cost":0}}`,
		`{"type":"step_finish","timestamp":2,"sessionID":"ses_acc","part":{"reason":"tool-calls","tokens":{"total":2000,"input":200,"output":20,"cache":{"read":1700,"write":80}},"cost":0}}`,
		`{"type":"step_finish","timestamp":3,"sessionID":"ses_acc","part":{"reason":"stop","tokens":{"total":3000,"input":300,"output":30,"cache":{"read":2600,"write":70}},"cost":0.25}}`,
	}

	ch := make(chan StreamEvent, 32)
	parser := &OpenCodeStreamParser{}
	for _, line := range lines {
		parser.ParseLine(line, ch)
	}
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	// Only the terminal step emits (metadata + done).
	if len(events) != 2 {
		t.Fatalf("expected 2 events (metadata + done), got %d: %+v", len(events), events)
	}
	meta := events[0].Meta
	if meta == nil {
		t.Fatal("expected metadata")
	}
	// total = 1000 + 2000 + 3000 (the agent's own per-step totals, summed)
	if meta.TotalTokens != 6000 {
		t.Errorf("total must be the SUM over steps: want 6000, got %d", meta.TotalTokens)
	}
	// input is normalized to the FULL prompt: raw input + cache read + write
	// (100+200+300) + (800+1700+2600) + (90+80+70)
	if want := 600 + 5100 + 240; meta.InputTokens != want {
		t.Errorf("input must fold in the cache split: want %d, got %d", want, meta.InputTokens)
	}
	if meta.OutputTokens != 60 {
		t.Errorf("output must be the SUM over steps: want 60, got %d", meta.OutputTokens)
	}
	if meta.CacheHitTokens != 5100 {
		t.Errorf("cache hit must be the summed cache.read: want 5100, got %d", meta.CacheHitTokens)
	}
	if meta.CacheMissTokens != 600 {
		t.Errorf("cache miss must be the summed raw input: want 600, got %d", meta.CacheMissTokens)
	}
	// The accumulator must be reset for the next turn.
	if parser.turn.steps != 0 || parser.turn.total != 0 {
		t.Errorf("accumulator must reset after the turn: %+v", parser.turn)
	}
}

// Two consecutive turns must not leak usage into each other.
func TestOpenCodeStream_TurnsDoNotLeak(t *testing.T) {
	ch := make(chan StreamEvent, 32)
	parser := &OpenCodeStreamParser{}

	parser.ParseLine(`{"type":"step_finish","timestamp":1,"part":{"reason":"stop","tokens":{"total":500,"input":400,"output":100},"cost":0}}`, ch)
	parser.ParseLine(`{"type":"step_finish","timestamp":2,"part":{"reason":"stop","tokens":{"total":700,"input":600,"output":100},"cost":0}}`, ch)
	close(ch)

	var metas []*Metadata
	for ev := range ch {
		if ev.Type == "metadata" && ev.Meta != nil {
			metas = append(metas, ev.Meta)
		}
	}
	if len(metas) != 2 {
		t.Fatalf("expected 2 metadata events, got %d", len(metas))
	}
	if metas[0].TotalTokens != 500 {
		t.Errorf("turn 1 total: want 500, got %d", metas[0].TotalTokens)
	}
	if metas[1].TotalTokens != 700 {
		t.Errorf("turn 2 total must NOT include turn 1: want 700, got %d", metas[1].TotalTokens)
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
	parser := &OpenCodeStreamParser{InputRemaps: openCodeInputRemaps}

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
	parser := &OpenCodeStreamParser{InputRemaps: openCodeInputRemaps}
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
	parser := &OpenCodeStreamParser{InputRemaps: openCodeInputRemaps}
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

// step_finish.cost is the SESSION running total, not this step's cost. It must
// be marked cumulative and preserved in cost_session_total so the ledger can
// diff it; writing it as a per-turn cost is what over-counted Claude by 2.7x.
func TestOpenCodeStream_ParseLine_StepFinishMarksCostCumulative(t *testing.T) {
	line := `{"type":"step_finish","timestamp":1,"sessionID":"ses_abc","part":{"reason":"stop","tokens":{"total":100,"input":80,"output":20},"cost":0.413}}`
	events := parseOpenCodeLine(line)
	if len(events) != 2 {
		t.Fatalf("expected metadata+done, got %d: %+v", len(events), events)
	}
	meta := events[0].Meta
	if meta == nil {
		t.Fatal("expected a metadata event")
	}
	if !meta.CostCumulative {
		t.Error("step_finish.cost must be flagged cumulative")
	}
	if meta.CostSessionTotal != 0.413 {
		t.Errorf("CostSessionTotal = %v, want 0.413", meta.CostSessionTotal)
	}
}

// A zero cost must NOT be flagged cumulative: there is nothing to diff, and
// flagging it would make the ledger treat a free turn as a rebase point.
func TestOpenCodeStream_ParseLine_StepFinishZeroCostIsNotCumulative(t *testing.T) {
	line := `{"type":"step_finish","timestamp":1,"sessionID":"ses_abc","part":{"reason":"stop","tokens":{"total":100,"input":80,"output":20},"cost":0}}`
	events := parseOpenCodeLine(line)
	if len(events) != 2 {
		t.Fatalf("expected metadata+done, got %d", len(events))
	}
	if events[0].Meta.CostCumulative {
		t.Error("a zero cost must not be marked cumulative")
	}
}

// A step_finish without a token block must still finish the turn: the cost may
// be present while usage is not (generation tools report no usage block).
func TestOpenCodeStream_ParseLine_StepFinishWithoutTokens(t *testing.T) {
	line := `{"type":"step_finish","timestamp":1,"sessionID":"ses_abc","part":{"reason":"stop","cost":0.5}}`
	events := parseOpenCodeLine(line)
	if len(events) != 2 {
		t.Fatalf("expected metadata+done, got %d: %+v", len(events), events)
	}
	meta := events[0].Meta
	if meta.InputTokens != 0 || meta.TotalTokens != 0 {
		t.Errorf("absent tokens must stay zero, got in=%d total=%d", meta.InputTokens, meta.TotalTokens)
	}
	if meta.CostSessionTotal != 0.5 {
		t.Errorf("CostSessionTotal = %v, want 0.5", meta.CostSessionTotal)
	}
}

// The agent's reported total can be SMALLER than input+output (an older
// CodeBuddy build did this). The parser must raise it rather than store a total
// that contradicts its own parts.
func TestOpenCodeStream_ParseLine_StepFinishRaisesInconsistentTotal(t *testing.T) {
	line := `{"type":"step_finish","timestamp":1,"sessionID":"ses_abc","part":{"reason":"stop","tokens":{"total":10,"input":80,"output":20},"cost":0}}`
	events := parseOpenCodeLine(line)
	if len(events) != 2 {
		t.Fatalf("expected metadata+done, got %d", len(events))
	}
	if got := events[0].Meta.TotalTokens; got != 100 {
		t.Errorf("TotalTokens = %d, want input+output = 100", got)
	}
}

// An unparseable step_finish is skipped, not fatal: one malformed line must not
// kill the stream.
func TestOpenCodeStream_ParseLine_StepFinishUnparseable(t *testing.T) {
	p := &OpenCodeStreamParser{}
	ch := make(chan StreamEvent, 8)
	p.parseStepFinish(json.RawMessage(`not json`), ch)
	close(ch)
	if len(ch) != 0 {
		t.Errorf("an unparseable part must emit nothing, got %d events", len(ch))
	}
}

// ── error / retry frames ────────────────────────────────────────────────────
//
// Fixtures below are the exact frames captured from opencode 2.2.6 against a
// mock provider: a 402 produces one step_start then one error frame and exit
// code 1 with an EMPTY stderr; a persistent 429 produces a step_start, then a
// `retry` frame per attempt (11 of them over ~85s), then a final error frame.

func TestOpenCodeStream_ErrorFrame_QuotaIsClassifiedAndTerminal(t *testing.T) {
	line := `{"type":"error","timestamp":1794063822858,"sessionID":"ses_q","error":{"type":"provider.quota","message":"Insufficient Balance","status":402,"response":{"body":"{\"error\":{\"type\":\"insufficient_quota\"}}"}}}`
	events := parseOpenCodeLine(line)

	if len(events) != 2 {
		t.Fatalf("expected the error plus a terminal done, got %d events: %+v", len(events), events)
	}
	errEv := events[0]
	if errEv.Type != "error" {
		t.Fatalf("expected an error event first, got %q", errEv.Type)
	}
	if errEv.Reason != ReasonQuotaExhausted {
		t.Errorf("a 402 must be classified as %q, got %q", ReasonQuotaExhausted, errEv.Reason)
	}
	if errEv.Error != "Insufficient Balance" {
		t.Errorf("the provider's own message must reach the user, got %q", errEv.Error)
	}
	if errEv.HTTPStatus != 402 {
		t.Errorf("expected HTTPStatus 402, got %d", errEv.HTTPStatus)
	}
	if events[1].Type != "done" {
		t.Fatalf("expected a done event to close the turn, got %q", events[1].Type)
	}
}

// A terminal error frame WITHOUT a quota signature stays a generic request
// failure: mislabelling an unrelated upstream error as "out of credit" would
// send the user to the billing page for nothing.
func TestOpenCodeStream_ErrorFrame_NonQuotaStaysRequestFailed(t *testing.T) {
	line := `{"type":"error","timestamp":1,"sessionID":"ses_q","error":{"type":"provider.api_error","message":"Internal server error","status":500}}`
	events := parseOpenCodeLine(line)

	if len(events) != 2 {
		t.Fatalf("expected error + done, got %d events", len(events))
	}
	if events[0].Reason != ReasonRequestFailed {
		t.Errorf("expected %q, got %q", ReasonRequestFailed, events[0].Reason)
	}
}

// A malformed frame (no error object at all) must still terminate the turn
// rather than leaving it hanging on a closed channel.
func TestOpenCodeStream_ErrorFrame_WithoutPayloadStillTerminates(t *testing.T) {
	events := parseOpenCodeLine(`{"type":"error","timestamp":1,"sessionID":"ses_q"}`)

	if len(events) != 2 {
		t.Fatalf("expected error + done, got %d events", len(events))
	}
	if events[0].Type != "error" || events[1].Type != "done" {
		t.Fatalf("expected error then done, got %q then %q", events[0].Type, events[1].Type)
	}
}

// A `retry` frame must produce NOTHING. Announcing it was tried and reverted:
// a warning becomes a permanent block, so a 429 that recovered on the next
// attempt left a stale "rate limited" card on a turn that actually succeeded —
// and because classification reads reasons off blocks, that stale card also made
// the turn look failed (a scheduled task reported `failed` for completed work).
func TestOpenCodeStream_RetryFrame_IsSilent(t *testing.T) {
	cases := map[string]string{
		"quota/limit retry": `{"type":"retry","timestamp":1,"sessionID":"ses_q","attempt":2,"error":{"type":"provider.rate-limit","message":"Rate limit exceeded","status":429}}`,
		"transient retry":   `{"type":"retry","timestamp":1,"sessionID":"ses_q","attempt":2,"error":{"type":"provider.transport","message":"ECONNREFUSED: Unable to connect"}}`,
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			events := parseOpenCodeLine(line)
			if len(events) != 0 {
				t.Fatalf("a retry frame must not emit anything, got %+v", events)
			}
		})
	}
}

// The terminal quota failure must still be reported — dropping the retry notice
// must not make the real failure silent.
func TestOpenCodeStream_RetryThenTerminalQuotaReportsOnlyTheTerminal(t *testing.T) {
	parser := &OpenCodeStreamParser{InputRemaps: openCodeInputRemaps}
	ch := make(chan StreamEvent, 16)
	parser.ParseLine(`{"type":"retry","timestamp":1,"sessionID":"ses_q","attempt":2,"error":{"type":"provider.rate-limit","message":"Rate limit exceeded","status":429}}`, ch)
	parser.ParseLine(`{"type":"error","timestamp":2,"sessionID":"ses_q","error":{"type":"provider.rate-limit","message":"Rate limit exceeded","status":429}}`, ch)
	close(ch)

	var quotaBlocks int
	var gotDone bool
	for ev := range ch {
		if ev.Reason == ReasonQuotaExhausted {
			quotaBlocks++
		}
		if ev.Type == "done" {
			gotDone = true
		}
	}
	assert.Equal(t, 1, quotaBlocks, "exactly one visible card for one failure")
	assert.True(t, gotDone, "the turn must terminate")
}

// A turn that hit a retry and then SUCCEEDED must not look failed. This is the
// regression the retry notice caused: the warning block stayed in the
// transcript and classification read it as the turn's outcome.
func TestOpenCodeStream_RetryThenSuccessHasNoFailureBlock(t *testing.T) {
	parser := &OpenCodeStreamParser{InputRemaps: openCodeInputRemaps}
	ch := make(chan StreamEvent, 32)
	parser.ParseLine(`{"type":"step_start","timestamp":1,"sessionID":"ses_q","part":{}}`, ch)
	parser.ParseLine(`{"type":"retry","timestamp":2,"sessionID":"ses_q","attempt":2,"error":{"type":"provider.rate-limit","message":"Rate limit exceeded","status":429}}`, ch)
	parser.ParseLine(`{"type":"text","timestamp":3,"sessionID":"ses_q","part":{"type":"text","text":"the answer"}}`, ch)
	parser.ParseLine(`{"type":"step_finish","timestamp":4,"sessionID":"ses_q","part":{"reason":"stop","tokens":{"total":10,"input":5,"output":5}}}`, ch)
	close(ch)

	for ev := range ch {
		assert.NotEqual(t, "warning", ev.Type,
			"a recovered retry must leave no warning block behind: %+v", ev)
	}
}

// ReportedStreamError is what stops the CLI driver from appending a second,
// retryable "backend exited abnormally" warning on top of the classified error.
func TestOpenCodeStream_ReportedStreamError(t *testing.T) {
	parser := &OpenCodeStreamParser{InputRemaps: openCodeInputRemaps}
	ch := make(chan StreamEvent, 16)

	if parser.ReportedStreamError() {
		t.Fatal("a fresh parser must not claim to have reported an error")
	}
	parser.ParseLine(`{"type":"error","timestamp":1,"sessionID":"ses_q","error":{"type":"provider.quota","message":"Insufficient Balance","status":402}}`, ch)
	if !parser.ReportedStreamError() {
		t.Fatal("after an error frame the parser must report it")
	}
}

func TestIsOpenCodeQuotaError(t *testing.T) {
	cases := []struct {
		name  string
		err   *OpenCodeError
		quota bool
	}{
		{"402 status", &OpenCodeError{Type: "provider.x", Status: 402}, true},
		{"provider.quota type", &OpenCodeError{Type: "provider.quota"}, true},
		{"insufficient balance text", &OpenCodeError{Type: "provider.api_error", Message: "Insufficient Balance"}, true},
		{"rate limit type", &OpenCodeError{Type: "provider.rate-limit", Message: "Rate limit exceeded"}, true},
		{"credit exhausted", &OpenCodeError{Message: "credit limit exceeded"}, true},
		{"quota in message only", &OpenCodeError{Message: "insufficient_quota reached"}, true},
		// OpenCode's own limit types, whose messages contain none of the phrases
		// above — these were classified as retryable request_failed before.
		{"provider.free-limit", &OpenCodeError{Type: "provider.free-limit", Message: "Free usage limit"}, true},
		{"provider.go-limit", &OpenCodeError{Type: "provider.go-limit", Message: "OpenCode Go usage limit reached."}, true},
		{"provider.chatgpt-limit", &OpenCodeError{Type: "provider.chatgpt-limit", Message: "subscription sharing"}, true},
		{"budget limit error", &OpenCodeError{Type: "BudgetLimitError", Message: "budget exhausted"}, true},
		// False-positive guards. "balance" alone matches "load balancer" and
		// "insufficient" alone matches "insufficient permissions"; either would
		// send a user with a working account to the billing page AND disable the
		// retry that would have fixed a transient failure.
		{"load balancer", &OpenCodeError{Type: "provider.transport", Message: "connection reset by load balancer"}, false},
		{"insufficient permissions", &OpenCodeError{Type: "provider.api_error", Message: "insufficient permissions for this tool"}, false},
		{"plain 500", &OpenCodeError{Type: "provider.api_error", Message: "Internal server error", Status: 500}, false},
		{"transport failure", &OpenCodeError{Type: "provider.transport", Message: "ECONNREFUSED"}, false},
		{"nil error", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.quota, isOpenCodeQuotaError(c.err))
		})
	}
}
