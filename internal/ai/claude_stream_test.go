package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// parseLine is a test helper that creates a StreamParser, parses one line, and returns the events.
func parseLine(line string) []StreamEvent {
	ch := make(chan StreamEvent, 64)
	parser := &StreamParser{}
	parser.ParseLine(line, ch)
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	return events
}

// parseLines is a test helper that feeds multiple lines through a single StreamParser.
func parseLines(lines []string) []StreamEvent {
	ch := make(chan StreamEvent, 64)
	parser := &StreamParser{}
	for _, line := range lines {
		if line != "" {
			parser.ParseLine(line, ch)
		}
	}
	close(ch)

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	return events
}

func TestStreamParser_AssistantContent(t *testing.T) {
	msg := ClaudeStreamMessage{
		Type: "assistant",
		Message: &ClaudeStreamMessageBody{
			Content: []ClaudeContentBlock{
				{Type: "text", Text: "Hello, world!"},
			},
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("failed to marshal test message: %v", err)
	}

	events := parseLine(string(data))

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "content" {
		t.Errorf("expected event type 'content', got %q", events[0].Type)
	}
	if events[0].Content != "Hello, world!" {
		t.Errorf("expected content 'Hello, world!', got %q", events[0].Content)
	}
}

func TestStreamParser_SystemEventsSkipped(t *testing.T) {
	lines := []string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"system","subtype":"start"}`,
	}

	events := parseLines(lines)

	if len(events) != 0 {
		t.Fatalf("expected 0 events for system messages, got %d: %+v", len(events), events)
	}
}

func TestStreamParser_MultipleContentBlocks(t *testing.T) {
	msg := ClaudeStreamMessage{
		Type: "assistant",
		Message: &ClaudeStreamMessageBody{
			Content: []ClaudeContentBlock{
				{Type: "text", Text: "First block"},
				{Type: "tool_use", ID: "tool1", Name: "Read"},
				{Type: "text", Text: "Second block"},
			},
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("failed to marshal test message: %v", err)
	}

	events := parseLine(string(data))

	// Should produce: 2 content events + 1 tool_use, in block order
	if len(events) != 3 {
		t.Fatalf("expected 3 events (2 content + 1 tool_use), got %d", len(events))
	}
	// First: first text
	if events[0].Type != "content" || events[0].Content != "First block" {
		t.Errorf("expected first content 'First block', got type=%q content=%q", events[0].Type, events[0].Content)
	}
	// Second: tool_use
	if events[1].Type != "tool_use" {
		t.Errorf("expected second event type 'tool_use', got %q", events[1].Type)
	}
	if events[1].Tool == nil || events[1].Tool.Name != "Read" {
		t.Errorf("expected tool name 'Read', got %v", events[1].Tool)
	}
	// Third: second text
	if events[2].Type != "content" || events[2].Content != "Second block" {
		t.Errorf("expected second content 'Second block', got type=%q content=%q", events[2].Type, events[2].Content)
	}
}

func TestStreamParser_ResultMetadata(t *testing.T) {
	msg := ClaudeStreamMessage{
		Type:         "result",
		Subtype:      "success",
		DurationMs:   5000,
		TotalCostUSD: 0.05,
		SessionID:    "sess-123",
		IsError:      false,
		Usage: &ClaudeStreamUsage{
			InputTokens:  100,
			OutputTokens: 200,
		},
		ModelUsage: map[string]ClaudeStreamModelUsage{
			"claude-3-5-sonnet": {InputTokens: 100, OutputTokens: 200},
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("failed to marshal test message: %v", err)
	}

	events := parseLine(string(data))

	// Result should produce: metadata event + done event
	if len(events) != 2 {
		t.Fatalf("expected 2 events (metadata + done), got %d", len(events))
	}

	// First event: metadata
	if events[0].Type != "metadata" {
		t.Errorf("expected first event type 'metadata', got %q", events[0].Type)
	}
	if events[0].Meta == nil {
		t.Fatal("expected metadata to be non-nil")
	}
	if events[0].Meta.SessionID != "sess-123" {
		t.Errorf("expected session ID 'sess-123', got %q", events[0].Meta.SessionID)
	}
	if events[0].Meta.DurationMs != 5000 {
		t.Errorf("expected duration 5000, got %d", events[0].Meta.DurationMs)
	}
	if events[0].Meta.CostUSD != 0.05 {
		t.Errorf("expected cost 0.05, got %f", events[0].Meta.CostUSD)
	}
	if events[0].Meta.InputTokens != 100 {
		t.Errorf("expected input tokens 100, got %d", events[0].Meta.InputTokens)
	}
	if events[0].Meta.OutputTokens != 200 {
		t.Errorf("expected output tokens 200, got %d", events[0].Meta.OutputTokens)
	}

	// Second event: done
	if events[1].Type != "done" {
		t.Errorf("expected second event type 'done', got %q", events[1].Type)
	}
}

func TestStreamParser_ResultWithIsError(t *testing.T) {
	msg := ClaudeStreamMessage{
		Type:      "result",
		IsError:   true,
		Result:    "something went wrong",
		SessionID: "sess-err",
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	events := parseLine(string(data))

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[0].Type != "warning" || events[0].Content != "something went wrong" {
		t.Errorf("expected warning event with error message, got %v", events[0])
	}
	if events[1].Meta == nil || !events[1].Meta.IsError {
		t.Fatalf("expected IsError=true in metadata")
	}
	if events[1].Meta.ErrorMessage != "something went wrong" {
		t.Errorf("expected error message 'something went wrong', got %q", events[1].Meta.ErrorMessage)
	}
	if events[2].Type != "done" {
		t.Errorf("expected third event type 'done', got %q", events[2].Type)
	}
}

func TestStreamParser_ThinkingDelta(t *testing.T) {
	lines := []string{
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me think..."}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":" about this"}}}`,
	}

	events := parseLines(lines)

	if len(events) != 2 {
		t.Fatalf("expected 2 thinking events, got %d", len(events))
	}
	if events[0].Type != "thinking" {
		t.Errorf("expected first event type 'thinking', got %q", events[0].Type)
	}
	if events[0].Content != "Let me think..." {
		t.Errorf("expected first thinking content 'Let me think...', got %q", events[0].Content)
	}
	if events[1].Content != " about this" {
		t.Errorf("expected second thinking content ' about this', got %q", events[1].Content)
	}
}

func TestStreamParser_ThinkingDedup(t *testing.T) {
	// Simulate: thinking_delta (incremental) then assistant message with thinking block (should be skipped)
	lines := []string{
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"My thought"}}}`,
		`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"My thought"},{"type":"text","text":"Hello"}]}}`,
	}

	events := parseLines(lines)

	// Should get: 1 thinking (from delta) + 1 content (from assistant text)
	// The assistant thinking block should be skipped due to receivedPartialThinking
	var thinkingEvents, contentEvents int
	for _, ev := range events {
		switch ev.Type {
		case "thinking":
			thinkingEvents++
		case "content":
			contentEvents++
		}
	}
	if thinkingEvents != 1 {
		t.Errorf("expected 1 thinking event, got %d", thinkingEvents)
	}
	if contentEvents != 1 {
		t.Errorf("expected 1 content event, got %d", contentEvents)
	}
}

func TestStreamParser_ClaudeThinkingBlock(t *testing.T) {
	// Claude without --include-partial-messages: thinking comes in assistant message
	msg := ClaudeStreamMessage{
		Type: "assistant",
		Message: &ClaudeStreamMessageBody{
			Content: []ClaudeContentBlock{
				{Type: "thinking", Thinking: "Deep analysis here..."},
				{Type: "text", Text: "The answer is 42."},
			},
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	events := parseLine(string(data))

	// Should produce: 1 thinking + 1 content
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Type != "thinking" {
		t.Errorf("expected first event type 'thinking', got %q", events[0].Type)
	}
	if events[0].Content != "Deep analysis here..." {
		t.Errorf("expected thinking content 'Deep analysis here...', got %q", events[0].Content)
	}
	if events[1].Type != "content" || events[1].Content != "The answer is 42." {
		t.Errorf("expected content 'The answer is 42.', got %q", events[1].Content)
	}
}

// --- New tests below ---

func TestStreamParser_TextDedup(t *testing.T) {
	// When text_delta events are received, the full assistant text should be skipped
	lines := []string{
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Hello world"}]}}`,
	}

	events := parseLines(lines)

	// Should get 2 content events from deltas, the full assistant text should be skipped
	var contentText string
	var contentCount int
	for _, ev := range events {
		if ev.Type == "content" {
			contentCount++
			contentText += ev.Content
		}
	}
	if contentCount != 2 {
		t.Errorf("expected 2 content events (from deltas only), got %d", contentCount)
	}
	if contentText != "Hello world" {
		t.Errorf("expected combined content 'Hello world', got %q", contentText)
	}
}

func TestStreamParser_TextDedup_CodebuddySimpleFormat(t *testing.T) {
	// Codebuddy simple format (no Message, uses Text field) should also be skipped when receivedPartial
	lines := []string{
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Partial"}}}`,
		`{"type":"assistant","subtype":"text","text":"Full text"}`,
	}

	events := parseLines(lines)

	var contentText string
	for _, ev := range events {
		if ev.Type == "content" {
			contentText += ev.Content
		}
	}
	if contentText != "Partial" {
		t.Errorf("expected only partial content 'Partial', got %q", contentText)
	}
}

func TestStreamParser_ToolUseStartStop(t *testing.T) {
	// Test full tool_use lifecycle: content_block_start -> input_json_delta -> content_block_stop
	// Uses "partial_json" field as actual Codebuddy/Claude CLI does
	lines := []string{
		`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_123","name":"Read"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"file_"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"path\":\"/src/main.go\"}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":1}}`,
	}

	events := parseLines(lines)

	// Should get 2 tool_use events: start and stop
	// Note: both events share the same *ToolCall pointer, so after stop
	// sets Done=true, the start event's Tool also appears Done=true.
	if len(events) != 2 {
		t.Fatalf("expected 2 tool_use events, got %d", len(events))
	}

	// Both events are tool_use type
	if events[0].Type != "tool_use" || events[1].Type != "tool_use" {
		t.Errorf("expected both events to be tool_use, got %q and %q", events[0].Type, events[1].Type)
	}

	// The tool name and ID are set from content_block_start
	if events[0].Tool == nil {
		t.Fatal("expected Tool to be non-nil")
	}
	if events[0].Tool.Name != "Read" {
		t.Errorf("expected tool name 'Read', got %q", events[0].Tool.Name)
	}
	if events[0].Tool.ID != "toolu_123" {
		t.Errorf("expected tool ID 'toolu_123', got %q", events[0].Tool.ID)
	}

	// The final event (stop) should have the accumulated input and Done=true
	if !events[1].Tool.Done {
		t.Error("expected stop event Tool.Done to be true")
	}
	if events[1].Tool.Input != `{"file_path":"/src/main.go"}` {
		t.Errorf("expected accumulated input '{\"file_path\":\"/src/main.go\"}', got %q", events[1].Tool.Input)
	}
}

func TestStreamParser_InputJsonDeltaNoCurrentTool(t *testing.T) {
	// input_json_delta without a currentTool should be silently ignored
	line := `{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"key\":\"val\"}"}}}`

	events := parseLine(line)

	if len(events) != 0 {
		t.Errorf("expected 0 events when no currentTool, got %d", len(events))
	}
}

func TestStreamParser_ConcurrentToolUse(t *testing.T) {
	// When AI invokes multiple tools concurrently, Codebuddy/Claude CLI may
	// emit content_block_start for the next tool before content_block_stop
	// for the previous one. The parser should auto-close the previous tool
	// when a new tool_use start arrives.
	lines := []string{
		// Tool A starts
		`{"type":"stream_event","event":{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_A","name":"Bash"}}}`,
		// Tool A input deltas
		`{"type":"stream_event","event":{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"comm"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"and\":\"ls\"}"}}}`,
		// Tool B starts — Tool A never got content_block_stop!
		`{"type":"stream_event","event":{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_B","name":"Read"}}}`,
		// Tool B input deltas
		`{"type":"stream_event","event":{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"file_"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"path\":\"/a.go\"}"}}}`,
		// Tool B stop
		`{"type":"stream_event","event":{"type":"content_block_stop","index":2}}`,
	}

	events := parseLines(lines)

	// Should get: tool_A_start, tool_A_auto_closed, tool_B_start, tool_B_stop
	toolEvents := make([]StreamEvent, 0)
	for _, e := range events {
		if e.Type == "tool_use" {
			toolEvents = append(toolEvents, e)
		}
	}

	if len(toolEvents) != 4 {
		t.Fatalf("expected 4 tool_use events (A start, A auto-close, B start, B stop), got %d", len(toolEvents))
	}

	// Event 0: Tool A start (done=false)
	if toolEvents[0].Tool.ID != "toolu_A" {
		t.Errorf("event 0: expected tool ID 'toolu_A', got %q", toolEvents[0].Tool.ID)
	}
	if toolEvents[0].Tool.Done {
		t.Error("event 0: expected Done=false for tool A start")
	}

	// Event 1: Tool A auto-closed (done=true, input accumulated)
	if toolEvents[1].Tool.ID != "toolu_A" {
		t.Errorf("event 1: expected tool ID 'toolu_A', got %q", toolEvents[1].Tool.ID)
	}
	if !toolEvents[1].Tool.Done {
		t.Error("event 1: expected Done=true for auto-closed tool A")
	}
	if toolEvents[1].Tool.Input != `{"command":"ls"}` {
		t.Errorf("event 1: expected tool A input '{\"command\":\"ls\"}', got %q", toolEvents[1].Tool.Input)
	}

	// Event 2: Tool B start (done=false, input empty)
	if toolEvents[2].Tool.ID != "toolu_B" {
		t.Errorf("event 2: expected tool ID 'toolu_B', got %q", toolEvents[2].Tool.ID)
	}
	if toolEvents[2].Tool.Done {
		t.Error("event 2: expected Done=false for tool B start")
	}

	// Event 3: Tool B stop (done=true, input accumulated)
	if toolEvents[3].Tool.ID != "toolu_B" {
		t.Errorf("event 3: expected tool ID 'toolu_B', got %q", toolEvents[3].Tool.ID)
	}
	if !toolEvents[3].Tool.Done {
		t.Error("event 3: expected Done=true for tool B stop")
	}
	if toolEvents[3].Tool.Input != `{"file_path":"/a.go"}` {
		t.Errorf("event 3: expected tool B input '{\"file_path\":\"/a.go\"}', got %q", toolEvents[3].Tool.Input)
	}
}

func TestStreamParser_MessageStartModel(t *testing.T) {
	// Model name extracted from message_start should be used in result
	lines := []string{
		`{"type":"stream_event","event":{"type":"message_start","message":{"model":"claude-3.5-sonnet"}}}`,
		`{"type":"result","session_id":"sess-1","duration_ms":1000}`,
	}

	events := parseLines(lines)

	// Find metadata event
	var metaEvent *StreamEvent
	for i := range events {
		if events[i].Type == "metadata" {
			metaEvent = &events[i]
			break
		}
	}
	if metaEvent == nil {
		t.Fatal("expected a metadata event")
		return
	}
	if metaEvent.Meta.Model != "claude-3.5-sonnet" {
		t.Errorf("expected model 'claude-3.5-sonnet', got %q", metaEvent.Meta.Model)
	}
}

func TestStreamParser_MessageStartOverridesProviderData(t *testing.T) {
	// Model from message_start should take priority over providerData
	lines := []string{
		`{"type":"stream_event","event":{"type":"message_start","message":{"model":"model-from-start"}}}`,
		`{"type":"result","session_id":"s1","providerData":{"model":"model-from-provider"}}`,
	}

	events := parseLines(lines)

	var metaEvent *StreamEvent
	for i := range events {
		if events[i].Type == "metadata" {
			metaEvent = &events[i]
			break
		}
	}
	if metaEvent == nil {
		t.Fatal("expected a metadata event")
		return
	}
	if metaEvent.Meta.Model != "model-from-start" {
		t.Errorf("expected model from message_start, got %q", metaEvent.Meta.Model)
	}
}

func TestStreamParser_FileHistorySnapshotSkipped(t *testing.T) {
	line := `{"type":"file-history-snapshot","files":["a.go","b.go"]}`
	events := parseLine(line)

	if len(events) != 0 {
		t.Errorf("expected 0 events for file-history-snapshot, got %d", len(events))
	}
}

func TestStreamParser_UnparseableLine(t *testing.T) {
	events := parseLine("not json at all")
	if len(events) != 0 {
		t.Errorf("expected 0 events for unparseable line, got %d", len(events))
	}
}

func TestStreamParser_EmptyTextDelta(t *testing.T) {
	// Empty text in text_delta should be silently ignored
	line := `{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":""}}}`
	events := parseLine(line)

	if len(events) != 0 {
		t.Errorf("expected 0 events for empty text_delta, got %d", len(events))
	}
}

func TestStreamParser_EmptyThinkingDelta(t *testing.T) {
	// Empty thinking in thinking_delta should be silently ignored
	line := `{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":""}}}`
	events := parseLine(line)

	if len(events) != 0 {
		t.Errorf("expected 0 events for empty thinking_delta, got %d", len(events))
	}
}

func TestStreamParser_StreamEventWithNilEvent(t *testing.T) {
	// stream_event with nil Event field should be silently ignored
	line := `{"type":"stream_event"}`
	events := parseLine(line)

	if len(events) != 0 {
		t.Errorf("expected 0 events for stream_event with nil event, got %d", len(events))
	}
}

func TestStreamParser_StreamEventWithNilDelta(t *testing.T) {
	// content_block_delta with nil Delta should be silently ignored
	line := `{"type":"stream_event","event":{"type":"content_block_delta","index":0}}`
	events := parseLine(line)

	if len(events) != 0 {
		t.Errorf("expected 0 events for delta with nil Delta, got %d", len(events))
	}
}

func TestStreamParser_ContentBlockStartNonToolUse(t *testing.T) {
	// content_block_start for "text" type should be silently ignored
	line := `{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text"}}}`
	events := parseLine(line)

	if len(events) != 0 {
		t.Errorf("expected 0 events for text content_block_start, got %d", len(events))
	}
}

func TestStreamParser_FullCodebuddyFlow(t *testing.T) {
	// Simulate a complete Codebuddy streaming session with thinking + tool_use + text
	lines := []string{
		`{"type":"stream_event","event":{"type":"message_start","message":{"model":"glm-4"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me read the file..."}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":0}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_001","name":"Read"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","text":"{\"file_path\":\"config.yaml\"}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":1}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":2,"content_block":{"type":"text"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"The port is "}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"20000."}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":2}}`,
		`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"Let me read the file..."},{"type":"tool_use","id":"toolu_001","name":"Read","input":{"file_path":"config.yaml"}},{"type":"text","text":"The port is 20000."}]}}`,
		`{"type":"result","session_id":"sess-1","duration_ms":3000,"total_cost_usd":0.01,"providerData":{"model":"glm-4","usage":{"inputTokens":50,"outputTokens":100}}}`,
	}

	events := parseLines(lines)

	// Expected events:
	// 1. thinking (from thinking_delta)
	// 2. tool_use start (from content_block_start)
	// 3. tool_use stop (from content_block_stop, with accumulated input)
	// 4. content "The port is " (from text_delta)
	// 5. content "20000." (from text_delta)
	// 6. metadata
	// 7. done
	//
	// Note: assistant message thinking is skipped (receivedPartialThinking)
	// Note: assistant message text is skipped (receivedPartial)
	// Note: assistant message tool_use is also skipped (receivedPartialToolUse) — avoids duplicate
	//       since content_block_start/stop already emitted the complete tool_use

	var thinkingCount, contentCount, toolUseCount, metadataCount, doneCount int
	for _, ev := range events {
		switch ev.Type {
		case "thinking":
			thinkingCount++
		case "content":
			contentCount++
		case "tool_use":
			toolUseCount++
		case "metadata":
			metadataCount++
		case "done":
			doneCount++
		}
	}

	if thinkingCount != 1 {
		t.Errorf("expected 1 thinking event, got %d", thinkingCount)
	}
	if contentCount != 2 {
		t.Errorf("expected 2 content events, got %d", contentCount)
	}
	// 2 from stream (start+stop) + 1 supplement from assistant (input_json_delta
	// used wrong "text" field instead of "partial_json", so Input was empty and
	// the assistant verbose message provides the full input)
	if toolUseCount != 3 {
		t.Errorf("expected 3 tool_use events, got %d", toolUseCount)
	}
	if metadataCount != 1 {
		t.Errorf("expected 1 metadata event, got %d", metadataCount)
	}
	if doneCount != 1 {
		t.Errorf("expected 1 done event, got %d", doneCount)
	}

	// Verify model from message_start
	var metaEvent *StreamEvent
	for i := range events {
		if events[i].Type == "metadata" {
			metaEvent = &events[i]
			break
		}
	}
	if metaEvent == nil {
		t.Fatal("expected a metadata event")
		return
	}
	if metaEvent.Meta.Model != "glm-4" {
		t.Errorf("expected model 'glm-4', got %v", metaEvent)
	}
}

func TestStreamParser_FullClaudeFlow(t *testing.T) {
	// Simulate a complete Claude streaming session (no --include-partial-messages)
	// Claude uses --verbose which outputs full assistant messages
	msg1 := ClaudeStreamMessage{
		Type: "assistant",
		Message: &ClaudeStreamMessageBody{
			Content: []ClaudeContentBlock{
				{Type: "thinking", Thinking: "Analyzing the code..."},
				{Type: "tool_use", ID: "toolu_abc", Name: "Bash", Input: json.RawMessage(`{"command":"ls -la"}`)},
			},
		},
	}
	msg2 := ClaudeStreamMessage{
		Type: "assistant",
		Message: &ClaudeStreamMessageBody{
			Content: []ClaudeContentBlock{
				{Type: "tool_use", ID: "toolu_abc", Name: "Bash", Input: json.RawMessage(`{"command":"ls -la"}`)},
				{Type: "text", Text: "Here are the files."},
			},
		},
	}
	msg3 := ClaudeStreamMessage{
		Type:       "result",
		SessionID:  "sess-claude",
		DurationMs: 5000,
		Usage:      &ClaudeStreamUsage{InputTokens: 200, OutputTokens: 150},
		ModelUsage: map[string]ClaudeStreamModelUsage{
			"claude-3.5-sonnet": {InputTokens: 200, OutputTokens: 150},
		},
	}

	lines := make([]string, 0, 3)
	for _, msg := range []ClaudeStreamMessage{msg1, msg2, msg3} {
		data, _ := json.Marshal(msg)
		lines = append(lines, string(data))
	}

	events := parseLines(lines)

	// Expected:
	// From msg1: thinking + tool_use (Bash, done=true)
	// From msg2: tool_use (Bash, done=true) + content
	// From msg3: metadata + done
	var thinkingCount, contentCount, toolUseCount, metadataCount, doneCount int
	for _, ev := range events {
		switch ev.Type {
		case "thinking":
			thinkingCount++
		case "content":
			contentCount++
		case "tool_use":
			toolUseCount++
		case "metadata":
			metadataCount++
		case "done":
			doneCount++
		}
	}

	if thinkingCount != 1 {
		t.Errorf("expected 1 thinking event, got %d", thinkingCount)
	}
	if contentCount != 1 {
		t.Errorf("expected 1 content event, got %d", contentCount)
	}
	if toolUseCount != 2 {
		t.Errorf("expected 2 tool_use events, got %d", toolUseCount)
	}
	if metadataCount != 1 {
		t.Errorf("expected 1 metadata event, got %d", metadataCount)
	}
	if doneCount != 1 {
		t.Errorf("expected 1 done event, got %d", doneCount)
	}

	// Verify model from ModelUsage
	var metaEvent *StreamEvent
	for i := range events {
		if events[i].Type == "metadata" {
			metaEvent = &events[i]
			break
		}
	}
	if metaEvent == nil {
		t.Fatal("expected a metadata event")
		return
	}
	if metaEvent.Meta.Model != "claude-3.5-sonnet" {
		t.Errorf("expected model 'claude-3.5-sonnet', got %v", metaEvent)
	}
}

func TestStreamParser_ToolUseFromAssistantWithInput(t *testing.T) {
	// Tool use in assistant message should carry the full input JSON
	msg := ClaudeStreamMessage{
		Type: "assistant",
		Message: &ClaudeStreamMessageBody{
			Content: []ClaudeContentBlock{
				{Type: "tool_use", ID: "toolu_xyz", Name: "Edit", Input: json.RawMessage(`{"file_path":"/app/main.go","old_string":"hello","new_string":"world"}`)},
			},
		},
	}
	data, _ := json.Marshal(msg)

	events := parseLine(string(data))

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "tool_use" {
		t.Fatalf("expected tool_use event, got %q", events[0].Type)
	}
	if events[0].Tool.Name != "Edit" {
		t.Errorf("expected tool name 'Edit', got %q", events[0].Tool.Name)
	}
	if events[0].Tool.ID != "toolu_xyz" {
		t.Errorf("expected tool ID 'toolu_xyz', got %q", events[0].Tool.ID)
	}
	if !events[0].Tool.Done {
		t.Error("expected tool to be done (from complete assistant message)")
	}
	// Input should be the raw JSON string
	expectedInput := `{"file_path":"/app/main.go","old_string":"hello","new_string":"world"}`
	if events[0].Tool.Input != expectedInput {
		t.Errorf("expected input %q, got %q", expectedInput, events[0].Tool.Input)
	}
}

func TestStreamParser_ToolUseInputInContentBlockStart(t *testing.T) {
	// Some CLIs (e.g., Claude CLI with certain models) include tool input
	// directly in the content_block_start event instead of sending separate
	// input_json_delta events. The parser should capture this input.
	lines := []string{
		// tool_use with input embedded in content_block_start — no input_json_delta
		`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_function_abc123","name":"Bash","input":{"command":"ls /workspace/","description":"List workspace"}}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":1}}`,
	}

	events := parseLines(lines)

	// Should get 2 tool_use events: start (done=false) and stop (done=true)
	toolEvents := 0
	for _, e := range events {
		if e.Type == "tool_use" {
			toolEvents++
		}
	}
	if toolEvents != 2 {
		t.Fatalf("expected 2 tool_use events, got %d", toolEvents)
	}

	// The stop event should have the full input from content_block_start
	var stopEvent *StreamEvent
	for i := range events {
		if events[i].Type == "tool_use" && events[i].Tool != nil && events[i].Tool.Done {
			stopEvent = &events[i]
			break
		}
	}
	if stopEvent == nil {
		t.Fatal("expected a tool_use stop event with Done=true")
		return
	}
	expectedInput := `{"command":"ls /workspace/","description":"List workspace"}`
	if stopEvent.Tool.Input != expectedInput {
		t.Errorf("expected stop event input %q, got %q", expectedInput, stopEvent.Tool.Input)
	}
}

func TestStreamParser_ToolUseInputInContentBlockStartWithDelta(t *testing.T) {
	// When content_block_start includes input AND input_json_delta is also sent,
	// the deltas should append to the start input (though this is unusual).
	lines := []string{
		// content_block_start with partial input
		`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_function_xyz","name":"Read","input":{"file_path":"/src/main.go"}}}}`,
		// input_json_delta appends more data
		`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":",\"limit\":100}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":1}}`,
	}

	events := parseLines(lines)

	// Find the stop event
	var stopEvent *StreamEvent
	for i := range events {
		if events[i].Type == "tool_use" && events[i].Tool != nil && events[i].Tool.Done {
			stopEvent = &events[i]
			break
		}
	}
	if stopEvent == nil {
		t.Fatal("expected a tool_use stop event with Done=true")
		return
	}
	// Input should include both the start input and the delta
	if stopEvent.Tool.Input == "" {
		t.Error("expected stop event to have accumulated input, but Input is empty")
	}
	// The input should contain the file_path from content_block_start
	if !strings.Contains(stopEvent.Tool.Input, "file_path") {
		t.Errorf("expected input to contain 'file_path', got %q", stopEvent.Tool.Input)
	}
}

func TestStreamParser_EmptyInputInStartDoesNotCorruptDelta(t *testing.T) {
	// When content_block_start has input:{} (placeholder) and input_json_delta
	// follows, the empty {} must NOT be set on currentTool.Input — otherwise
	// appending deltas would produce "{}{...}" which is invalid JSON.
	lines := []string{
		`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_999","name":"Bash","input":{}}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"command\""}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":":\"ls\"}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":1}}`,
	}

	events := parseLines(lines)

	// Find the stop event
	var stopEvent *StreamEvent
	for i := range events {
		if events[i].Type == "tool_use" && events[i].Tool != nil && events[i].Tool.Done {
			stopEvent = &events[i]
			break
		}
	}
	if stopEvent == nil {
		t.Fatal("expected a tool_use stop event with Done=true")
		return
	}

	// Input should be valid JSON accumulated from deltas only (NOT "{}{...}")
	expectedInput := `{"command":"ls"}`
	if stopEvent.Tool.Input != expectedInput {
		t.Errorf("expected input %q, got %q", expectedInput, stopEvent.Tool.Input)
	}

	// Verify it's valid JSON
	var parsed map[string]any
	if err := json.Unmarshal([]byte(stopEvent.Tool.Input), &parsed); err != nil {
		t.Errorf("input is not valid JSON: %v, raw=%q", err, stopEvent.Tool.Input)
	}
}

func TestStreamParser_InterleavedToolUse(t *testing.T) {
	// When AI uses the Agent tool to launch parallel sub-agents, the CLI's
	// --include-partial-messages output interleaves content_block_start/delta/stop
	// events for different tools at different indices. Each tool has its own
	// content block index. The parser must correctly route input_json_delta
	// events to the right tool and close the right tool on content_block_stop.
	lines := []string{
		// Tool A (Read) starts at index 1
		`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_A","name":"Read"}}}`,
		// Tool B (Bash) starts at index 2 (interleaved — A hasn't stopped yet)
		`{"type":"stream_event","event":{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_B","name":"Bash"}}}`,
		// Tool C (Read) starts at index 3 (also interleaved)
		`{"type":"stream_event","event":{"type":"content_block_start","index":3,"content_block":{"type":"tool_use","id":"toolu_C","name":"Read"}}}`,
		// Deltas for tool A
		`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"file_"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"path\":\"/a.go\"}"}}}`,
		// Deltas for tool B
		`{"type":"stream_event","event":{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"comm"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"and\":\"ls\"}"}}}`,
		// Deltas for tool C
		`{"type":"stream_event","event":{"type":"content_block_delta","index":3,"delta":{"type":"input_json_delta","partial_json":"{\"file_"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":3,"delta":{"type":"input_json_delta","partial_json":"path\":\"/b.go\"}"}}}`,
		// Stops arrive in different order: C first, then A, then B
		`{"type":"stream_event","event":{"type":"content_block_stop","index":3}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":1}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":2}}`,
	}

	events := parseLines(lines)

	// Should get 6 tool_use events: 3 starts + 3 stops
	toolEvents := make([]StreamEvent, 0)
	for _, e := range events {
		if e.Type == "tool_use" {
			toolEvents = append(toolEvents, e)
		}
	}

	if len(toolEvents) != 6 {
		t.Fatalf("expected 6 tool_use events (3 starts + 3 stops), got %d", len(toolEvents))
	}

	// Verify start events (done=false)
	starts := []struct {
		idx      int
		id       string
		name     string
		wantDone bool
	}{
		{0, "toolu_A", "Read", false},
		{1, "toolu_B", "Bash", false},
		{2, "toolu_C", "Read", false},
	}
	for _, s := range starts {
		if toolEvents[s.idx].Tool.ID != s.id {
			t.Errorf("start event %d: expected ID %q, got %q", s.idx, s.id, toolEvents[s.idx].Tool.ID)
		}
		if toolEvents[s.idx].Tool.Name != s.name {
			t.Errorf("start event %d: expected Name %q, got %q", s.idx, s.name, toolEvents[s.idx].Tool.Name)
		}
		if toolEvents[s.idx].Tool.Done != s.wantDone {
			t.Errorf("start event %d: expected Done=%v, got %v", s.idx, s.wantDone, toolEvents[s.idx].Tool.Done)
		}
	}

	// Verify stop events (done=true, input accumulated correctly)
	// Stop order: C (index 3), A (index 1), B (index 2) → events 3, 4, 5
	stops := []struct {
		idx   int
		id    string
		input string
	}{
		{3, "toolu_C", `{"file_path":"/b.go"}`},
		{4, "toolu_A", `{"file_path":"/a.go"}`},
		{5, "toolu_B", `{"command":"ls"}`},
	}
	for _, s := range stops {
		if toolEvents[s.idx].Tool.ID != s.id {
			t.Errorf("stop event %d: expected ID %q, got %q", s.idx, s.id, toolEvents[s.idx].Tool.ID)
		}
		if !toolEvents[s.idx].Tool.Done {
			t.Errorf("stop event %d: expected Done=true", s.idx)
		}
		if toolEvents[s.idx].Tool.Input != s.input {
			t.Errorf("stop event %d: expected input %q, got %q", s.idx, s.input, toolEvents[s.idx].Tool.Input)
		}
	}
}

func TestStreamParser_InterleavedToolUseNoInputLoss(t *testing.T) {
	// Regression test for the bug where parallel sub-agent tool calls caused
	// empty input tool_use blocks. In the old single-currentTool implementation,
	// content_block_start for a new tool would prematurely close the previous
	// tool (with empty input if deltas hadn't arrived yet), and deltas for the
	// closed tool would be silently dropped or misrouted.
	//
	// With the activeTools map keyed by index, each tool independently tracks
	// its input regardless of interleaving.
	lines := []string{
		// 5 tools start in rapid succession (simulating parallel Agent sub-agents)
		`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"Read"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_2","name":"Read"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":3,"content_block":{"type":"tool_use","id":"toolu_3","name":"Bash"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":4,"content_block":{"type":"tool_use","id":"toolu_4","name":"Read"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":5,"content_block":{"type":"tool_use","id":"toolu_5","name":"Bash"}}}`,
		// All deltas arrive after all starts (worst case for old implementation)
		`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"file_path\":\"/1.go\"}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"file_path\":\"/2.go\"}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":3,"delta":{"type":"input_json_delta","partial_json":"{\"command\":\"ls\"}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":4,"delta":{"type":"input_json_delta","partial_json":"{\"file_path\":\"/4.go\"}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":5,"delta":{"type":"input_json_delta","partial_json":"{\"command\":\"pwd\"}"}}}`,
		// All stops
		`{"type":"stream_event","event":{"type":"content_block_stop","index":1}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":2}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":3}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":4}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":5}}`,
	}

	events := parseLines(lines)

	// Collect stop events (done=true) and verify NONE have empty input
	emptyStops := 0
	for _, e := range events {
		if e.Type == "tool_use" && e.Tool.Done {
			if e.Tool.Input == "" || e.Tool.Input == "{}" {
				emptyStops++
				t.Errorf("tool %s (%s) has empty input on stop — input was lost!", e.Tool.ID, e.Tool.Name)
			}
		}
	}

	if emptyStops > 0 {
		t.Fatalf("found %d tool stop events with empty input (input loss bug)", emptyStops)
	}

	// Verify all 5 tools have correct input
	expectedInputs := map[string]string{
		"toolu_1": `{"file_path":"/1.go"}`,
		"toolu_2": `{"file_path":"/2.go"}`,
		"toolu_3": `{"command":"ls"}`,
		"toolu_4": `{"file_path":"/4.go"}`,
		"toolu_5": `{"command":"pwd"}`,
	}
	for _, e := range events {
		if e.Type == "tool_use" && e.Tool.Done {
			if expected, ok := expectedInputs[e.Tool.ID]; ok {
				if e.Tool.Input != expected {
					t.Errorf("tool %s: expected input %q, got %q", e.Tool.ID, expected, e.Tool.Input)
				}
			}
		}
	}
}

// --- tool_result content block tests ---

func TestStreamParser_ToolResultContentBlockStreaming(t *testing.T) {
	// Simulate a tool_result content block via stream_event:
	// content_block_start → text_delta → content_block_stop
	lines := []string{
		// First: a tool_use block completes
		`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_read1","name":"Read"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"file_path\":\"/a.go\"}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":1}}`,
		// Then: tool_result content block
		`{"type":"stream_event","event":{"type":"content_block_start","index":2,"content_block":{"type":"tool_result","tool_use_id":"toolu_read1"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"package main\n"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"func main() {}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":2}}`,
	}

	events := parseLines(lines)

	// Expected: tool_use start, tool_use stop, tool_result
	var toolResultEvents []StreamEvent
	for _, e := range events {
		if e.Type == "tool_result" {
			toolResultEvents = append(toolResultEvents, e)
		}
	}

	if len(toolResultEvents) != 1 {
		t.Fatalf("expected 1 tool_result event, got %d", len(toolResultEvents))
	}

	// Verify the tool_result references the correct tool_use ID
	if toolResultEvents[0].Tool.ID != "toolu_read1" {
		t.Errorf("expected tool_result ID 'toolu_read1', got %q", toolResultEvents[0].Tool.ID)
	}
	// Verify accumulated output
	if toolResultEvents[0].Tool.Output != "package main\nfunc main() {}" {
		t.Errorf("expected accumulated output, got %q", toolResultEvents[0].Tool.Output)
	}
	// Default status should be "success" (not error)
	if toolResultEvents[0].Tool.Status != "success" {
		t.Errorf("expected status 'success', got %q", toolResultEvents[0].Tool.Status)
	}
}

func TestStreamParser_ToolResultContentBlockWithError(t *testing.T) {
	// tool_result with is_error=true should produce status="error"
	lines := []string{
		`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_bash1","name":"Bash"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"command\":\"bad-cmd\"}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":1}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":2,"content_block":{"type":"tool_result","tool_use_id":"toolu_bash1","is_error":true}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"command not found"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":2}}`,
	}

	events := parseLines(lines)

	var toolResultEvents []StreamEvent
	for _, e := range events {
		if e.Type == "tool_result" {
			toolResultEvents = append(toolResultEvents, e)
		}
	}

	if len(toolResultEvents) != 1 {
		t.Fatalf("expected 1 tool_result event, got %d", len(toolResultEvents))
	}
	if toolResultEvents[0].Tool.Status != "error" {
		t.Errorf("expected status 'error', got %q", toolResultEvents[0].Tool.Status)
	}
	if toolResultEvents[0].Tool.Output != "command not found" {
		t.Errorf("expected output 'command not found', got %q", toolResultEvents[0].Tool.Output)
	}
}

func TestStreamParser_ToolResultContentBlockWithInitialContent(t *testing.T) {
	// Some CLIs may include the full output in content_block_start's Content field
	lines := []string{
		`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_r1","name":"Read"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":1}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":2,"content_block":{"type":"tool_result","tool_use_id":"toolu_r1","content":"preloaded output"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":2}}`,
	}

	events := parseLines(lines)

	var toolResultEvents []StreamEvent
	for _, e := range events {
		if e.Type == "tool_result" {
			toolResultEvents = append(toolResultEvents, e)
		}
	}

	if len(toolResultEvents) != 1 {
		t.Fatalf("expected 1 tool_result event, got %d", len(toolResultEvents))
	}
	if toolResultEvents[0].Tool.Output != "preloaded output" {
		t.Errorf("expected output 'preloaded output', got %q", toolResultEvents[0].Tool.Output)
	}
}

func TestStreamParser_ToolResultContentBlockWithIDFallback(t *testing.T) {
	// When tool_use_id is missing, fall back to ID field
	lines := []string{
		`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_x1","name":"Grep"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":1}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":2,"content_block":{"type":"tool_result","id":"toolu_x1","content":"match found"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":2}}`,
	}

	events := parseLines(lines)

	var toolResultEvents []StreamEvent
	for _, e := range events {
		if e.Type == "tool_result" {
			toolResultEvents = append(toolResultEvents, e)
		}
	}

	if len(toolResultEvents) != 1 {
		t.Fatalf("expected 1 tool_result event, got %d", len(toolResultEvents))
	}
	if toolResultEvents[0].Tool.ID != "toolu_x1" {
		t.Errorf("expected ID fallback 'toolu_x1', got %q", toolResultEvents[0].Tool.ID)
	}
}

func TestStreamParser_ToolResultTextDeltaDoesNotLeakAsContent(t *testing.T) {
	// text_delta events within a tool_result content block should NOT
	// be emitted as content events — they should be accumulated into
	// the tool result output instead.
	lines := []string{
		`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_leak1","name":"Read"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":1}}`,
		// tool_result content block — text_delta here must NOT become content
		`{"type":"stream_event","event":{"type":"content_block_start","index":2,"content_block":{"type":"tool_result","tool_use_id":"toolu_leak1"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"this is tool output, not assistant content"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":2}}`,
		// After tool_result, a text block starts — this SHOULD become content
		`{"type":"stream_event","event":{"type":"content_block_start","index":3,"content_block":{"type":"text"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":3,"delta":{"type":"text_delta","text":"The file contains..."}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":3}}`,
	}

	events := parseLines(lines)

	// Count content events — only the post-tool-result text should be content
	var contentEvents []StreamEvent
	for _, e := range events {
		if e.Type == "content" {
			contentEvents = append(contentEvents, e)
		}
	}

	if len(contentEvents) != 1 {
		t.Fatalf("expected 1 content event (post-tool-result text), got %d", len(contentEvents))
	}
	if contentEvents[0].Content != "The file contains..." {
		t.Errorf("expected content 'The file contains...', got %q", contentEvents[0].Content)
	}

	// Verify the tool_result was properly captured
	var toolResultEvents []StreamEvent
	for _, e := range events {
		if e.Type == "tool_result" {
			toolResultEvents = append(toolResultEvents, e)
		}
	}
	if len(toolResultEvents) != 1 {
		t.Fatalf("expected 1 tool_result event, got %d", len(toolResultEvents))
	}
	if toolResultEvents[0].Tool.Output != "this is tool output, not assistant content" {
		t.Errorf("tool output should contain the leaked text, got %q", toolResultEvents[0].Tool.Output)
	}
}

func TestStreamParser_ToolResultInAssistantVerbose(t *testing.T) {
	// Tool result in assistant verbose format (non-streaming)
	msg := ClaudeStreamMessage{
		Type: "assistant",
		Message: &ClaudeStreamMessageBody{
			Content: []ClaudeContentBlock{
				{Type: "tool_use", ID: "toolu_v1", Name: "Read", Input: json.RawMessage(`{"file_path":"/a.go"}`)},
				{Type: "tool_result", ID: "toolu_v1", Content: json.RawMessage(`"file contents"`), IsError: false},
				{Type: "text", Text: "Here is the file."},
			},
		},
	}
	data, _ := json.Marshal(msg)

	events := parseLine(string(data))

	// Expected: tool_use, tool_result, content
	var toolResultEvents []StreamEvent
	for _, e := range events {
		if e.Type == "tool_result" {
			toolResultEvents = append(toolResultEvents, e)
		}
	}
	if len(toolResultEvents) != 1 {
		t.Fatalf("expected 1 tool_result event, got %d", len(toolResultEvents))
	}
	if toolResultEvents[0].Tool.ID != "toolu_v1" {
		t.Errorf("expected tool_result ID 'toolu_v1', got %q", toolResultEvents[0].Tool.ID)
	}
	if toolResultEvents[0].Tool.Output != "file contents" {
		t.Errorf("expected output 'file contents', got %q", toolResultEvents[0].Tool.Output)
	}
	if toolResultEvents[0].Tool.Status != "success" {
		t.Errorf("expected status 'success', got %q", toolResultEvents[0].Tool.Status)
	}
}

func TestStreamParser_ToolResultInAssistantVerboseError(t *testing.T) {
	msg := ClaudeStreamMessage{
		Type: "assistant",
		Message: &ClaudeStreamMessageBody{
			Content: []ClaudeContentBlock{
				{Type: "tool_result", ID: "toolu_err1", Content: json.RawMessage(`"permission denied"`), IsError: true},
			},
		},
	}
	data, _ := json.Marshal(msg)

	events := parseLine(string(data))

	var toolResultEvents []StreamEvent
	for _, e := range events {
		if e.Type == "tool_result" {
			toolResultEvents = append(toolResultEvents, e)
		}
	}
	if len(toolResultEvents) != 1 {
		t.Fatalf("expected 1 tool_result event, got %d", len(toolResultEvents))
	}
	if toolResultEvents[0].Tool.Status != "error" {
		t.Errorf("expected status 'error', got %q", toolResultEvents[0].Tool.Status)
	}
}

func TestStreamParser_ToolResultInAssistantVerboseTextFallback(t *testing.T) {
	// When Content is empty but Text is present, use Text as output
	msg := ClaudeStreamMessage{
		Type: "assistant",
		Message: &ClaudeStreamMessageBody{
			Content: []ClaudeContentBlock{
				{Type: "tool_result", ID: "toolu_tf1", Text: "text fallback output"},
			},
		},
	}
	data, _ := json.Marshal(msg)

	events := parseLine(string(data))

	var toolResultEvents []StreamEvent
	for _, e := range events {
		if e.Type == "tool_result" {
			toolResultEvents = append(toolResultEvents, e)
		}
	}
	if len(toolResultEvents) != 1 {
		t.Fatalf("expected 1 tool_result event, got %d", len(toolResultEvents))
	}
	if toolResultEvents[0].Tool.Output != "text fallback output" {
		t.Errorf("expected output from Text field, got %q", toolResultEvents[0].Tool.Output)
	}
}

func TestStreamParser_EmptyInputJsonDeltaSupplementedByAssistant(t *testing.T) {
	// Regression test: some CLIs/models send input_json_delta with empty partial_json,
	// resulting in tool_use blocks with no Input. The assistant verbose message
	// contains the full Input — we must supplement it, not skip it.
	lines := []string{
		`{"type":"stream_event","event":{"type":"message_start","message":{"model":"glm-4"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me check..."}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":0}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"bf6w0tee","name":"Bash"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":""}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":2}}`,
		// Assistant verbose message with full tool_use input
		`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"Let me check..."},{"type":"tool_use","id":"bf6w0tee","name":"Bash","input":{"command":"ls -la"}},{"type":"text","text":"Done."}]}}`,
		`{"type":"result","session_id":"sess-1","duration_ms":3000}`,
	}

	events := parseLines(lines)

	// Collect all tool_use events
	var toolUseEvents []StreamEvent
	for _, ev := range events {
		if ev.Type == "tool_use" {
			toolUseEvents = append(toolUseEvents, ev)
		}
	}

	// Expect 3 tool_use events:
	// 1. content_block_start (empty Input, Done=false)
	// 2. content_block_stop (empty Input, Done=true)
	// 3. supplement from assistant (full Input, Done=true)
	if len(toolUseEvents) != 3 {
		t.Fatalf("expected 3 tool_use events, got %d", len(toolUseEvents))
	}

	// First event: empty input from content_block_start
	if toolUseEvents[0].Tool.Input != "" {
		t.Errorf("expected empty Input in first event, got %q", toolUseEvents[0].Tool.Input)
	}
	if toolUseEvents[0].Tool.Done {
		t.Error("expected Done=false in first event")
	}

	// Second event: still empty from content_block_stop
	if toolUseEvents[1].Tool.Input != "" {
		t.Errorf("expected empty Input in second event, got %q", toolUseEvents[1].Tool.Input)
	}
	if !toolUseEvents[1].Tool.Done {
		t.Error("expected Done=true in second event")
	}

	// Third event: supplemented from assistant verbose message
	if toolUseEvents[2].Tool.Input == "" {
		t.Error("expected non-empty Input in supplement event")
	}
	if !toolUseEvents[2].Tool.Done {
		t.Error("expected Done=true in supplement event")
	}
	// Verify the supplemented input contains the actual data
	var input map[string]any
	if err := json.Unmarshal([]byte(toolUseEvents[2].Tool.Input), &input); err != nil {
		t.Fatalf("failed to parse supplement input JSON: %v", err)
	}
	if cmd, ok := input["command"].(string); !ok || cmd != "ls -la" {
		t.Errorf("expected command='ls -la', got %v", input["command"])
	}
}

func TestStreamParser_EmptyInputNotSupplementedWhenAssistantAlsoEmpty(t *testing.T) {
	// When both stream_event and assistant have empty input, no supplement event should be emitted.
	lines := []string{
		`{"type":"stream_event","event":{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_empty","name":"Bash"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":""}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":2}}`,
		// Assistant with empty input too
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_empty","name":"Bash","input":{}}]}}`,
	}

	events := parseLines(lines)

	var toolUseCount int
	for _, ev := range events {
		if ev.Type == "tool_use" {
			toolUseCount++
		}
	}

	// Only 2 from stream (start+stop), no supplement since assistant input is also empty
	if toolUseCount != 2 {
		t.Errorf("expected 2 tool_use events (no supplement), got %d", toolUseCount)
	}
}

func TestStreamParser_ToolResultInUserMessage(t *testing.T) {
	// Regression test: Claude/Codebuddy CLI puts tool_result in user messages
	// (because tool results are returned to the model as "user" role).
	// The parser must extract these so tool_use blocks get their output/status.
	lines := []string{
		`{"type":"stream_event","event":{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_001","name":"Read"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"file_path\":\"/a.go\"}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":2}}`,
		// User message containing tool_result
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_001","content":[{"type":"text","text":"file contents here"}]}]}}`,
		`{"type":"result","session_id":"sess-1"}`,
	}

	events := parseLines(lines)

	var toolResultEvents []StreamEvent
	for _, ev := range events {
		if ev.Type == "tool_result" {
			toolResultEvents = append(toolResultEvents, ev)
		}
	}

	if len(toolResultEvents) != 1 {
		t.Fatalf("expected 1 tool_result event from user message, got %d", len(toolResultEvents))
	}
	if toolResultEvents[0].Tool.ID != "toolu_001" {
		t.Errorf("expected tool_use_id 'toolu_001', got %q", toolResultEvents[0].Tool.ID)
	}
	if toolResultEvents[0].Tool.Output != "file contents here" {
		t.Errorf("expected output 'file contents here', got %q", toolResultEvents[0].Tool.Output)
	}
	if toolResultEvents[0].Tool.Status != "success" {
		t.Errorf("expected status 'success', got %q", toolResultEvents[0].Tool.Status)
	}
}

func TestStreamParser_ToolResultInUserMessageError(t *testing.T) {
	// Error tool_result in user message
	lines := []string{
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_err","is_error":true,"content":"command failed"}]}}`,
	}

	events := parseLines(lines)

	var toolResultEvents []StreamEvent
	for _, ev := range events {
		if ev.Type == "tool_result" {
			toolResultEvents = append(toolResultEvents, ev)
		}
	}

	if len(toolResultEvents) != 1 {
		t.Fatalf("expected 1 tool_result event, got %d", len(toolResultEvents))
	}
	if toolResultEvents[0].Tool.Status != "error" {
		t.Errorf("expected status 'error', got %q", toolResultEvents[0].Tool.Status)
	}
	if toolResultEvents[0].Tool.Output != "command failed" {
		t.Errorf("expected output 'command failed', got %q", toolResultEvents[0].Tool.Output)
	}
}

func TestStreamParser_FullFlowWithEmptyInputJsonDeltaAndUserToolResult(t *testing.T) {
	// End-to-end test simulating the exact scenario that caused the bug:
	// 1. stream_event sends tool_use with empty input_json_delta
	// 2. user message contains tool_result with actual output
	// 3. assistant verbose message contains full tool_use input
	// All three pieces must be correctly assembled.
	lines := []string{
		`{"type":"stream_event","event":{"type":"message_start","message":{"model":"glm-4"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"bf6w0tee","name":"Bash"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":""}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":2}}`,
		// User message with tool_result (CLI returns tool output as user role)
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"bf6w0tee","content":[{"type":"text","text":"total 0\ndrwxr-xr-x  5 user  staff  160 May 12 10:00 ."}]}]}}`,
		// Assistant verbose message with full tool_use input
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"bf6w0tee","name":"Bash","input":{"command":"ls -la"}},{"type":"text","text":"Here are the files."}]}}`,
		`{"type":"result","session_id":"sess-1","duration_ms":3000}`,
	}

	events := parseLines(lines)

	var toolUseEvents []StreamEvent
	var toolResultEvents []StreamEvent
	for _, ev := range events {
		if ev.Type == "tool_use" {
			toolUseEvents = append(toolUseEvents, ev)
		}
		if ev.Type == "tool_result" {
			toolResultEvents = append(toolResultEvents, ev)
		}
	}

	// Expect 3 tool_use events: start (empty), stop (empty), supplement (full input)
	if len(toolUseEvents) != 3 {
		t.Fatalf("expected 3 tool_use events, got %d", len(toolUseEvents))
	}
	// The supplemented event should have the full input
	var input map[string]any
	if err := json.Unmarshal([]byte(toolUseEvents[2].Tool.Input), &input); err != nil {
		t.Fatalf("failed to parse supplement input: %v", err)
	}
	if cmd, ok := input["command"].(string); !ok || cmd != "ls -la" {
		t.Errorf("expected command='ls -la', got %v", input["command"])
	}

	// Expect 1 tool_result event from user message
	if len(toolResultEvents) != 1 {
		t.Fatalf("expected 1 tool_result event, got %d", len(toolResultEvents))
	}
	if toolResultEvents[0].Tool.ID != "bf6w0tee" {
		t.Errorf("expected tool_use_id 'bf6w0tee', got %q", toolResultEvents[0].Tool.ID)
	}
	if !strings.Contains(toolResultEvents[0].Tool.Output, "drwxr-xr-x") {
		t.Errorf("expected output containing 'drwxr-xr-x', got %q", toolResultEvents[0].Tool.Output)
	}
}

// --- Issue #60: Resume dedup tests ---

func TestStreamParser_ResumeDedup_VerboseMessageAfterDeltaIsSkipped(t *testing.T) {
	// In normal streaming with --include-partial-messages, content arrives as
	// text_delta (incremental) followed by an assistant verbose message (complete).
	// The parser correctly skips the verbose message because receivedPartial=true.
	lines := []string{
		`{"type":"stream_event","event":{"type":"message_start","message":{"model":"claude-3.5-sonnet"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"I need to plan..."}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Here is my plan."}}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_epm","name":"ExitPlanMode"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":2}}`,
		// Assistant verbose message — thinking and text should be SKIPPED
		`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"I need to plan..."},{"type":"text","text":"Here is my plan."},{"type":"tool_use","id":"toolu_epm","name":"ExitPlanMode","input":{}}]}}`,
		`{"type":"result","session_id":"sess-1","duration_ms":5000}`,
	}

	events := parseLines(lines)

	var thinkingCount, contentCount int
	for _, ev := range events {
		switch ev.Type {
		case "thinking":
			thinkingCount++
		case "content":
			contentCount++
		}
	}

	if thinkingCount != 1 {
		t.Errorf("expected 1 thinking event (from delta only), got %d", thinkingCount)
	}
	if contentCount != 1 {
		t.Errorf("expected 1 content event (from delta only), got %d", contentCount)
	}
}

func TestStreamParser_ResumeDedup_MessageStartResetsFlags(t *testing.T) {
	// When a new message_start arrives, partial flags are reset so that
	// content from the new turn is not suppressed.
	// This is the normal behavior (ISS-028).
	lines := []string{
		// Turn 1: text_delta sets receivedPartial=true
		`{"type":"stream_event","event":{"type":"message_start","message":{"model":"claude-3.5-sonnet"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Turn 1 content."}}}`,
		// Turn 2: message_start resets receivedPartial=false
		`{"type":"stream_event","event":{"type":"message_start","message":{"model":"claude-3.5-sonnet"}}}`,
		// New text_delta should be emitted (not suppressed)
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Turn 2 content."}}}`,
	}

	events := parseLines(lines)

	var contentCount int
	var contentText string
	for _, ev := range events {
		if ev.Type == "content" {
			contentCount++
			contentText += ev.Content
		}
	}

	if contentCount != 2 {
		t.Errorf("expected 2 content events (one per turn), got %d", contentCount)
	}
	if contentText != "Turn 1 content.Turn 2 content." {
		t.Errorf("expected combined content from both turns, got %q", contentText)
	}
}

func TestStreamParser_ResumeDedup_AssistantVerboseWithoutDelta(t *testing.T) {
	// When there is NO --include-partial-messages (verbose-only mode),
	// content comes from assistant messages as full text blocks.
	// Each assistant message represents a complete turn — no dedup needed.
	// This tests the scenario where message_start resets flags correctly
	// even without prior deltas.
	msg1 := ClaudeStreamMessage{
		Type: "assistant",
		Message: &ClaudeStreamMessageBody{
			Content: []ClaudeContentBlock{
				{Type: "thinking", Thinking: "First thought"},
				{Type: "text", Text: "First response."},
			},
		},
	}
	msg2 := ClaudeStreamMessage{
		Type: "assistant",
		Message: &ClaudeStreamMessageBody{
			Content: []ClaudeContentBlock{
				{Type: "text", Text: "Second response after tool."},
			},
		},
	}

	lines := make([]string, 0, 2)
	for _, msg := range []ClaudeStreamMessage{msg1, msg2} {
		data, _ := json.Marshal(msg)
		lines = append(lines, string(data))
	}

	events := parseLines(lines)

	var thinkingCount, contentCount int
	for _, ev := range events {
		switch ev.Type {
		case "thinking":
			thinkingCount++
		case "content":
			contentCount++
		}
	}

	if thinkingCount != 1 {
		t.Errorf("expected 1 thinking event, got %d", thinkingCount)
	}
	if contentCount != 2 {
		t.Errorf("expected 2 content events, got %d", contentCount)
	}
}
