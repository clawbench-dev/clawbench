package codex

import (
	"encoding/json"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
)

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
		result := ParseACPToolCall(tc)
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
		result := ParseACPToolCall(tc)
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
		result := ParseACPToolCall(tc)
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
		result := ParseACPToolCall(tc)
		assert.Equal(t, "Bash", result.Name)
	})

	t.Run("ordinary read falls back to Read", func(t *testing.T) {
		tc := acp.SessionUpdateToolCall{
			ToolCallId: "call_00_read1",
			Kind:       acp.ToolKindRead,
			Title:      "Read file '/x/main.go'",
			RawInput:   map[string]any{"filePath": "/x/main.go"},
		}
		result := ParseACPToolCall(tc)
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
		result := ParseACPToolCallUpdate(tcu)
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
		result := ParseACPToolCallUpdate(tcu)
		assert.Equal(t, "wait", result.Name)
	})
}
