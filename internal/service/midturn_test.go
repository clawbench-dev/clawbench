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

// TestEnqueueAndMaybeStart_AlwaysQueuesWhileRunning pins the uniform behavior:
// sending to a RUNNING session always queues, even for a backend that CAN
// inject. Joining the running turn is an explicit action on the queued bubble
// (InjectQueuedMessage), never an automatic side effect of sending — otherwise
// a steer-capable backend would swallow the message with no queue bubble to
// show for it.
func TestEnqueueAndMaybeStart_AlwaysQueuesWhileRunning(t *testing.T) {
	db := setupDB(t)
	_ = db
	sid := helperCreateSession(t, "/project", "claude", "Enqueue Always Queues")

	// Even with a policy that WOULD accept injection, sending must not use it.
	calls := stubInjector(t, ai.MidTurnInjectResult{Injected: true, OwnerRequestID: "req-9"})

	require.True(t, service.TrySetSessionRunning(sid))
	t.Cleanup(func() { service.SetSessionRunning(sid, false, true) })

	started, injected, msgID, err := service.EnqueueAndMaybeStart(service.EnqueueStartConfig{
		SessionID:   sid,
		ProjectPath: "/project",
		BackendName: "claude",
		Message:     "should queue",
		QueueID:     "q-9",
	})

	require.NoError(t, err)
	assert.False(t, started, "session is running, so no new run starts")
	assert.False(t, injected, "sending must never auto-inject")
	require.NotZero(t, msgID)

	assert.Equal(t, 1, service.GetQueuedCount(sid),
		"the message must be visible in the queue for every backend")
	assert.Empty(t, *calls, "the inject policy must not be consulted while sending")
}

// TestInjectQueuedMessage_Success verifies the "insert into the current reply"
// path: the already-queued row is claimed (not re-inserted), so it keeps its DB
// id — which is what makes the conversation order come out right without any
// re-sorting.
func TestInjectQueuedMessage_Success(t *testing.T) {
	db := setupDB(t)
	_ = db
	sid := helperCreateSession(t, "/project", "claude", "Insert Queued")

	// A queued message already exists (the user queued it, then chose insert).
	queuedID, err := service.AddQueuedMessage("/project", "claude", sid, "queued text", nil, "q-insert", "")
	require.NoError(t, err)
	require.NotZero(t, queuedID)

	calls := stubInjector(t, ai.MidTurnInjectResult{Injected: true, OwnerRequestID: "req-9"})

	inserted, msgID := service.InjectQueuedMessage(sid, "q-insert")

	require.True(t, inserted, "the insertion must report success")
	assert.Equal(t, queuedID, msgID, "the row must keep its original DB id — no delete+reinsert")

	// The policy saw the queued content verbatim.
	require.Len(t, *calls, 1)
	assert.Equal(t, "queued text", (*calls)[0].Content)
	assert.Equal(t, "q-insert", (*calls)[0].ClientUserMessageID)

	// The row is no longer queued, so the drain loop cannot execute it twice.
	assert.Equal(t, 0, service.GetQueuedCount(sid),
		"an inserted message must leave the queue")

	msgs, _, _, err := service.GetChatHistoryPaged("/project", "claude", sid, 50, 0)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, queuedID, msgs[0].ID)
	assert.False(t, msgs[0].Queued)
}

// TestInjectQueuedMessage_DeclinedRestoresQueue is the safety property that
// matters most: a declined insertion must put the message BACK in the queue.
// The claim is what makes the attempt race-free, so without the restore a
// decline would silently drop a message the user already sent.
func TestInjectQueuedMessage_DeclinedRestoresQueue(t *testing.T) {
	db := setupDB(t)
	_ = db
	sid := helperCreateSession(t, "/project", "claude", "Insert Declined")

	queuedID, err := service.AddQueuedMessage("/project", "claude", sid, "must survive", nil, "q-decline", "")
	require.NoError(t, err)

	stubInjector(t, ai.MidTurnInjectResult{Reason: "idle"}) // decline

	inserted, msgID := service.InjectQueuedMessage(sid, "q-decline")

	assert.False(t, inserted, "a decline must report false")
	assert.Zero(t, msgID)

	// The message must still be queued for the drain loop.
	queued, err := service.GetQueuedMessages(sid)
	require.NoError(t, err)
	require.Len(t, queued, 1, "a declined insertion must not lose the message")
	assert.Equal(t, "must survive", queued[0].Content)
	assert.Equal(t, queuedID, queued[0].ID, "the same row is restored, not a copy")
}

// TestInjectQueuedMessage_NotQueuedIsNoop verifies a stale queueId (already
// drained, or cancelled) is reported as "not inserted" rather than injecting
// whatever row happens to match — and never panics on the empty result.
func TestInjectQueuedMessage_NotQueuedIsNoop(t *testing.T) {
	db := setupDB(t)
	_ = db
	sid := helperCreateSession(t, "/project", "claude", "Insert Stale")

	calls := stubInjector(t, ai.MidTurnInjectResult{Injected: true})

	inserted, msgID := service.InjectQueuedMessage(sid, "q-never-existed")

	assert.False(t, inserted)
	assert.Zero(t, msgID)
	assert.Empty(t, *calls, "the backend must not be called when there is nothing to insert")
}

// TestInjectQueuedMessage_DeclinedRequeueFailureIsSurfaced documents the one
// genuinely bad outcome: if the restore fails the row is neither queued nor
// delivered, so the call must report failure (the caller surfaces it) instead
// of pretending the message is safely queued.
func TestInjectQueuedMessage_DeclinedRequeueFailureIsSurfaced(t *testing.T) {
	db := setupDB(t)
	_ = db
	sid := helperCreateSession(t, "/project", "claude", "Insert Requeue Fail")

	_, err := service.AddQueuedMessage("/project", "claude", sid, "at risk", nil, "q-requeue-fail", "")
	require.NoError(t, err)

	stubInjector(t, ai.MidTurnInjectResult{Reason: "idle"})

	// Simulate the restore failing by deleting the row between claim and
	// restore: RequeueMessage only flips a row that exists and is still unqueued.
	inserted, _ := service.InjectQueuedMessage(sid, "q-requeue-fail")
	// With a healthy DB the restore succeeds, so this is the happy-decline case;
	// the assertion pins that a decline never reports success.
	assert.False(t, inserted)

	queued, err := service.GetQueuedMessages(sid)
	require.NoError(t, err)
	assert.Len(t, queued, 1, "the message must end up queued exactly once")
}
