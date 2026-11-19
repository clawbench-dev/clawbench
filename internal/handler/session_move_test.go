package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── PATCH /api/ai/session/update { projectPath } — issue #685 ──────────────
//
// Moving a conversation to another existing project must keep its history
// visible, re-file its message rows (history queries filter on BOTH project_id
// and session_id), and refuse targets that are not already registered projects.

// moveTargetProject creates a second registered project directory under the
// watch root and returns its path.
func moveTargetProject(t *testing.T, env *testEnv) string {
	t.Helper()
	target := canonPath(env.ProjectDir + "-target")
	require.NoError(t, os.MkdirAll(target, 0o755))
	// Register it the way any real project is registered (opening it once).
	_, err := store.ProjectIDForPath(target)
	require.NoError(t, err)
	return target
}

func TestServeAISessionUpdate_MoveKeepsHistory(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	target := moveTargetProject(t, env)
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "moving", "claude", "", "default", "chat")
	require.NoError(t, err)
	oldProjectID, err := store.ProjectIDForPath(env.ProjectDir)
	require.NoError(t, err)
	_, err = env.DB().Exec(
		"INSERT INTO chat_history (project_id, role, content, session_id, backend) VALUES (?, 'user', 'hello', ?, 'claude')",
		oldProjectID, sessionID,
	)
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"projectPath": target,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	assertOK(t, w)

	// The conversation is now listed under the target project…
	assert.Equal(t, store.NormalizeProjectPath(target), store.NormalizeProjectPath(service.GetSessionProjectPath(sessionID)))
	// …with its messages, and the message rows moved with it (otherwise the
	// history query, which filters on both ids, would render it empty).
	var msgProjectID int64
	require.NoError(t, env.DB().QueryRow(
		"SELECT project_id FROM chat_history WHERE session_id = ?", sessionID,
	).Scan(&msgProjectID))
	newProjectID, err := store.ProjectIDForPath(target)
	require.NoError(t, err)
	assert.Equal(t, newProjectID, msgProjectID)

	// It must disappear from the OLD project's session list.
	listReq := newRequest(t, http.MethodGet, "/api/ai/sessions", nil)
	listReq = withProjectCookie(listReq, env.ProjectDir)
	listW := callHandler(ServeSessions, listReq)
	assertOK(t, listW)
	assert.NotContains(t, listW.Body.String(), sessionID)
}

func TestServeAISessionUpdate_MoveRejectsUnregisteredProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "stays", "claude", "", "default", "chat")
	require.NoError(t, err)

	// Never registered: a typo / stale client must not file the conversation
	// under it (nor create it).
	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"projectPath": env.ProjectDir + "-never-seen",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	assertStatus(t, w, http.StatusUnprocessableEntity)

	assert.Equal(t, store.NormalizeProjectPath(env.ProjectDir), store.NormalizeProjectPath(service.GetSessionProjectPath(sessionID)))
}

func TestServeAISessionUpdate_MoveRejectsRegisteredProjectWhoseDirectoryVanished(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Registered while the list was loaded, deleted before the user picked it:
	// the row still exists but the agent would have no working directory.
	target := moveTargetProject(t, env)
	require.NoError(t, os.RemoveAll(target))

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "stays", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"projectPath": target,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	assertStatus(t, w, http.StatusUnprocessableEntity)

	assert.Equal(t, store.NormalizeProjectPath(env.ProjectDir), store.NormalizeProjectPath(service.GetSessionProjectPath(sessionID)))
}

func TestServeAISessionUpdate_MoveGroupMovesEveryMemberRow(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	target := moveTargetProject(t, env)
	groupID, _, err := service.CreateGroupWithMembers(env.ProjectDir, "moving group", "claude", []service.GroupMemberSpec{
		{AgentID: "claude", Backend: "claude", DisplayName: "Alice"},
		{AgentID: "claude", Backend: "claude", DisplayName: "Bob"},
	})
	require.NoError(t, err)
	members, err := service.ListGroupMembersIncludingRemoved(groupID)
	require.NoError(t, err)
	require.Len(t, members, 2)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+groupID, map[string]any{
		"projectPath": target,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	assertOK(t, w)

	// The group row and every member row are under the target project now; the
	// group would be half-broken otherwise.
	targetID, err := store.ProjectIDForPath(target)
	require.NoError(t, err)
	for _, id := range append([]string{groupID}, memberIDs(members)...) {
		var projectID int64
		require.NoError(t, env.DB().QueryRow(
			"SELECT project_id FROM chat_sessions WHERE id = ?", id).Scan(&projectID))
		assert.Equal(t, targetID, projectID, "session %s must move with its group", id)
	}
}

func TestServeAISessionUpdate_MoveGroupRefusesUnavailableMembers(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	target := moveTargetProject(t, env)
	groupID, _, err := service.CreateGroupWithMembers(env.ProjectDir, "scoped group", "claude", []service.GroupMemberSpec{
		{AgentID: "claude", Backend: "claude", DisplayName: "Alice"},
	})
	require.NoError(t, err)

	// The member's agent exists only in the SOURCE project, so the target
	// project cannot see or re-add it.
	srcProjectID, err := store.ProjectIDForPath(env.ProjectDir)
	require.NoError(t, err)
	model.Agents["claude"].ProjectID = &srcProjectID

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+groupID, map[string]any{
		"projectPath": target,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	assertStatus(t, w, http.StatusUnprocessableEntity)
	var resp struct {
		MsgKey string         `json:"msgKey"`
		Detail map[string]any `json:"detail"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "MoveMembersUnavailable", resp.MsgKey)
	assert.Equal(t, []any{"Alice"}, resp.Detail["members"])

	assert.Equal(t, store.NormalizeProjectPath(env.ProjectDir), store.NormalizeProjectPath(service.GetSessionProjectPath(groupID)))
}

func TestServeAISessionUpdate_MoveRefusesTaskExecution(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	target := moveTargetProject(t, env)
	// A task execution belongs to the task that produced it.
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "task", "claude", "", "default", "scheduled")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"projectPath": target,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	assertStatus(t, w, http.StatusUnprocessableEntity)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "MoveNotAllowed", resp["msgKey"])

	assert.Equal(t, store.NormalizeProjectPath(env.ProjectDir), store.NormalizeProjectPath(service.GetSessionProjectPath(sessionID)))
}

// memberIDs lists the member row ids (the host row is included).
func memberIDs(members []service.GroupMember) []string {
	ids := make([]string, 0, len(members))
	for _, m := range members {
		ids = append(ids, m.ID)
	}
	return ids
}

func TestServeAISessionUpdate_MoveSameProjectIsNoOp(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "same", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"projectPath": env.ProjectDir,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	assertOK(t, w)
	assert.Equal(t, store.NormalizeProjectPath(env.ProjectDir), store.NormalizeProjectPath(service.GetSessionProjectPath(sessionID)))
}

func TestServeAISessionUpdate_MoveRefusesWhileMessageStreaming(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	target := moveTargetProject(t, env)
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "busy", "claude", "", "default", "chat")
	require.NoError(t, err)
	oldProjectID, err := store.ProjectIDForPath(env.ProjectDir)
	require.NoError(t, err)
	_, err = env.DB().Exec(
		"INSERT INTO chat_history (project_id, role, content, session_id, backend, streaming) VALUES (?, 'assistant', 'partial', ?, 'claude', 1)",
		oldProjectID, sessionID,
	)
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"projectPath": target,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	assertStatus(t, w, http.StatusConflict)

	// Nothing moved: the half-written reply must keep its old attribution.
	assert.Equal(t, store.NormalizeProjectPath(env.ProjectDir), store.NormalizeProjectPath(service.GetSessionProjectPath(sessionID)))
	var msgProjectID int64
	require.NoError(t, env.DB().QueryRow(
		"SELECT project_id FROM chat_history WHERE session_id = ?", sessionID,
	).Scan(&msgProjectID))
	assert.Equal(t, oldProjectID, msgProjectID)
}

// An empty path is not "the current project": it is a client that sent
// nothing usable, and must be refused before any lookup.
func TestServeAISessionUpdate_MoveRejectsEmptyPath(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "stays", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"projectPath": "   ",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	assertStatus(t, w, http.StatusUnprocessableEntity)

	assert.Equal(t, store.NormalizeProjectPath(env.ProjectDir), store.NormalizeProjectPath(service.GetSessionProjectPath(sessionID)))
}

// Anything that is not one of the three known move errors is a genuine write
// failure and must not be dressed up as a 422 the client would "fix" by
// picking another project.
func TestWriteSessionMoveError_UnexpectedIsInternal(t *testing.T) {
	req := newRequest(t, http.MethodPatch, "/api/ai/session/update", nil)
	w := httptest.NewRecorder()

	writeSessionMoveError(w, req, errors.New("disk on fire"))

	assertStatus(t, w, http.StatusInternalServerError)
}

func TestServeAISessionUpdate_MoveRejectsRegisteredProjectOutsideRootPaths(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Registered and on disk, but NOT under a configured root path — what a
	// shared database / multi-root server can hold. Moving there would make
	// every later request for the conversation fail with AccessDenied.
	target := moveTargetProject(t, env)
	origRoots := model.RootPaths
	model.RootPaths = []string{env.WatchDir + "-elsewhere"}
	t.Cleanup(func() { model.RootPaths = origRoots })

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "rootless", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"projectPath": target,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	assertStatus(t, w, http.StatusUnprocessableEntity)
	assertErrorMsgKey(t, w, "MoveTargetInvalid")

	assert.Equal(t, store.NormalizeProjectPath(env.ProjectDir), store.NormalizeProjectPath(service.GetSessionProjectPath(sessionID)))
}

func TestServeAISessionUpdate_MoveRunningChatReportsSessionRunning(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	target := moveTargetProject(t, env)
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "busy", "claude", "", "default", "chat")
	require.NoError(t, err)
	stubSessionRunning(t, sessionID)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"projectPath": target,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	assertStatus(t, w, http.StatusConflict)
	// A single conversation, not a group: the client must not be told the
	// "group is discussing" (that key was previously returned for both).
	assertErrorMsgKey(t, w, "SessionRunning")
}

func TestServeAISessionUpdate_MoveStreamingChatReportsSessionRunning(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	target := moveTargetProject(t, env)
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "half", "claude", "", "default", "chat")
	require.NoError(t, err)
	oldProjectID, err := store.ProjectIDForPath(env.ProjectDir)
	require.NoError(t, err)
	_, err = env.DB().Exec(
		"INSERT INTO chat_history (project_id, role, content, session_id, backend, streaming) VALUES (?, 'assistant', 'partial', ?, 'claude', 1)",
		oldProjectID, sessionID,
	)
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"projectPath": target,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	assertStatus(t, w, http.StatusConflict)
	assertErrorMsgKey(t, w, "SessionRunning")
}

func TestServeAISessionUpdate_MoveRunningGroupReportsGroupRunning(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	target := moveTargetProject(t, env)
	groupID, _, err := service.CreateGroupWithMembers(env.ProjectDir, "busy group", "claude", []service.GroupMemberSpec{
		{AgentID: "claude", Backend: "claude", DisplayName: "Alice"},
	})
	require.NoError(t, err)
	stubSessionRunning(t, groupID)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+groupID, map[string]any{
		"projectPath": target,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	assertStatus(t, w, http.StatusConflict)
	assertErrorMsgKey(t, w, "GroupRunning")
}

func TestServeAISessionUpdate_MoveRefusesProjectScopedChatAgent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	target := moveTargetProject(t, env)
	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "scoped agent", "claude", "claude", "default", "chat")
	require.NoError(t, err)
	origList := model.AgentList
	scoped := &model.Agent{ID: "claude-agent", Name: "Claude Scoped"}
	orig := model.Agents["claude"]
	model.Agents["claude"] = scoped
	model.AgentList = []*model.Agent{scoped}
	t.Cleanup(func() {
		if orig != nil {
			model.Agents["claude"] = orig
		}
		model.AgentList = origList
	})
	srcProjectID, err := store.ProjectIDForPath(env.ProjectDir)
	require.NoError(t, err)
	scoped.ProjectID = &srcProjectID

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"projectPath": target,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	assertStatus(t, w, http.StatusUnprocessableEntity)
	assertErrorMsgKey(t, w, "MoveAgentUnavailable")
	var resp struct {
		Detail map[string]any `json:"detail"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Claude Scoped", resp.Detail["Agent"])

	assert.Equal(t, store.NormalizeProjectPath(env.ProjectDir), store.NormalizeProjectPath(service.GetSessionProjectPath(sessionID)))
}

// stubSessionRunning forces IsSessionRunning to report true for one session
// until the test ends, so the "is anything still writing?" guards can be
// exercised without spawning a real turn.
func stubSessionRunning(t *testing.T, sessionID string) {
	t.Helper()
	orig := service.IsSessionRunning
	service.IsSessionRunning = func(id string) bool {
		return id == sessionID || orig(id)
	}
	t.Cleanup(func() { service.IsSessionRunning = orig })
}

// assertErrorMsgKey decodes the response envelope and checks its msgKey.
func assertErrorMsgKey(t *testing.T, w *httptest.ResponseRecorder, want string) {
	t.Helper()
	var resp struct {
		MsgKey string `json:"msgKey"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "body: %s", w.Body.String())
	assert.Equal(t, want, resp.MsgKey)
}
