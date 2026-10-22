package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The public session-share endpoint is unauthenticated, so payload assembly is
// capped by a semaphore. These tests pin the three verdicts acquireSessionShareSlot
// can return, because each maps to a different response and only one of them is
// "proceed".

// A client that disconnects while queued must not take a slot — otherwise the
// slot leaks and every later reader starves.
func TestAcquireSessionShareSlot_ClientGoneWhileQueued(t *testing.T) {
	held := fillSessionShareSlots(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ok, timedOut := acquireSessionShareSlot(ctx)
	assert.False(t, ok, "a cancelled request must not take a slot")
	assert.False(t, timedOut, "the client going away is not a timeout")
	assert.Len(t, sessionShareSem, held, "no slot may be taken or leaked")
}

// A saturated pool must answer with a timeout verdict rather than waiting
// forever: an unbounded queue piles up goroutines holding request buffers, which
// is worse than refusing service.
func TestAcquireSessionShareSlot_SaturatedPoolTimesOut(t *testing.T) {
	held := fillSessionShareSlots(t)

	orig := sessionShareQueueTimeout
	sessionShareQueueTimeout = 20 * time.Millisecond
	t.Cleanup(func() { sessionShareQueueTimeout = orig })

	start := time.Now()
	ok, timedOut := acquireSessionShareSlot(context.Background())
	assert.False(t, ok)
	assert.True(t, timedOut, "a saturated pool must report a timeout")
	assert.Less(t, time.Since(start), 5*time.Second, "it must give up on the configured bound")
	assert.Len(t, sessionShareSem, held)
}

// With no queue window configured the endpoint answers immediately instead of
// holding the connection at all.
func TestAcquireSessionShareSlot_ZeroTimeoutDoesNotWait(t *testing.T) {
	held := fillSessionShareSlots(t)

	orig := sessionShareQueueTimeout
	sessionShareQueueTimeout = 0
	t.Cleanup(func() { sessionShareQueueTimeout = orig })

	ok, timedOut := acquireSessionShareSlot(context.Background())
	assert.False(t, ok)
	assert.True(t, timedOut)
	assert.Len(t, sessionShareSem, held)
}

// A freed slot is reusable: the semaphore must not lose capacity after a
// request completes.
func TestSessionShareSlot_ReleaseRestoresCapacity(t *testing.T) {
	ok, _ := acquireSessionShareSlot(context.Background())
	require.True(t, ok)
	releaseSessionShareSlot()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ok, timedOut := acquireSessionShareSlot(ctx)
	assert.True(t, ok, "a released slot must be acquirable again")
	assert.False(t, timedOut)
	releaseSessionShareSlot()
}

// fillSessionShareSlots takes every slot and returns how many it holds.
func fillSessionShareSlots(t *testing.T) int {
	t.Helper()
	held := 0
	for held < cap(sessionShareSem) {
		ok, _ := acquireSessionShareSlot(context.Background())
		require.True(t, ok)
		held++
	}
	t.Cleanup(func() {
		for range held {
			releaseSessionShareSlot()
		}
	})
	return held
}

// The semaphore's timeout verdict must reach the client as 503 with Retry-After,
// not as a silently empty 200. Before this, the endpoint blocked without bound
// and the documented 503 never existed.
func TestSharePublicPayload_Returns503WhenSaturated(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, ids := seedShareSession(t, env, "sess-busy")
	token := createSessionShareViaAPI(t, env, sessionID, ids)

	// Occupy every slot and make the queue window vanish, so the handler cannot
	// proceed and cannot wait.
	held := fillSessionShareSlots(t)
	_ = held
	orig := sessionShareQueueTimeout
	sessionShareQueueTimeout = 0
	t.Cleanup(func() { sessionShareQueueTimeout = orig })

	w := callHandler(ServeSharePublic, newRequest(t, http.MethodGet, "/api/share/"+token+"/session", nil))
	assertStatus(t, w, http.StatusServiceUnavailable)
	assert.Equal(t, "5", w.Header().Get("Retry-After"))
	assert.NotEmpty(t, w.Body.String(), "a 503 must carry a message, not an empty body")
}

// A revoked token is still 404 even when the pool is saturated: existence must
// not depend on load, and a saturated pool must not turn a revoked link into a
// 503 that suggests retrying.
func TestSharePublicPayload_RevokedStill404WhenSaturated(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	fillSessionShareSlots(t)
	orig := sessionShareQueueTimeout
	sessionShareQueueTimeout = 0
	t.Cleanup(func() { sessionShareQueueTimeout = orig })

	w := callHandler(ServeSharePublic, newRequest(t, http.MethodGet, "/api/share/deadbeef/session", nil))
	assertStatus(t, w, http.StatusNotFound)
}
