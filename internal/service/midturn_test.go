package service_test

import (
	"context"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubInjector replaces the mid-turn seam with a canned answer and restores it
// afterwards. This is why the seam is an indirection: the whole core-flow
// decision can be exercised without a live ACP connection.
//
// It also records the request so tests can assert what core flow passed through.
func stubInjector(t *testing.T, res ai.MidTurnInjectResult) *[]ai.MidTurnInjectRequest {
	t.Helper()
	orig := service.SetInjectMidTurnForTest(nil)
	t.Cleanup(func() { service.SetInjectMidTurnForTest(orig) })

	var calls []ai.MidTurnInjectRequest
	service.SetInjectMidTurnForTest(func(_ context.Context, backendID, sessionID, agentID, content string, files []model.FileEntry, clientUserMessageID string) ai.MidTurnInjectResult {
		calls = append(calls, ai.MidTurnInjectRequest{
			SessionID:           sessionID,
			AgentID:             agentID,
			Content:             content,
			Files:               files,
			ClientUserMessageID: clientUserMessageID,
		})
		return res
	})
	return &calls
}

// TestTryInjectMidTurn_DeclinedPersistsNothing verifies that when the backend
// declines injection, the caller is told to queue and no row is written here
// (the queue path owns persistence in that case).
func TestTryInjectMidTurn_DeclinedPersistsNothing(t *testing.T) {
	db := setupDB(t)
	_ = db
	sid := helperCreateSession(t, "/project", "claude", "Inject Declined")

	calls := stubInjector(t, ai.MidTurnInjectResult{}) // zero value = queue it

	injected, msgID := service.TryInjectMidTurn(service.EnqueueStartConfig{
		SessionID:   sid,
		ProjectPath: "/project",
		BackendName: "claude",
		Message:     "hello",
		QueueID:     "q-1",
	}, "client-1")

	assert.False(t, injected, "declined injection must report false so the caller queues")
	assert.Zero(t, msgID)
	assert.Len(t, *calls, 1, "the policy should have been consulted once")

	// Nothing persisted: the queue path will do that.
	assert.Equal(t, 0, service.GetQueuedCount(sid))
}

// TestTryInjectMidTurn_InjectedPersistsNormalMessage verifies a successful
// injection writes a NORMAL (not queued) user row, so the drain loop never
// picks it up and runs it a second time.
func TestTryInjectMidTurn_InjectedPersistsNormalMessage(t *testing.T) {
	db := setupDB(t)
	_ = db
	sid := helperCreateSession(t, "/project", "claude", "Inject Accepted")

	calls := stubInjector(t, ai.MidTurnInjectResult{Injected: true, OwnerRequestID: "req-1"})

	injected, msgID := service.TryInjectMidTurn(service.EnqueueStartConfig{
		SessionID:   sid,
		ProjectPath: "/project",
		BackendName: "claude",
		Message:     "injected text",
		QueueID:     "q-2",
	}, "client-1")

	require.True(t, injected)
	require.NotZero(t, msgID, "a successful injection must persist a real row")

	// Must NOT be queued — otherwise the drain loop would execute it again.
	assert.Equal(t, 0, service.GetQueuedCount(sid),
		"injected message must be persisted as a normal message, not queued")

	msgs, _, _, err := service.GetChatHistoryPaged("/project", "claude", sid, 50, 0)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "injected text", msgs[0].Content)
	assert.False(t, msgs[0].Queued, "row must not carry the queued flag")

	// Core flow must have passed the message through verbatim.
	require.Len(t, *calls, 1)
	assert.Equal(t, "injected text", (*calls)[0].Content)
	assert.Equal(t, "q-2", (*calls)[0].ClientUserMessageID)
}

// TestEnqueueAndMaybeStart_InjectsWhenRunning verifies the integration point:
// a message for a RUNNING session is injected instead of queued.
func TestEnqueueAndMaybeStart_InjectsWhenRunning(t *testing.T) {
	db := setupDB(t)
	_ = db
	sid := helperCreateSession(t, "/project", "claude", "Enqueue Inject")

	calls := stubInjector(t, ai.MidTurnInjectResult{Injected: true, OwnerRequestID: "req-9"})

	// Mark the session as running so EnqueueAndMaybeStart takes the injection path.
	require.True(t, service.TrySetSessionRunning(sid))
	t.Cleanup(func() { service.SetSessionRunning(sid, false, true) })

	started, injected, msgID, err := service.EnqueueAndMaybeStart(service.EnqueueStartConfig{
		SessionID:   sid,
		ProjectPath: "/project",
		BackendName: "claude",
		Message:     "mid-turn",
		QueueID:     "q-9",
	})

	require.NoError(t, err)
	assert.False(t, started, "an injected message does not start a new run")
	assert.True(t, injected, "message should have joined the running turn")
	require.NotZero(t, msgID)
	assert.Equal(t, 0, service.GetQueuedCount(sid), "injected message must not be left in the queue")

	require.Len(t, *calls, 1)
	assert.Equal(t, "mid-turn", (*calls)[0].Content)
}

// TestEnqueueAndMaybeStart_QueuesWhenInjectionDeclined verifies the fallback:
// a decline leaves the existing queue behavior completely intact.
func TestEnqueueAndMaybeStart_QueuesWhenInjectionDeclined(t *testing.T) {
	db := setupDB(t)
	_ = db
	sid := helperCreateSession(t, "/project", "claude", "Enqueue Fallback")

	calls := stubInjector(t, ai.MidTurnInjectResult{Reason: "idle"}) // turn ended — queue it

	require.True(t, service.TrySetSessionRunning(sid))
	t.Cleanup(func() { service.SetSessionRunning(sid, false, true) })

	started, injected, msgID, err := service.EnqueueAndMaybeStart(service.EnqueueStartConfig{
		SessionID:   sid,
		ProjectPath: "/project",
		BackendName: "claude",
		Message:     "queued text",
		QueueID:     "q-10",
	})

	require.NoError(t, err)
	assert.False(t, started, "session is running, so no new run starts")
	assert.False(t, injected, "declined injection must fall back to the queue")
	require.NotZero(t, msgID)
	assert.Equal(t, 1, service.GetQueuedCount(sid), "declined message must be queued as before")

	require.Len(t, *calls, 1, "the policy is consulted before falling back to the queue")
}
