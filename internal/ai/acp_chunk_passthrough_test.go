package ai

import (
	"context"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"

	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// Regression: no frame-level content de-duplication (removed 2026-10-30)
// ---------------------------------------------------------------------------
//
// CodeBuddy streams model output character by character, so a GFM delimiter row
// arrives as several frames that are legitimately byte-identical. A retry
// (AcpTeamBridge re-sending after its 5 s timeout) produces the exact same
// bytes, which is why every content-based guard deleted real output — e.g.
// `|---|---|---|---|---|` landed in chat_history as `|---|---|---|`.
//
// These tests pin the wire contract: identical adjacent frames are ALL
// forwarded. See docs/dev/acp_dup_chunk_port.md.

// collectEventText concatenates the payloads of every event of one type, so a
// test can assert on the assembled stream exactly as the executor's accumulator
// would see it.
func collectEventText(ch chan StreamEvent, eventType string) string {
	var out string
	for {
		select {
		case ev := <-ch:
			if ev.Type == eventType {
				out += ev.Content
			}
		default:
			return out
		}
	}
}

// codebuddyTextChunk builds an agent_message_chunk carrying the _meta a real
// CodeBuddy frame has, so the mapper takes the same path as production.
func codebuddyTextChunk(text, member string) acp.SessionUpdate {
	mid := "01a18c91-3f70-72e0-8222-18068884a4a8"
	meta := map[string]any{
		metaKeyCodeBuddyMessageID:             mid,
		"timestamp":                           "1793359697100",
		metaKeyCodeBuddyConversationRequestID: "01a18c913e5c7b1f81f6c61cbd8333cb",
		metaKeyCodeBuddyRequestID:             "01a18c913e5c7b1f81f6c61cbd8333cb",
	}
	if member != "" {
		meta[metaKeyCodeBuddyMemberEvent] = member
	}
	return acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			Meta:      meta,
			MessageId: &mid,
			Content:   acp.TextBlock(text),
		},
	}
}

// A 5-column delimiter row streams as two byte-identical 8-character frames plus
// a shorter one. Dropping either leaves a table whose separator has fewer cells
// than its header, which marked then refuses to render as a table.
func TestMapACPSessionUpdate_KeepsAdjacentIdenticalTextChunks(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy-agent", Backend: "codebuddy"}, "sid")
	ch := make(chan StreamEvent, 64)

	for _, frame := range []string{"|---|---|", "|---|---|", "|---|"} {
		mapACPSessionUpdate(codebuddyTextChunk(frame, ""), ch, context.Background(), conn, nil)
	}

	assert.Equal(t, "|---|---||---|---||---|", collectEventText(ch, "content"),
		"byte-identical adjacent chunks are content, not retries")
}

// Same for thinking: the guard used to apply to agent_thought_chunk as well, and
// repetitive fragments (blank lines, indentation) are the norm there.
func TestMapACPSessionUpdate_KeepsAdjacentIdenticalThoughtChunks(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy-agent", Backend: "codebuddy"}, "sid")
	ch := make(chan StreamEvent, 64)

	mid := "01a18c91-3f70-72e0-8222-18068884a4a8"
	for _, frame := range []string{"\n", "\n"} {
		mapACPSessionUpdate(acp.SessionUpdate{
			AgentThoughtChunk: &acp.SessionUpdateAgentThoughtChunk{
				Meta:      map[string]any{metaKeyCodeBuddyMessageID: mid, "timestamp": "1793359697100"},
				MessageId: &mid,
				Content:   acp.TextBlock(frame),
			},
		}, ch, context.Background(), conn, nil)
	}

	assert.Equal(t, "\n\n", collectEventText(ch, "thinking"))
}

// Team member frames are the only ones that CAN be retried (the retry lives in
// AcpTeamBridge's member forwarding). We still forward them unchanged: the wire
// cannot tell a retry from a member writing the same fragment twice, and losing
// content silently is worse than an occasional doubled paragraph. This test
// documents that deliberate trade-off rather than a limitation.
func TestMapACPSessionUpdate_KeepsAdjacentIdenticalMemberChunks(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy-agent", Backend: "codebuddy"}, "sid")
	ch := make(chan StreamEvent, 64)

	for _, frame := range []string{"|---|---|", "|---|---|"} {
		mapACPSessionUpdate(codebuddyTextChunk(frame, "researcher"), ch, context.Background(), conn, nil)
	}

	assert.Equal(t, "|---|---||---|---|", collectEventText(ch, "content"))
}
