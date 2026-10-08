package service

import (
	"context"
	"testing"

	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
