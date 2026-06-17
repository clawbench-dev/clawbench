package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/model"
)

// --- mapACPToolCall tests ---

func TestMapACPToolCall_BasicFields(t *testing.T) {
	tc := acp.SessionUpdateToolCall{
		ToolCallId: acp.ToolCallId("tc-123"),
		Title:      "Read",
		Kind:       acp.ToolKindRead,
	}
	event := mapACPToolCall(tc, "")

	assert.Equal(t, "tool_use", event.Type)
	require.NotNil(t, event.Tool)
	assert.Equal(t, "Read", event.Tool.Name)
	assert.Equal(t, "tc-123", event.Tool.ID)
	assert.False(t, event.Tool.Done)
}

func TestMapACPToolCall_WithRawInput(t *testing.T) {
	tc := acp.SessionUpdateToolCall{
		ToolCallId: acp.ToolCallId("tc-456"),
		Title:      "Write",
		Kind:       acp.ToolKindEdit,
		RawInput:   map[string]any{"path": "/tmp/test.txt", "content": "hello"},
	}
	event := mapACPToolCall(tc, "")

	assert.Equal(t, "Write", event.Tool.Name)
	assert.Contains(t, event.Tool.Input, "path")
	assert.Contains(t, event.Tool.Input, "hello")
}

func TestMapACPToolCall_NoTitleUsesKind(t *testing.T) {
	tc := acp.SessionUpdateToolCall{
		ToolCallId: acp.ToolCallId("tc-789"),
		Title:      "",
		Kind:       acp.ToolKindRead,
	}
	event := mapACPToolCall(tc, "")
	// Kind fallback now maps to PascalCase canonical name, not lowercase string(kind)
	assert.Equal(t, "Read", event.Tool.Name)
}

func TestMapACPToolCall_ContentWithTerminal(t *testing.T) {
	// Simulates Claude ACP agent sending Terminal tool call with Content instead of RawInput
	tc := acp.SessionUpdateToolCall{
		ToolCallId: acp.ToolCallId("tc-term-1"),
		Title:      "Terminal",
		Kind:       acp.ToolKindExecute,
		Content: []acp.ToolCallContent{
			{
				Terminal: &acp.ToolCallContentTerminal{
					TerminalId: "term-1",
					Type:       "terminal",
				},
			},
		},
	}
	event := mapACPToolCall(tc, "")

	assert.Equal(t, "Bash", event.Tool.Name)
	assert.Contains(t, event.Tool.Input, "command")
	assert.Contains(t, event.Tool.Input, "Terminal")
}

func TestMapACPToolCall_ContentWithText(t *testing.T) {
	// Simulates ACP tool call with text content block but no RawInput
	tc := acp.SessionUpdateToolCall{
		ToolCallId: acp.ToolCallId("tc-text-1"),
		Title:      "Bash",
		Kind:       acp.ToolKindExecute,
		Content: []acp.ToolCallContent{
			{
				Content: &acp.ToolCallContentContent{
					Content: acp.ContentBlock{
						Text: &acp.ContentBlockText{Text: "Run a command"},
					},
					Type: "content",
				},
			},
		},
	}
	event := mapACPToolCall(tc, "")

	assert.Equal(t, "Bash", event.Tool.Name)
	assert.Contains(t, event.Tool.Input, "description")
	assert.Contains(t, event.Tool.Input, "Run a command")
}

func TestMapACPToolCall_RawInputPreferredOverContent(t *testing.T) {
	// When both RawInput and Content exist, RawInput takes precedence
	tc := acp.SessionUpdateToolCall{
		ToolCallId: acp.ToolCallId("tc-both"),
		Title:      "Bash",
		Kind:       acp.ToolKindExecute,
		RawInput:   map[string]any{"command": "ls -la", "description": "List files"},
		Content: []acp.ToolCallContent{
			{
				Terminal: &acp.ToolCallContentTerminal{
					TerminalId: "term-1",
					Type:       "terminal",
				},
			},
		},
	}
	event := mapACPToolCall(tc, "")

	assert.Equal(t, "Bash", event.Tool.Name)
	assert.Contains(t, event.Tool.Input, "ls -la")
}

func TestMapACPToolCall_WithFilePathRawInput(t *testing.T) {
	// Open Code ACP uses "filePath" (camelCase) in rawInput for read tools
	tc := acp.SessionUpdateToolCall{
		ToolCallId: acp.ToolCallId("tc-read-fp"),
		Title:      "read",
		Kind:       acp.ToolKindRead,
		RawInput:   map[string]any{"filePath": "/home/user/project/main.go"},
	}
	event := mapACPToolCall(tc, "")

	assert.Equal(t, "Read", event.Tool.Name)
	assert.Contains(t, event.Tool.Input, "file_path")
	assert.NotContains(t, event.Tool.Input, "filePath")
	assert.Contains(t, event.Tool.Input, "/home/user/project/main.go")
}

// --- mapACPToolCallUpdate tests ---

func TestMapACPToolCallUpdate_Completed(t *testing.T) {
	completed := acp.ToolCallStatusCompleted
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-123"),
		Status:     &completed,
		RawOutput:  map[string]any{"result": "file contents"},
	}
	event := mapACPToolCallUpdate(tcu, "")

	assert.Equal(t, "tool_result", event.Type)
	require.NotNil(t, event.Tool)
	assert.Equal(t, "tc-123", event.Tool.ID)
	assert.True(t, event.Tool.Done)
	assert.Equal(t, "success", event.Tool.Status)
	assert.Contains(t, event.Tool.Output, "file contents")
}

func TestMapACPToolCallUpdate_Failed(t *testing.T) {
	failed := acp.ToolCallStatusFailed
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-fail"),
		Status:     &failed,
		RawOutput:  map[string]any{"error": "permission denied"},
	}
	event := mapACPToolCallUpdate(tcu, "")

	assert.Equal(t, "tool_result", event.Type)
	assert.True(t, event.Tool.Done)
	assert.Equal(t, "error", event.Tool.Status)
}

func TestMapACPToolCallUpdate_InProgress(t *testing.T) {
	inProgress := acp.ToolCallStatusInProgress
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-wip"),
		Status:     &inProgress,
	}
	event := mapACPToolCallUpdate(tcu, "")

	assert.Equal(t, "tool_use", event.Type)
	assert.False(t, event.Tool.Done)
	assert.Equal(t, "", event.Tool.Status)
}

func TestMapACPToolCallUpdate_InProgressIgnoresRawOutput(t *testing.T) {
	// In-progress updates should NOT extract output — intermediate RawOutput
	// may contain partial/garbage data (e.g., a lone "}") that would be
	// persisted to the DB if the session is cancelled before completion.
	inProgress := acp.ToolCallStatusInProgress
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-wip"),
		Status:     &inProgress,
		RawOutput:  "}",
	}
	event := mapACPToolCallUpdate(tcu, "")

	assert.Equal(t, "tool_use", event.Type)
	assert.False(t, event.Tool.Done)
	assert.Equal(t, "", event.Tool.Output, "in-progress updates must not extract output")
}

func TestMapACPToolCallUpdate_Pending(t *testing.T) {
	pending := acp.ToolCallStatusPending
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-pend"),
		Status:     &pending,
	}
	event := mapACPToolCallUpdate(tcu, "")

	assert.Equal(t, "tool_use", event.Type)
	assert.False(t, event.Tool.Done)
}

func TestMapACPToolCallUpdate_InProgressWithRawInput(t *testing.T) {
	// Simulates OpenCode ACP task tool: tool_call_update with rawInput containing
	// description/prompt/subagent_type and status=in_progress
	inProgress := acp.ToolCallStatusInProgress
	title := "Explore project structure"
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("call_function_bla436bgujiz_1"),
		Status:     &inProgress,
		Title:      &title,
		RawInput: map[string]any{
			"description":   "Explore project structure",
			"prompt":        "Explore the codebase thoroughly",
			"subagent_type": "explore",
		},
	}
	event := mapACPToolCallUpdate(tcu, "")

	assert.Equal(t, "tool_use", event.Type, "in_progress update should emit tool_use")
	require.NotNil(t, event.Tool)
	assert.False(t, event.Tool.Done)
	assert.NotEmpty(t, event.Tool.Input, "rawInput should be extracted as tool input")

	// Verify input contains the expected fields
	var input map[string]any
	err := json.Unmarshal([]byte(event.Tool.Input), &input)
	require.NoError(t, err)
	assert.Equal(t, "Explore project structure", input["description"])
	assert.Equal(t, "explore", input["subagent_type"])
	assert.Contains(t, input, "prompt")
}

func TestMapACPToolCallUpdate_CompletedWithRawInput(t *testing.T) {
	// Simulates completed tool_call_update that also has rawInput
	completed := acp.ToolCallStatusCompleted
	title := "Explore project structure"
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("call_function_bla436bgujiz_1"),
		Status:     &completed,
		Title:      &title,
		RawInput: map[string]any{
			"description":   "Explore project structure",
			"prompt":        "Explore the codebase thoroughly",
			"subagent_type": "explore",
		},
		RawOutput: map[string]any{"result": "project summary"},
	}
	event := mapACPToolCallUpdate(tcu, "")

	assert.Equal(t, "tool_result", event.Type, "completed update should emit tool_result")
	require.NotNil(t, event.Tool)
	assert.True(t, event.Tool.Done)
	assert.Equal(t, "success", event.Tool.Status)
	// tool_result should still have input (for AccumulateBlock to potentially use)
	assert.NotEmpty(t, event.Tool.Input, "completed tool_result should also carry input")
}

func TestMapACPToolCallUpdate_CompletedWithTitleNoOverride(t *testing.T) {
	// Open Code ACP changes the title to a descriptive string (e.g. file path)
	// when a tool call completes. The tool name should NOT be overwritten.
	completed := acp.ToolCallStatusCompleted
	readKind := acp.ToolKindRead
	title := "cmd/server" // descriptive title, not a tool name
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-read-path"),
		Status:     &completed,
		Kind:       &readKind,
		Title:      &title,
		RawOutput:  map[string]any{"output": "directory listing"},
	}
	event := mapACPToolCallUpdate(tcu, "")

	assert.Equal(t, "tool_result", event.Type)
	// Name should remain empty (not overwritten by "cmd/server")
	// because completed status should not update name from descriptive titles
	assert.Equal(t, "", event.Tool.Name, "completed title should not override tool name")
}

func TestMapACPToolCallUpdate_InProgressWithTitleUpdate(t *testing.T) {
	// In-progress updates SHOULD still update the tool name from title
	inProgress := acp.ToolCallStatusInProgress
	readKind := acp.ToolKindRead
	title := "read"
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-read-1"),
		Status:     &inProgress,
		Kind:       &readKind,
		Title:      &title,
	}
	event := mapACPToolCallUpdate(tcu, "")

	assert.Equal(t, "tool_use", event.Type)
	assert.Equal(t, "Read", event.Tool.Name, "in-progress title should update tool name")
}

func TestMapACPToolCallUpdate_InProgressWithDescriptiveTitle(t *testing.T) {
	// In-progress execute tool with descriptive title (e.g. from Claude)
	inProgress := acp.ToolCallStatusInProgress
	executeKind := acp.ToolKindExecute
	title := "Show all branches"
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-bash-1"),
		Status:     &inProgress,
		Kind:       &executeKind,
		Title:      &title,
	}
	event := mapACPToolCallUpdate(tcu, "")

	assert.Equal(t, "tool_use", event.Type)
	// Descriptive title for execute kind won't match patterns, but that's OK
	// for in-progress — the tool name was already set by the initial tool_call
}

// --- extractToolName tests ---

func TestExtractToolName_ToolCallIdPrefix(t *testing.T) {
	// Kimi ACP uses toolCallId prefixes like "read_file-", "list_directory-",
	// "glob-", "run_shell_command-", "ask-" to encode the tool type.
	assert.Equal(t, "Read", extractToolName("README.md", acp.ToolKindRead, "kimi", "read_file-1780647417975-1"))
	assert.Equal(t, "LS", extractToolName("cmd/server", acp.ToolKindSearch, "kimi", "list_directory-1780647430067-5"))
	assert.Equal(t, "Glob", extractToolName("'cmd/server/**/*.go'", acp.ToolKindSearch, "kimi", "glob-1780647418037-4"))
	assert.Equal(t, "Bash", extractToolName("ls -R cmd/server", acp.ToolKindExecute, "kimi", "run_shell_command-1780647441920-8"))
	assert.Equal(t, "AskUserQuestion", extractToolName("ask", acp.ToolKindOther, "kimi", "ask-4f1164a8-5d96-4ea6-b3b7-7babeb2c8809"))
	assert.Equal(t, "Write", extractToolName("file.txt", acp.ToolKindEdit, "kimi", "write_file-123-1"))
	assert.Equal(t, "Edit", extractToolName("file.go", acp.ToolKindEdit, "kimi", "edit_file-123-2"))
	assert.Equal(t, "Grep", extractToolName("pattern", acp.ToolKindSearch, "kimi", "search_file-123-3"))
	assert.Equal(t, "Grep", extractToolName("dir", acp.ToolKindSearch, "kimi", "search_directory-123-4"))
}

func TestExtractToolName_ToolCallIdPrefixPriority(t *testing.T) {
	// toolCallId prefix takes priority over title matching for Kimi ACP
	assert.Equal(t, "Read", extractToolName("README.md", acp.ToolKindRead, "kimi", "read_file-1-1"))
	// Without toolCallId, "README.md" has a dot → treated as non-canonical word → kind fallback
	assert.Equal(t, "Read", extractToolName("README.md", acp.ToolKindRead, ""))
}

func TestExtractToolName_ToolCallIdPrefixUnknown(t *testing.T) {
	// Unknown prefix falls through to title/kind matching
	assert.Equal(t, "Bash", extractToolName("Bash", acp.ToolKindExecute, "", "unknown_prefix-123"))
}

func TestExtractToolName_TitlePreferred(t *testing.T) {
	assert.Equal(t, "Read", extractToolName("Read", acp.ToolKindRead, ""))
	assert.Equal(t, "MyCustomTool", extractToolName("MyCustomTool", acp.ToolKindEdit, ""))
	assert.Equal(t, "MultiEdit", extractToolName("MultiEdit file", acp.ToolKindEdit, ""))
	assert.Equal(t, "WebSearch", extractToolName("WebSearch query", acp.ToolKindSearch, ""))
	assert.Equal(t, "Bash", extractToolName("Bash command", acp.ToolKindExecute, ""))
	assert.Equal(t, "EnterPlanMode", extractToolName("EnterPlanMode", acp.ToolKindSwitchMode, ""))
	assert.Equal(t, "AskUserQuestion", extractToolName("AskUserQuestion prompt", acp.ToolKindOther, ""))
	assert.Equal(t, "TaskCreate", extractToolName("TaskCreate new task", acp.ToolKindOther, ""))
	assert.Equal(t, "ComputerUse", extractToolName("ComputerUse action", acp.ToolKindOther, ""))
	assert.Equal(t, "save_memory", extractToolName("save_memory", acp.ToolKindOther, ""))
}

func TestExtractToolName_KindFallback(t *testing.T) {
	// When title is empty, fall back to ACP ToolKind → canonical mapping
	assert.Equal(t, "Read", extractToolName("", acp.ToolKindRead, ""))
	assert.Equal(t, "Edit", extractToolName("", acp.ToolKindEdit, ""))
	assert.Equal(t, "Bash", extractToolName("", acp.ToolKindExecute, ""))
	assert.Equal(t, "Grep", extractToolName("", acp.ToolKindSearch, ""))
	assert.Equal(t, "WebFetch", extractToolName("", acp.ToolKindFetch, ""))
	assert.Equal(t, "DeepThink", extractToolName("", acp.ToolKindThink, ""))
	assert.Equal(t, "EnterPlanMode", extractToolName("", acp.ToolKindSwitchMode, ""))
	assert.Equal(t, "Edit", extractToolName("", acp.ToolKindDelete, ""))
	assert.Equal(t, "Edit", extractToolName("", acp.ToolKindMove, ""))
	assert.Equal(t, "Skill", extractToolName("", acp.ToolKindOther, ""))
}

func TestExtractToolName_PrefixOrdering(t *testing.T) {
	// Longer prefixes must match before shorter ones
	assert.Equal(t, "MultiEdit", extractToolName("MultiEdit changes", acp.ToolKindEdit, ""))
	assert.Equal(t, "WebSearch", extractToolName("WebSearch for golang", acp.ToolKindSearch, ""))
	assert.Equal(t, "WebFetch", extractToolName("WebFetch url", acp.ToolKindFetch, ""))
	assert.Equal(t, "NotebookEdit", extractToolName("NotebookEdit cell", acp.ToolKindEdit, ""))
	assert.Equal(t, "EnterPlanMode", extractToolName("EnterPlanMode", acp.ToolKindSwitchMode, ""))
	assert.Equal(t, "ExitPlanMode", extractToolName("ExitPlanMode", acp.ToolKindSwitchMode, ""))
}

func TestExtractToolName_LowerCaseAliases(t *testing.T) {
	// Lowercase single-word titles should map to canonical PascalCase
	assert.Equal(t, "Bash", extractToolName("bash", acp.ToolKindExecute, ""))
	assert.Equal(t, "Bash", extractToolName("terminal", acp.ToolKindExecute, ""))
	assert.Equal(t, "Bash", extractToolName("shell", acp.ToolKindExecute, ""))
	assert.Equal(t, "Read", extractToolName("read", acp.ToolKindRead, ""))
	assert.Equal(t, "Write", extractToolName("write", acp.ToolKindEdit, ""))
	assert.Equal(t, "Edit", extractToolName("edit", acp.ToolKindEdit, ""))
	assert.Equal(t, "Glob", extractToolName("glob", acp.ToolKindSearch, ""))
	assert.Equal(t, "Grep", extractToolName("grep", acp.ToolKindSearch, ""))
	assert.Equal(t, "LS", extractToolName("ls", acp.ToolKindOther, ""))
}

func TestExtractToolName_TerminalPrefix(t *testing.T) {
	// "Terminal" should map to "Bash" via acpToolNamePatterns
	assert.Equal(t, "Bash", extractToolName("Terminal", acp.ToolKindExecute, ""))
}

// --- Kimi ACP tool call pattern tests ---

func TestMapACPToolCall_KimiReadFile(t *testing.T) {
	// Kimi ACP read_file: kind=read, title=filename, locations=[{path}], no rawInput
	tc := acp.SessionUpdateToolCall{
		ToolCallId: acp.ToolCallId("read_file-1780647417975-1"),
		Title:      "README.md",
		Kind:       acp.ToolKindRead,
		Locations: []acp.ToolCallLocation{
			{Path: "/home/user/project/README.md"},
		},
	}
	event := mapACPToolCall(tc, "")

	assert.Equal(t, "tool_use", event.Type)
	require.NotNil(t, event.Tool)
	assert.Equal(t, "Read", event.Tool.Name, "toolCallId prefix 'read_file' should map to Read")
	assert.Equal(t, "read_file-1780647417975-1", event.Tool.ID)
	assert.False(t, event.Tool.Done)
	assert.Contains(t, event.Tool.Input, "file_path")
	assert.Contains(t, event.Tool.Input, "/home/user/project/README.md")
}

func TestMapACPToolCall_KimiListDirectory(t *testing.T) {
	// Kimi ACP list_directory: kind=search, title=dirname, no locations
	tc := acp.SessionUpdateToolCall{
		ToolCallId: acp.ToolCallId("list_directory-1780647430067-5"),
		Title:      "cmd/server",
		Kind:       acp.ToolKindSearch,
	}
	event := mapACPToolCall(tc, "")

	assert.Equal(t, "tool_use", event.Type)
	require.NotNil(t, event.Tool)
	assert.Equal(t, "LS", event.Tool.Name, "toolCallId prefix 'list_directory' should map to LS")
	assert.Contains(t, event.Tool.Input, "path")
	assert.Contains(t, event.Tool.Input, "cmd/server")
}

func TestMapACPToolCall_KimiGlob(t *testing.T) {
	// Kimi ACP glob: kind=search, title=pattern, no locations
	tc := acp.SessionUpdateToolCall{
		ToolCallId: acp.ToolCallId("glob-1780647418037-4"),
		Title:      "'cmd/server/**/*.go'",
		Kind:       acp.ToolKindSearch,
	}
	event := mapACPToolCall(tc, "")

	assert.Equal(t, "tool_use", event.Type)
	require.NotNil(t, event.Tool)
	assert.Equal(t, "Glob", event.Tool.Name, "toolCallId prefix 'glob' should map to Glob")
	assert.Contains(t, event.Tool.Input, "pattern")
	assert.Contains(t, event.Tool.Input, "cmd/server")
}

func TestMapACPToolCall_KimiShellCommand(t *testing.T) {
	// Kimi ACP run_shell_command: kind=execute, title=command, no rawInput
	tc := acp.SessionUpdateToolCall{
		ToolCallId: acp.ToolCallId("run_shell_command-1780647441920-8"),
		Title:      "ls -R cmd/server",
		Kind:       acp.ToolKindExecute,
	}
	event := mapACPToolCall(tc, "")

	assert.Equal(t, "tool_use", event.Type)
	require.NotNil(t, event.Tool)
	assert.Equal(t, "Bash", event.Tool.Name, "toolCallId prefix 'run_shell_command' should map to Bash")
	assert.Contains(t, event.Tool.Input, "command")
	assert.Contains(t, event.Tool.Input, "ls -R cmd/server")
}

func TestMapACPToolCall_KimiReadFileNoLocations(t *testing.T) {
	// Kimi ACP read_file with no locations — should use title as file_path fallback
	tc := acp.SessionUpdateToolCall{
		ToolCallId: acp.ToolCallId("read_file-123-1"),
		Title:      "main.go",
		Kind:       acp.ToolKindRead,
	}
	event := mapACPToolCall(tc, "")

	assert.Equal(t, "Read", event.Tool.Name)
	assert.Contains(t, event.Tool.Input, "file_path")
	assert.Contains(t, event.Tool.Input, "main.go")
}

func TestMapACPToolCallUpdate_KimiCompleted(t *testing.T) {
	// Kimi ACP completed tool_call_update with locations
	completed := acp.ToolCallStatusCompleted
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("read_file-1780647417975-1"),
		Status:     &completed,
		Kind:       nil,
		Title:      nil,
		Locations: []acp.ToolCallLocation{
			{Path: "/home/user/project/README.md"},
		},
		RawOutput: map[string]any{"result": "file contents here"},
	}
	event := mapACPToolCallUpdate(tcu, "")

	assert.Equal(t, "tool_result", event.Type)
	assert.True(t, event.Tool.Done)
	assert.Equal(t, "success", event.Tool.Status)
	assert.Contains(t, event.Tool.Output, "file contents here")
}

func TestMapACPToolCallUpdate_KimiCompletedWithContent(t *testing.T) {
	// Kimi ACP completed update with Content blocks instead of RawOutput
	completed := acp.ToolCallStatusCompleted
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("glob-1780647418037-4"),
		Status:     &completed,
		Content: []acp.ToolCallContent{
			{
				Content: &acp.ToolCallContentContent{
					Content: acp.ContentBlock{
						Text: &acp.ContentBlockText{Text: "No files found"},
					},
					Type: "content",
				},
			},
		},
	}
	event := mapACPToolCallUpdate(tcu, "")

	assert.Equal(t, "tool_result", event.Type)
	assert.True(t, event.Tool.Done)
	assert.Contains(t, event.Tool.Output, "No files found")
}

func TestMapACPToolCallUpdate_KimiFailedWithContent(t *testing.T) {
	// Kimi ACP failed update with Content blocks
	failed := acp.ToolCallStatusFailed
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("read_file-1780647453119-9"),
		Status:     &failed,
		Content: []acp.ToolCallContent{
			{
				Content: &acp.ToolCallContentContent{
					Content: acp.ContentBlock{
						Text: &acp.ContentBlockText{Text: "File path is ignored by configured ignore patterns."},
					},
					Type: "content",
				},
			},
		},
	}
	event := mapACPToolCallUpdate(tcu, "")

	assert.Equal(t, "tool_result", event.Type)
	assert.True(t, event.Tool.Done)
	assert.Equal(t, "error", event.Tool.Status)
	assert.Contains(t, event.Tool.Output, "ignored by configured ignore patterns")
}

func TestMapACPToolCallUpdate_RawOutputPreferredOverContent(t *testing.T) {
	// When both RawOutput and Content exist, RawOutput takes precedence
	completed := acp.ToolCallStatusCompleted
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-both"),
		Status:     &completed,
		RawOutput:  map[string]any{"result": "from rawOutput"},
		Content: []acp.ToolCallContent{
			{
				Content: &acp.ToolCallContentContent{
					Content: acp.ContentBlock{
						Text: &acp.ContentBlockText{Text: "from content"},
					},
					Type: "content",
				},
			},
		},
	}
	event := mapACPToolCallUpdate(tcu, "")

	assert.Contains(t, event.Tool.Output, "from rawOutput")
	assert.NotContains(t, event.Tool.Output, "from content")
}

// --- extractACPToolOutputFromContent tests ---

func TestExtractACPToolOutputFromContent_SingleText(t *testing.T) {
	result := extractACPToolOutputFromContent([]acp.ToolCallContent{
		{
			Content: &acp.ToolCallContentContent{
				Content: acp.ContentBlock{
					Text: &acp.ContentBlockText{Text: "No files found"},
				},
				Type: "content",
			},
		},
	})
	assert.Equal(t, "No files found", result)
}

func TestExtractACPToolOutputFromContent_MultipleText(t *testing.T) {
	result := extractACPToolOutputFromContent([]acp.ToolCallContent{
		{
			Content: &acp.ToolCallContentContent{
				Content: acp.ContentBlock{
					Text: &acp.ContentBlockText{Text: "line 1"},
				},
				Type: "content",
			},
		},
		{
			Content: &acp.ToolCallContentContent{
				Content: acp.ContentBlock{
					Text: &acp.ContentBlockText{Text: "line 2"},
				},
				Type: "content",
			},
		},
	})
	assert.Equal(t, "line 1\nline 2", result)
}

func TestExtractACPToolOutputFromContent_Empty(t *testing.T) {
	result := extractACPToolOutputFromContent(nil)
	assert.Equal(t, "", result)
}

func TestExtractACPToolOutputFromContent_TerminalOnly(t *testing.T) {
	// Terminal content doesn't produce text output
	result := extractACPToolOutputFromContent([]acp.ToolCallContent{
		{
			Terminal: &acp.ToolCallContentTerminal{
				TerminalId: "term-1",
				Type:       "terminal",
			},
		},
	})
	assert.Equal(t, "", result)
}

// --- extractInputFromLocationsAndTitle tests ---

func TestExtractInputFromLocationsAndTitle_ReadWithLocations(t *testing.T) {
	input := extractInputFromLocationsAndTitle(
		[]acp.ToolCallLocation{{Path: "/home/user/project/main.go"}},
		"main.go",
		acp.ToolKindRead,
		"read_file-123-1",
	)
	require.NotNil(t, input)
	assert.Equal(t, "/home/user/project/main.go", input["file_path"])
}

func TestExtractInputFromLocationsAndTitle_ReadNoLocations(t *testing.T) {
	input := extractInputFromLocationsAndTitle(
		nil,
		"main.go",
		acp.ToolKindRead,
		"read_file-123-1",
	)
	require.NotNil(t, input)
	assert.Equal(t, "main.go", input["file_path"])
}

func TestExtractInputFromLocationsAndTitle_Glob(t *testing.T) {
	input := extractInputFromLocationsAndTitle(
		nil,
		"'cmd/server/**/*.go'",
		acp.ToolKindSearch,
		"glob-123-4",
	)
	require.NotNil(t, input)
	assert.Equal(t, "'cmd/server/**/*.go'", input["pattern"])
}

func TestExtractInputFromLocationsAndTitle_ListDirectory(t *testing.T) {
	input := extractInputFromLocationsAndTitle(
		nil,
		"cmd/server",
		acp.ToolKindSearch,
		"list_directory-123-5",
	)
	require.NotNil(t, input)
	assert.Equal(t, "cmd/server", input["path"])
}

func TestExtractInputFromLocationsAndTitle_GenericSearch(t *testing.T) {
	input := extractInputFromLocationsAndTitle(
		nil,
		"some query",
		acp.ToolKindSearch,
		"unknown_search-123",
	)
	require.NotNil(t, input)
	assert.Equal(t, "some query", input["path"])
}

func TestExtractInputFromLocationsAndTitle_EditWithLocations(t *testing.T) {
	input := extractInputFromLocationsAndTitle(
		[]acp.ToolCallLocation{{Path: "/home/user/project/main.go"}},
		"main.go",
		acp.ToolKindEdit,
		"edit_file-123-2",
	)
	require.NotNil(t, input)
	assert.Equal(t, "/home/user/project/main.go", input["file_path"])
}

func TestExtractInputFromLocationsAndTitle_ExecuteReturnsNil(t *testing.T) {
	// Execute kind is handled by the title→command fallback, not locations
	input := extractInputFromLocationsAndTitle(
		nil,
		"ls -la",
		acp.ToolKindExecute,
		"run_shell_command-123-8",
	)
	assert.Nil(t, input)
}

// --- mapACPError tests ---

func TestMapACPError_ParseError(t *testing.T) {
	event := mapACPError(-32700, "parse error")
	assert.Equal(t, "error", event.Type)
	assert.Equal(t, ReasonParseError, event.Reason)
}

func TestMapACPError_InvalidRequest(t *testing.T) {
	event := mapACPError(-32600, "invalid request")
	assert.Equal(t, ReasonParseError, event.Reason)
}

func TestMapACPError_MethodNotFound(t *testing.T) {
	event := mapACPError(-32601, "method not found")
	assert.Equal(t, ReasonBackendExit, event.Reason)
}

func TestMapACPError_Cancelled(t *testing.T) {
	event := mapACPError(-32800, "cancelled")
	assert.Equal(t, ReasonContextCancel, event.Reason)
}

func TestMapACPError_AuthRequired(t *testing.T) {
	event := mapACPError(-32000, "auth required")
	assert.Equal(t, ReasonRequestFailed, event.Reason)
}

func TestMapACPError_UnknownCode(t *testing.T) {
	event := mapACPError(-99999, "unknown")
	assert.Equal(t, ReasonBackendExit, event.Reason) // default
}

// --- forwardACPEvent tests ---

func TestForwardACPEvent_Basic(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	forwardACPEvent(ch, StreamEvent{Type: "content", Content: "hello"})

	select {
	case event := <-ch:
		assert.Equal(t, "content", event.Type)
		assert.Equal(t, "hello", event.Content)
	default:
		t.Fatal("expected event on channel")
	}
}

func TestForwardACPEvent_ChannelFull(t *testing.T) {
	ch := make(chan StreamEvent, 1)
	ch <- StreamEvent{Type: "content", Content: "fill"} // fill buffer

	// Should not block — drops event
	forwardACPEvent(ch, StreamEvent{Type: "content", Content: "overflow"})
}

// --- NewACPBackend validation tests ---

// --- mapACPSessionUpdate plan_update tests ---

func TestMapACPSessionUpdate_PlanUpdate(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	entries := []acp.PlanEntry{
		{Content: "Read project files", Priority: acp.PlanEntryPriorityHigh, Status: acp.PlanEntryStatusCompleted},
		{Content: "Implement feature", Priority: acp.PlanEntryPriorityHigh, Status: acp.PlanEntryStatusInProgress},
		{Content: "Write tests", Priority: acp.PlanEntryPriorityMedium, Status: acp.PlanEntryStatusPending},
	}

	update := acp.SessionUpdate{
		Plan: &acp.SessionUpdatePlan{
			Entries: entries,
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil, nil)

	// Drain events, skipping raw_output, expect exactly 1 plan_update
	events := drainACPEvents(ch, 1)
	require.Len(t, events, 1)
	assert.Equal(t, "plan_update", events[0].Type)
	require.NotNil(t, events[0].Plan)
	assert.Len(t, events[0].Plan.Entries, 3)

	// Verify each entry's fields
	assert.Equal(t, "Read project files", events[0].Plan.Entries[0].Content)
	assert.Equal(t, "high", events[0].Plan.Entries[0].Priority)
	assert.Equal(t, "completed", events[0].Plan.Entries[0].Status)

	assert.Equal(t, "Implement feature", events[0].Plan.Entries[1].Content)
	assert.Equal(t, "high", events[0].Plan.Entries[1].Priority)
	assert.Equal(t, "in_progress", events[0].Plan.Entries[1].Status)

	assert.Equal(t, "Write tests", events[0].Plan.Entries[2].Content)
	assert.Equal(t, "medium", events[0].Plan.Entries[2].Priority)
	assert.Equal(t, "pending", events[0].Plan.Entries[2].Status)

	assertNoMoreACPEvents(ch, t)
}

func TestNewACPBackend_InvalidTransport(t *testing.T) {
	agent := &model.Agent{
		ID:         "test",
		Backend:    "claude",
		Transport:  "cli",
		AcpCommand: "",
	}
	_, err := NewACPBackend(agent)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not support acp-stdio")
}

// --- shouldSetConfig / markConfigSet / resetLastSetConfig tests ---

func TestACPConn_ShouldSetConfig_Initial(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "test", AcpCommand: "ignored"}, "test-sid")
	assert.True(t, conn.shouldSetConfig("model", "claude-3.5"), "first set should always proceed")
	assert.True(t, conn.shouldSetConfig("thinkingEffort", "high"), "first set should always proceed")
}

func TestACPConn_ShouldSetConfig_SameValue(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "test", AcpCommand: "ignored"}, "test-sid")
	conn.markConfigSet("model", "claude-3.5")
	assert.False(t, conn.shouldSetConfig("model", "claude-3.5"), "same value should skip")
}

func TestACPConn_ShouldSetConfig_DifferentValue(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "test", AcpCommand: "ignored"}, "test-sid")
	conn.markConfigSet("model", "claude-3.5")
	assert.True(t, conn.shouldSetConfig("model", "gpt-4o"), "different value should proceed")
}

func TestACPConn_ShouldSetConfig_Reset(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "test", AcpCommand: "ignored"}, "test-sid")
	conn.markConfigSet("model", "claude-3.5")
	conn.resetLastSetConfig()
	assert.True(t, conn.shouldSetConfig("model", "claude-3.5"), "after reset, should proceed")
}

// --- isACPPeerDisconnected tests ---

func TestIsACPPeerDisconnected_PeerDisconnected(t *testing.T) {
	err := acp.NewInternalError(map[string]any{"error": "peer disconnected before response"})
	assert.True(t, isACPPeerDisconnected(err))
}

func TestIsACPPeerDisconnected_BrokenPipe(t *testing.T) {
	err := acp.NewInternalError(map[string]any{"error": "write |1: broken pipe"})
	assert.True(t, isACPPeerDisconnected(err), "broken pipe should trigger retry")
}

func TestIsACPPeerDisconnected_NormalError(t *testing.T) {
	err := acp.NewInternalError(map[string]any{"error": "session not found"})
	assert.False(t, isACPPeerDisconnected(err))
}

func TestIsACPPeerDisconnected_WrappedBrokenPipe(t *testing.T) {
	err := fmt.Errorf("acp: prompt: %w", acp.NewInternalError(map[string]any{"error": "write |1: broken pipe"}))
	assert.True(t, isACPPeerDisconnected(err), "wrapped broken pipe should trigger retry")
}

// --- isConfigKilledConnection tests ---

func TestIsConfigKilledConnection_Direct(t *testing.T) {
	err := errConfigKilledConnection("model", "glm-5.1")
	assert.True(t, isConfigKilledConnection(err))
}

func TestIsConfigKilledConnection_AllConfigIDs(t *testing.T) {
	for _, id := range []string{"model", "thinkingEffort", "mode"} {
		err := errConfigKilledConnection(id, "test-value")
		assert.True(t, isConfigKilledConnection(err), id+" should be detected")
		assert.Contains(t, err.Error(), id)
	}
}

func TestIsConfigKilledConnection_Wrapped(t *testing.T) {
	err := fmt.Errorf("outer: %w", errConfigKilledConnection("model", "glm-5.1"))
	assert.True(t, isConfigKilledConnection(err), "wrapped config killed connection should be detected")
}

func TestIsConfigKilledConnection_OtherError(t *testing.T) {
	err := fmt.Errorf("some other error")
	assert.False(t, isConfigKilledConnection(err))
}

func TestNewACPBackend_ValidStdio(t *testing.T) {
	agent := &model.Agent{
		ID:         "test",
		Backend:    "claude",
		Transport:  "acp-stdio",
		AcpCommand: "claude acp",
	}
	backend, err := NewACPBackend(agent)
	assert.NoError(t, err)
	assert.Equal(t, "claude", backend.Name())
}

func TestNewACPBackend_InvalidHTTP(t *testing.T) {
	agent := &model.Agent{
		ID:         "test",
		Backend:    "codebuddy",
		Transport:  "acp-http",
		AcpCommand: "",
	}
	_, err := NewACPBackend(agent)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not support acp-stdio")
}

// --- mapACPSessionUpdate AgentMessageChunk tests ---

func TestMapACPSessionUpdate_AgentMessageChunk(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	update := acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			Content: acp.ContentBlock{
				Text: &acp.ContentBlockText{Text: "hello world"},
			},
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil, nil)

	// Should emit thinking_done + content (2 events)
	events := drainACPEvents(ch, 2)

	assert.Equal(t, "thinking_done", events[0].Type)
	assert.Equal(t, "content", events[1].Type)
	assert.Equal(t, "hello world", events[1].Content)

	assertNoMoreACPEvents(ch, t)
}

func TestMapACPSessionUpdate_RawOutputEmitted(t *testing.T) {
	// Every ACP notification should emit a raw_output event for debugging/storage
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	update := acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			Content: acp.ContentBlock{
				Text: &acp.ContentBlockText{Text: "hello"},
			},
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil, nil)

	// Drain all events including raw_output
	var rawEvents []StreamEvent
	var otherEvents []StreamEvent
	for {
		select {
		case event := <-ch:
			if event.Type == "raw_output" {
				rawEvents = append(rawEvents, event)
			} else {
				otherEvents = append(otherEvents, event)
			}
		default:
			goto done
		}
	}
done:

	// Should have exactly 1 raw_output event
	assert.Len(t, rawEvents, 1, "expected exactly 1 raw_output event")
	if len(rawEvents) > 0 {
		assert.Contains(t, rawEvents[0].RawOutput, "agent_message_chunk")
	}

	// Other events should be present (thinking_done + content)
	assert.Len(t, otherEvents, 2)
}

func TestMapACPSessionUpdate_AgentMessageChunk_NilText(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	update := acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			Content: acp.ContentBlock{}, // Text is nil
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil, nil)

	// Should emit thinking_done only (no content event when Text is nil)
	events := drainACPEvents(ch, 1)
	assert.Equal(t, "thinking_done", events[0].Type)

	assertNoMoreACPEvents(ch, t)
}

// --- mapACPSessionUpdate AgentThoughtChunk tests ---

func TestMapACPSessionUpdate_AgentThoughtChunk(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	update := acp.SessionUpdate{
		AgentThoughtChunk: &acp.SessionUpdateAgentThoughtChunk{
			Content: acp.ContentBlock{
				Text: &acp.ContentBlockText{Text: "thinking about the problem"},
			},
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil, nil)

	events := drainACPEvents(ch, 1)
	assert.Equal(t, "thinking", events[0].Type)
	assert.Equal(t, "thinking about the problem", events[0].Content)

	assertNoMoreACPEvents(ch, t)
}

func TestMapACPSessionUpdate_AgentThoughtChunk_NilText(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	update := acp.SessionUpdate{
		AgentThoughtChunk: &acp.SessionUpdateAgentThoughtChunk{
			Content: acp.ContentBlock{}, // Text is nil
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil, nil)

	assertNoMoreACPEvents(ch, t) // no events when Text is nil
}

// --- mapACPSessionUpdate ToolCall tests ---

func TestMapACPSessionUpdate_ToolCall(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	update := acp.SessionUpdate{
		ToolCall: &acp.SessionUpdateToolCall{
			ToolCallId: acp.ToolCallId("tc-1"),
			Title:      "Read",
			Kind:       acp.ToolKindRead,
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil, nil)

	// thinking_done + tool_use (2 events)
	events := drainACPEvents(ch, 2)
	assert.Equal(t, "thinking_done", events[0].Type)
	assert.Equal(t, "tool_use", events[1].Type)
	require.NotNil(t, events[1].Tool)
	assert.Equal(t, "Read", events[1].Tool.Name)
	assert.Equal(t, "tc-1", events[1].Tool.ID)
	assert.False(t, events[1].Tool.Done)

	assertNoMoreACPEvents(ch, t)
}

// --- mapACPSessionUpdate ToolCallUpdate tests ---

func TestMapACPSessionUpdate_ToolCallUpdate_Completed(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	completed := acp.ToolCallStatusCompleted
	update := acp.SessionUpdate{
		ToolCallUpdate: &acp.SessionToolCallUpdate{
			ToolCallId: acp.ToolCallId("tc-1"),
			Status:     &completed,
			RawOutput:  map[string]any{"result": "done"},
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil, nil)

	events := drainACPEvents(ch, 1)
	assert.Equal(t, "tool_result", events[0].Type)
	require.NotNil(t, events[0].Tool)
	assert.Equal(t, "tc-1", events[0].Tool.ID)
	assert.True(t, events[0].Tool.Done)
	assert.Equal(t, "success", events[0].Tool.Status)

	assertNoMoreACPEvents(ch, t)
}

func TestMapACPSessionUpdate_ToolCallUpdate_ThinkCompleted(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	thinkKind := acp.ToolKindThink
	completed := acp.ToolCallStatusCompleted
	update := acp.SessionUpdate{
		ToolCallUpdate: &acp.SessionToolCallUpdate{
			ToolCallId: acp.ToolCallId("tc-think"),
			Kind:       &thinkKind,
			Status:     &completed,
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil, nil)

	// tool_result + thinking_done (think tool completion emits thinking_done)
	events := drainACPEvents(ch, 2)
	assert.Equal(t, "tool_result", events[0].Type)
	assert.Equal(t, "thinking_done", events[1].Type)

	assertNoMoreACPEvents(ch, t)
}

func TestMapACPSessionUpdate_ToolCallUpdate_ThinkFailed(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	thinkKind := acp.ToolKindThink
	failed := acp.ToolCallStatusFailed
	update := acp.SessionUpdate{
		ToolCallUpdate: &acp.SessionToolCallUpdate{
			ToolCallId: acp.ToolCallId("tc-think"),
			Kind:       &thinkKind,
			Status:     &failed,
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil, nil)

	// tool_result + thinking_done (think tool failure also emits thinking_done)
	events := drainACPEvents(ch, 2)
	assert.Equal(t, "tool_result", events[0].Type)
	assert.Equal(t, "thinking_done", events[1].Type)

	assertNoMoreACPEvents(ch, t)
}

func TestMapACPSessionUpdate_ToolCallUpdate_ThinkInProgress(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	thinkKind := acp.ToolKindThink
	inProgress := acp.ToolCallStatusInProgress
	update := acp.SessionUpdate{
		ToolCallUpdate: &acp.SessionToolCallUpdate{
			ToolCallId: acp.ToolCallId("tc-think"),
			Kind:       &thinkKind,
			Status:     &inProgress,
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil, nil)

	// Only tool_use — thinking_done NOT emitted for in-progress think tool
	events := drainACPEvents(ch, 1)
	assert.Equal(t, "tool_use", events[0].Type)

	assertNoMoreACPEvents(ch, t)
}

func TestMapACPSessionUpdate_ToolCallUpdate_NonThinkCompleted(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	readKind := acp.ToolKindRead
	completed := acp.ToolCallStatusCompleted
	update := acp.SessionUpdate{
		ToolCallUpdate: &acp.SessionToolCallUpdate{
			ToolCallId: acp.ToolCallId("tc-read"),
			Kind:       &readKind,
			Status:     &completed,
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil, nil)

	// Only tool_result — non-think tool does NOT emit thinking_done
	events := drainACPEvents(ch, 1)
	assert.Equal(t, "tool_result", events[0].Type)

	assertNoMoreACPEvents(ch, t)
}

// --- mapACPSessionUpdate AvailableCommandsUpdate tests ---

func TestMapACPSessionUpdate_AvailableCommandsUpdate(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	update := acp.SessionUpdate{
		AvailableCommandsUpdate: &acp.SessionAvailableCommandsUpdate{
			AvailableCommands: []acp.AvailableCommand{
				{
					Name:        "/compact",
					Description: "Compact conversation history",
				},
				{
					Name:        "/ask",
					Description: "Ask a question",
					Input: &acp.AvailableCommandInput{
						Unstructured: &acp.UnstructuredCommandInput{
							Hint: "your question",
						},
					},
				},
			},
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil, nil)

	events := drainACPEvents(ch, 1)
	assert.Equal(t, "commands_update", events[0].Type)
	require.Len(t, events[0].Commands, 2)

	assert.Equal(t, "/compact", events[0].Commands[0].Name)
	assert.Equal(t, "Compact conversation history", events[0].Commands[0].Description)
	assert.Equal(t, "", events[0].Commands[0].InputHint) // no input

	assert.Equal(t, "/ask", events[0].Commands[1].Name)
	assert.Equal(t, "Ask a question", events[0].Commands[1].Description)
	assert.Equal(t, "your question", events[0].Commands[1].InputHint) // has input hint

	assertNoMoreACPEvents(ch, t)
}

// --- mapACPSessionUpdate CurrentModeUpdate tests ---

func TestMapACPSessionUpdate_CurrentModeUpdate(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	update := acp.SessionUpdate{
		CurrentModeUpdate: &acp.SessionCurrentModeUpdate{
			CurrentModeId: acp.SessionModeId("code"),
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil, nil)

	// CurrentModeUpdate now forwards mode_update SSE —
	// agent mode changes take priority over user manual selection.
	events := drainACPEvents(ch, 1)
	assert.Equal(t, "mode_update", events[0].Type)
	require.NotNil(t, events[0].Mode)
	assert.Equal(t, "code", events[0].Mode.CurrentModeID)
}

func TestMapACPSessionUpdate_CurrentModeUpdate_WithCacheEntry(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	agent := &model.Agent{ID: "test-current-mode", Backend: "acp-stdio"}
	entry := newACPConn(agent, "test-current-mode-sid")
	// Pre-populate available modes in registry so IsModeAvailable can validate
	GetAgentCapabilityRegistry().UpdateModes(agent.ID, []ModeDef{
		{ID: "ask", Name: "Ask"},
		{ID: "code", Name: "Code"},
		{ID: "architect", Name: "Architect"},
	})
	entry.SetCurrentModeID("architect")

	update := acp.SessionUpdate{
		CurrentModeUpdate: &acp.SessionCurrentModeUpdate{
			CurrentModeId: acp.SessionModeId("code"),
		},
	}

	mapACPSessionUpdate(update, ch, ctx, entry, nil)

	// Session current mode should be updated — "code" is a valid mode in availableModes
	assert.Equal(t, "code", entry.GetCurrentModeID())

	// mode_update SSE should be forwarded because currentModeId changed
	events := drainACPEvents(ch, 1)
	assert.Equal(t, "mode_update", events[0].Type)
	require.NotNil(t, events[0].Mode)
	assert.Equal(t, "code", events[0].Mode.CurrentModeID)
}

func TestMapACPSessionUpdate_CurrentModeUpdate_InvalidModeRejected(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	agent := &model.Agent{ID: "test-invalid-mode", Backend: "acp-stdio"}
	entry := newACPConn(agent, "test-invalid-mode-sid")
	// Pre-populate available modes in registry
	GetAgentCapabilityRegistry().UpdateModes(agent.ID, []ModeDef{
		{ID: "ask", Name: "Ask"},
		{ID: "code", Name: "Code"},
	})
	entry.SetCurrentModeID("architect")

	update := acp.SessionUpdate{
		CurrentModeUpdate: &acp.SessionCurrentModeUpdate{
			// "bypass_permissions" is NOT in availableModes — bridge adapter artifact
			CurrentModeId: acp.SessionModeId("bypass_permissions"),
		},
	}

	mapACPSessionUpdate(update, ch, ctx, entry, nil)

	// Session current mode should NOT be updated — invalid mode rejected
	assert.Equal(t, "architect", entry.GetCurrentModeID())

	// No SSE event forwarded — invalid mode was filtered
	assertNoMoreACPEvents(ch, t)
}

// --- mapACPSessionUpdate ConfigOptionUpdate tests ---

func TestMapACPSessionUpdate_ConfigOptionUpdate_Mode(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	modeCategory := acp.SessionConfigOptionCategoryMode
	ungrouped := acp.SessionConfigSelectOptionsUngrouped(
		[]acp.SessionConfigSelectOption{
			{Name: "Ask", Value: acp.SessionConfigValueId("ask")},
			{Name: "Code", Value: acp.SessionConfigValueId("code")},
		},
	)

	update := acp.SessionUpdate{
		ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Id:           acp.SessionConfigId("mode"),
						Name:         "Mode",
						Category:     &modeCategory,
						CurrentValue: acp.SessionConfigValueId("code"),
						Options:      acp.SessionConfigSelectOptions{Ungrouped: &ungrouped},
					},
				},
			},
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil, nil)

	events := drainACPEvents(ch, 1)
	assert.Equal(t, "config_update", events[0].Type)
	require.NotNil(t, events[0].Config)
	assert.Equal(t, "mode", events[0].Config.ConfigID)
	assert.Equal(t, "code", events[0].Config.CurrentID)
	require.Len(t, events[0].Config.Options, 1)
	assert.Equal(t, "mode", events[0].Config.Options[0].Category)
	require.Len(t, events[0].Config.Options[0].Values, 2)
	assert.Equal(t, "ask", events[0].Config.Options[0].Values[0].ID)
	assert.Equal(t, "Ask", events[0].Config.Options[0].Values[0].Name)
	assert.Equal(t, "code", events[0].Config.Options[0].Values[1].ID)

	assertNoMoreACPEvents(ch, t)
}

func TestMapACPSessionUpdate_ConfigOptionUpdate_Mode_SameModesNoForward(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	agent := &model.Agent{ID: "test-same-modes", Backend: "acp-stdio"}
	entry := newACPConn(agent, "test-same-modes-sid")
	GetAgentCapabilityRegistry().UpdateModes(agent.ID, []ModeDef{
		{ID: "ask", Name: "Ask"},
		{ID: "code", Name: "Code"},
	})
	entry.SetCurrentModeID("ask")

	modeCategory := acp.SessionConfigOptionCategoryMode
	ungrouped := acp.SessionConfigSelectOptionsUngrouped(
		[]acp.SessionConfigSelectOption{
			{Name: "Ask", Value: acp.SessionConfigValueId("ask")},
			{Name: "Code", Value: acp.SessionConfigValueId("code")},
		},
	)

	update := acp.SessionUpdate{
		ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Id:           acp.SessionConfigId("mode"),
						Name:         "Mode",
						Category:     &modeCategory,
						CurrentValue: acp.SessionConfigValueId("code"),
						Options:      acp.SessionConfigSelectOptions{Ungrouped: &ungrouped},
					},
				},
			},
		},
	}

	mapACPSessionUpdate(update, ch, ctx, entry, nil)

	// Same available modes but currentModeId changed (ask → code) → config_update SSE forwarded
	events := drainACPEvents(ch, 1)
	assert.Equal(t, "config_update", events[0].Type)
	require.NotNil(t, events[0].Config)
	assert.Equal(t, "code", events[0].Config.CurrentID)

	// Session current mode should be updated — "code" is a valid mode
	assert.Equal(t, "code", entry.GetCurrentModeID())
}

func TestMapACPSessionUpdate_ConfigOptionUpdate_Mode_InvalidModeRejected(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	agent := &model.Agent{ID: "test-invalid-config-mode", Backend: "acp-stdio"}
	entry := newACPConn(agent, "test-invalid-config-mode-sid")
	GetAgentCapabilityRegistry().UpdateModes(agent.ID, []ModeDef{
		{ID: "ask", Name: "Ask"},
		{ID: "code", Name: "Code"},
	})
	entry.SetCurrentModeID("ask")

	modeCategory := acp.SessionConfigOptionCategoryMode
	ungrouped := acp.SessionConfigSelectOptionsUngrouped(
		[]acp.SessionConfigSelectOption{
			{Name: "Ask", Value: acp.SessionConfigValueId("ask")},
			{Name: "Code", Value: acp.SessionConfigValueId("code")},
		},
	)

	update := acp.SessionUpdate{
		ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Id:           acp.SessionConfigId("mode"),
						Name:         "Mode",
						Category:     &modeCategory,
						CurrentValue: acp.SessionConfigValueId("bypass_permissions"),
						Options:      acp.SessionConfigSelectOptions{Ungrouped: &ungrouped},
					},
				},
			},
		},
	}

	mapACPSessionUpdate(update, ch, ctx, entry, nil)

	// config_update SSE should be forwarded (currentModeId changed)
	events := drainACPEvents(ch, 1)
	assert.Equal(t, "config_update", events[0].Type)
	require.NotNil(t, events[0].Config)
	assert.Equal(t, "bypass_permissions", events[0].Config.CurrentID)

	// Session current mode should NOT be updated — "bypass_permissions" not in availableModes
	assert.Equal(t, "ask", entry.GetCurrentModeID())
}

func TestMapACPSessionUpdate_ConfigOptionUpdate_Mode_SameModesSameCurrentNoForward(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	agent := &model.Agent{ID: "test-same-same", Backend: "acp-stdio"}
	entry := newACPConn(agent, "test-same-same-sid")
	GetAgentCapabilityRegistry().UpdateModes(agent.ID, []ModeDef{
		{ID: "ask", Name: "Ask"},
		{ID: "code", Name: "Code"},
	})
	entry.SetCurrentModeID("code")

	modeCategory := acp.SessionConfigOptionCategoryMode
	ungrouped := acp.SessionConfigSelectOptionsUngrouped(
		[]acp.SessionConfigSelectOption{
			{Name: "Ask", Value: acp.SessionConfigValueId("ask")},
			{Name: "Code", Value: acp.SessionConfigValueId("code")},
		},
	)

	update := acp.SessionUpdate{
		ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Id:           acp.SessionConfigId("mode"),
						Name:         "Mode",
						Category:     &modeCategory,
						CurrentValue: acp.SessionConfigValueId("code"),
						Options:      acp.SessionConfigSelectOptions{Ungrouped: &ungrouped},
					},
				},
			},
		},
	}

	mapACPSessionUpdate(update, ch, ctx, entry, nil)

	// Same available modes AND same currentModeId → no SSE forwarded
	assertNoMoreACPEvents(ch, t)

	assert.Equal(t, "code", entry.GetCurrentModeID())
}

func TestMapACPSessionUpdate_ConfigOptionUpdate_Mode_NewModeForward(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	agent := &model.Agent{ID: "test-new-mode", Backend: "acp-stdio"}
	entry := newACPConn(agent, "test-new-mode-sid")
	GetAgentCapabilityRegistry().UpdateModes(agent.ID, []ModeDef{
		{ID: "ask", Name: "Ask"},
		{ID: "code", Name: "Code"},
	})
	entry.SetCurrentModeID("ask")

	modeCategory := acp.SessionConfigOptionCategoryMode
	ungrouped := acp.SessionConfigSelectOptionsUngrouped(
		[]acp.SessionConfigSelectOption{
			{Name: "Ask", Value: acp.SessionConfigValueId("ask")},
			{Name: "Code", Value: acp.SessionConfigValueId("code")},
			{Name: "Architect", Value: acp.SessionConfigValueId("architect")},
		},
	)

	update := acp.SessionUpdate{
		ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Id:           acp.SessionConfigId("mode"),
						Name:         "Mode",
						Category:     &modeCategory,
						CurrentValue: acp.SessionConfigValueId("architect"),
						Options:      acp.SessionConfigSelectOptions{Ungrouped: &ungrouped},
					},
				},
			},
		},
	}

	mapACPSessionUpdate(update, ch, ctx, entry, nil)

	// New mode "architect" → config_update SSE forwarded
	events := drainACPEvents(ch, 1)
	assert.Equal(t, "config_update", events[0].Type)
	require.NotNil(t, events[0].Config)

	// Session current mode should be updated with "architect"
	// "architect" is in the new available modes, so it's valid
	assert.Equal(t, "architect", entry.GetCurrentModeID())
	// Registry should have 3 modes now
	regModes := GetAgentCapabilityRegistry().Get(agent.ID)
	require.NotNil(t, regModes)
	require.Len(t, regModes.AvailableModes, 3)

	assertNoMoreACPEvents(ch, t)
}

func TestMapACPSessionUpdate_ConfigOptionUpdate_ThoughtLevel(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	thoughtCategory := acp.SessionConfigOptionCategoryThoughtLevel
	ungrouped := acp.SessionConfigSelectOptionsUngrouped(
		[]acp.SessionConfigSelectOption{
			{Name: "Low", Value: acp.SessionConfigValueId("low")},
			{Name: "High", Value: acp.SessionConfigValueId("high")},
		},
	)

	update := acp.SessionUpdate{
		ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Id:           acp.SessionConfigId("thinking"),
						Name:         "Thinking",
						Category:     &thoughtCategory,
						CurrentValue: acp.SessionConfigValueId("high"),
						Options:      acp.SessionConfigSelectOptions{Ungrouped: &ungrouped},
					},
				},
			},
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil, nil)

	events := drainACPEvents(ch, 1)
	assert.Equal(t, "thinking_effort_update", events[0].Type)
	require.NotNil(t, events[0].ThinkingEffort)
	assert.Equal(t, "high", events[0].ThinkingEffort.CurrentID)
	require.Len(t, events[0].ThinkingEffort.AvailableLevels, 2)
	assert.Equal(t, "low", events[0].ThinkingEffort.AvailableLevels[0].ID)
	assert.Equal(t, "Low", events[0].ThinkingEffort.AvailableLevels[0].Name)
	assert.Equal(t, "high", events[0].ThinkingEffort.AvailableLevels[1].ID)

	assertNoMoreACPEvents(ch, t)
}

func TestMapACPSessionUpdate_ConfigOptionUpdate_Model(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	modelCategory := acp.SessionConfigOptionCategoryModel
	ungrouped := acp.SessionConfigSelectOptionsUngrouped(
		[]acp.SessionConfigSelectOption{
			{Name: "Claude 3.5", Value: acp.SessionConfigValueId("claude-3.5")},
			{Name: "GPT-4o", Value: acp.SessionConfigValueId("gpt-4o")},
		},
	)

	update := acp.SessionUpdate{
		ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Id:           acp.SessionConfigId("model"),
						Name:         "Model",
						Category:     &modelCategory,
						CurrentValue: acp.SessionConfigValueId("claude-3.5"),
						Options:      acp.SessionConfigSelectOptions{Ungrouped: &ungrouped},
					},
				},
			},
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil, nil)

	events := drainACPEvents(ch, 1)
	assert.Equal(t, "model_list_update", events[0].Type)
	require.NotNil(t, events[0].ModelList)
	assert.Equal(t, "claude-3.5", events[0].ModelList.CurrentModelID)
	require.Len(t, events[0].ModelList.Models, 2)
	assert.Equal(t, "claude-3.5", events[0].ModelList.Models[0].ID)
	assert.Equal(t, "Claude 3.5", events[0].ModelList.Models[0].Name)

	assertNoMoreACPEvents(ch, t)
}

func TestMapACPSessionUpdate_ConfigOptionUpdate_MultipleCategories(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	modeCategory := acp.SessionConfigOptionCategoryMode
	thoughtCategory := acp.SessionConfigOptionCategoryThoughtLevel
	modeUngrouped := acp.SessionConfigSelectOptionsUngrouped(
		[]acp.SessionConfigSelectOption{
			{Name: "Code", Value: acp.SessionConfigValueId("code")},
		},
	)
	thoughtUngrouped := acp.SessionConfigSelectOptionsUngrouped(
		[]acp.SessionConfigSelectOption{
			{Name: "High", Value: acp.SessionConfigValueId("high")},
		},
	)

	update := acp.SessionUpdate{
		ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Id:           acp.SessionConfigId("mode"),
						Name:         "Mode",
						Category:     &modeCategory,
						CurrentValue: acp.SessionConfigValueId("code"),
						Options:      acp.SessionConfigSelectOptions{Ungrouped: &modeUngrouped},
					},
				},
				{
					Select: &acp.SessionConfigOptionSelect{
						Id:           acp.SessionConfigId("thinking"),
						Name:         "Thinking",
						Category:     &thoughtCategory,
						CurrentValue: acp.SessionConfigValueId("high"),
						Options:      acp.SessionConfigSelectOptions{Ungrouped: &thoughtUngrouped},
					},
				},
			},
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil, nil)

	// Should emit both config_update and thinking_effort_update
	events := drainACPEvents(ch, 2)
	assert.Equal(t, "config_update", events[0].Type)
	assert.Equal(t, "thinking_effort_update", events[1].Type)

	assertNoMoreACPEvents(ch, t)
}

func TestMapACPSessionUpdate_ConfigOptionUpdate_SkipNoSelect(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	update := acp.SessionUpdate{
		ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{
			ConfigOptions: []acp.SessionConfigOption{
				{}, // Select is nil — should be skipped
			},
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil, nil)

	assertNoMoreACPEvents(ch, t) // no events when Select is nil
}

func TestMapACPSessionUpdate_ConfigOptionUpdate_SkipNoCategory(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	update := acp.SessionUpdate{
		ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Id:           acp.SessionConfigId("unknown"),
						Name:         "Unknown",
						Category:     nil, // no category — should be skipped
						CurrentValue: acp.SessionConfigValueId("val"),
					},
				},
			},
		},
	}

	mapACPSessionUpdate(update, ch, ctx, nil, nil)

	assertNoMoreACPEvents(ch, t)
}

// --- mapACPSessionUpdate Empty/SessionInfoUpdate tests ---

func TestMapACPSessionUpdate_Empty(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	update := acp.SessionUpdate{} // all nil fields

	mapACPSessionUpdate(update, ch, ctx, nil, nil)

	assertNoMoreACPEvents(ch, t) // no events for empty update
}

func TestMapACPSessionUpdate_SessionInfoUpdate(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	ctx := context.Background()

	update := acp.SessionUpdate{
		SessionInfoUpdate: &acp.SessionSessionInfoUpdate{},
	}

	mapACPSessionUpdate(update, ch, ctx, nil, nil)

	assertNoMoreACPEvents(ch, t) // SessionInfoUpdate emits no stream events
}

// --- extractACPToolOutput tests ---

func TestExtractACPToolOutput_String(t *testing.T) {
	assert.Equal(t, "hello", extractACPToolOutput("hello"))
}

func TestExtractACPToolOutput_Bool(t *testing.T) {
	assert.Equal(t, "true", extractACPToolOutput(true))
	assert.Equal(t, "false", extractACPToolOutput(false))
}

func TestExtractACPToolOutput_Number(t *testing.T) {
	assert.Equal(t, "42", extractACPToolOutput(42))
	assert.Equal(t, "3.14", extractACPToolOutput(3.14))
}

func TestExtractACPToolOutput_Map_ResultKey(t *testing.T) {
	result := extractACPToolOutput(map[string]any{"result": "file contents"})
	assert.Equal(t, "file contents", result)
}

func TestExtractACPToolOutput_Map_OutputKey(t *testing.T) {
	result := extractACPToolOutput(map[string]any{"output": "command output"})
	assert.Equal(t, "command output", result)
}

func TestExtractACPToolOutput_Map_ContentKey(t *testing.T) {
	result := extractACPToolOutput(map[string]any{"content": "file content"})
	assert.Equal(t, "file content", result)
}

func TestExtractACPToolOutput_Map_TextKey(t *testing.T) {
	result := extractACPToolOutput(map[string]any{"text": "plain text"})
	assert.Equal(t, "plain text", result)
}

func TestExtractACPToolOutput_Map_MessageKey(t *testing.T) {
	result := extractACPToolOutput(map[string]any{"message": "success"})
	assert.Equal(t, "success", result)
}

func TestExtractACPToolOutput_Map_StdoutWithStderr(t *testing.T) {
	result := extractACPToolOutput(map[string]any{
		"stdout": "command output",
		"stderr": "some warnings",
	})
	assert.Equal(t, "command output\nsome warnings", result)
}

func TestExtractACPToolOutput_Map_StdoutNoStderr(t *testing.T) {
	result := extractACPToolOutput(map[string]any{"stdout": "output only"})
	assert.Equal(t, "output only", result)
}

func TestExtractACPToolOutput_Map_ResultPriorityOverOutput(t *testing.T) {
	result := extractACPToolOutput(map[string]any{
		"result": "from result key",
		"output": "from output key",
	})
	assert.Equal(t, "from result key", result)
}

func TestExtractACPToolOutput_Map_ErrorKey(t *testing.T) {
	result := extractACPToolOutput(map[string]any{"error": "permission denied"})
	assert.Equal(t, "permission denied", result)
}

func TestExtractACPToolOutput_Map_ErrorKeyWithMessage(t *testing.T) {
	result := extractACPToolOutput(map[string]any{
		"error": map[string]any{"message": "not found"},
	})
	assert.Equal(t, "not found", result)
}

func TestExtractACPToolOutput_Map_NestedValue(t *testing.T) {
	result := extractACPToolOutput(map[string]any{
		"result": map[string]any{"key": "value"},
	})
	assert.Contains(t, result, `"key"`)
	assert.Contains(t, result, `"value"`)
}

func TestExtractACPToolOutput_Map_EmptyValue(t *testing.T) {
	// Empty string values in priority keys should be skipped, falling to next key
	result := extractACPToolOutput(map[string]any{
		"result": "",
		"output": "fallback",
	})
	assert.Equal(t, "fallback", result)
}

func TestExtractACPToolOutput_Map_NoKnownKey(t *testing.T) {
	result := extractACPToolOutput(map[string]any{
		"custom_field": "custom value",
	})
	// Falls back to pretty-printed JSON of entire object
	assert.Contains(t, result, `"custom_field"`)
}

func TestExtractACPToolOutput_Array_AllStrings(t *testing.T) {
	result := extractACPToolOutput([]any{"line1", "line2", "line3"})
	assert.Equal(t, "line1\nline2\nline3", result)
}

func TestExtractACPToolOutput_Array_MixedTypes(t *testing.T) {
	result := extractACPToolOutput([]any{"text", 42})
	// Non-string elements → pretty-print as JSON
	assert.Contains(t, result, "text")
}

// Duplicate removed: TestExtractACPToolOutput_Array_Empty is in acp_events_test.go

func TestExtractACPToolOutput_NilValue(t *testing.T) {
	// nil interface → json.MarshalIndent produces "null"
	result := extractACPToolOutput(nil)
	assert.Equal(t, "null", result)
}

// --- truncateToolOutput tests ---

func TestTruncateToolOutput_Short(t *testing.T) {
	assert.Equal(t, "hello", truncateToolOutput("hello"))
}

func TestTruncateToolOutput_ExactLimit(t *testing.T) {
	s := strings.Repeat("x", maxToolOutputBytes)
	assert.Equal(t, s, truncateToolOutput(s))
}

func TestTruncateToolOutput_OverLimit(t *testing.T) {
	originalLen := maxToolOutputBytes + 100
	s := strings.Repeat("x", originalLen)
	result := truncateToolOutput(s)
	// First part is exactly maxToolOutputBytes chars, then newline + truncation marker
	prefix := result[:maxToolOutputBytes]
	assert.Equal(t, strings.Repeat("x", maxToolOutputBytes), prefix)
	assert.Contains(t, result, "\n[truncated:")
	assert.Contains(t, result, fmt.Sprintf("original %d bytes", originalLen))
}

func TestTruncateToolOutput_Empty(t *testing.T) {
	assert.Equal(t, "", truncateToolOutput(""))
}

// --- mapACPToolCall execute-kind fallback to title ---

func TestMapACPToolCall_ExecuteKindFallbackToTitle(t *testing.T) {
	tc := acp.SessionUpdateToolCall{
		ToolCallId: acp.ToolCallId("exec-1"),
		Title:      "echo hello",
		Kind:       acp.ToolKindExecute,
		// No RawInput, no Content
	}
	event := mapACPToolCall(tc, "")
	require.NotNil(t, event.Tool)
	assert.Contains(t, event.Tool.Input, "command")
	assert.Contains(t, event.Tool.Input, "echo hello")
}

func TestMapACPToolCall_ExecuteKindNoTitle(t *testing.T) {
	tc := acp.SessionUpdateToolCall{
		ToolCallId: acp.ToolCallId("exec-2"),
		Title:      "",
		Kind:       acp.ToolKindExecute,
	}
	event := mapACPToolCall(tc, "")
	require.NotNil(t, event.Tool)
	assert.Empty(t, event.Tool.Input)
}

func TestMapACPToolCall_NonExecuteKindNoFallback(t *testing.T) {
	tc := acp.SessionUpdateToolCall{
		ToolCallId: acp.ToolCallId("read-1"),
		Title:      "main.go",
		Kind:       acp.ToolKindRead,
		// No RawInput, no Content
	}
	event := mapACPToolCall(tc, "")
	require.NotNil(t, event.Tool)
	// Read kind should NOT fallback to title as command
	assert.NotContains(t, event.Tool.Input, "command")
}

// --- mapACPToolCallUpdate execute-kind fallback ---

func TestMapACPToolCallUpdate_ExecuteKindFallbackToTitle(t *testing.T) {
	title := "ls /tmp"
	kind := acp.ToolKindExecute
	status := acp.ToolCallStatusCompleted
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("exec-upd-1"),
		Title:      &title,
		Kind:       &kind,
		Status:     &status,
	}
	event := mapACPToolCallUpdate(tcu, "")
	require.NotNil(t, event.Tool)
	assert.Contains(t, event.Tool.Input, "command")
	assert.Contains(t, event.Tool.Input, "ls /tmp")
}

// --- extractInputFromContentUpdate ---

func TestExtractInputFromContentUpdate_Terminal(t *testing.T) {
	title := "npm test"
	tcu := acp.SessionToolCallUpdate{
		Title: &title,
		Content: []acp.ToolCallContent{
			{Terminal: &acp.ToolCallContentTerminal{TerminalId: "term-1"}},
		},
	}
	input := extractInputFromContentUpdate(tcu)
	assert.NotNil(t, input)
	assert.Equal(t, "npm test", input["command"])
}

func TestExtractInputFromContentUpdate_Text(t *testing.T) {
	textStr := "Running tests..."
	tcu := acp.SessionToolCallUpdate{
		Content: []acp.ToolCallContent{
			{
				Content: &acp.ToolCallContentContent{
					Content: acp.TextBlock(textStr),
				},
			},
		},
	}
	input := extractInputFromContentUpdate(tcu)
	assert.NotNil(t, input)
	assert.Equal(t, "Running tests...", input["description"])
}

func TestExtractInputFromContentUpdate_Empty(t *testing.T) {
	tcu := acp.SessionToolCallUpdate{
		Content: []acp.ToolCallContent{},
	}
	input := extractInputFromContentUpdate(tcu)
	assert.Nil(t, input)
}

func TestExtractInputFromContentUpdate_NilTitle(t *testing.T) {
	tcu := acp.SessionToolCallUpdate{
		Title: nil,
		Content: []acp.ToolCallContent{
			{Terminal: &acp.ToolCallContentTerminal{TerminalId: "term-2"}},
		},
	}
	input := extractInputFromContentUpdate(tcu)
	// Terminal without title → no command key
	assert.NotNil(t, input)
	_, hasCommand := input["command"]
	assert.False(t, hasCommand)
}

// --- modeStateFromConfigState ---

func TestModeStateFromConfigState_Nil(t *testing.T) {
	assert.Nil(t, modeStateFromConfigState(nil))
}

func TestModeStateFromConfigState_NoModeCategory(t *testing.T) {
	cs := &ConfigOptionState{
		ConfigID:  "thinking_effort",
		CurrentID: "high",
		Options: []ConfigOptionDef{
			{ID: "thinking_effort", Category: "thought_level", Values: []ConfigOptionValue{{ID: "high", Name: "High"}}},
		},
	}
	assert.Nil(t, modeStateFromConfigState(cs))
}

func TestModeStateFromConfigState_ValidModeOptions(t *testing.T) {
	cs := &ConfigOptionState{
		ConfigID:  "mode",
		CurrentID: "code",
		Options: []ConfigOptionDef{
			{ID: "mode", Category: "mode", Values: []ConfigOptionValue{
				{ID: "code", Name: "Code"},
				{ID: "ask", Name: "Ask"},
				{ID: "architect", Name: "Architect"},
			}},
		},
	}
	ms := modeStateFromConfigState(cs)
	require.NotNil(t, ms)
	assert.Equal(t, "code", ms.CurrentModeID)
	assert.Len(t, ms.AvailableModes, 3)
	assert.Equal(t, "code", ms.AvailableModes[0].ID)
	assert.Equal(t, "Ask", ms.AvailableModes[1].Name)
}

func TestModeStateFromConfigState_EmptyValuesNoCurrentID(t *testing.T) {
	cs := &ConfigOptionState{
		ConfigID:  "mode",
		CurrentID: "",
		Options: []ConfigOptionDef{
			{ID: "mode", Category: "mode", Values: []ConfigOptionValue{}},
		},
	}
	assert.Nil(t, modeStateFromConfigState(cs))
}

// --- extractACP*FromResume ---

func TestExtractACPModeStateFromResume_Nil(t *testing.T) {
	assert.Nil(t, extractACPModeStateFromResume(nil))
}

func TestExtractACPModeStateFromResume_WithModes(t *testing.T) {
	modeID := acp.SessionModeId("code")
	resumeResp := &acp.ResumeSessionResponse{
		Modes: &acp.SessionModeState{
			CurrentModeId: modeID,
			AvailableModes: []acp.SessionMode{
				{Id: modeID, Name: "Code"},
				{Id: acp.SessionModeId("ask"), Name: "Ask"},
			},
		},
	}
	ms := extractACPModeStateFromResume(resumeResp)
	require.NotNil(t, ms)
	assert.Equal(t, "code", ms.CurrentModeID)
	assert.Len(t, ms.AvailableModes, 2)
}

func TestExtractACPConfigOptionsFromResume_Nil(t *testing.T) {
	assert.Nil(t, extractACPConfigOptionsFromResume(nil))
}

func TestExtractACPThinkingEffortFromResume_Nil(t *testing.T) {
	assert.Nil(t, extractACPThinkingEffortFromResume(nil))
}

func TestExtractACPModelListFromResume_Nil(t *testing.T) {
	assert.Nil(t, extractACPModelListFromResume(nil))
}

// --- isACPPeerDisconnected ---

func TestIsACPPeerDisconnected_RequestErrorWithPeerDisconnect(t *testing.T) {
	err := &acp.RequestError{
		Code: -32603,
		Data: map[string]any{"error": "peer disconnected during prompt"},
	}
	assert.True(t, isACPPeerDisconnected(err))
}

func TestIsACPPeerDisconnected_RequestErrorBrokenPipe(t *testing.T) {
	err := &acp.RequestError{
		Code: -32603,
		Data: map[string]any{"error": "broken pipe on write"},
	}
	assert.True(t, isACPPeerDisconnected(err))
}

func TestIsACPPeerDisconnected_RequestErrorOtherCode(t *testing.T) {
	err := &acp.RequestError{
		Code: -32000,
		Data: map[string]any{"error": "peer disconnected"},
	}
	assert.False(t, isACPPeerDisconnected(err))
}

func TestIsACPPeerDisconnected_RequestErrorNoPeerMsg(t *testing.T) {
	err := &acp.RequestError{
		Code: -32603,
		Data: map[string]any{"error": "something else"},
	}
	assert.False(t, isACPPeerDisconnected(err))
}

func TestIsACPPeerDisconnected_NonRequestError(t *testing.T) {
	err := fmt.Errorf("peer disconnected unexpectedly")
	assert.True(t, isACPPeerDisconnected(err))
}

func TestIsACPPeerDisconnected_NonPeerError(t *testing.T) {
	err := fmt.Errorf("timeout exceeded")
	assert.False(t, isACPPeerDisconnected(err))
}

// --- isUnknownConfigOption ---

func TestIsUnknownConfigOption_RequestErrorWithDetails(t *testing.T) {
	err := &acp.RequestError{
		Code: -32603,
		Data: map[string]any{"details": "Unknown config option: thinkingEffort"},
	}
	assert.True(t, isUnknownConfigOption(err))
}

func TestIsUnknownConfigOption_RequestErrorNoDetails(t *testing.T) {
	err := &acp.RequestError{
		Code: -32603,
		Data: map[string]any{"error": "peer disconnected before response"},
	}
	assert.False(t, isUnknownConfigOption(err))
}

func TestIsUnknownConfigOption_PlainError(t *testing.T) {
	err := fmt.Errorf("Unknown config option: thinkingEffort")
	assert.True(t, isUnknownConfigOption(err))
}

func TestIsUnknownConfigOption_UnrelatedError(t *testing.T) {
	err := fmt.Errorf("some other error")
	assert.False(t, isUnknownConfigOption(err))
}

// --- IsACPResourceNotFound ---

func TestIsACPResourceNotFound_RequestError(t *testing.T) {
	err := &acp.RequestError{
		Code:    -32002,
		Message: "Resource not found: abc-123",
		Data:    map[string]any{"uri": "abc-123"},
	}
	assert.True(t, IsACPResourceNotFound(err))
}

func TestIsACPResourceNotFound_WrappedRequestError(t *testing.T) {
	innerErr := &acp.RequestError{
		Code:    -32002,
		Message: "Resource not found: abc-123",
	}
	err := fmt.Errorf("acp: session/load: %w", innerErr)
	assert.True(t, IsACPResourceNotFound(err))
}

func TestIsACPResourceNotFound_PlainError(t *testing.T) {
	err := fmt.Errorf("Resource not found: some-id")
	assert.True(t, IsACPResourceNotFound(err))
}

func TestIsACPResourceNotFound_OtherCode(t *testing.T) {
	err := &acp.RequestError{
		Code:    -32603,
		Message: "Internal error",
	}
	assert.False(t, IsACPResourceNotFound(err))
}

func TestIsACPResourceNotFound_UnrelatedError(t *testing.T) {
	err := fmt.Errorf("some other error")
	assert.False(t, IsACPResourceNotFound(err))
}

// --- extractModeStateFromModes ---

func TestExtractModeStateFromModes_Nil(t *testing.T) {
	assert.Nil(t, extractModeStateFromModes(nil))
}

func TestExtractModeStateFromModes_Empty(t *testing.T) {
	modes := &acp.SessionModeState{}
	assert.Nil(t, extractModeStateFromModes(modes))
}

func TestExtractModeStateFromModes_CurrentOnly(t *testing.T) {
	modes := &acp.SessionModeState{
		CurrentModeId: acp.SessionModeId("code"),
	}
	ms := extractModeStateFromModes(modes)
	require.NotNil(t, ms)
	assert.Equal(t, "code", ms.CurrentModeID)
	assert.Empty(t, ms.AvailableModes)
}

// --- extractConfigOptionsFromOpts ---

func TestExtractConfigOptionsFromOpts_Empty(t *testing.T) {
	assert.Nil(t, extractConfigOptionsFromOpts(nil))
	assert.Nil(t, extractConfigOptionsFromOpts([]acp.SessionConfigOption{}))
}

// --- extractThinkingEffortFromOpts ---

func TestExtractThinkingEffortFromOpts_NoThoughtLevelCategory(t *testing.T) {
	cat := acp.SessionConfigOptionCategoryMode
	opts := []acp.SessionConfigOption{
		{Select: &acp.SessionConfigOptionSelect{Category: &cat}},
	}
	assert.Nil(t, extractThinkingEffortFromOpts(opts))
}

// --- extractModelListFromOpts ---

func TestExtractModelListFromOpts_NoModelCategory(t *testing.T) {
	cat := acp.SessionConfigOptionCategoryMode
	opts := []acp.SessionConfigOption{
		{Select: &acp.SessionConfigOptionSelect{Category: &cat}},
	}
	assert.Nil(t, extractModelListFromOpts(opts))
}

// --- ACP test helpers ---

// drainACPEvents reads exactly count non-raw_output events from ch, skipping raw_output events.
// It reads up to count*2 events to account for interleaved raw_output events.
func drainACPEvents(ch chan StreamEvent, count int) []StreamEvent {
	events := make([]StreamEvent, 0, count)
	maxReads := count * 3 // allow for interleaved raw_output events
	for range maxReads {
		select {
		case event := <-ch:
			if event.Type == "raw_output" {
				continue // skip debug raw output events
			}
			events = append(events, event)
			if len(events) == count {
				return events
			}
		default:
			return events
		}
	}
	return events
}

// assertNoMoreACPEvents fails the test if there are pending non-raw_output events on ch.
func assertNoMoreACPEvents(ch chan StreamEvent, t *testing.T) {
	t.Helper()
	for {
		select {
		case event := <-ch:
			if event.Type == "raw_output" {
				continue // skip debug raw output events
			}
			t.Fatalf("expected no more events on channel, got %q", event.Type)
		default:
			return
		}
	}
}
