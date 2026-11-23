package handler

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"clawbench/internal/service"
	"clawbench/internal/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A session can leave the visible list through three different handlers, and
// every one of them must announce it. Without the broadcast the OTHER devices
// keep an open chat panel pointing at an id that no longer resolves — the
// reported symptom is "PC keeps showing the session the phone just archived,
// and only notices when a send 404s".
//
// These tests drive the HTTP handler (not service.BroadcastSessionRemoved
// directly) because the defect being pinned is a MISSING call site, and only a
// handler-level test can catch one.

// installRemovalSubscriber wires a WS manager with one subscriber and returns
// it, restoring the previous manager when the test ends.
func installRemovalSubscriber(t *testing.T) *ws.ClientSubscription {
	t.Helper()
	origMgr := ws.GetManager()
	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	t.Cleanup(func() { ws.SetManagerForTest(origMgr) })

	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "removal-test-client", "")
	require.NotNil(t, sub)
	return sub
}

// findSessionRemoved returns the session_removed payload among the buffered
// events, or nil when no such event was broadcast.
func findSessionRemoved(t *testing.T, sub *ws.ClientSubscription) map[string]any {
	t.Helper()
	for _, ev := range sub.GetBufferedEvents() {
		if ev.Event != "session_removed" {
			continue
		}
		b, err := json.Marshal(ev.Data)
		require.NoError(t, err)
		var data map[string]any
		require.NoError(t, json.Unmarshal(b, &data))
		return data
	}
	return nil
}

// Archiving a session that HAS messages must broadcast the removal, carrying the
// session's own project so receivers can tell whose row vanished.
func TestArchiveSession_BroadcastsSessionRemoved(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sub := installRemovalSubscriber(t)

	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "archive-broadcast", "codebuddy", "", "default", "chat")
	require.NoError(t, err)
	// One finalized message keeps the session off the empty-session hard-delete
	// path, so this test covers the soft-archive branch specifically.
	_, err = service.AddChatMessage(env.ProjectDir, "codebuddy", sessionID, "user", "hello", nil, false, "")
	require.NoError(t, err)

	req := newRequest(t, http.MethodDelete, "/api/ai/session/archive?session_id="+sessionID, nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ArchiveSession, req)
	assertOK(t, w)

	data := findSessionRemoved(t, sub)
	require.NotNil(t, data, "archiving a session must broadcast session_removed, got %v", sub.GetBufferedEvents())
	assert.Equal(t, sessionID, data["session_id"])
	assert.Equal(t, env.ProjectDir, data["project_path"])
}

// Archiving an EMPTY session takes the hard-delete branch (a separate call site
// in the handler) and must broadcast too — it removes the row just as visibly.
func TestArchiveEmptySession_BroadcastsSessionRemoved(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sub := installRemovalSubscriber(t)

	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "empty-archive", "codebuddy", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodDelete, "/api/ai/session/archive?session_id="+sessionID, nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ArchiveSession, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Equal(t, true, result["destroyed"], "empty sessions take the hard-delete branch")

	data := findSessionRemoved(t, sub)
	require.NotNil(t, data, "hard-deleting an empty session must broadcast session_removed")
	assert.Equal(t, sessionID, data["session_id"])
	assert.Equal(t, env.ProjectDir, data["project_path"])
}

// Permanently destroying a session must broadcast as well.
func TestDestroySession_BroadcastsSessionRemoved(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sub := installRemovalSubscriber(t)

	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "destroy-broadcast", "codebuddy", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodDelete, "/api/ai/session/destroy?session_id="+sessionID, nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(DestroySession, req)
	assertOK(t, w)

	data := findSessionRemoved(t, sub)
	require.NotNil(t, data, "destroying a session must broadcast session_removed")
	assert.Equal(t, sessionID, data["session_id"])
	assert.Equal(t, env.ProjectDir, data["project_path"])
}

// The project path must be read BEFORE the row disappears. DestroySession is
// reachable WITHOUT a project cookie (the cross-project overview uses it), so if
// the handler fell back to the cookie instead of the session's own project, the
// payload would carry "" and every receiver watching another project would
// mistake the removal for one of its own rows.
func TestDestroySession_BroadcastsOwningProjectWithoutCookie(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sub := installRemovalSubscriber(t)

	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "cross-project-destroy", "codebuddy", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodDelete, "/api/ai/session/destroy?session_id="+sessionID, nil)
	// Deliberately no project cookie — this is the cross-project destroy shape.
	w := callHandler(DestroySession, req)
	assertOK(t, w)

	data := findSessionRemoved(t, sub)
	require.NotNil(t, data, "a cookie-less destroy must still broadcast session_removed")
	assert.Equal(t, sessionID, data["session_id"])
	assert.Equal(t, env.ProjectDir, data["project_path"],
		"the payload must carry the session's own project, not the (absent) caller cookie")
}
