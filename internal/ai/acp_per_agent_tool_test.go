package ai

import (
	"encoding/json"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
)

func TestParseClaudeACPToolCall(t *testing.T) {
	t.Run("meta.claudeCode.toolName preferred", func(t *testing.T) {
		tc := acp.SessionUpdateToolCall{
			Meta:       map[string]any{"claudeCode": map[string]any{"toolName": "Edit"}},
			ToolCallId: "call_001",
			Title:      "Edit file",
			Kind:       acp.ToolKindEdit,
			RawInput:   map[string]any{"file_path": "/tmp/a.txt", "old_string": "foo", "new_string": "bar"},
		}
		result := parseClaudeACPToolCall(tc)
		assert.Equal(t, "Edit", result.Name)
		assert.Equal(t, "call_001", result.ID)
		assert.False(t, result.Done)
		assert.Contains(t, result.Input, "file_path")
		assert.Contains(t, result.Input, "old_string")
	})

	t.Run("fallback to extractToolName when no meta", func(t *testing.T) {
		tc := acp.SessionUpdateToolCall{
			ToolCallId: "call_002",
			Title:      "Read",
			Kind:       acp.ToolKindRead,
		}
		result := parseClaudeACPToolCall(tc)
		assert.Equal(t, "Read", result.Name)
	})

	t.Run("execute kind fallback to title as command", func(t *testing.T) {
		tc := acp.SessionUpdateToolCall{
			ToolCallId: "call_003",
			Title:      "ls -la",
			Kind:       acp.ToolKindExecute,
		}
		result := parseClaudeACPToolCall(tc)
		assert.Equal(t, "Bash", result.Name)
		assert.Contains(t, result.Input, "command")
	})

	t.Run("locations and title fallback", func(t *testing.T) {
		tc := acp.SessionUpdateToolCall{
			ToolCallId: "call_004",
			Title:      "main.go",
			Kind:       acp.ToolKindRead,
			Locations:  []acp.ToolCallLocation{{Path: "/src/main.go"}},
		}
		result := parseClaudeACPToolCall(tc)
		assert.Equal(t, "Read", result.Name)
		assert.Contains(t, result.Input, "file_path")
	})
}

func TestParseClaudeACPToolCallUpdate(t *testing.T) {
	completed := acp.ToolCallStatusCompleted
	failed := acp.ToolCallStatusFailed
	inProgress := acp.ToolCallStatusInProgress

	t.Run("completed with meta toolName", func(t *testing.T) {
		title := "Bash"
		tcu := acp.SessionToolCallUpdate{
			Meta:       map[string]any{"claudeCode": map[string]any{"toolName": "Bash"}},
			ToolCallId: "call_001",
			Title:      &title,
			Status:     &completed,
			RawOutput:  "command output",
		}
		result := parseClaudeACPToolCallUpdate(tcu)
		assert.True(t, result.Done)
		assert.Equal(t, "success", result.Status)
		assert.Equal(t, "Bash", result.Name)
		assert.Contains(t, result.Output, "command output")
	})

	t.Run("failed", func(t *testing.T) {
		tcu := acp.SessionToolCallUpdate{
			ToolCallId: "call_002",
			Status:     &failed,
			RawOutput:  "error message",
		}
		result := parseClaudeACPToolCallUpdate(tcu)
		assert.True(t, result.Done)
		assert.Equal(t, "error", result.Status)
	})

	t.Run("in progress - no output extraction", func(t *testing.T) {
		title := "Read"
		tcu := acp.SessionToolCallUpdate{
			ToolCallId: "call_003",
			Title:      &title,
			Status:     &inProgress,
			RawInput:   map[string]any{"file_path": "/tmp/test.go"},
		}
		result := parseClaudeACPToolCallUpdate(tcu)
		assert.False(t, result.Done)
		assert.Empty(t, result.Output)
		assert.Contains(t, result.Input, "file_path")
	})
}

func TestParseCodeBuddyACPToolCall(t *testing.T) {
	t.Run("meta flat key preferred", func(t *testing.T) {
		tc := acp.SessionUpdateToolCall{
			Meta:       map[string]any{"codebuddy.ai/toolName": "Bash"},
			ToolCallId: "019e95ac",
			Title:      "Bash",
			Kind:       acp.ToolKindOther,
			RawInput:   map[string]any{},
		}
		result := parseCodeBuddyACPToolCall(tc)
		assert.Equal(t, "Bash", result.Name)
	})

	t.Run("fallback to extractToolName when no meta", func(t *testing.T) {
		tc := acp.SessionUpdateToolCall{
			ToolCallId: "019e95ad",
			Title:      "Read",
			Kind:       acp.ToolKindRead,
		}
		result := parseCodeBuddyACPToolCall(tc)
		assert.Equal(t, "Read", result.Name)
	})
}

func TestParseOpenCodeACPToolCall(t *testing.T) {
	t.Run("camelCase rawInput normalized", func(t *testing.T) {
		tc := acp.SessionUpdateToolCall{
			ToolCallId: "call_00_xxx",
			Title:      "read",
			Kind:       acp.ToolKindRead,
			RawInput:   map[string]any{"filePath": "/home/user/file.go"},
		}
		result := parseOpenCodeACPToolCall(tc)
		assert.Equal(t, "Read", result.Name)
		// filePath should be normalized to file_path
		var input map[string]any
		_ = json.Unmarshal([]byte(result.Input), &input)
		assert.Contains(t, input, "file_path")
		assert.NotContains(t, input, "filePath")
	})
}

func TestParseKimiACPToolCall(t *testing.T) {
	t.Run("read_file prefix from toolCallId", func(t *testing.T) {
		tc := acp.SessionUpdateToolCall{
			ToolCallId: "read_file-1780629348181-1",
			Kind:       acp.ToolKindRead,
			Locations:  []acp.ToolCallLocation{{Path: "/home/user/README.md"}},
			Title:      "README.md",
		}
		result := parseKimiACPToolCall(tc)
		assert.Equal(t, "Read", result.Name)
		assert.Contains(t, result.Input, "file_path")
	})

	t.Run("glob prefix from toolCallId", func(t *testing.T) {
		tc := acp.SessionUpdateToolCall{
			ToolCallId: "glob-1780629348181-2",
			Kind:       acp.ToolKindSearch,
			Title:      "**/*.go",
		}
		result := parseKimiACPToolCall(tc)
		assert.Equal(t, "Glob", result.Name)
		assert.Contains(t, result.Input, "pattern")
	})

	t.Run("list_directory prefix from toolCallId", func(t *testing.T) {
		tc := acp.SessionUpdateToolCall{
			ToolCallId: "list_directory-1780629348181-3",
			Kind:       acp.ToolKindSearch,
			Title:      "/home/user",
		}
		result := parseKimiACPToolCall(tc)
		assert.Equal(t, "LS", result.Name)
		assert.Contains(t, result.Input, "path")
	})
}

func TestParseKimiACPToolCallUpdate(t *testing.T) {
	completed := acp.ToolCallStatusCompleted

	t.Run("completed with content", func(t *testing.T) {
		tcu := acp.SessionToolCallUpdate{
			ToolCallId: "read_file-1780629348181-1",
			Status:     &completed,
			Content: []acp.ToolCallContent{
				acp.ToolContent(acp.TextBlock("file contents here")),
			},
		}
		result := parseKimiACPToolCallUpdate(tcu)
		assert.True(t, result.Done)
		assert.Equal(t, "success", result.Status)
		assert.Contains(t, result.Output, "file contents here")
	})

	t.Run("orphan completed update without prior tool_call", func(t *testing.T) {
		tcu := acp.SessionToolCallUpdate{
			ToolCallId: "orphan_call_001",
			Status:     &completed,
			Content: []acp.ToolCallContent{
				acp.ToolContent(acp.TextBlock("result text")),
			},
		}
		result := parseKimiACPToolCallUpdate(tcu)
		assert.True(t, result.Done)
		assert.Equal(t, "orphan_call_001", result.ID)
		assert.Contains(t, result.Output, "result text")
	})
}

func TestParseCodexACPToolCall(t *testing.T) {
	completed := acp.ToolCallStatusCompleted

	t.Run("subagent start via _meta.codex.subagent maps to Agent", func(t *testing.T) {
		tc := acp.SessionUpdateToolCall{
			Meta: map[string]any{"codex": map[string]any{"subagent": map[string]any{
				"activity": "started", "path": "/root/codebase_research", "threadId": "thr-01",
			}}},
			ToolCallId: "call_00_start1",
			Kind:       acp.ToolKindOther,
			Title:      "Start subagent codebase_research",
			Status:     acp.ToolCallStatusInProgress,
			RawInput:   map[string]any{"activityKind": "started", "agentPath": "/root/codebase_research", "agentThreadId": "thr-01"},
		}
		result := parseCodexACPToolCall(tc)
		assert.Equal(t, "Agent", result.Name)
		assert.Equal(t, "call_00_start1", result.ID)
		// agent_thread_id (the correlation anchor) must survive normalization.
		var input map[string]any
		assert.NoError(t, json.Unmarshal([]byte(result.Input), &input))
		assert.Equal(t, "thr-01", input["agentThreadId"])
		assert.Equal(t, "/root/codebase_research", input["agentPath"])
	})

	t.Run("complete subagent with meta maps to Agent", func(t *testing.T) {
		tc := acp.SessionUpdateToolCall{
			Meta: map[string]any{"codex": map[string]any{"subagent": map[string]any{
				"activity": "completed", "path": "/root/codebase_research", "threadId": "thr-01",
			}}},
			ToolCallId: "subagent-completed-thr-01",
			Kind:       acp.ToolKindOther,
			Title:      "Complete subagent codebase_research",
			RawInput:   map[string]any{"activityKind": "completed", "agentPath": "/root/codebase_research", "agentThreadId": "thr-01"},
		}
		result := parseCodexACPToolCall(tc)
		assert.Equal(t, "Agent", result.Name)
	})

	t.Run("collaboration wait via _meta.codex.collaboration stays wait", func(t *testing.T) {
		tc := acp.SessionUpdateToolCall{
			Meta: map[string]any{"codex": map[string]any{"collaboration": map[string]any{
				"tool": "wait", "senderThreadId": "root-1", "receiverThreadIds": []any{},
			}}},
			ToolCallId: "call_00_wait1",
			Kind:       acp.ToolKindOther,
			Title:      "wait",
			Status:     acp.ToolCallStatusInProgress,
			RawInput:   map[string]any{"agentsStates": map[string]any{}, "senderThreadId": "root-1", "status": "inProgress"},
		}
		result := parseCodexACPToolCall(tc)
		assert.Equal(t, "wait", result.Name)
		assert.Equal(t, "call_00_wait1", result.ID)
	})

	t.Run("ordinary execute falls back to Bash", func(t *testing.T) {
		tc := acp.SessionUpdateToolCall{
			ToolCallId: "call_00_bash1",
			Kind:       acp.ToolKindExecute,
			Title:      "ls -la",
			RawInput:   map[string]any{"command": "ls -la"},
		}
		result := parseCodexACPToolCall(tc)
		assert.Equal(t, "Bash", result.Name)
	})

	t.Run("ordinary read falls back to Read", func(t *testing.T) {
		tc := acp.SessionUpdateToolCall{
			ToolCallId: "call_00_read1",
			Kind:       acp.ToolKindRead,
			Title:      "Read file '/x/main.go'",
			RawInput:   map[string]any{"filePath": "/x/main.go"},
		}
		result := parseCodexACPToolCall(tc)
		assert.Equal(t, "Read", result.Name)
	})

	t.Run("completed update terminal status mapped", func(t *testing.T) {
		// tool_call_update for "Complete subagent" with meta activity=completed.
		title := "Complete subagent probe"
		tcu := acp.SessionToolCallUpdate{
			Meta: map[string]any{"codex": map[string]any{"subagent": map[string]any{
				"activity": "completed", "path": "/root/probe", "threadId": "thr-02",
			}}},
			ToolCallId: "subagent-completed-thr-02",
			Title:      &title,
			Status:     &completed,
			RawInput:   map[string]any{"activityKind": "completed", "agentPath": "/root/probe", "agentThreadId": "thr-02"},
		}
		result := parseCodexACPToolCallUpdate(tcu)
		assert.True(t, result.Done)
		assert.Equal(t, "success", result.Status)
		assert.Equal(t, "Agent", result.Name)
	})

	t.Run("collaboration wait update keeps name wait when no title", func(t *testing.T) {
		tcu := acp.SessionToolCallUpdate{
			Meta: map[string]any{"codex": map[string]any{"collaboration": map[string]any{
				"tool": "wait", "senderThreadId": "root-1", "receiverThreadIds": []any{},
			}}},
			ToolCallId: "call_00_wait1",
			Status:     &completed,
		}
		result := parseCodexACPToolCallUpdate(tcu)
		assert.Equal(t, "wait", result.Name)
	})
}
