package service

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupDrainTest installs a ws manager for the drain tests and restores the
// previous one when the test ends.
//
// The restore is not optional: the manager is a package-level global, and
// leaving one installed leaks into every later test in the binary. Tests that
// emit session events (via SetSessionRunning, CancelSession, …) then reach
// emitSessionEvent → GetSessionTitle with no test DB configured, which
// dereferences a nil pool and panics — taking down unrelated tests whose
// failures look like real bugs.
// SubmitRunForTest registers a live runner for a session so turn-scoped tests
// have the execution a turn belongs to.
func SubmitRunForTest(t *testing.T, sessionID string) {
	t.Helper()
	_, created := TryClaimSessionRun(sessionID)
	require.True(t, created, "expected to claim an idle session")
	t.Cleanup(func() { FinishSessionRun(sessionID) })
}

func setupDrainTest(t *testing.T) {
	t.Helper()
	prev := ws.GetManager()
	ws.SetManagerForTest(ws.NewManagerForTest())
	t.Cleanup(func() { ws.SetManagerForTest(prev) })
}

// drainTestSchema is the chat_history/chat_sessions schema used by drain tests
// (mirrors the main test schema in chat_test.go but lives in package service).
const drainTestSchema = `
CREATE TABLE IF NOT EXISTS chat_history (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_path TEXT NOT NULL,
	role TEXT NOT NULL CHECK(role IN ('user', 'assistant')),
	content TEXT NOT NULL,
	files TEXT,
	session_id TEXT,
	backend TEXT NOT NULL DEFAULT 'claude',
	streaming INTEGER NOT NULL DEFAULT 0,
	indexed INTEGER NOT NULL DEFAULT 0,
	external_message_id TEXT DEFAULT '',
	queue_id TEXT DEFAULT '',
	queued INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	completed_at DATETIME
);
CREATE TABLE IF NOT EXISTS chat_sessions (
	id TEXT PRIMARY KEY,
	project_path TEXT NOT NULL,
	backend TEXT NOT NULL,
	title TEXT NOT NULL,
	agent_id TEXT DEFAULT '',
	title_renamed INTEGER NOT NULL DEFAULT 0,
	title_source TEXT NOT NULL DEFAULT '',
	archived INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

// setupDrainSession creates a DB session + queued messages for drain tests.
func setupDrainSession(t *testing.T, sessionID string) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	assert.NoError(t, err)
	// database/sql hands ":memory:" a fresh database per connection, so the
	// schema below would be invisible to any second connection the pool opens
	// under load (a local-only pass, CI-only flake). Pin the pool to one
	// connection — the same guard every other in-memory helper in this package
	// uses.
	db.SetMaxOpenConns(1)
	_, err = db.Exec(drainTestSchema)
	assert.NoError(t, err)
	cleanup := SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		db.Close()
	})
	_, err = db.Exec(
		`INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, '/test', 'codebuddy', 'Drain')`,
		sessionID,
	)
	assert.NoError(t, err)
}

func TestDrainLoop_UserCancel_ClearsQueueAndEmitsCancel(t *testing.T) {
	setupDrainTest(t)
	sessionID := "drain-test-user-cancel"
	setupDrainSession(t, sessionID)
	defer ClearQueuedMessages(sessionID)

	// Enqueue some messages so queue_cancel has queueIDs to emit
	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "pending1", nil, "q1", "")
	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "pending2", nil, "q2", "")

	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		ExecuteRunWithMessage: func(msg model.ChatMessage) DrainResult {
			return DrainResult{}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	RunDrainLoop(cfg, DrainResult{CancelReason: cancelReasonUser})
	assert.Equal(t, statusCancelled, finalEvent.Type)

	// Queue should be cleared
	assert.Equal(t, 0, GetQueuedCount(sessionID))
}

func TestDrainLoop_UserCancel_WithQueueIDs_EmitsQueueCancel(t *testing.T) {
	setupDrainTest(t)
	sessionID := "drain-test-queue-cancel-event"
	setupDrainSession(t, sessionID)
	defer ClearQueuedMessages(sessionID)

	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "pending", nil, "qc1", "")

	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		ExecuteRunWithMessage: func(msg model.ChatMessage) DrainResult {
			return DrainResult{}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	// queue_cancel is emitted via ws.EmitToSession which requires a subscriber;
	// we verify the DB side effect (queue cleared) instead.
	RunDrainLoop(cfg, DrainResult{CancelReason: cancelReasonUser})
	assert.Equal(t, statusCancelled, finalEvent.Type)
	assert.Equal(t, 0, GetQueuedCount(sessionID))
}

func TestDrainLoop_UserCancel_NoQueueIDs_NoQueueCancelEvent(t *testing.T) {
	setupDrainTest(t)
	sessionID := "drain-test-no-queue-ids"
	setupDrainSession(t, sessionID)
	defer ClearQueuedMessages(sessionID)

	// No messages in queue — queue_cancel should not be emitted (no queueIDs)
	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		ExecuteRunWithMessage: func(msg model.ChatMessage) DrainResult {
			return DrainResult{}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	RunDrainLoop(cfg, DrainResult{CancelReason: cancelReasonUser})
	assert.Equal(t, statusCancelled, finalEvent.Type)
}

func TestDrainLoop_ErrorResult_EmitsErrorEvent(t *testing.T) {
	setupDrainTest(t)
	sessionID := "drain-test-error"
	setupDrainSession(t, sessionID)
	defer ClearQueuedMessages(sessionID)

	// Messages still queued when the run errors must be cleared (ISS-239),
	// otherwise they stay queued=1 forever with no drain loop left to run them.
	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "stuck1", nil, "q-e1", "")
	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "stuck2", nil, "q-e2", "")

	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		ExecuteRunWithMessage: func(msg model.ChatMessage) DrainResult {
			return DrainResult{}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	RunDrainLoop(cfg, DrainResult{Err: "runtime failure"})
	assert.Equal(t, "error", finalEvent.Type)
	assert.Equal(t, "runtime failure", finalEvent.Error)
	// The queued messages behind the failed run must not be left stuck.
	assert.Equal(t, 0, GetQueuedCount(sessionID))
}

func TestDrainLoop_EmptyResult_EmitsErrorWithReason(t *testing.T) {
	setupDrainTest(t)
	sessionID := "drain-test-empty"
	setupDrainSession(t, sessionID)
	defer ClearQueuedMessages(sessionID)

	// An empty result aborts the queue just like an error.
	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "stuck", nil, "q-emp", "")

	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		ExecuteRunWithMessage: func(msg model.ChatMessage) DrainResult {
			return DrainResult{}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	RunDrainLoop(cfg, DrainResult{Empty: true})
	assert.Equal(t, "error", finalEvent.Type)
	assert.Equal(t, "AI returned no content", finalEvent.Error)
	assert.Equal(t, ai.ReasonEmpty, finalEvent.Reason)
	assert.Equal(t, 0, GetQueuedCount(sessionID))
}

func TestDrainLoop_NonUserCancelReason_EmitsCancelled(t *testing.T) {
	setupDrainTest(t)
	sessionID := "drain-test-other-cancel"
	setupDrainSession(t, sessionID)
	defer ClearQueuedMessages(sessionID)

	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		ExecuteRunWithMessage: func(msg model.ChatMessage) DrainResult {
			return DrainResult{}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	RunDrainLoop(cfg, DrainResult{CancelReason: "disconnect"})
	assert.Equal(t, statusCancelled, finalEvent.Type)
}

func TestDrainLoop_QueueEmpty_EmitsDone(t *testing.T) {
	setupDrainTest(t)
	sessionID := "drain-test-empty-queue"
	setupDrainSession(t, sessionID)
	defer ClearQueuedMessages(sessionID)

	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		ExecuteRunWithMessage: func(msg model.ChatMessage) DrainResult {
			return DrainResult{}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	RunDrainLoop(cfg, DrainResult{})
	assert.Equal(t, "done", finalEvent.Type)
}

func TestDrainLoop_QueueHasNextMessage_ExecutesAndLoops(t *testing.T) {
	setupDrainTest(t)
	sessionID := "drain-test-next-msg"
	setupDrainSession(t, sessionID)
	defer ClearQueuedMessages(sessionID)

	// Enqueue two messages via DB
	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "first queued", []model.FileEntry{{Path: "/a/b.go"}}, "q1", "")
	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "second queued", nil, "q2", "")

	var executeCount int32
	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		ExecuteRunWithMessage: func(msg model.ChatMessage) DrainResult {
			atomic.AddInt32(&executeCount, 1)
			return DrainResult{}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	RunDrainLoop(cfg, DrainResult{})

	// Both queued messages should have been executed
	assert.Equal(t, int32(2), atomic.LoadInt32(&executeCount))
	assert.Equal(t, "done", finalEvent.Type)
}

// TestDrainLoop_QueueMessageReturnsError_StopsLoopAndClearsRest is the exact
// ISS-239 reproduce: several messages queued, an intermediate run fails — the
// remaining queued messages must be cleared (not left pending forever) and the
// loop exits with an error event.
func TestDrainLoop_QueueMessageReturnsError_StopsLoopAndClearsRest(t *testing.T) {
	setupDrainTest(t)
	sessionID := "drain-test-msg-error"
	setupDrainSession(t, sessionID)
	defer ClearQueuedMessages(sessionID)

	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "ok", nil, "q-ok", "")
	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "will error", nil, "q-err", "")
	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "behind error", nil, "q-later", "")

	var executeCount int32
	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		ExecuteRunWithMessage: func(msg model.ChatMessage) DrainResult {
			atomic.AddInt32(&executeCount, 1)
			if msg.QueueID == "q-err" {
				return DrainResult{Err: "execution failed"}
			}
			return DrainResult{}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	RunDrainLoop(cfg, DrainResult{})
	assert.Equal(t, "error", finalEvent.Type)
	assert.Equal(t, "execution failed", finalEvent.Error)
	// The failing message ran, the one behind it must NOT run…
	assert.Equal(t, int32(2), atomic.LoadInt32(&executeCount))
	// …and the queue must be fully cleared so nothing is stuck pending.
	assert.Equal(t, 0, GetQueuedCount(sessionID))
}

func TestDrainLoop_QueueMessageCancelled_StopsLoop(t *testing.T) {
	setupDrainTest(t)
	sessionID := "drain-test-msg-cancel"
	setupDrainSession(t, sessionID)
	defer ClearQueuedMessages(sessionID)

	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "will cancel", nil, "q1", "")

	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		ExecuteRunWithMessage: func(msg model.ChatMessage) DrainResult {
			return DrainResult{CancelReason: cancelReasonUser}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	RunDrainLoop(cfg, DrainResult{})
	assert.Equal(t, statusCancelled, finalEvent.Type)
}

func TestDrainLoop_UserCancelWithQueueIDsOnly_IncludesOnlyNonEmptyQueueIDs(t *testing.T) {
	setupDrainTest(t)
	sessionID := "drain-test-mixed-qids"
	setupDrainSession(t, sessionID)
	defer ClearQueuedMessages(sessionID)

	// Mix: one with QueueID, one without (AddQueuedMessage auto-generates when empty)
	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "a", nil, "has-id", "")
	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "no-id", nil, "", "")

	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		ExecuteRunWithMessage: func(msg model.ChatMessage) DrainResult {
			return DrainResult{}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	RunDrainLoop(cfg, DrainResult{CancelReason: cancelReasonUser})
	assert.Equal(t, statusCancelled, finalEvent.Type)
	assert.Equal(t, 0, GetQueuedCount(sessionID))
}

// TestRetireRunner_KeepsGoingWhenWorkArrived verifies the exit decision cannot
// strand a message. A send that lands after the loop last checked the queue must
// keep the runner alive: retiring would leave that message with no consumer,
// which is the "no answer until I cancel and re-send" bug this replaced the
// SignalDrain/WaitForEnqueue dance to prevent.
func TestRetireRunner_KeepsGoingWhenWorkArrived(t *testing.T) {
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	sessionID := "drain-retire-race"
	_, created := TryClaimSessionRun(sessionID)
	require.True(t, created)

	// A concurrent send arrives: it sees the runner and marks it as having work.
	_, createdAgain := TryClaimSessionRun(sessionID)
	require.False(t, createdAgain, "the second submit must reuse the runner")

	// The runner must NOT retire — the work has to be picked up.
	assert.False(t, retireRunner(sessionID), "runner must keep going when work arrived")
	assert.True(t, IsSessionRunning(sessionID), "session must still be running")

	// Once the work is drained, the next retire succeeds.
	assert.True(t, retireRunner(sessionID), "runner may exit when nothing is pending")
	assert.False(t, IsSessionRunning(sessionID))
}

// TestRetireRunner_ExitsWhenIdle verifies the plain exit path: no late work
// means the runner retires and the session stops being reported as running.
func TestRetireRunner_ExitsWhenIdle(t *testing.T) {
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	sessionID := "drain-retire-idle"
	_, created := TryClaimSessionRun(sessionID)
	require.True(t, created)

	assert.True(t, retireRunner(sessionID))
	assert.False(t, IsSessionRunning(sessionID))
}

// TestCancelQueuedMessage_DeletesRow verifies that canceling a queued message
// truly deletes its chat_history row — the canceled message must never resurface
// as a formal message after the current turn completes (regression: previously
// it only flipped queued=0, leaving an indistinguishable "no-reply user
// message" that loadHistory resurrected).
func TestCancelQueuedMessage_DeletesRow(t *testing.T) {
	setupDrainTest(t)
	sessionID := "drain-cancel-delete"
	setupDrainSession(t, sessionID)

	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "will cancel", nil, "q-cancel", "")
	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "stays", nil, "q-keep", "")
	assert.Equal(t, 2, GetQueuedCount(sessionID))

	err := CancelQueuedMessage(sessionID, "q-cancel")
	assert.NoError(t, err)

	assert.Equal(t, 1, GetQueuedCount(sessionID))

	// The canceled row is gone from chat_history entirely.
	var remaining int
	err = UnsafeDBForTest().QueryRow(
		"SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND queue_id = ?",
		sessionID, "q-cancel",
	).Scan(&remaining)
	assert.NoError(t, err)
	assert.Zero(t, remaining, "canceled queued message must be deleted, not kept as queued=0")
}

// TestCancelQueuedMessage_Idempotent verifies canceling a queueId that is no
// longer queued (already drained or already canceled) is a no-op and harmless.
func TestCancelQueuedMessage_Idempotent(t *testing.T) {
	setupDrainTest(t)
	sessionID := "drain-cancel-idempotent"
	setupDrainSession(t, sessionID)

	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "already gone", nil, "q-gone", "")
	assert.NoError(t, CancelQueuedMessage(sessionID, "q-gone"))
	// Second cancel — row already deleted, must not error.
	assert.NoError(t, CancelQueuedMessage(sessionID, "q-gone"))
	assert.Equal(t, 0, GetQueuedCount(sessionID))
}

// TestClearQueuedMessages_DeletesRows verifies clearing the queue (session
// cancel / force-cancel) deletes the queued rows outright.
func TestClearQueuedMessages_DeletesRows(t *testing.T) {
	setupDrainTest(t)
	sessionID := "drain-clear-delete"
	setupDrainSession(t, sessionID)

	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "a", nil, "q-a", "")
	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "b", nil, "q-b", "")
	assert.Equal(t, 2, GetQueuedCount(sessionID))

	assert.NoError(t, ClearQueuedMessages(sessionID))

	assert.Equal(t, 0, GetQueuedCount(sessionID))
	var remaining int
	err := UnsafeDBForTest().QueryRow(
		"SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND queue_id != ''",
		sessionID,
	).Scan(&remaining)
	assert.NoError(t, err)
	assert.Zero(t, remaining, "cleared queued messages must be deleted, not kept as queued=0")
}

func TestDrainLoop_PersistentDequeueError_AbortsAfterRetryWindow(t *testing.T) {
	setupDrainTest(t)
	sessionID := "drain-test-persistent-err"
	setupDrainSession(t, sessionID)
	defer ClearQueuedMessages(sessionID)

	// A queued message exists so the abort path has a queue to clear.
	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "pending", nil, "q-err", "")

	// Replace the dequeue with one that always fails.
	origDequeue := dequeueQueuedMessage
	dequeueQueuedMessage = func(sessionID string) (model.ChatMessage, bool, error) {
		return model.ChatMessage{}, false, assert.AnError
	}
	defer func() { dequeueQueuedMessage = origDequeue }()

	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		ExecuteRunWithMessage: func(msg model.ChatMessage) DrainResult {
			return DrainResult{}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	start := time.Now()
	RunDrainLoop(cfg, DrainResult{})

	// Must terminate with an error event instead of spinning forever.
	assert.Equal(t, eventTypeError, finalEvent.Type)
	assert.Contains(t, finalEvent.Error, "dequeue failed")
	// Bounded: 5 retries × 100ms ≈ 500ms, far below an infinite loop.
	assert.Less(t, time.Since(start), 5*time.Second, "drain loop must not spin forever on persistent dequeue failure")
	// Queue was cleared by the abort path so no stuck pending message survives.
	assert.Equal(t, 0, GetQueuedCount(sessionID))
}

func TestDrainLoop_TransientDequeueError_RetriesAndRecovers(t *testing.T) {
	setupDrainTest(t)
	sessionID := "drain-test-transient-err"
	setupDrainSession(t, sessionID)
	defer ClearQueuedMessages(sessionID)

	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "msg", nil, "q-tx", "")

	// Fail the first 2 dequeue calls, then succeed — simulating a brief DB blip.
	origDequeue := dequeueQueuedMessage
	var calls int
	dequeueQueuedMessage = func(sid string) (model.ChatMessage, bool, error) {
		calls++
		if calls <= 2 {
			return model.ChatMessage{}, false, assert.AnError
		}
		return origDequeue(sid)
	}
	defer func() { dequeueQueuedMessage = origDequeue }()

	var executeCount int32
	var finalEvent ai.StreamEvent
	cfg := DrainConfig{
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		ExecuteRunWithMessage: func(msg model.ChatMessage) DrainResult {
			atomic.AddInt32(&executeCount, 1)
			return DrainResult{}
		},
		MarkDoneAndSendFinal: func(event ai.StreamEvent) {
			finalEvent = event
		},
	}

	RunDrainLoop(cfg, DrainResult{})

	// Transient failures are absorbed; the queued message still executes and
	// the loop finishes with a normal done.
	assert.Equal(t, int32(1), atomic.LoadInt32(&executeCount))
	assert.Equal(t, "done", finalEvent.Type)
}

// TestDrainHandleTerminal_InterruptKeepsQueue is the core guard for the
// "interrupt and send" action: an interrupted turn must NOT drop the queue and
// must NOT emit a terminal event, because the queued message is exactly what
// should run next. Contrast with the user-cancel branch, which clears the queue.
func TestDrainHandleTerminal_InterruptKeepsQueue(t *testing.T) {
	setupDrainTest(t)
	sessionID := "drain-test-interrupt"
	setupDrainSession(t, sessionID)
	defer ClearQueuedMessages(sessionID)

	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "next message", nil, "q-interrupt", "")

	var finalEvents []ai.StreamEvent
	cfg := DrainConfig{
		SessionID:            sessionID,
		MarkDoneAndSendFinal: func(e ai.StreamEvent) { finalEvents = append(finalEvents, e) },
	}

	done := drainHandleTerminal(cfg, DrainResult{CancelReason: cancelReasonInterrupt})

	if done {
		t.Fatal("interrupt must NOT end the drain loop — the next queued message still has to run")
	}
	if len(finalEvents) != 0 {
		t.Errorf("interrupt must not emit a terminal event, got %+v", finalEvents)
	}

	// The queue must survive: this is what separates interrupt from cancel.
	queued, err := GetQueuedMessages(sessionID)
	if err != nil {
		t.Fatalf("GetQueuedMessages failed: %v", err)
	}
	if len(queued) != 1 || queued[0].Content != "next message" {
		t.Fatalf("the queued message must survive an interrupt, got %+v", queued)
	}
}

// TestDrainHandleTerminal_UserCancelStillClearsQueue pins the contrast: the
// existing cancel semantics must be unchanged by the interrupt branch above.
func TestDrainHandleTerminal_UserCancelStillClearsQueue(t *testing.T) {
	setupDrainTest(t)
	sessionID := "drain-test-cancel-contrast"
	setupDrainSession(t, sessionID)
	defer ClearQueuedMessages(sessionID)

	_, _ = AddQueuedMessage("/test", "codebuddy", sessionID, "dropped", nil, "q-cancel", "")

	var finalEvents []ai.StreamEvent
	cfg := DrainConfig{
		SessionID:            sessionID,
		MarkDoneAndSendFinal: func(e ai.StreamEvent) { finalEvents = append(finalEvents, e) },
	}

	done := drainHandleTerminal(cfg, DrainResult{CancelReason: cancelReasonUser})

	if !done {
		t.Fatal("user cancel must end the drain loop")
	}
	if len(finalEvents) != 1 || finalEvents[0].Type != statusCancelled {
		t.Errorf("user cancel must emit exactly one cancelled event, got %+v", finalEvents)
	}
	queued, _ := GetQueuedMessages(sessionID)
	if len(queued) != 0 {
		t.Errorf("user cancel must clear the queue, got %d rows", len(queued))
	}
}

// TestInterruptSessionTurn verifies the turn-scoped cancel registry: it must
// report whether a turn was actually stopped, and record the interrupt reason
// so the executor finalizes the turn without stamping it "cancelled".
func TestInterruptSessionTurn(t *testing.T) {
	sessionID := "interrupt-turn-test"
	// Turn registration requires a live runner (a turn belongs to an execution).
	SubmitRunForTest(t, sessionID)

	// No turn registered → nothing to interrupt.
	if InterruptSessionTurnIfCurrent(sessionID, 1) {
		t.Fatal("interrupting with no registered turn must report false")
	}

	// Register a turn and interrupt it.
	ctx, cancel := context.WithCancel(context.Background())
	turnID := RegisterSessionTurnCancel(sessionID, cancel)
	t.Cleanup(func() { UnregisterSessionTurnCancel(sessionID) })

	if !InterruptSessionTurnIfCurrent(sessionID, turnID) {
		t.Fatal("interrupting a registered turn must report true")
	}
	select {
	case <-ctx.Done():
		// expected: the turn's context was cancelled
	default:
		t.Fatal("the turn's context must be cancelled")
	}

	// The reason must be recorded for the executor to read.
	if reason := GetAndClearCancelReason(sessionID); reason != cancelReasonInterrupt {
		t.Errorf("cancel reason = %q, want %q", reason, cancelReasonInterrupt)
	}

	// The registry entry is consumed — a second interrupt is a no-op.
	if InterruptSessionTurnIfCurrent(sessionID, turnID) {
		t.Error("a consumed turn must not be interruptible twice")
	}
}

// TestInterruptSessionTurn_DoesNotClobberExistingReason is the guard against a
// real user cancel being swallowed by a concurrent interrupt.
//
// sessionCancelReasons is a single slot per session; the executor reads it as
// "why did this turn end". If InterruptSessionTurn overwrote a "user" reason
// with "interrupt", the drain loop would keep the queue and treat a genuine
// cancel as a redirect — the user's cancel would be ignored and later queued
// messages would run against a cancelled session context.
func TestInterruptSessionTurn_DoesNotClobberExistingReason(t *testing.T) {
	sessionID := "interrupt-reason-ownership"
	SubmitRunForTest(t, sessionID)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	turnID := RegisterSessionTurnCancel(sessionID, cancel)
	t.Cleanup(func() { UnregisterSessionTurnCancel(sessionID) })

	// A user cancel lands first (this is what CancelSession does).
	SetCancelReason(sessionID, cancelReasonUser)

	// The interrupt must still stop the turn (the caller asked to)...
	if !InterruptSessionTurnIfCurrent(sessionID, turnID) {
		t.Fatal("interrupt must still stop a registered turn")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("the turn's context must be cancelled")
	}

	// ...but it must NOT steal the reason: the user cancel must win.
	if reason := GetAndClearCancelReason(sessionID); reason != cancelReasonUser {
		t.Errorf("cancel reason = %q, want %q (the interrupt must not clobber it)",
			reason, cancelReasonUser)
	}
}

// TestInterruptSessionTurnIfCurrent_RefusesReplacedTurn is the guard for the
// check-then-act race: the caller reads the current turn id, then the old turn
// finishes and the drain loop starts the NEXT queued message before the
// interrupt lands. Interrupting that new turn would cut off a reply the user
// never asked to stop, so the id check must refuse.
func TestInterruptSessionTurnIfCurrent_RefusesReplacedTurn(t *testing.T) {
	sessionID := "interrupt-replaced-turn"
	SubmitRunForTest(t, sessionID)

	// The turn the caller inspected (T1).
	ctx1, cancel1 := context.WithCancel(context.Background())
	t.Cleanup(cancel1)
	turn1 := RegisterSessionTurnCancel(sessionID, cancel1)

	// T1 ends and the drain loop starts T2 before the interrupt is applied.
	UnregisterSessionTurnCancel(sessionID)
	ctx2, cancel2 := context.WithCancel(context.Background())
	t.Cleanup(cancel2)
	RegisterSessionTurnCancel(sessionID, cancel2)
	t.Cleanup(func() { UnregisterSessionTurnCancel(sessionID) })

	// The stale id must NOT stop T2.
	if InterruptSessionTurnIfCurrent(sessionID, turn1) {
		t.Fatal("a stale turn id must not interrupt the turn that replaced it")
	}
	select {
	case <-ctx2.Done():
		t.Fatal("the NEW turn must be left running")
	default:
	}
	select {
	case <-ctx1.Done():
		t.Fatal("T1 is already over; it must not be touched")
	default:
	}
}

// TestInterruptSessionTurn_ClaimsEmptySlot verifies the normal case: when no
// reason was recorded, the interrupt records its own so the executor knows the
// turn was redirected (not cancelled).
func TestInterruptSessionTurn_ClaimsEmptySlot(t *testing.T) {
	sessionID := "interrupt-claims-empty"
	SubmitRunForTest(t, sessionID)

	_, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	turnID := RegisterSessionTurnCancel(sessionID, cancel)
	t.Cleanup(func() { UnregisterSessionTurnCancel(sessionID) })

	if !InterruptSessionTurnIfCurrent(sessionID, turnID) {
		t.Fatal("interrupt must stop a registered turn")
	}
	if reason := GetAndClearCancelReason(sessionID); reason != cancelReasonInterrupt {
		t.Errorf("cancel reason = %q, want %q", reason, cancelReasonInterrupt)
	}
}
