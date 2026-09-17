package ai

import (
	"context"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// Replayed-chunk filter (codebuddy resume defect defense)
//
// codebuddy CLI occasionally re-emits the final agent_message_chunk of the
// PREVIOUS turn at the start of the NEW turn's stream when an ACP session is
// reused across prompts. mapACPSessionUpdate drops such chunks (identified by
// requestId == lastCompletedRequestID) so stale text never reaches the UI or
// DB persistence, and the stale _meta never pollutes the message metadata.
// ---------------------------------------------------------------------------

// codebuddyChunk builds a codebuddy agent_message_chunk update carrying the
// given requestId and text.
func codebuddyChunk(requestID, text string) acp.SessionUpdate {
	meta := map[string]any{}
	if requestID != "" {
		meta[metaKeyCodeBuddyRequestID] = requestID
		meta[metaKeyCodeBuddyTraceID] = "trace-" + requestID
	}
	return acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: text}},
			Meta:    meta,
		},
	}
}

func codebuddyThoughtChunk(requestID, text string) acp.SessionUpdate {
	meta := map[string]any{}
	if requestID != "" {
		meta[metaKeyCodeBuddyRequestID] = requestID
	}
	return acp.SessionUpdate{
		AgentThoughtChunk: &acp.SessionUpdateAgentThoughtChunk{
			Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: text}},
			Meta:    meta,
		},
	}
}

// drainCh reads all currently available events from ch, returning their types
// and, for content/thinking events, the concatenated text.
func drainCh(t *testing.T, ch <-chan StreamEvent) (types []string, content string) {
	t.Helper()
	for {
		select {
		case evt := <-ch:
			types = append(types, evt.Type)
			if evt.Type == "content" || evt.Type == "thinking" {
				content += evt.Content
			}
		default:
			return types, content
		}
	}
}

func TestMapACPSessionUpdate_DropsReplayedPrevTurnChunk(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	conn := &ACPConn{agent: &model.Agent{ID: "cb", Backend: "codebuddy"}}
	conn.setLastCompletedRequestID("prev-turn-rid")

	// First chunk carries the PREVIOUS turn's rid → must be dropped entirely
	// (no content forward, no _meta accumulation).
	mapACPSessionUpdate(codebuddyChunk("prev-turn-rid", "REPLAYED-STALE-TEXT"),
		ch, context.Background(), conn, nil)

	types, content := drainCh(t, ch)
	// thinking_done is harmless and still emitted; no content event though.
	assert.NotContains(t, types, "content", "stale chunk must not forward content")
	assert.Empty(t, content)

	// The stale chunk's meta must NOT be merged into the turn accumulator.
	acc := conn.getMetaAccum()
	if acc != nil && acc.Trace != nil {
		assert.NotEqual(t, "prev-turn-rid", acc.Trace.RequestID,
			"stale chunk _meta must not pollute metaAccum")
	}
}

func TestMapACPSessionUpdate_RealStreamPassesAndKeepsOrder(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	conn := &ACPConn{agent: &model.Agent{ID: "cb", Backend: "codebuddy"}}
	conn.setLastCompletedRequestID("prev-turn-rid")

	// Stale chunk first.
	mapACPSessionUpdate(codebuddyChunk("prev-turn-rid", "REPLAYED-"),
		ch, context.Background(), conn, nil)
	// Genuine chunks all share the NEW turn rid — the filter must NOT drop them.
	mapACPSessionUpdate(codebuddyChunk("new-turn-rid", "真"),
		ch, context.Background(), conn, nil)
	mapACPSessionUpdate(codebuddyChunk("new-turn-rid", "实"),
		ch, context.Background(), conn, nil)
	mapACPSessionUpdate(codebuddyChunk("new-turn-rid", "内"),
		ch, context.Background(), conn, nil)

	_, content := drainCh(t, ch)
	assert.Equal(t, "真实内", content, "genuine stream content must survive intact")
	assert.NotContains(t, content, "REPLAYED-", "stale text must not be concatenated")

	// metaAccum reflects the genuine turn rid only.
	acc := conn.getMetaAccum()
	require.NotNil(t, acc)
	require.NotNil(t, acc.Trace)
	assert.Equal(t, "new-turn-rid", acc.Trace.RequestID)
}

func TestMapACPSessionUpdate_FirstTurnNoLastRID(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	conn := &ACPConn{agent: &model.Agent{ID: "cb", Backend: "codebuddy"}}
	// lastCompletedRequestID is empty on a fresh connection → no filtering.
	mapACPSessionUpdate(codebuddyChunk("any-rid", "你好"),
		ch, context.Background(), conn, nil)

	types, content := drainCh(t, ch)
	assert.Contains(t, types, "content")
	assert.Equal(t, "你好", content)
}

func TestMapACPSessionUpdate_ChunkWithoutRIDNotFiltered(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	conn := &ACPConn{agent: &model.Agent{ID: "cb", Backend: "codebuddy"}}
	conn.setLastCompletedRequestID("prev-turn-rid")

	// A chunk with no requestId must be forwarded normally and must not update
	// the completed-turn baseline.
	mapACPSessionUpdate(codebuddyChunk("", "无 rid"),
		ch, context.Background(), conn, nil)

	types, _ := drainCh(t, ch)
	assert.Contains(t, types, "content")
	assert.Equal(t, "prev-turn-rid", conn.getLastCompletedRequestID(),
		"a rid-less chunk must not clobber the completed-turn baseline")
}

func TestMapACPSessionUpdate_ThinkingNotFiltered(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	conn := &ACPConn{agent: &model.Agent{ID: "cb", Backend: "codebuddy"}}
	conn.setLastCompletedRequestID("prev-turn-rid")

	// agent_thought_chunk carrying the previous rid is left untouched — a
	// thinking-only turn can legitimately reuse a requestId (msg 43244 case).
	mapACPSessionUpdate(codebuddyThoughtChunk("prev-turn-rid", "思考中"),
		ch, context.Background(), conn, nil)

	types, content := drainCh(t, ch)
	assert.Contains(t, types, "thinking")
	assert.Equal(t, "思考中", content)
}

func TestMapACPSessionUpdate_NonCodeBuddyBackendNotFiltered(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	conn := &ACPConn{agent: &model.Agent{ID: "oc", Backend: "opencode"}}
	conn.setLastCompletedRequestID("prev-turn-rid")

	// Filter is scoped to codebuddy — other backends that don't emit
	// codebuddy.ai/* _meta are never dropped.
	mapACPSessionUpdate(codebuddyChunk("prev-turn-rid", "opencode text"),
		ch, context.Background(), conn, nil)

	types, content := drainCh(t, ch)
	assert.Contains(t, types, "content")
	assert.Equal(t, "opencode text", content)
}

func TestMapACPSessionUpdate_NilConnNoPanic(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	// conn == nil is used by MapACPSessionUpdateForTest paths — filter must be skipped.
	mapACPSessionUpdate(codebuddyChunk("any-rid", "nil conn text"),
		ch, context.Background(), nil, nil)

	types, content := drainCh(t, ch)
	assert.Contains(t, types, "content")
	assert.Equal(t, "nil conn text", content)
}

func TestEmitPromptTailMetadata_RecordsCompletedRID(t *testing.T) {
	// Simulate a completed CodeBuddy turn: message chunks accumulate their
	// _meta (genuine new rid), then the tail-metadata path records the
	// completed-turn requestId baseline.
	ch := make(chan StreamEvent, 10)
	conn := &ACPConn{agent: &model.Agent{ID: "cb", Backend: "codebuddy"}}
	conn.setLastCompletedRequestID("prev-turn-rid")

	mapACPSessionUpdate(codebuddyChunk("prev-turn-rid", "REPLAYED"),
		ch, context.Background(), conn, nil)
	mapACPSessionUpdate(codebuddyChunk("current-turn-rid", "真实内容"),
		ch, context.Background(), conn, nil)

	// Drain the content events so the channel holds only the tail metadata.
	drainCh(t, ch)

	conn.emitPromptTailMetadata(acp.PromptResponse{StopReason: acp.StopReason("end_turn")}, ch)

	// Completed-turn baseline is the genuine current rid, not the stale one.
	assert.Equal(t, "current-turn-rid", conn.getLastCompletedRequestID())

	// The metadata event forwarded for persistence carries the genuine rid.
	select {
	case evt := <-ch:
		require.Equal(t, "metadata", evt.Type)
		require.NotNil(t, evt.Meta)
		assert.Equal(t, "current-turn-rid", evt.Meta.RequestID)
		assert.NotEqual(t, "prev-turn-rid", evt.Meta.RequestID)
	default:
		t.Fatal("expected a metadata event from emitPromptTailMetadata")
	}
}

func TestEmitPromptTailMetadata_RidlessTurnKeepsBaseline(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	conn := &ACPConn{agent: &model.Agent{ID: "cb", Backend: "codebuddy"}}
	conn.setLastCompletedRequestID("prev-turn-rid")

	// A turn with no _meta (e.g. bare stopReason) must not clear the baseline.
	conn.emitPromptTailMetadata(acp.PromptResponse{StopReason: acp.StopReason("end_turn")}, ch)
	assert.Equal(t, "prev-turn-rid", conn.getLastCompletedRequestID(),
		"a turn without a requestId must keep the previous baseline")
}

// TestSetLastCompletedRequestID_IgnoresEmpty guards the setter contract.
func TestSetLastCompletedRequestID_IgnoresEmpty(t *testing.T) {
	conn := &ACPConn{}
	conn.setLastCompletedRequestID("")
	assert.Empty(t, conn.getLastCompletedRequestID())
	conn.setLastCompletedRequestID("rid-a")
	assert.Equal(t, "rid-a", conn.getLastCompletedRequestID())
	conn.setLastCompletedRequestID("") // no-op
	assert.Equal(t, "rid-a", conn.getLastCompletedRequestID())
}

// ---------------------------------------------------------------------------
// Multi-requestId turns (production incident fffc1395, 2026-09-16)
//
// A CodeBuddy turn is not guaranteed a single requestId: every model generation
// / message group inside the turn gets its own. Measured across 400 session
// transcripts, 677 of 4891 turns (13.8%) carry two or more. The replay filter
// used to compare against ONE scalar, which — because the accumulator merges
// trace fields first-wins — held the turn's FIRST id, while the replayed chunk
// carries the turn's LAST id. The replay therefore went undetected and its text
// was concatenated in front of the next reply.
//
// These tests pin the baseline to the turn's full id set.
// ---------------------------------------------------------------------------

// A turn that used two requestIds must recognize a replay carrying EITHER of
// them — in particular the LAST one, which the scalar baseline never held.
func TestReplayFilter_MultiRequestIDTurn_DropsReplayOfLastID(t *testing.T) {
	ch := make(chan StreamEvent, 16)
	conn := &ACPConn{agent: &model.Agent{ID: "cb", Backend: "codebuddy"}}

	// --- Turn A: two generations, each with its own requestId. ---
	conn.resetTurnRequestIDs()
	mapACPSessionUpdate(codebuddyChunk("rid-A1", "第一代输出"),
		ch, context.Background(), conn, nil)
	mapACPSessionUpdate(codebuddyChunk("rid-A2", "第二代结论"),
		ch, context.Background(), conn, nil)
	drainCh(t, ch)
	conn.emitPromptTailMetadata(acp.PromptResponse{StopReason: acp.StopReason("end_turn")}, ch)
	drainCh(t, ch)

	// The canonical scalar keeps the FIRST id (first-wins merge) — that is the
	// pre-existing, documented behavior for the persisted trace identity.
	assert.Equal(t, "rid-A1", conn.getLastCompletedRequestID())

	// But BOTH ids must be recognized as belonging to the completed turn.
	assert.True(t, conn.isCompletedTurnRequestID("rid-A1"), "turn A's first id must be in the baseline")
	assert.True(t, conn.isCompletedTurnRequestID("rid-A2"),
		"turn A's LAST id must be in the baseline — this is the id the replay carries")

	// --- Turn B: CodeBuddy replays turn A's tail, carrying the LAST id. ---
	conn.resetTurnRequestIDs()
	mapACPSessionUpdate(codebuddyChunk("rid-A2", "上一轮的结论文本"),
		ch, context.Background(), conn, nil)

	types, content := drainCh(t, ch)
	assert.NotContains(t, types, "content",
		"a replay carrying the previous turn's LAST id must not forward content")
	assert.Empty(t, content, "stale text must never reach the stream")

	// --- The live turn's own chunks still pass. ---
	mapACPSessionUpdate(codebuddyChunk("rid-B1", "本轮真实内容"),
		ch, context.Background(), conn, nil)
	types, content = drainCh(t, ch)
	assert.Contains(t, types, "content", "genuine chunks of the new turn must survive")
	assert.Equal(t, "本轮真实内容", content)
}

// The replay filter must not become a rolling per-chunk mute: all genuine
// chunks of the live turn share its own ids and must pass untouched.
func TestReplayFilter_MultiRequestIDTurn_LiveTurnUnaffected(t *testing.T) {
	ch := make(chan StreamEvent, 16)
	conn := &ACPConn{agent: &model.Agent{ID: "cb", Backend: "codebuddy"}}

	// Turn A observed two ids (via the production accumulation path), then
	// completed with the first one as its canonical id.
	conn.resetTurnRequestIDs()
	conn.mergeMetaExtraction(&metaExtraction{Trace: &metaTrace{RequestID: "rid-A1"}})
	conn.mergeMetaExtraction(&metaExtraction{Trace: &metaTrace{RequestID: "rid-A2"}})
	conn.setLastCompletedRequestID("rid-A1")

	conn.resetTurnRequestIDs()
	for _, chunk := range []struct{ rid, text string }{
		{"rid-B1", "甲"},
		{"rid-B1", "乙"},
		{"rid-B2", "丙"}, // a second generation within the live turn
		{"rid-B2", "丁"},
	} {
		mapACPSessionUpdate(codebuddyChunk(chunk.rid, chunk.text),
			ch, context.Background(), conn, nil)
	}

	_, content := drainCh(t, ch)
	assert.Equal(t, "甲乙丙丁", content,
		"a live turn spanning multiple requestIds must stream in full")
}

// The per-turn id set must not leak across turns: ids observed in an earlier
// turn must stop being treated as replays once a new turn completes.
func TestReplayFilter_TurnRequestIDsDoNotAccumulateAcrossTurns(t *testing.T) {
	conn := &ACPConn{agent: &model.Agent{ID: "cb", Backend: "codebuddy"}}

	// Turn A.
	conn.resetTurnRequestIDs()
	conn.mergeMetaExtraction(&metaExtraction{Trace: &metaTrace{RequestID: "rid-A"}})
	conn.setLastCompletedRequestID("rid-A")
	assert.True(t, conn.isCompletedTurnRequestID("rid-A"))

	// Turn B completes with only its own id.
	conn.resetTurnRequestIDs()
	conn.mergeMetaExtraction(&metaExtraction{Trace: &metaTrace{RequestID: "rid-B"}})
	conn.setLastCompletedRequestID("rid-B")

	assert.True(t, conn.isCompletedTurnRequestID("rid-B"), "turn B's id is the current baseline")
	assert.False(t, conn.isCompletedTurnRequestID("rid-A"),
		"turn A's id must not remain in the baseline — otherwise the filter would "+
			"eventually drop unrelated turns' chunks")
}

// A CANCELLED turn never reaches the tail-metadata path, so its ids are never
// promoted — but they must not survive into a later turn's baseline either.
//
// This is what the per-Prompt reset guards: without it, a cancelled turn's ids
// stay in turnRequestIDs, get unioned into the next completed turn's baseline,
// and can then match (and silently mute) a live chunk of a later turn that
// reuses an id — e.g. a retry of the same prompt after the cancellation.
func TestReplayFilter_CancelledTurnIDsDoNotPolluteNextBaseline(t *testing.T) {
	conn := &ACPConn{agent: &model.Agent{ID: "cb", Backend: "codebuddy"}}

	// Turn A is cancelled mid-stream: ids are observed, then Prompt returns on
	// the cancel path without ever calling setLastCompletedRequestID.
	conn.resetTurnRequestIDs()
	conn.mergeMetaExtraction(&metaExtraction{Trace: &metaTrace{RequestID: "rid-A1"}})
	conn.mergeMetaExtraction(&metaExtraction{Trace: &metaTrace{RequestID: "rid-A2"}})

	// Turn B starts — the per-Prompt reset runs here — and completes normally.
	conn.resetTurnRequestIDs()
	conn.mergeMetaExtraction(&metaExtraction{Trace: &metaTrace{RequestID: "rid-B1"}})
	conn.setLastCompletedRequestID("rid-B1")

	assert.True(t, conn.isCompletedTurnRequestID("rid-B1"))
	assert.False(t, conn.isCompletedTurnRequestID("rid-A1"),
		"a cancelled turn's first id must not reach the next turn's baseline")
	assert.False(t, conn.isCompletedTurnRequestID("rid-A2"),
		"a cancelled turn's last id must not reach the next turn's baseline")
}

// A turn with no requestId at all must leave the previous baseline intact, so a
// trace-less turn cannot silently disable the filter.
func TestReplayFilter_RidlessTurnKeepsBaselineSet(t *testing.T) {
	conn := &ACPConn{agent: &model.Agent{ID: "cb", Backend: "codebuddy"}}
	conn.resetTurnRequestIDs()
	conn.mergeMetaExtraction(&metaExtraction{Trace: &metaTrace{RequestID: "rid-A1"}})
	conn.mergeMetaExtraction(&metaExtraction{Trace: &metaTrace{RequestID: "rid-A2"}})
	conn.setLastCompletedRequestID("rid-A1")

	conn.resetTurnRequestIDs()
	conn.setLastCompletedRequestID("") // no ids observed this turn

	assert.True(t, conn.isCompletedTurnRequestID("rid-A1"))
	assert.True(t, conn.isCompletedTurnRequestID("rid-A2"))
}

// An empty requestId is never a replay match — a chunk without trace identity
// cannot be attributed to a turn.
func TestReplayFilter_EmptyRIDNeverMatches(t *testing.T) {
	conn := &ACPConn{agent: &model.Agent{ID: "cb", Backend: "codebuddy"}}
	conn.setLastCompletedRequestID("rid-A")
	assert.False(t, conn.isCompletedTurnRequestID(""))
}
