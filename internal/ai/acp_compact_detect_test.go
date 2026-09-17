package ai

import (
	"context"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"

	"clawbench/internal/model"
)

// drainCompactionEvents runs the real ACP event mapper and returns every
// compact_detected signal it emitted.
func drainCompactionEvents(t *testing.T, ch chan StreamEvent) int {
	t.Helper()
	detected := 0
	for {
		select {
		case ev := <-ch:
			if ev.Type == "compact_detected" {
				detected++
			}
		default:
			return detected
		}
	}
}

// TestMapACPSessionUpdate_CodeBuddyCompactionFlag covers the real CodeBuddy ACP
// shape: the compaction flag rides on the _meta of whatever update the agent is
// sending, repeated across many updates. Exactly one signal must reach the
// service layer.
func TestMapACPSessionUpdate_CodeBuddyCompactionFlag(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	ch := make(chan StreamEvent, 64)

	compactionMeta := map[string]any{
		"codebuddy.ai/isCompactInternal": true,
		"codebuddy.ai/compactType":       "user-command",
	}
	for range 3 {
		mapACPSessionUpdate(acp.SessionUpdate{
			AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
				Meta:    compactionMeta,
				Content: acp.TextBlock("chunk"),
			},
		}, ch, context.Background(), conn, nil)
	}

	assert.Equal(t, 1, drainCompactionEvents(t, ch),
		"the repeated compaction flag must produce exactly one signal")
}

// TestMapACPSessionUpdate_CompactionResetPerTurn verifies the latch is per-turn:
// a long session that compacts twice must re-inject twice, so the second
// compaction cannot be swallowed by the first one's latch.
func TestMapACPSessionUpdate_CompactionResetPerTurn(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	ch := make(chan StreamEvent, 64)
	meta := map[string]any{"codebuddy.ai/isCompactInternal": true}

	mapACPSessionUpdate(acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{Meta: meta, Content: acp.TextBlock("a")},
	}, ch, context.Background(), conn, nil)
	assert.Equal(t, 1, drainCompactionEvents(t, ch))

	// Next turn (Prompt start) clears the latch.
	conn.compactReported.Store(false)

	mapACPSessionUpdate(acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{Meta: meta, Content: acp.TextBlock("b")},
	}, ch, context.Background(), conn, nil)
	assert.Equal(t, 1, drainCompactionEvents(t, ch), "a second compaction must report again")
}

// TestMapACPSessionUpdate_CancelledCompactionIsNotACompaction pins the negative
// direction for CodeBuddy: a cancelled (or limit-reached) compaction restores the
// original history, so the session must NOT be flagged.
func TestMapACPSessionUpdate_CancelledCompactionIsNotACompaction(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	ch := make(chan StreamEvent, 64)

	mapACPSessionUpdate(acp.SessionUpdate{
		SessionInfoUpdate: &acp.SessionSessionInfoUpdate{
			Meta: map[string]any{"codebuddy.ai/compact-cancelled": map[string]any{"cancelled": true}},
		},
	}, ch, context.Background(), conn, nil)

	assert.Equal(t, 0, drainCompactionEvents(t, ch),
		"a cancelled compaction must not flag the session as compacted")
}

// TestMapACPSessionUpdate_CancelledCompactionClearsPendingReport covers the
// ordering where the flag was already observed before the agent reported the
// cancellation: the history is restored, so the pending report must be dropped.
func TestMapACPSessionUpdate_CancelledCompactionClearsPendingReport(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	ch := make(chan StreamEvent, 64)

	// Compaction starts and is reported.
	mapACPSessionUpdate(acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			Meta:    map[string]any{"codebuddy.ai/isCompactInternal": true},
			Content: acp.TextBlock("summary"),
		},
	}, ch, context.Background(), conn, nil)
	assert.Equal(t, 1, drainCompactionEvents(t, ch))

	// The agent then reports it cancelled the compaction. The latch must reopen
	// so a subsequent real compaction is not mistaken for this attempt.
	mapACPSessionUpdate(acp.SessionUpdate{
		SessionInfoUpdate: &acp.SessionSessionInfoUpdate{
			Meta: map[string]any{"codebuddy.ai/compact-cancelled": map[string]any{"cancelled": true}},
		},
	}, ch, context.Background(), conn, nil)
	assert.False(t, conn.compactReported.Load(),
		"a cancelled compaction must reopen the latch for the next attempt")
}

// TestMapACPSessionUpdate_CompactionRetriedWhenChannelFull pins the retry
// contract: this path runs on the SDK's shared notification goroutine where
// blocking is forbidden, so a send can be dropped. The agent repeats the flag
// across updates, and the latch must stay open so the next update retries —
// otherwise one dropped send loses the re-injection permanently.
func TestMapACPSessionUpdate_CompactionRetriedWhenChannelFull(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	// Unbuffered channel with no reader: every send is dropped.
	ch := make(chan StreamEvent)

	meta := map[string]any{"codebuddy.ai/isCompactInternal": true}
	mapACPSessionUpdate(acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{Meta: meta, Content: acp.TextBlock("a")},
	}, ch, context.Background(), conn, nil)

	assert.False(t, conn.compactReported.Load(),
		"a dropped signal must not latch, so a later update can retry")

	// Drain into a buffered channel: the next update must report the compaction.
	buffered := make(chan StreamEvent, 8)
	mapACPSessionUpdate(acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{Meta: meta, Content: acp.TextBlock("b")},
	}, buffered, context.Background(), conn, nil)

	assert.Equal(t, 1, drainCompactionEvents(t, buffered), "the retry must deliver the signal")
	assert.True(t, conn.compactReported.Load(), "a delivered signal must latch")
}

// TestMapACPSessionUpdate_NilConnDoesNotPanic covers the conn-less call path
// (MapACPSessionUpdateForTest and the LoadSession replay parsers pass nil): the
// compaction branches must skip cleanly rather than dereferencing conn.
func TestMapACPSessionUpdate_NilConnDoesNotPanic(t *testing.T) {
	ch := make(chan StreamEvent, 32)
	assert.NotPanics(t, func() {
		mapACPSessionUpdate(acp.SessionUpdate{
			AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
				Meta:    map[string]any{"codebuddy.ai/isCompactInternal": true},
				Content: acp.TextBlock("x"),
			},
		}, ch, context.Background(), nil, nil)
		mapACPSessionUpdate(acp.SessionUpdate{
			SessionInfoUpdate: &acp.SessionSessionInfoUpdate{
				Meta: map[string]any{"codebuddy.ai/compact-cancelled": map[string]any{"cancelled": true}},
			},
		}, ch, context.Background(), nil, nil)
		mapACPSessionUpdate(acp.SessionUpdate{
			AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{Content: acp.TextBlock("Compacting completed.")},
		}, ch, context.Background(), nil, nil)
	})
}

// TestMapACPSessionUpdate_ClaudeCompactionText covers the claude-agent-acp
// adapter, which announces a finished compaction as a plain text chunk rather
// than a _meta flag.
func TestMapACPSessionUpdate_ClaudeCompactionText(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "claude", Backend: "claude"}, "s1")
	ch := make(chan StreamEvent, 64)

	mapACPSessionUpdate(acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{Content: acp.TextBlock("\n\nCompacting completed.")},
	}, ch, context.Background(), conn, nil)

	// Collect once: draining twice would hide the content event.
	detected := 0
	foundContent := false
	for {
		select {
		case ev := <-ch:
			switch ev.Type {
			case "compact_detected":
				detected++
			case "content":
				if ev.Content == "\n\nCompacting completed." {
					foundContent = true
				}
			}
		default:
			assert.Equal(t, 1, detected, "the Claude compaction notice must flag the session")
			assert.True(t, foundContent, "the compaction notice must still be forwarded as content")
			return
		}
	}
}

// TestMapACPSessionUpdate_NoCompactionOnNormalTurn guards the negative direction:
// a normal ACP turn must not flag the session.
func TestMapACPSessionUpdate_NoCompactionOnNormalTurn(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	ch := make(chan StreamEvent, 64)

	mapACPSessionUpdate(acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			Meta:    map[string]any{"codebuddy.ai/requestId": "abc"},
			Content: acp.TextBlock("normal reply"),
		},
	}, ch, context.Background(), conn, nil)
	mapACPSessionUpdate(acp.SessionUpdate{
		AgentThoughtChunk: &acp.SessionUpdateAgentThoughtChunk{Content: acp.TextBlock("thinking")},
	}, ch, context.Background(), conn, nil)

	assert.Equal(t, 0, drainCompactionEvents(t, ch))
}
