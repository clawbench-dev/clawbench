package service

import (
	"context"
	"testing"
	"time"

	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A group MEMBER turn interrupted by "interrupt and send" must report the
// interrupt as its cancel reason, so its partial reply is finalized WITHOUT the
// "cancelled" badge.
//
// The interrupt is recorded on the GROUP session (the endpoint interrupts by
// group id), but a member executor's own slot is keyed by the MEMBER row, so it
// reads empty and falls through to the generic ctx.Err()==Canceled branch —
// stamping a deliberately-cut-short member speech as "cancelled", which says the
// user abandoned the reply (the opposite of interrupt semantics). The executor
// must fall back to the TIMELINE (group) slot for member turns.
func TestExecutorMemberTurn_ReadsGroupInterruptReason(t *testing.T) {
	setupExecutorDB(t)

	memberSID, err := CreateSession("/test", "test", "member", "agent-1", "", "default", "group_member")
	require.NoError(t, err)
	groupSID, err := CreateSession("/test", "test", "group", "agent-1", "", "default", "group")
	require.NoError(t, err)

	// The interrupt records its reason on the GROUP session.
	SetCancelReason(groupSID, cancelReasonInterrupt)
	t.Cleanup(func() { GetAndClearCancelReason(groupSID) })

	// The turn ctx IS cancelled by the interrupt, so without the fallback the
	// generic ctx-cancel branch would win.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := RunConfig{
		Mode:              ModeInteractive,
		ProjectPath:       "/test",
		BackendName:       "test",
		SessionID:         memberSID,
		TimelineSessionID: groupSID,
		SpeakerID:         memberSID,
		AgentID:           "agent-1",
	}
	executor := NewSessionExecutor(ctx, cfg)
	defer executor.unregisterActiveStream()

	result := executor.buildResult(true, time.Now())
	assert.Equal(t, cancelReasonInterrupt, result.CancelReason,
		"a member turn must read the interrupt recorded on the group session")

	// The group slot must SURVIVE the member's read: RunGroupTurnDrain consumes
	// it AFTER runLoop to translate the group-level reason for the drain loop.
	// If the member's read cleared it, the translation would see "" and the
	// drain loop would drop the queue — the opposite of interrupt semantics.
	assert.Equal(t, cancelReasonInterrupt, GetCancelReason(groupSID),
		"the member's read must not consume the group's cancel reason")
}

// TestExecutorTimelineRedirectsPersistence verifies that a group turn
// (SessionID = member row, TimelineSessionID = group row) writes its streaming
// message to the GROUP timeline and stamps the speaker (member row id), while
// the server-side activeStreams registry stays keyed by the MEMBER row (so
// graceful shutdown does not collide concurrent members).
func TestExecutorTimelineRedirectsPersistence(t *testing.T) {
	setupExecutorDB(t)

	memberSID, err := CreateSession("/test", "test", "member", "agent-1", "", "default", "group_member")
	require.NoError(t, err)
	groupSID, err := CreateSession("/test", "test", "group", "agent-1", "", "default", "group")
	require.NoError(t, err)

	msgID, err := AddChatMessageWithAgent("/test", "test", groupSID, "assistant", `{"blocks":[]}`, nil, true, "", memberSID)
	require.NoError(t, err)

	cfg := RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        "/test",
		BackendName:        "test",
		SessionID:          memberSID,
		TimelineSessionID:  groupSID,
		SpeakerID:          memberSID,
		AgentID:            "agent-1",
		StreamingMessageID: msgID,
	}
	executor := NewSessionExecutor(context.Background(), cfg)
	defer executor.unregisterActiveStream()

	// The executor must target the group timeline.
	assert.Equal(t, groupSID, executor.timelineSID())

	// activeStreams stays keyed by the member row (connection/execution identity).
	_, ok := activeStreams.Load(memberSID)
	assert.True(t, ok, "activeStreams must be keyed by the member row, not the group")

	// The streaming message lives on the group timeline and carries the speaker.
	msg, err := GetMessageByID(msgID)
	require.NoError(t, err)
	assert.Equal(t, groupSID, msg.SessionID)
	assert.Equal(t, memberSID, msg.AgentID)

	// It must NOT appear on the member's timeline.
	var onMember int
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM chat_history WHERE session_id = ?", memberSID).Scan(&onMember))
	assert.Equal(t, 0, onMember, "member timeline must have no chat_history rows")
}
