package ai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- parseCodexToolStart tests ---

func TestCodexTool_ParseStart_CommandExecution(t *testing.T) {
	msg := &CodexStreamMessage{
		Type: "item.started",
		Item: &CodexItem{
			ID:      "item_1",
			Type:    "command_execution",
			Command: "bash -lc 'ls -la'",
			Status:  "in_progress",
		},
	}
	tc := parseCodexToolStart(msg)
	assert.NotNil(t, tc)
	assert.Equal(t, "Bash", tc.Name)
	assert.Equal(t, "item_1", tc.ID)
	assert.Equal(t, `{"command":"bash -lc 'ls -la'"}`, tc.Input)
	assert.False(t, tc.Done)
	assert.Empty(t, tc.Output)
	assert.Empty(t, tc.Status)
}

func TestCodexTool_ParseStart_NonCommandExecution(t *testing.T) {
	// agent_message items should return nil
	msg := &CodexStreamMessage{
		Type: "item.started",
		Item: &CodexItem{
			ID:   "item_0",
			Type: "agent_message",
			Text: "hello",
		},
	}
	tc := parseCodexToolStart(msg)
	assert.Nil(t, tc)
}

func TestCodexTool_ParseStart_NilItem(t *testing.T) {
	msg := &CodexStreamMessage{
		Type: "item.started",
		Item: nil,
	}
	tc := parseCodexToolStart(msg)
	assert.Nil(t, tc)
}

func TestCodexTool_ParseStart_EmptyCommand(t *testing.T) {
	msg := &CodexStreamMessage{
		Type: "item.started",
		Item: &CodexItem{
			ID:      "item_2",
			Type:    "command_execution",
			Command: "",
			Status:  "in_progress",
		},
	}
	tc := parseCodexToolStart(msg)
	assert.NotNil(t, tc)
	assert.Equal(t, "Bash", tc.Name)
	// execCommandJSON with empty string still produces valid JSON
	assert.Equal(t, `{"command":""}`, tc.Input)
}

// --- parseCodexToolComplete tests ---

func TestCodexTool_ParseComplete_CommandExecution(t *testing.T) {
	exitCode := 0
	msg := &CodexStreamMessage{
		Type: "item.completed",
		Item: &CodexItem{
			ID:               "item_1",
			Type:             "command_execution",
			Command:          "bash -lc 'ls -la'",
			AggregatedOutput: "file1.txt\nfile2.txt",
			ExitCode:         &exitCode,
			Status:           "completed",
		},
	}
	tc := parseCodexToolComplete(msg)
	assert.NotNil(t, tc)
	assert.Equal(t, "Bash", tc.Name)
	assert.Equal(t, "item_1", tc.ID)
	assert.Equal(t, `{"command":"bash -lc 'ls -la'"}`, tc.Input)
	assert.True(t, tc.Done)
	assert.Equal(t, "file1.txt\nfile2.txt", tc.Output)
	assert.Equal(t, "success", tc.Status)
}

func TestCodexTool_ParseComplete_FailedExitCode(t *testing.T) {
	exitCode := 1
	msg := &CodexStreamMessage{
		Type: "item.completed",
		Item: &CodexItem{
			ID:               "item_2",
			Type:             "command_execution",
			Command:          "bash -lc 'exit 1'",
			AggregatedOutput: "error: something went wrong",
			ExitCode:         &exitCode,
			Status:           "completed",
		},
	}
	tc := parseCodexToolComplete(msg)
	assert.NotNil(t, tc)
	assert.True(t, tc.Done)
	assert.Equal(t, "error", tc.Status)
	assert.Contains(t, tc.Output, "error: something went wrong")
}

func TestCodexTool_ParseComplete_NonZeroExitCode(t *testing.T) {
	exitCode := 127
	msg := &CodexStreamMessage{
		Type: "item.completed",
		Item: &CodexItem{
			ID:               "item_3",
			Type:             "command_execution",
			Command:          "nonexistent_cmd",
			AggregatedOutput: "command not found",
			ExitCode:         &exitCode,
			Status:           "completed",
		},
	}
	tc := parseCodexToolComplete(msg)
	assert.NotNil(t, tc)
	assert.Equal(t, "error", tc.Status)
}

func TestCodexTool_ParseComplete_NilExitCode(t *testing.T) {
	// Exit code may be nil when not yet available
	msg := &CodexStreamMessage{
		Type: "item.completed",
		Item: &CodexItem{
			ID:               "item_4",
			Type:             "command_execution",
			Command:          "echo hi",
			AggregatedOutput: "hi",
			ExitCode:         nil,
			Status:           "completed",
		},
	}
	tc := parseCodexToolComplete(msg)
	assert.NotNil(t, tc)
	assert.True(t, tc.Done)
	// nil exit code: output non-empty → status should be "success"
	assert.Equal(t, "success", tc.Status)
}

func TestCodexTool_ParseComplete_ZeroExitCodeNoOutput(t *testing.T) {
	exitCode := 0
	msg := &CodexStreamMessage{
		Type: "item.completed",
		Item: &CodexItem{
			ID:               "item_5",
			Type:             "command_execution",
			Command:          "true",
			AggregatedOutput: "",
			ExitCode:         &exitCode,
			Status:           "completed",
		},
	}
	tc := parseCodexToolComplete(msg)
	assert.NotNil(t, tc)
	assert.True(t, tc.Done)
	// Zero exit code + empty output → no status (neither success nor error)
	assert.Empty(t, tc.Status)
}

func TestCodexTool_ParseComplete_NonCommandExecution(t *testing.T) {
	msg := &CodexStreamMessage{
		Type: "item.completed",
		Item: &CodexItem{
			ID:   "item_0",
			Type: "agent_message",
			Text: "Hello!",
		},
	}
	tc := parseCodexToolComplete(msg)
	assert.Nil(t, tc)
}

func TestCodexTool_ParseComplete_NilItem(t *testing.T) {
	msg := &CodexStreamMessage{
		Type: "item.completed",
		Item: nil,
	}
	tc := parseCodexToolComplete(msg)
	assert.Nil(t, tc)
}

func TestCodexTool_ParseComplete_OutputTruncation(t *testing.T) {
	// Output exceeding maxToolOutputBytes should be truncated
	exitCode := 0
	longOutputBytes := make([]byte, maxToolOutputBytes+1000) // exceeds 50KB
	for i := range longOutputBytes {
		longOutputBytes[i] = 'x'
	}
	longOutput := string(longOutputBytes)
	msg := &CodexStreamMessage{
		Type: "item.completed",
		Item: &CodexItem{
			ID:               "item_6",
			Type:             "command_execution",
			Command:          "cat large_file",
			AggregatedOutput: longOutput,
			ExitCode:         &exitCode,
			Status:           "completed",
		},
	}
	tc := parseCodexToolComplete(msg)
	assert.NotNil(t, tc)
	assert.Contains(t, tc.Output, "[truncated:")
	assert.LessOrEqual(t, len(tc.Output), maxToolOutputBytes+100) // some slack for truncation marker
}

func TestCodexTool_ParseComplete_InputIsNormalizedJSON(t *testing.T) {
	exitCode := 0
	msg := &CodexStreamMessage{
		Type: "item.completed",
		Item: &CodexItem{
			ID:               "item_7",
			Type:             "command_execution",
			Command:          `bash -c 'echo "hello world"'`,
			AggregatedOutput: "hello world",
			ExitCode:         &exitCode,
			Status:           "completed",
		},
	}
	tc := parseCodexToolComplete(msg)
	assert.NotNil(t, tc)
	// Input must be valid JSON with "command" key
	var parsed map[string]string
	err := json.Unmarshal([]byte(tc.Input), &parsed)
	assert.NoError(t, err)
	assert.Equal(t, `bash -c 'echo "hello world"'`, parsed["command"])
}

// --- emitBashToolCall tests ---

func TestCodexTool_EmitBashToolCall_Started(t *testing.T) {
	ch := make(chan StreamEvent, 1)
	emitBashToolCall(ch, "exec-0", `{"command":"ls"}`, "", false, nil)

	select {
	case ev := <-ch:
		assert.Equal(t, "tool_use", ev.Type)
		assert.NotNil(t, ev.Tool)
		assert.Equal(t, "Bash", ev.Tool.Name)
		assert.Equal(t, "exec-0", ev.Tool.ID)
		assert.Equal(t, `{"command":"ls"}`, ev.Tool.Input)
		assert.False(t, ev.Tool.Done)
		assert.Empty(t, ev.Tool.Output)
		assert.Empty(t, ev.Tool.Status)
	default:
		t.Fatal("expected event on channel")
	}
}

func TestCodexTool_EmitBashToolCall_CompletedSuccess(t *testing.T) {
	ch := make(chan StreamEvent, 1)
	exitCode := 0
	emitBashToolCall(ch, "exec-0", `{"command":"ls"}`, "file1.txt\nfile2.txt", true, &exitCode)

	select {
	case ev := <-ch:
		assert.Equal(t, "tool_use", ev.Type)
		assert.Equal(t, "Bash", ev.Tool.Name)
		assert.True(t, ev.Tool.Done)
		assert.Equal(t, "file1.txt\nfile2.txt", ev.Tool.Output)
		assert.Equal(t, "success", ev.Tool.Status)
	default:
		t.Fatal("expected event on channel")
	}
}

func TestCodexTool_EmitBashToolCall_CompletedError(t *testing.T) {
	ch := make(chan StreamEvent, 1)
	exitCode := 1
	emitBashToolCall(ch, "exec-1", `{"command":"exit 1"}`, "error output", true, &exitCode)

	select {
	case ev := <-ch:
		assert.Equal(t, "tool_use", ev.Type)
		assert.Equal(t, "Bash", ev.Tool.Name)
		assert.True(t, ev.Tool.Done)
		assert.Equal(t, "error", ev.Tool.Status)
	default:
		t.Fatal("expected event on channel")
	}
}

func TestCodexTool_EmitBashToolCall_CompletedNilExitCode(t *testing.T) {
	ch := make(chan StreamEvent, 1)
	emitBashToolCall(ch, "exec-2", `{"command":"echo hi"}`, "hi", true, nil)

	select {
	case ev := <-ch:
		assert.Equal(t, "tool_use", ev.Type)
		assert.True(t, ev.Tool.Done)
		// nil exit code + non-empty output → "success"
		assert.Equal(t, "success", ev.Tool.Status)
	default:
		t.Fatal("expected event on channel")
	}
}

func TestCodexTool_EmitBashToolCall_CompletedNoOutput(t *testing.T) {
	ch := make(chan StreamEvent, 1)
	exitCode := 0
	emitBashToolCall(ch, "exec-3", `{"command":"true"}`, "", true, &exitCode)

	select {
	case ev := <-ch:
		assert.Equal(t, "tool_use", ev.Type)
		assert.True(t, ev.Tool.Done)
		// Zero exit code + empty output → no status
		assert.Empty(t, ev.Tool.Status)
	default:
		t.Fatal("expected event on channel")
	}
}

func TestCodexTool_EmitBashToolCall_NonZeroExitCodeWithOutput(t *testing.T) {
	ch := make(chan StreamEvent, 1)
	exitCode := 2
	emitBashToolCall(ch, "exec-4", `{"command":"bad_cmd"}`, "some error\nlines", true, &exitCode)

	select {
	case ev := <-ch:
		assert.Equal(t, "error", ev.Tool.Status)
		assert.Equal(t, "some error\nlines", ev.Tool.Output)
	default:
		t.Fatal("expected event on channel")
	}
}

// --- Integration: parseCodexToolStart + parseCodexToolComplete round-trip ---

func TestCodexTool_StartAndCompleteRoundTrip(t *testing.T) {
	// Simulate a full tool call lifecycle: started then completed
	startMsg := &CodexStreamMessage{
		Type: "item.started",
		Item: &CodexItem{
			ID:      "item_1",
			Type:    "command_execution",
			Command: "git status",
			Status:  "in_progress",
		},
	}
	startTC := parseCodexToolStart(startMsg)
	assert.NotNil(t, startTC)
	assert.Equal(t, "item_1", startTC.ID)
	assert.Equal(t, "Bash", startTC.Name)
	assert.False(t, startTC.Done)

	exitCode := 0
	completeMsg := &CodexStreamMessage{
		Type: "item.completed",
		Item: &CodexItem{
			ID:               "item_1",
			Type:             "command_execution",
			Command:          "git status",
			AggregatedOutput: "On branch main\nnothing to commit",
			ExitCode:         &exitCode,
			Status:           "completed",
		},
	}
	completeTC := parseCodexToolComplete(completeMsg)
	assert.NotNil(t, completeTC)
	assert.Equal(t, "item_1", completeTC.ID)
	assert.Equal(t, "Bash", completeTC.Name)
	assert.True(t, completeTC.Done)
	assert.Equal(t, startTC.Input, completeTC.Input) // same command → same input JSON
	assert.Equal(t, "On branch main\nnothing to commit", completeTC.Output)
	assert.Equal(t, "success", completeTC.Status)
}
