package ai

import (
	"strings"
	"testing"
)

func TestPiStreamParser_SessionEvent(t *testing.T) {
	parser := &PiStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"session","version":3,"id":"019e2110-274a-73ec-9e14-f1a7b5c13e6f","timestamp":"2025-01-01T00:00:00Z","cwd":"/home/user/project"}`, ch)

	// Parser captures session ID internally — CLIBackend.ExecuteStream()
	// handles the session_capture event emission via GetCapturedSessionID().
	if id := parser.GetCapturedSessionID(); id != "019e2110-274a-73ec-9e14-f1a7b5c13e6f" {
		t.Errorf("expected captured session ID, got '%s'", id)
	}
}

func TestPiStreamParser_ThinkingDelta(t *testing.T) {
	parser := &PiStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":"The user wants me to say hello."},"message":{"role":"assistant"}}`, ch)

	select {
	case evt := <-ch:
		if evt.Type != "thinking" {
			t.Errorf("expected thinking event, got %s", evt.Type)
		}
		if evt.Content != "The user wants me to say hello." {
			t.Errorf("unexpected content: %s", evt.Content)
		}
	default:
		t.Error("expected event on channel")
	}
}

func TestPiStreamParser_TextDelta(t *testing.T) {
	parser := &PiStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"Hello!"},"message":{"role":"assistant"}}`, ch)

	select {
	case evt := <-ch:
		if evt.Type != "content" {
			t.Errorf("expected content event, got %s", evt.Type)
		}
		if evt.Content != "Hello!" {
			t.Errorf("unexpected content: %s", evt.Content)
		}
	default:
		t.Error("expected event on channel")
	}
}

func TestPiStreamParser_ToolcallEnd(t *testing.T) {
	parser := &PiStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_end","contentIndex":1,"toolCall":{"type":"toolCall","id":"call_1","name":"read","arguments":{"path":"/etc/hostname","limit":5}}},"message":{"role":"assistant"}}`, ch)

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
		if evt.Tool.ID != "call_1" {
			t.Errorf("expected tool ID 'call_1', got '%s'", evt.Tool.ID)
		}
		if !evt.Tool.Done {
			t.Error("expected Done=true")
		}
		// Pi read uses "path" → should be normalized to "file_path"
		if !strings.Contains(evt.Tool.Input, `"file_path"`) {
			t.Errorf("expected input field 'file_path', got '%s'", evt.Tool.Input)
		}
	default:
		t.Error("expected event on channel")
	}
}

func TestPiStreamParser_ToolExecutionEnd(t *testing.T) {
	parser := &PiStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"tool_execution_end","toolCallId":"call_1","toolName":"bash","result":{"content":[{"type":"text","text":"xulongzhe-KLVL-WXX9"}]},"isError":false}`, ch)

	select {
	case evt := <-ch:
		if evt.Type != "tool_result" {
			t.Errorf("expected tool_result event, got %s", evt.Type)
		}
		if evt.Tool == nil {
			t.Fatal("expected Tool to be non-nil")
		}
		if evt.Tool.ID != "call_1" {
			t.Errorf("expected tool ID 'call_1', got '%s'", evt.Tool.ID)
		}
		if evt.Tool.Status != "success" {
			t.Errorf("expected status 'success', got '%s'", evt.Tool.Status)
		}
		if !strings.Contains(evt.Tool.Output, "xulongzhe-KLVL-WXX9") {
			t.Errorf("expected output to contain 'xulongzhe-KLVL-WXX9', got '%s'", evt.Tool.Output)
		}
	default:
		t.Error("expected event on channel")
	}
}

func TestPiStreamParser_ToolExecutionEndError(t *testing.T) {
	parser := &PiStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"tool_execution_end","toolCallId":"call_2","toolName":"bash","result":{"content":[{"type":"text","text":"permission denied"}]},"isError":true}`, ch)

	select {
	case evt := <-ch:
		if evt.Type != "tool_result" {
			t.Errorf("expected tool_result event, got %s", evt.Type)
		}
		if evt.Tool.Status != "error" {
			t.Errorf("expected status 'error', got '%s'", evt.Tool.Status)
		}
	default:
		t.Error("expected event on channel")
	}
}

func TestPiStreamParser_ToolExecutionEndMultiContent(t *testing.T) {
	parser := &PiStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"tool_execution_end","toolCallId":"call_3","toolName":"bash","result":{"content":[{"type":"text","text":"line1"},{"type":"text","text":"line2"}]},"isError":false}`, ch)

	select {
	case evt := <-ch:
		if evt.Type != "tool_result" {
			t.Errorf("expected tool_result event, got %s", evt.Type)
		}
		// Multiple text content items should be joined with newline
		if evt.Tool.Output != "line1\nline2" {
			t.Errorf("expected joined output 'line1\\nline2', got '%s'", evt.Tool.Output)
		}
	default:
		t.Error("expected event on channel")
	}
}

func TestPiStreamParser_MessageEndMetadata(t *testing.T) {
	parser := &PiStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"message_end","message":{"role":"assistant","usage":{"input":1396,"output":27,"cacheRead":0,"cacheWrite":0,"totalTokens":1423,"cost":{"input":0.004188,"output":0.000405,"cacheRead":0,"cacheWrite":0,"total":0.004593}},"stopReason":"stop","responseId":"resp_123"}}`, ch)

	select {
	case evt := <-ch:
		if evt.Type != "metadata" {
			t.Errorf("expected metadata event, got %s", evt.Type)
		}
		if evt.Meta == nil {
			t.Fatal("expected Meta to be non-nil")
		}
		if evt.Meta.InputTokens != 1396 {
			t.Errorf("expected 1396 input tokens, got %d", evt.Meta.InputTokens)
		}
		if evt.Meta.OutputTokens != 27 {
			t.Errorf("expected 27 output tokens, got %d", evt.Meta.OutputTokens)
		}
		if evt.Meta.CostUSD != 0.004593 {
			t.Errorf("expected cost 0.004593, got %f", evt.Meta.CostUSD)
		}
	default:
		t.Error("expected event on channel")
	}
}

func TestPiStreamParser_AgentEndDone(t *testing.T) {
	parser := &PiStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"agent_end","messages":[]}`, ch)

	select {
	case evt := <-ch:
		if evt.Type != "done" {
			t.Errorf("expected done event, got %s", evt.Type)
		}
	default:
		t.Error("expected event on channel")
	}
}

func TestPiStreamParser_ErrorMessage(t *testing.T) {
	parser := &PiStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"message_end","message":{"role":"assistant","stopReason":"error","errorMessage":"403 forbidden"}}`, ch)

	// Should emit error event
	events := drainEvents(ch, 2)
	var foundError bool
	for _, evt := range events {
		if evt.Type == "error" {
			foundError = true
			if evt.Error != "403 forbidden" {
				t.Errorf("expected error message '403 forbidden', got '%s'", evt.Error)
			}
			// A missing reason made the turn look like a clean finish.
			if evt.Reason != ReasonRequestFailed {
				t.Errorf("expected reason %q, got %q", ReasonRequestFailed, evt.Reason)
			}
		}
	}
	if !foundError {
		t.Error("expected error event from message_end with stopReason=error")
	}
}

func TestPiStreamParser_SkipsUnknownTypes(t *testing.T) {
	parser := &PiStreamParser{}
	ch := make(chan StreamEvent, 10)
	// All of these should produce no events
	skipTypes := []string{
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"turn_end"}`,
		`{"type":"message_start"}`,
		`{"type":"tool_execution_update"}`,
		`{"type":"compaction_start","reason":"context_window"}`,
		`{"type":"compaction_end"}`,
		`{"type":"auto_retry_start"}`,
		`{"type":"auto_retry_end"}`,
		`{"type":"queue_update"}`,
	}
	for _, line := range skipTypes {
		parser.ParseLine(line, ch)
	}

	select {
	case evt := <-ch:
		t.Errorf("expected no events for unknown types, got %+v", evt)
	default:
		// expected
	}
}

func TestPiStreamParser_ToolcallStartAndDeltaNoEvent(t *testing.T) {
	parser := &PiStreamParser{}
	ch := make(chan StreamEvent, 10)

	// toolcall_start — no event emitted
	parser.ParseLine(`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_start","contentIndex":1},"message":{"role":"assistant","content":[{"type":"toolCall","id":"call_abc","name":"edit","arguments":{},"partialJson":"","index":1}]}}`, ch)

	select {
	case evt := <-ch:
		t.Errorf("expected no event from toolcall_start, got %+v", evt)
	default:
	}

	// toolcall_delta — no event emitted
	parser.ParseLine(`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_delta","contentIndex":1,"delta":"{\"path\": \"/tmp/test.go\"}"},"message":{"role":"assistant","content":[{"type":"toolCall","id":"call_abc","name":"edit","arguments":{},"partialJson":"{\"path\": \"/tmp/test.go\"}","index":1}]}}`, ch)

	select {
	case evt := <-ch:
		t.Errorf("expected no event from toolcall_delta, got %+v", evt)
	default:
	}
}

func TestPiStreamParser_ToolcallEndWithEdit(t *testing.T) {
	parser := &PiStreamParser{}
	ch := make(chan StreamEvent, 10)

	// toolcall_end — emit tool_use with normalized name and input
	parser.ParseLine(`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_end","contentIndex":1,"toolCall":{"type":"toolCall","id":"call_abc","name":"edit","arguments":{"path":"/tmp/test.go","edits":[{"oldText":"foo","newText":"bar"}]}}},"message":{"role":"assistant"}}`, ch)

	select {
	case evt := <-ch:
		if evt.Type != "tool_use" {
			t.Errorf("expected tool_use event, got %s", evt.Type)
		}
		if evt.Tool == nil {
			t.Fatal("expected Tool to be non-nil")
		}
		if evt.Tool.Name != "Edit" {
			t.Errorf("expected canonical tool name 'Edit', got '%s'", evt.Tool.Name)
		}
		if !evt.Tool.Done {
			t.Error("expected Done=true")
		}
		// Pi edit: path → file_path, oldText → old_string, newText → new_string
		if !strings.Contains(evt.Tool.Input, `"file_path"`) {
			t.Errorf("expected 'file_path' in input, got '%s'", evt.Tool.Input)
		}
		if !strings.Contains(evt.Tool.Input, `"old_string"`) {
			t.Errorf("expected 'old_string' in input, got '%s'", evt.Tool.Input)
		}
		if !strings.Contains(evt.Tool.Input, `"new_string"`) {
			t.Errorf("expected 'new_string' in input, got '%s'", evt.Tool.Input)
		}
	default:
		t.Error("expected event on channel")
	}
}

func TestPiStreamParser_ThinkingEndEmitsThinkingDone(t *testing.T) {
	parser := &PiStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"message_update","assistantMessageEvent":{"type":"thinking_end","contentIndex":0},"message":{"role":"assistant"}}`, ch)

	select {
	case evt := <-ch:
		if evt.Type != "thinking_done" {
			t.Errorf("expected thinking_done event, got %s", evt.Type)
		}
	default:
		t.Error("expected thinking_done event on channel")
	}
}

func TestPiStreamParser_ThinkingStartNoEvent(t *testing.T) {
	parser := &PiStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine(`{"type":"message_update","assistantMessageEvent":{"type":"thinking_start","contentIndex":0},"message":{"role":"assistant"}}`, ch)

	select {
	case evt := <-ch:
		t.Errorf("expected no event from thinking_start, got %+v", evt)
	default:
	}
}

func TestPiStreamParser_UnparseableLine(t *testing.T) {
	parser := &PiStreamParser{}
	ch := make(chan StreamEvent, 10)
	parser.ParseLine("not json at all", ch)
	parser.ParseLine("", ch)

	select {
	case evt := <-ch:
		t.Errorf("expected no events for unparseable lines, got %+v", evt)
	default:
	}
}

func TestPiStreamParser_FullStreamWithToolUse(t *testing.T) {
	parser := &PiStreamParser{}
	ch := make(chan StreamEvent, 100)

	lines := []string{
		`{"type":"session","version":3,"id":"019e211b-95a0-747e-9805-b9ba8c401d08","timestamp":"2026-05-13T11:31:56.449Z","cwd":"/home/user/project"}`,
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":"read /etc/hostname"}],"timestamp":1}}`,
		`{"type":"message_end","message":{"role":"user","content":[{"type":"text","text":"read /etc/hostname"}],"timestamp":1}}`,
		`{"type":"message_start","message":{"role":"assistant","content":[],"stopReason":"stop"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":"I'll read the file."},"message":{"role":"assistant"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"thinking_end","contentIndex":0},"message":{"role":"assistant"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_end","contentIndex":1,"toolCall":{"id":"call_1","name":"read","arguments":{"path":"/etc/hostname"}}},"message":{"role":"assistant"}}`,
		`{"type":"message_end","message":{"role":"assistant","stopReason":"toolUse"}}`,
		`{"type":"tool_execution_start","toolCallId":"call_1","toolName":"read","args":{"path":"/etc/hostname"}}`,
		`{"type":"tool_execution_end","toolCallId":"call_1","toolName":"read","result":{"content":[{"type":"text","text":"myhost"}]},"isError":false}`,
		`{"type":"turn_end"}`,
		`{"type":"turn_start"}`,
		`{"type":"message_start","message":{"role":"assistant","content":[]}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"The hostname is myhost"},"message":{"role":"assistant"}}`,
		`{"type":"message_end","message":{"role":"assistant","usage":{"input":100,"output":20,"totalTokens":120,"cost":{"input":0.001,"output":0.0003,"total":0.0013}},"stopReason":"stop"}}`,
		`{"type":"agent_end","messages":[]}`,
	}

	for _, line := range lines {
		parser.ParseLine(line, ch)
	}

	// Drain all events
	var events []StreamEvent
	for {
		select {
		case evt := <-ch:
			events = append(events, evt)
		default:
			goto verify
		}
	}
verify:

	// Expected event types in order
	// Note: session_capture is NOT emitted by the parser directly;
	// CLIBackend.ExecuteStream() handles that via GetCapturedSessionID().
	expected := []string{"thinking", "thinking_done", "tool_use", "tool_result", "content", "metadata", "done"}
	if len(events) < len(expected) {
		t.Fatalf("expected at least %d events, got %d", len(expected), len(events))
	}

	eventTypes := make([]string, 0, len(events))
	for _, e := range events {
		eventTypes = append(eventTypes, e.Type)
	}

	for _, exp := range expected {
		found := false
		for _, actual := range eventTypes {
			if actual == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing expected event type %q in %v", exp, eventTypes)
		}
	}

	// Verify session ID captured
	if parser.GetCapturedSessionID() != "019e211b-95a0-747e-9805-b9ba8c401d08" {
		t.Errorf("session ID not captured correctly, got %q", parser.GetCapturedSessionID())
	}

	// Verify tool_use normalization
	for _, e := range events {
		if e.Type == "tool_use" && e.Tool != nil {
			if e.Tool.Name != "Read" {
				t.Errorf("expected tool name 'Read', got %q", e.Tool.Name)
			}
			if e.Tool.ID != "call_1" {
				t.Errorf("expected tool ID 'call_1', got %q", e.Tool.ID)
			}
		}
		if e.Type == "tool_result" && e.Tool != nil {
			if e.Tool.Output != "myhost" {
				t.Errorf("expected tool output 'myhost', got %q", e.Tool.Output)
			}
			if e.Tool.Status != "success" {
				t.Errorf("expected tool status 'success', got %q", e.Tool.Status)
			}
		}
	}
}

// drainEvents reads up to n events from the channel
func drainEvents(ch chan StreamEvent, n int) []StreamEvent {
	var events []StreamEvent
	for range n {
		select {
		case evt := <-ch:
			events = append(events, evt)
		default:
			return events
		}
	}
	return events
}
