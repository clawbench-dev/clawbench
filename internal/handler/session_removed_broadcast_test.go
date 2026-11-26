package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
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

// The project path must be read BEFORE the row disappears. The caller's cookie
// may name a different project than the session's owner, so the payload must
// carry the session's own project — receivers watching another project would
// otherwise mistake the removal for one of their own rows.
func TestDestroySession_BroadcastsOwningProjectNotCallerProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sub := installRemovalSubscriber(t)

	sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "cross-project-destroy", "codebuddy", "", "default", "chat")
	require.NoError(t, err)

	// A valid cookie for a DIFFERENT project: the handler only requires that a
	// project is selected, not that it owns the session.
	callerDir := filepath.Join(env.WatchDir, "caller-project")
	require.NoError(t, os.MkdirAll(callerDir, 0o755))

	req := newRequest(t, http.MethodDelete, "/api/ai/session/destroy?session_id="+sessionID, nil)
	withProjectCookie(req, callerDir)
	w := callHandler(DestroySession, req)
	assertOK(t, w)

	data := findSessionRemoved(t, sub)
	require.NotNil(t, data, "destroying a session must broadcast session_removed")
	assert.Equal(t, sessionID, data["session_id"])
	assert.Equal(t, env.ProjectDir, data["project_path"],
		"the payload must carry the session's own project, not the caller's cookie")
}

// A project never goes to zero sessions: removing the LAST one makes the server
// create the session that takes over, and every client is told which one it is.
//
// This is what keeps the replacement decision in ONE place. It used to live in
// the frontend, where three paths (the DELETE response, the session_removed
// broadcast, and the 404 recovery) each decided it, so removing a project's only
// session left the user with two new ones — 「移除唯一的会话后反而变成两个」.
//
// All three removal paths share the rule, so all three are checked here.
func TestSessionRemoval_BackfillsAndReportsReplacement(t *testing.T) {
	cases := []struct {
		name string
		// withMessage=false takes the empty-session hard-delete branch, which is
		// a separate call site inside ArchiveSession.
		withMessage bool
		url         func(sessionID string) string
		handler     http.HandlerFunc
	}{
		{
			name:        "archive soft-delete",
			withMessage: true,
			url:         func(id string) string { return "/api/ai/session/archive?session_id=" + id },
			handler:     ArchiveSession,
		},
		{
			name:        "archive empty-session hard-delete",
			withMessage: false,
			url:         func(id string) string { return "/api/ai/session/archive?session_id=" + id },
			handler:     ArchiveSession,
		},
		{
			name:        "destroy",
			withMessage: false,
			url:         func(id string) string { return "/api/ai/session/destroy?session_id=" + id },
			handler:     DestroySession,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, teardown := setupTestEnv(t)
			defer teardown()

			sub := installRemovalSubscriber(t)

			// The project's ONLY session.
			sessionID, err := service.CreateSession(env.ProjectDir, "codebuddy", "only-"+tc.name, "codebuddy", "", "default", "chat")
			require.NoError(t, err)
			if tc.withMessage {
				_, err = service.AddChatMessage(env.ProjectDir, "codebuddy", sessionID, "user", "hello", nil, false, "")
				require.NoError(t, err)
			}

			req := newRequest(t, http.MethodDelete, tc.url(sessionID), nil)
			withProjectCookie(req, env.ProjectDir)
			w := callHandler(tc.handler, req)
			assertOK(t, w)

			var result map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
			replacement, _ := result["replacementSessionId"].(string)
			require.NotEmpty(t, replacement, "removing the last session must report the replacement")

			// The replacement is a real, DIFFERENT session, and it is what the
			// project now holds.
			assert.NotEqual(t, sessionID, replacement)
			latestID, _, err := service.GetLatestSessionID(env.ProjectDir)
			require.NoError(t, err)
			assert.Equal(t, replacement, latestID, "the reported replacement must be the project's session")

			// The broadcast names the same session, so other devices switch to it
			// instead of creating one of their own.
			data := findSessionRemoved(t, sub)
			require.NotNil(t, data, "removal must broadcast session_removed")
			assert.Equal(t, replacement, data["replacement_session_id"])

			// Exactly one session was left behind — the double-creation symptom
			// would show up here as 2.
			count, err := service.GetSessionCount(env.ProjectDir)
			require.NoError(t, err)
			assert.Equal(t, 1, count, "removing the only session must leave exactly one replacement")
		})
	}
}

// Removing one of SEVERAL sessions must not create anything: the project is not
// empty, so clients switch to a session that already existed.
func TestSessionRemoval_NoBackfillWhenSessionsRemain(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sub := installRemovalSubscriber(t)

	keepID, err := service.CreateSession(env.ProjectDir, "codebuddy", "keep", "codebuddy", "", "default", "chat")
	require.NoError(t, err)
	doomedID, err := service.CreateSession(env.ProjectDir, "codebuddy", "doomed", "codebuddy", "", "default", "chat")
	require.NoError(t, err)
	// Soft archive, so the survivor is what remains.
	_, err = service.AddChatMessage(env.ProjectDir, "codebuddy", doomedID, "user", "hello", nil, false, "")
	require.NoError(t, err)

	req := newRequest(t, http.MethodDelete, "/api/ai/session/archive?session_id="+doomedID, nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ArchiveSession, req)
	assertOK(t, w)

	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Empty(t, result["replacementSessionId"], "no replacement is needed while a session remains")

	data := findSessionRemoved(t, sub)
	require.NotNil(t, data, "removal must broadcast session_removed")
	assert.Nil(t, data["replacement_session_id"], "the event must not name a replacement either")

	count, err := service.GetSessionCount(env.ProjectDir)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	latestID, _, err := service.GetLatestSessionID(env.ProjectDir)
	require.NoError(t, err)
	assert.Equal(t, keepID, latestID)
}
