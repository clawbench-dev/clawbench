package service

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// session_runner.go unifies session running state and cancellability into one
// registry entry. These tests pin the invariants that unification exists to
// guarantee — above all that a running session can always be cancelled, and that
// the runner cannot exit and strand a message submitted concurrently.

func TestSubmitRunner_CreatesThenReuses(t *testing.T) {
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	sessionID := "runner-reuse"

	ctx1, created := TryClaimSessionRun(sessionID)
	require.True(t, created, "the first submit creates the runner")
	require.NotNil(t, ctx1)

	ctx2, createdAgain := TryClaimSessionRun(sessionID)
	assert.False(t, createdAgain, "a live runner must be reused, not duplicated")
	assert.Equal(t, ctx1, ctx2, "the reused runner must expose the same context")
	assert.True(t, IsSessionRunning(sessionID))
}

// TestSubmitRunner_ConcurrentCreatesExactlyOne is the core guarantee behind
// "only one execution goroutine per session": under concurrency exactly one
// caller may be told it created the runner.
func TestSubmitRunner_ConcurrentCreatesExactlyOne(t *testing.T) {
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	const goroutines = 32
	sessionID := "runner-concurrent"

	var (
		mu      sync.Mutex
		creates int
		wg      sync.WaitGroup
	)
	start := make(chan struct{})
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			<-start
			if _, created := TryClaimSessionRun(sessionID); created {
				mu.Lock()
				creates++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()

	assert.Equal(t, 1, creates, "exactly one submit may create the runner")
}

// TestRetireRunner_WakeClosesExitRace is the regression test for the reported
// symptom. A submit that lands after the runner last looked at the queue must
// keep the runner alive; if it did not, that message would have no consumer and
// the user would see no answer until they cancelled and re-sent.
func TestRetireRunner_WakeClosesExitRace(t *testing.T) {
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	sessionID := "runner-wake-race"
	_, created := TryClaimSessionRun(sessionID)
	require.True(t, created)

	// Simulate the interleaving: the runner has just found the queue empty and
	// is about to retire, and a send arrives in that window.
	_, createdAgain := TryClaimSessionRun(sessionID)
	require.False(t, createdAgain)

	assert.False(t, retireRunner(sessionID),
		"the runner must not exit while a submit is outstanding")
	assert.True(t, IsSessionRunning(sessionID), "the session must still be running")

	// Only once the pending work is consumed may the runner retire.
	assert.True(t, retireRunner(sessionID))
	assert.False(t, IsSessionRunning(sessionID))
}

func TestRetireRunner_IdleExits(t *testing.T) {
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	sessionID := "runner-idle"
	TryClaimSessionRun(sessionID)

	assert.True(t, retireRunner(sessionID), "an idle runner retires")
	assert.False(t, IsSessionRunning(sessionID))
}

func TestRetireRunner_AlreadyGoneIsIdempotent(t *testing.T) {
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	assert.True(t, retireRunner("runner-absent"),
		"retiring an absent runner reports success so the loop can exit")
}

// TestCancelSession_CancelsThroughRegistry verifies the unified guarantee: a
// session reported as running is always cancellable. This is the invariant that
// let CancelSession drop its old "running but no cancel func" force-clear branch.
func TestCancelSession_CancelsThroughRegistry(t *testing.T) {
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	sessionID := "runner-cancel"
	ctx, created := TryClaimSessionRun(sessionID)
	require.True(t, created)
	require.NoError(t, ctx.Err())

	require.True(t, CancelSession(sessionID))

	assert.Error(t, ctx.Err(), "the execution context must be cancelled")
	assert.False(t, IsSessionRunning(sessionID), "the session must no longer be running")
	assert.Equal(t, "user", GetAndClearCancelReason(sessionID))
}

// TestIsSessionRunning_ImpliesCancelable asserts the structural invariant
// directly: whenever the session reports as running, CancelSession must actually
// stop it (no force-clear fallback, no stuck state).
func TestIsSessionRunning_ImpliesCancelable(t *testing.T) {
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	sessionID := "runner-invariant"
	ctx, _ := TryClaimSessionRun(sessionID)
	require.True(t, IsSessionRunning(sessionID))

	require.True(t, CancelSession(sessionID))
	assert.Error(t, ctx.Err(), "a running session must be genuinely cancellable")
}

func TestFinishSessionRun_RemovesAndCancels(t *testing.T) {
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	sessionID := "runner-finish"
	ctx, _ := TryClaimSessionRun(sessionID)

	FinishSessionRun(sessionID)

	assert.Error(t, ctx.Err(), "finishing a run cancels its context")
	assert.False(t, IsSessionRunning(sessionID))
}

// TestFinishSessionRun_ConcurrentWithCancel exercises the cancel/finish race: a
// user cancel landing while the run is winding down must not panic or leave the
// session stuck running.
func TestFinishSessionRun_ConcurrentWithCancel(t *testing.T) {
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	for i := 0; i < 50; i++ {
		sessionID := "runner-race"
		TryClaimSessionRun(sessionID)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); CancelSession(sessionID) }()
		go func() { defer wg.Done(); FinishSessionRun(sessionID) }()
		wg.Wait()

		assert.False(t, IsSessionRunning(sessionID),
			"after both cancel and finish the session must not be running")
	}
}

func TestSetSessionRunning_TrueRegistersCancelableRunner(t *testing.T) {
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	sessionID := "runner-scheduler-style"
	SetSessionRunning(sessionID, true, true)
	require.True(t, IsSessionRunning(sessionID))

	// A session marked running this way must also be cancellable, otherwise a
	// user cancel would have nothing to act on.
	require.True(t, CancelSession(sessionID))
	assert.False(t, IsSessionRunning(sessionID))
}

func TestSetSessionRunning_FalseRetires(t *testing.T) {
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	sessionID := "runner-clear"
	TryClaimSessionRun(sessionID)
	require.True(t, IsSessionRunning(sessionID))

	SetSessionRunning(sessionID, false, true)

	assert.False(t, IsSessionRunning(sessionID))
}

// TestRegisterExternalExecution_IsCancelable verifies the scheduler's adoption
// path: an externally-owned execution registered this way becomes cancellable
// through CancelSession, which previously could not reach scheduled runs at all.
func TestRegisterExternalExecution_IsCancelable(t *testing.T) {
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	sessionID := "runner-external"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	RegisterExternalExecution(sessionID, ctx, cancel)
	require.True(t, IsSessionRunning(sessionID))

	require.True(t, CancelSession(sessionID))
	assert.Error(t, ctx.Err(), "the external execution must actually be cancelled")
}

func TestGetRunningSessionIDs_ReflectsRegistry(t *testing.T) {
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	TryClaimSessionRun("runner-a")
	TryClaimSessionRun("runner-b")

	ids := GetRunningSessionIDs()
	assert.ElementsMatch(t, []string{"runner-a", "runner-b"}, ids)

	FinishSessionRun("runner-a")
	assert.ElementsMatch(t, []string{"runner-b"}, GetRunningSessionIDs())
}

// --- turn registration now lives on the runner entry ---

func TestTurnRegistration_Lifecycle(t *testing.T) {
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	sessionID := "runner-turn"
	TryClaimSessionRun(sessionID)

	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	id := RegisterSessionTurnCancel(sessionID, cancel)

	gotID, ok := CurrentTurnID(sessionID)
	require.True(t, ok)
	assert.Equal(t, id, gotID)

	UnregisterSessionTurnCancel(sessionID)
	_, ok = CurrentTurnID(sessionID)
	assert.False(t, ok, "a finished turn must not stay interruptible")
}

func TestCurrentTurnID_NoTurnRegistered(t *testing.T) {
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	TryClaimSessionRun("runner-turn-none")
	_, ok := CurrentTurnID("runner-turn-none")
	assert.False(t, ok)
}

// TestInterruptSessionTurnIfCurrent_StaleIDDoesNotCancel verifies the
// check-then-act guard survives the move onto the runner entry: an interrupt
// aimed at an already-finished turn must not kill the turn that replaced it.
func TestInterruptSessionTurnIfCurrent_StaleIDDoesNotCancel(t *testing.T) {
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	sessionID := "runner-turn-stale"
	TryClaimSessionRun(sessionID)

	_, cancelOld := context.WithCancel(context.Background())
	defer cancelOld()
	oldID := RegisterSessionTurnCancel(sessionID, cancelOld)

	// The old turn ends and a new one starts.
	UnregisterSessionTurnCancel(sessionID)
	newCtx, cancelNew := context.WithCancel(context.Background())
	defer cancelNew()
	newID := RegisterSessionTurnCancel(sessionID, cancelNew)
	require.NotEqual(t, oldID, newID)

	// The stale interrupt must be rejected and must NOT cancel the new turn.
	assert.False(t, InterruptSessionTurnIfCurrent(sessionID, oldID))
	assert.NoError(t, newCtx.Err(), "the current turn must survive a stale interrupt")

	// The current interrupt succeeds.
	assert.True(t, InterruptSessionTurnIfCurrent(sessionID, newID))
	assert.Error(t, newCtx.Err())
}

// TestInterruptSessionTurnIfCurrent_OnlyOneWinner verifies the atomic
// read-and-clear: two concurrent interrupts cannot both report success.
func TestInterruptSessionTurnIfCurrent_OnlyOneWinner(t *testing.T) {
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	sessionID := "runner-turn-race"
	TryClaimSessionRun(sessionID)

	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	id := RegisterSessionTurnCancel(sessionID, cancel)

	const goroutines = 16
	var (
		mu      sync.Mutex
		winners int
		wg      sync.WaitGroup
	)
	start := make(chan struct{})
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			<-start
			if InterruptSessionTurnIfCurrent(sessionID, id) {
				mu.Lock()
				winners++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()

	assert.Equal(t, 1, winners, "exactly one interrupt may claim the turn")
}

// TestCancelAllSessions_CancelsEveryRunner verifies graceful shutdown reaches
// every registered execution and records the restart reason the executor needs
// to render its "service restarted" banner.
func TestCancelAllSessions_CancelsEveryRunner(t *testing.T) {
	cleanupAllSessionState()
	t.Cleanup(cleanupAllSessionState)

	ctxA, _ := TryClaimSessionRun("runner-shutdown-a")
	ctxB, _ := TryClaimSessionRun("runner-shutdown-b")

	CancelAllSessions()

	assert.Error(t, ctxA.Err(), "every runner context must be cancelled")
	assert.Error(t, ctxB.Err())
	assert.Empty(t, GetRunningSessionIDs())
	assert.Equal(t, cancelReasonRestart, GetAndClearCancelReason("runner-shutdown-a"))
	assert.Equal(t, cancelReasonRestart, GetAndClearCancelReason("runner-shutdown-b"))
}
