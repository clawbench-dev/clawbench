package ai

import (
	"context"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- OpenCode v2 sub-agent attribution (see acp_opencode_children.go) ---

func openCodeChildMeta(childID, parentID, title string) map[string]any {
	return map[string]any{
		metaKeyOpenCodeChildSession: map[string]any{
			"id":       childID,
			"parentID": parentID,
			"depth":    float64(1),
			"title":    title,
		},
	}
}

func sendOpenCodeUpdate(t *testing.T, c *ClawBenchACPClient, sessionID string, update acp.SessionUpdate) {
	t.Helper()
	require.NoError(t, c.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: acp.SessionId(sessionID),
		Update:    update,
	}))
}

// drainEvents returns everything currently buffered in the channel.
func drainOpenCodeAttributionEvents(ch chan StreamEvent) []StreamEvent {
	var out []StreamEvent
	for {
		select {
		case e := <-ch:
			out = append(out, e)
		default:
			return out
		}
	}
}

func TestOpenCodeChildAttribution_ByTitle(t *testing.T) {
	c := NewClawBenchACPClient()
	ch := make(chan StreamEvent, 64)
	c.RegisterSession("ses_root", ch)

	status := acp.ToolCallStatusInProgress
	title := "subagent"
	sendOpenCodeUpdate(t, c, "ses_root", acp.SessionUpdate{
		ToolCallUpdate: &acp.SessionToolCallUpdate{
			ToolCallId:    "call_parent",
			Title:         &title,
			Status:        &status,
			RawInput:      map[string]any{"agent": "general", "description": "probe", "prompt": "do it"},
			SessionUpdate: "tool_call_update",
		},
	})

	sendOpenCodeUpdate(t, c, "ses_root", acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			Meta:          openCodeChildMeta("ses_child", "ses_root", "probe"),
			Content:       acp.ContentBlock{Text: &acp.ContentBlockText{Text: "child text", Type: "text"}},
			SessionUpdate: "agent_message_chunk",
		},
	})

	var got *StreamEvent
	for _, e := range drainOpenCodeAttributionEvents(ch) {
		if e.Type == "content" {
			ev := e
			got = &ev
		}
	}
	require.NotNil(t, got, "child content should be forwarded once attributed")
	assert.Equal(t, "child text", got.Content)
	assert.Equal(t, "call_parent", got.ParentToolCallID,
		"child frame must be stamped with the spawning subagent tool call")
}

func TestOpenCodeChildAttribution_ByChildSessionID(t *testing.T) {
	c := NewClawBenchACPClient()
	ch := make(chan StreamEvent, 64)
	c.RegisterSession("ses_root", ch)

	// The tool result carries the exact child session id; the child title does
	// NOT match the description, so only the session-id link can attribute it.
	status := acp.ToolCallStatusCompleted
	title := "subagent"
	sendOpenCodeUpdate(t, c, "ses_root", acp.SessionUpdate{
		ToolCallUpdate: &acp.SessionToolCallUpdate{
			ToolCallId:    "call_parent",
			Title:         &title,
			Status:        &status,
			RawInput:      map[string]any{"agent": "general", "description": "probe", "prompt": "do it"},
			RawOutput:     map[string]any{"metadata": map[string]any{"sessionID": "ses_child", "status": "completed"}},
			SessionUpdate: "tool_call_update",
		},
	})

	sendOpenCodeUpdate(t, c, "ses_root", acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			Meta:          openCodeChildMeta("ses_child", "ses_root", "a different title"),
			Content:       acp.ContentBlock{Text: &acp.ContentBlockText{Text: "child text", Type: "text"}},
			SessionUpdate: "agent_message_chunk",
		},
	})

	var got *StreamEvent
	for _, e := range drainOpenCodeAttributionEvents(ch) {
		if e.Type == "content" {
			ev := e
			got = &ev
		}
	}
	require.NotNil(t, got)
	assert.Equal(t, "call_parent", got.ParentToolCallID)
}

func TestOpenCodeChildAttribution_UnattributedIsDropped(t *testing.T) {
	c := NewClawBenchACPClient()
	ch := make(chan StreamEvent, 64)
	c.RegisterSession("ses_root", ch)

	sendOpenCodeUpdate(t, c, "ses_root", acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			Meta:          openCodeChildMeta("ses_unknown", "ses_root", "no such parent"),
			Content:       acp.ContentBlock{Text: &acp.ContentBlockText{Text: "leaked?", Type: "text"}},
			SessionUpdate: "agent_message_chunk",
		},
	})

	for _, e := range drainOpenCodeAttributionEvents(ch) {
		if e.Type == "content" {
			t.Fatalf("unattributed child content must not be forwarded, got %+v", e)
		}
	}
}

func TestOpenCodeChildAttribution_NestedSubagent(t *testing.T) {
	c := NewClawBenchACPClient()
	ch := make(chan StreamEvent, 64)
	c.RegisterSession("ses_root", ch)

	// Root subagent call spawns ses_child.
	status := acp.ToolCallStatusInProgress
	title := "subagent"
	sendOpenCodeUpdate(t, c, "ses_root", acp.SessionUpdate{
		ToolCallUpdate: &acp.SessionToolCallUpdate{
			ToolCallId:    "call_root",
			Title:         &title,
			Status:        &status,
			RawInput:      map[string]any{"agent": "general", "description": "outer", "prompt": "outer task"},
			SessionUpdate: "tool_call_update",
		},
	})

	// A nested subagent call inside ses_child: its frame carries the child meta
	// and its own toolCallId, and must itself be attributed to call_root.
	title2 := "subagent"
	sendOpenCodeUpdate(t, c, "ses_root", acp.SessionUpdate{
		ToolCallUpdate: &acp.SessionToolCallUpdate{
			Meta:          openCodeChildMeta("ses_child", "ses_root", "outer"),
			ToolCallId:    "ses_child:call_inner",
			Title:         &title2,
			Status:        &status,
			RawInput:      map[string]any{"agent": "general", "description": "inner", "prompt": "inner task"},
			SessionUpdate: "tool_call_update",
		},
	})

	// The grandchild (inside ses_child) must map to the nested call, not the root.
	sendOpenCodeUpdate(t, c, "ses_root", acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			Meta:          openCodeChildMeta("ses_grandchild", "ses_child", "inner"),
			Content:       acp.ContentBlock{Text: &acp.ContentBlockText{Text: "grandchild", Type: "text"}},
			SessionUpdate: "agent_message_chunk",
		},
	})

	var got *StreamEvent
	for _, e := range drainOpenCodeAttributionEvents(ch) {
		if e.Type == "content" {
			ev := e
			got = &ev
		}
	}
	require.NotNil(t, got)
	assert.Equal(t, "ses_child:call_inner", got.ParentToolCallID,
		"grandchild must fold under the nested subagent call, not the root one")
}

func TestOpenCodeChildAttribution_ChildToolCall(t *testing.T) {
	c := NewClawBenchACPClient()
	ch := make(chan StreamEvent, 64)
	c.RegisterSession("ses_root", ch)

	status := acp.ToolCallStatusInProgress
	title := "subagent"
	sendOpenCodeUpdate(t, c, "ses_root", acp.SessionUpdate{
		ToolCallUpdate: &acp.SessionToolCallUpdate{
			ToolCallId:    "call_parent",
			Title:         &title,
			Status:        &status,
			RawInput:      map[string]any{"agent": "general", "description": "probe", "prompt": "do it"},
			SessionUpdate: "tool_call_update",
		},
	})

	sendOpenCodeUpdate(t, c, "ses_root", acp.SessionUpdate{
		ToolCall: &acp.SessionUpdateToolCall{
			Meta:          openCodeChildMeta("ses_child", "ses_root", "probe"),
			ToolCallId:    "ses_child:call_1",
			Title:         "write",
			Kind:          acp.ToolKindEdit,
			Status:        acp.ToolCallStatusPending,
			SessionUpdate: "tool_call",
		},
	})

	var got *StreamEvent
	for _, e := range drainOpenCodeAttributionEvents(ch) {
		if e.Type == "tool_use" && e.Tool != nil && e.Tool.ID == "ses_child:call_1" {
			ev := e
			got = &ev
		}
	}
	require.NotNil(t, got, "child tool call should be forwarded")
	assert.Equal(t, "call_parent", got.Tool.ParentToolCallID)
}
