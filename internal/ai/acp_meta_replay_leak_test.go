package ai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/ai/acptest"
	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// Cross-turn _meta leak via replayed usage_update
//
// Incident (session de70b99a, 2026-09-16): a turn that never ran the model was
// persisted with the PREVIOUS turn's messageId while carrying its own requestId.
// The stale identity made the record look like a normal completion
// (stopReason=end_turn, outcome=SUCCESS), masking the fact that no model call
// ever happened.
//
// Root cause: the agent_message_chunk path drops replayed chunks whose
// requestId equals the last completed turn's (see acp_events.go), but the
// usage_update path merges its _meta into the accumulator unconditionally. A
// replayed usage_update therefore injects the previous turn's trace identity
// into the current turn's metadata.
// ---------------------------------------------------------------------------

// codebuddyUsageUpdate builds a CodeBuddy usage_update carrying the given trace
// identity, mirroring the shape CodeBuddy sends (OpenAI-style usage + the
// codebuddy.ai/* namespace on the same _meta).
func codebuddyUsageUpdate(requestID, messageID string, promptTokens int) acp.SessionUpdate {
	meta := map[string]any{
		"usage": map[string]any{
			"prompt_tokens":     promptTokens,
			"completion_tokens": 7,
		},
	}
	if requestID != "" {
		meta[metaKeyCodeBuddyRequestID] = requestID
		meta[metaKeyCodeBuddyTraceID] = "trace-" + requestID
	}
	if messageID != "" {
		meta[metaKeyCodeBuddyMessageID] = messageID
	}
	return acp.SessionUpdate{
		UsageUpdate: &acp.SessionUsageUpdate{
			Used: int64(promptTokens),
			Size: 256000,
			Meta: acptest.Meta(meta),
		},
	}
}

// A replayed usage_update from the previous turn must not contribute trace
// identity to the current turn's accumulated metadata.
func TestUsageUpdate_ReplayedTurnDoesNotLeakTraceIdentity(t *testing.T) {
	ch := make(chan StreamEvent, 16)
	conn := newACPConn(&model.Agent{ID: "cb", Backend: "codebuddy"}, "s1")

	// Turn A completed successfully and registered its requestId as the
	// baseline the replay filter compares against.
	conn.setLastCompletedRequestID("req-turn-A")
	conn.ResetTurnOutput()

	// Turn B starts. CodeBuddy replays turn A's usage_update (stale requestId)
	// before emitting turn B's own notifications.
	mapACPSessionUpdate(codebuddyUsageUpdate("req-turn-A", "msg-turn-A", 1000), ch, context.Background(), conn, nil)

	acc := conn.getAndClearMetaAccum()
	require.NotNil(t, acc, "the usage counters themselves should still accumulate")
	if acc.Trace != nil {
		assert.NotEqual(t, "req-turn-A", acc.Trace.RequestID,
			"a replayed usage_update must not set the current turn's requestId")
		assert.NotEqual(t, "msg-turn-A", acc.Trace.MessageID,
			"a replayed usage_update must not set the current turn's messageId")
	}
}

// Turn B's own usage_update must still accumulate normally after a replay was
// dropped — the filter must not be a blanket mute on usage_update.
func TestUsageUpdate_LiveTurnStillAccumulates(t *testing.T) {
	ch := make(chan StreamEvent, 16)
	conn := newACPConn(&model.Agent{ID: "cb", Backend: "codebuddy"}, "s1")
	conn.setLastCompletedRequestID("req-turn-A")
	conn.ResetTurnOutput()

	// Stale replay first, then the live turn's own notification.
	mapACPSessionUpdate(codebuddyUsageUpdate("req-turn-A", "msg-turn-A", 1000), ch, context.Background(), conn, nil)
	mapACPSessionUpdate(codebuddyUsageUpdate("req-turn-B", "msg-turn-B", 2000), ch, context.Background(), conn, nil)

	acc := conn.getAndClearMetaAccum()
	require.NotNil(t, acc)
	require.NotNil(t, acc.Trace, "the live turn's trace identity must survive")
	assert.Equal(t, "req-turn-B", acc.Trace.RequestID)
	assert.Equal(t, "msg-turn-B", acc.Trace.MessageID)
}

// The usage counters are the context-window occupancy, which is legitimate to
// report even on a replayed notification — only the trace identity is stale.
// Dropping the whole update would lose the context chip data.
func TestUsageUpdate_ReplayStillUpdatesContextUsage(t *testing.T) {
	ch := make(chan StreamEvent, 16)
	conn := newACPConn(&model.Agent{ID: "cb", Backend: "codebuddy"}, "s1")
	conn.setLastCompletedRequestID("req-turn-A")
	conn.ResetTurnOutput()

	mapACPSessionUpdate(codebuddyUsageUpdate("req-turn-A", "msg-turn-A", 1234), ch, context.Background(), conn, nil)

	// The forwarded event must still carry the occupancy numbers.
	var sawUsage bool
	for {
		select {
		case evt := <-ch:
			if evt.Type == "usage_update" && evt.Usage != nil && evt.Usage.Used == 1234 {
				sawUsage = true
			}
			continue
		default:
		}
		break
	}
	assert.True(t, sawUsage, "the context-usage numbers must still reach the frontend")
}

// The replay filter is CodeBuddy-specific: another backend that happens to send
// a requestId equal to the baseline must not have its trace identity dropped.
func TestUsageUpdate_NonCodeBuddyUnaffected(t *testing.T) {
	ch := make(chan StreamEvent, 16)
	conn := newACPConn(&model.Agent{ID: "claude", Backend: "claude"}, "s1")
	conn.setLastCompletedRequestID("req-turn-A")
	conn.ResetTurnOutput()

	// Claude carries quota/trace under its own keys; feed one the CodeBuddy
	// adapter would recognize so the filter has something to (not) drop.
	update := acp.SessionUpdate{
		UsageUpdate: &acp.SessionUsageUpdate{
			Used: 100, Size: 200000,
			Meta: acptest.Meta(map[string]any{
				metaKeyCodeBuddyRequestID: "req-turn-A",
				metaKeyCodeBuddyMessageID: "msg-turn-A",
			}),
		},
	}
	assert.False(t, isReplayedTurnMeta(conn, "claude", MetaAny(update.UsageUpdate.Meta)),
		"the replay filter is CodeBuddy-specific and must not affect other backends")

	mapACPSessionUpdate(update, ch, context.Background(), conn, nil)
}

// Token counters are ACCUMULATED per model call, so a replayed notification
// would add the previous turn's calls to this turn's total — double-billing a
// whole turn. The replay filter must therefore drop the usage block too, not
// only the trace identity. The raw used/size occupancy is still forwarded
// (covered by TestUsageUpdate_ReplayStillUpdatesContextUsage).
func TestUsageUpdate_ReplayedTurnDoesNotAccumulateTokens(t *testing.T) {
	ch := make(chan StreamEvent, 16)
	conn := newACPConn(&model.Agent{ID: "cb", Backend: "codebuddy"}, "s1")
	conn.setLastCompletedRequestID("req-turn-A")
	conn.ResetTurnOutput()

	// Previous turn's call replays at the start of the new turn.
	mapACPSessionUpdate(codebuddyUsageUpdate("req-turn-A", "msg-turn-A", 5000), ch, context.Background(), conn, nil)
	// The new turn's own first call.
	mapACPSessionUpdate(codebuddyUsageUpdate("req-turn-B", "msg-turn-B", 1000), ch, context.Background(), conn, nil)

	acc := conn.getAndClearMetaAccum()
	require.NotNil(t, acc)
	require.NotNil(t, acc.Usage, "the live turn's counters must be present")
	assert.Equal(t, 1000, acc.Usage.InputTokens,
		"the replayed turn's 5000 tokens must not be added to this turn")
}

// The replay filter must be decided by the backend's registered hook, not by a
// comparison against a hard-coded id in the core.
//
// The two directions matter equally:
//   - a backend that DOES declare the hook gets replayed frames dropped;
//   - a backend that declares none must let them through, even when its
//     requestId happens to equal the baseline (its ids are not comparable
//     across turns, so a match is a coincidence, not a replay).
//
// Wiring the decision through LookupACPBehaviorFn is what lets a new backend
// opt in without editing internal/ai.
func TestReplayedTurnMeta_ComesFromTheRegisteredBehavior(t *testing.T) {
	orig := LookupACPBehaviorFn
	t.Cleanup(func() { LookupACPBehaviorFn = orig })

	// Every backend that declares the hook gets this test implementation, which
	// answers "yes" for one magic id. That isolates the DISPATCH (which the core
	// owns) from the comparison itself (which the backend owns).
	behaviors := map[string]*ACPBehavior{
		"with-filter": {
			IsReplayedTurnMeta: func(_ *ACPConn, meta map[string]any) bool {
				return meta["rid"] == "magic"
			},
		},
		"without-filter": {CostFieldCarriesCredit: true},
	}
	LookupACPBehaviorFn = func(id string) *ACPBehavior { return behaviors[id] }

	conn := newACPConn(&model.Agent{ID: "x", Backend: "with-filter"}, "s1")

	assert.True(t, isReplayedTurnMeta(conn, "with-filter", map[string]any{"rid": "magic"}),
		"the declaring backend's hook must be consulted")
	assert.False(t, isReplayedTurnMeta(conn, "with-filter", map[string]any{"rid": "other"}),
		"the hook's own answer must be respected")

	assert.False(t, isReplayedTurnMeta(conn, "without-filter", map[string]any{"rid": "magic"}),
		"a backend declaring no hook must let every frame through")
	assert.False(t, isReplayedTurnMeta(conn, "unregistered", map[string]any{"rid": "magic"}),
		"an unknown backend must let every frame through")

	assert.False(t, isReplayedTurnMeta(nil, "with-filter", map[string]any{"rid": "magic"}),
		"a nil connection can never be a replay")
	assert.False(t, isReplayedTurnMeta(conn, "with-filter", nil),
		"empty meta can never be a replay")
}
