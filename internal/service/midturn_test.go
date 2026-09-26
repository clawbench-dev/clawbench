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

	started, msgID, err := service.EnqueueAndMaybeStart(service.EnqueueStartConfig{
		SessionID:   sid,
		ProjectPath: "/project",
		BackendName: "claude",
		Message:     "should queue",
		QueueID:     "q-9",
	})

	require.NoError(t, err)
	assert.False(t, started, "session is running, so no new run starts")
	// A busy session's message is NOT materialized: it has no chat_history row
	// yet, so there is no message id to hand back.
	assert.Zero(t, msgID, "a queued (not yet run) message has no chat_history id")

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

	inserted, msgID, err := service.InjectQueuedMessage(sid, "q-insert")
	require.NoError(t, err)

	require.True(t, inserted, "the insertion must report success")
	require.NotZero(t, msgID, "the injected message must be materialized with a chat_history id")

	// The policy saw the queued content verbatim.
	require.Len(t, *calls, 1)
	assert.Equal(t, "queued text", (*calls)[0].Content)
	assert.Equal(t, "q-insert", (*calls)[0].ClientUserMessageID)

	// The row is no longer queued, so the drain loop cannot execute it twice.
	assert.Equal(t, 0, service.GetQueuedCount(sid),
		"an inserted message must leave the queue")

	// It is a normal conversation row now, with the content the queue held.
	msgs, _, err := service.GetChatHistoryPaged("/project", "claude", sid, 50, 0)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, msgID, msgs[0].ID)
	assert.Equal(t, "queued text", msgs[0].Content)
}

// TestInjectQueuedMessage_DeclinedRestoresQueue is the safety property that
// matters most: a declined insertion must put the message BACK in the queue.
// The claim is what makes the attempt race-free, so without the restore a
// decline would silently drop a message the user already sent.
func TestInjectQueuedMessage_DeclinedRestoresQueue(t *testing.T) {
	db := setupDB(t)
	_ = db
	sid := helperCreateSession(t, "/project", "claude", "Insert Declined")

	_, err := service.AddQueuedMessage("/project", "claude", sid, "must survive", nil, "q-decline", "")
	require.NoError(t, err)

	stubInjector(t, ai.MidTurnInjectResult{Reason: "idle"}) // decline

	inserted, msgID, err := service.InjectQueuedMessage(sid, "q-decline")
	require.NoError(t, err)

	assert.False(t, inserted, "a decline must report false")
	assert.Zero(t, msgID)

	// The message must still be queued for the drain loop. The restored row
	// keeps its client-facing queue_id (the identity the UI and the drain loop
	// address it by); only the internal table id is freshly allocated, which is
	// why the assertion is on QueueID rather than ID.
	queued, err := service.GetQueuedMessages(sid)
	require.NoError(t, err)
	require.Len(t, queued, 1, "a declined insertion must not lose the message")
	assert.Equal(t, "must survive", queued[0].Text)
	assert.Equal(t, "q-decline", queued[0].QueueID, "the same queue identity is restored, not a copy")
}

// TestInjectQueuedMessage_NotQueuedIsNoop verifies a stale queueId (already
// drained, or cancelled) is reported as "not inserted" rather than injecting
// whatever row happens to match — and never panics on the empty result.
func TestInjectQueuedMessage_NotQueuedIsNoop(t *testing.T) {
	db := setupDB(t)
	_ = db
	sid := helperCreateSession(t, "/project", "claude", "Insert Stale")

	calls := stubInjector(t, ai.MidTurnInjectResult{Injected: true})

	inserted, msgID, err := service.InjectQueuedMessage(sid, "q-never-existed")
	require.NoError(t, err)

	assert.False(t, inserted)
	assert.Zero(t, msgID)
	assert.Empty(t, *calls, "the backend must not be called when there is nothing to insert")
}

// TestInjectQueuedMessage_DeclinedRequeueFailureIsSurfaced verifies the ONE
// genuinely bad outcome: the row was claimed but the restore failed, so it is
// neither queued nor delivered. This MUST come back as an error — returning a
// benign decline would make the handler tell the user "still queued" about a
// message the drain loop can no longer see.
//
// The failure is produced for real (the row is deleted between claim and
// restore) rather than asserted around a healthy DB.
func TestInjectQueuedMessage_DeclinedRequeueFailureIsSurfaced(t *testing.T) {
	db := setupDB(t)
	_ = db
	sid := helperCreateSession(t, "/project", "claude", "Insert Requeue Fail")

	_, err := service.AddQueuedMessage("/project", "claude", sid, "at risk", nil, "q-requeue-fail", "")
	require.NoError(t, err)

	// Decline the injection, and delete the materialized row before the restore
	// runs. The injector stub is the last thing to happen before
	// RequeueMaterialized, so deleting there lands exactly in the window we need
	// to exercise. The materialized row is identified by session+content: a
	// claimed message is a plain user row with no queue anchor column.
	orig := service.SetInjectMidTurnForTest(nil)
	t.Cleanup(func() { service.SetInjectMidTurnForTest(orig) })
	service.SetInjectMidTurnForTest(func(_ context.Context, backendID, sessionID, agentID, content string, files []model.FileEntry, clientUserMessageID string) ai.MidTurnInjectResult {
		// Simulate the row vanishing (e.g. a concurrent rewind) after the claim.
		_, delErr := service.WriteExec("DELETE FROM chat_history WHERE session_id = ? AND role = 'user' AND content = ?", sessionID, content)
		require.NoError(t, delErr)
		return ai.MidTurnInjectResult{Reason: "idle"} // decline
	})

	inserted, msgID, err := service.InjectQueuedMessage(sid, "q-requeue-fail")

	assert.False(t, inserted)
	assert.Zero(t, msgID)
	require.Error(t, err, "a failed restore must be surfaced as an error, not a benign decline")
	assert.ErrorIs(t, err, service.ErrMessageStranded,
		"the error must be identifiable so the handler can tell the user to resend")

	// Nothing is queued: the message really is stranded, which is exactly why
	// the caller must not claim otherwise.
	queued, qerr := service.GetQueuedMessages(sid)
	require.NoError(t, qerr)
	assert.Empty(t, queued)
}

// TestInjectQueuedMessage_DeclinedRequeueSucceeds is the contrast: a decline
// whose restore WORKS must report a benign decline (no error) and leave the
// message queued for the drain loop.
func TestInjectQueuedMessage_DeclinedRequeueSucceeds(t *testing.T) {
	db := setupDB(t)
	_ = db
	sid := helperCreateSession(t, "/project", "claude", "Insert Decline OK")

	_, err := service.AddQueuedMessage("/project", "claude", sid, "still queued", nil, "q-decline-ok", "")
	require.NoError(t, err)

	stubInjector(t, ai.MidTurnInjectResult{Reason: "idle"})

	inserted, msgID, err := service.InjectQueuedMessage(sid, "q-decline-ok")

	assert.False(t, inserted)
	assert.Zero(t, msgID)
	assert.NoError(t, err, "a decline that restored the row is benign, not an error")

	queued, qerr := service.GetQueuedMessages(sid)
	require.NoError(t, qerr)
	require.Len(t, queued, 1, "the message must be back in the queue")
	assert.Equal(t, "still queued", queued[0].Text)
}
