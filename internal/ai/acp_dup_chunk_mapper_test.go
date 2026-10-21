package ai

import (
	"context"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"

	"clawbench/internal/model"
)

// drainContentText concatenates every content/thinking payload the mapper
// emitted, so a test can assert on the assembled text exactly as the executor's
// accumulator would.
func drainContentText(t *testing.T, ch chan StreamEvent) string {
	t.Helper()
	var out string
	for {
		select {
		case ev := <-ch:
			if ev.Type == "content" || ev.Type == "thinking" {
				out += ev.Content
			}
		default:
			return out
		}
	}
}

// The production shape (2026-10-21): the agent's team bridge retries a member
// update whose send raced its own timeout, so the SAME chunk arrives 2-3 times
// with identical _meta. Without the guard the accumulated reply reads
// "已已把报告把报告…" — each delta doubled.
func TestMapACPSessionUpdate_DropsDuplicateAgentMessageChunks(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	ch := make(chan StreamEvent, 64)
	meta := map[string]any{
		metaKeyCodeBuddyMessageID:   "msg-1",
		metaKeyCodeBuddyTimestamp:   "1792590363737",
		metaKeyCodeBuddyMemberEvent: "review-media",
	}

	// Three deliveries of the same chunk, then one genuinely new chunk.
	for range 3 {
		mapACPSessionUpdate(acp.SessionUpdate{
			AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{Meta: meta, Content: acp.TextBlock("已把报告")},
		}, ch, context.Background(), conn, nil)
	}
	newMeta := map[string]any{
		metaKeyCodeBuddyMessageID:   "msg-1",
		metaKeyCodeBuddyTimestamp:   "1792590363800",
		metaKeyCodeBuddyMemberEvent: "review-media",
	}
	mapACPSessionUpdate(acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{Meta: newMeta, Content: acp.TextBlock("发给 team-lead")},
	}, ch, context.Background(), conn, nil)

	assert.Equal(t, "已把报告发给 team-lead", drainContentText(t, ch))
}

func TestMapACPSessionUpdate_DropsDuplicateAgentThoughtChunks(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	ch := make(chan StreamEvent, 64)
	meta := map[string]any{
		metaKeyCodeBuddyMessageID:   "msg-1",
		metaKeyCodeBuddyTimestamp:   "1792590363737",
		metaKeyCodeBuddyMemberEvent: "review-media",
	}

	for range 2 {
		mapACPSessionUpdate(acp.SessionUpdate{
			AgentThoughtChunk: &acp.SessionUpdateAgentThoughtChunk{Meta: meta, Content: acp.TextBlock("thinking")},
		}, ch, context.Background(), conn, nil)
	}

	assert.Equal(t, "thinking", drainContentText(t, ch))
}

// A chunk WITHOUT the identity pair must never be dropped: dropping content we
// cannot identify is worse than the duplication we are fixing.
func TestMapACPSessionUpdate_KeepsChunksWithoutIdentity(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	ch := make(chan StreamEvent, 64)

	for range 3 {
		mapACPSessionUpdate(acp.SessionUpdate{
			AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{Content: acp.TextBlock("x")},
		}, ch, context.Background(), conn, nil)
	}

	assert.Equal(t, "xxx", drainContentText(t, ch))
}

// The guard is CodeBuddy-only. Another agent repeating identical text (a model
// legitimately emitting the same token twice) must reach the client intact.
func TestMapACPSessionUpdate_DedupDoesNotApplyToOtherBackends(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "claude", Backend: "claude"}, "s1")
	ch := make(chan StreamEvent, 64)
	meta := map[string]any{
		metaKeyCodeBuddyMessageID: "msg-1",
		metaKeyCodeBuddyTimestamp: "1792590363737",
	}

	for range 3 {
		mapACPSessionUpdate(acp.SessionUpdate{
			AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{Meta: meta, Content: acp.TextBlock("ha")},
		}, ch, context.Background(), conn, nil)
	}

	assert.Equal(t, "hahaha", drainContentText(t, ch))
}

// A new turn must accept the same chunk identity again — otherwise the first
// chunk of the next turn could be swallowed.
func TestMapACPSessionUpdate_DedupResetsPerTurn(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	ch := make(chan StreamEvent, 64)
	meta := map[string]any{
		metaKeyCodeBuddyMessageID: "msg-1",
		metaKeyCodeBuddyTimestamp: "1792590363737",
	}
	emit := func() {
		mapACPSessionUpdate(acp.SessionUpdate{
			AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{Meta: meta, Content: acp.TextBlock("a")},
		}, ch, context.Background(), conn, nil)
	}

	emit()
	emit() // duplicate, dropped
	assert.Equal(t, "a", drainContentText(t, ch))

	conn.resetChunkDedup() // Prompt start of the next turn
	emit()
	assert.Equal(t, "a", drainContentText(t, ch), "next turn must accept the same identity")
}
