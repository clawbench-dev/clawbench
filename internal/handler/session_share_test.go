package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedShareSession inserts a session owned by env.ProjectDir with two finalized
// messages, and returns the session id plus the message ids.
func seedShareSession(t *testing.T, env *testEnv, sessionID string) (string, []int64) {
	t.Helper()

	_, err := env.DB().Exec(
		`INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, model)
		 VALUES (?, ?, 'codebuddy', 'Shared chat', 'codebuddy', 'claude-sonnet-4')`,
		sessionID, env.ProjectDir,
	)
	require.NoError(t, err)

	ids := make([]int64, 0, 2)
	for _, m := range []struct{ role, content string }{
		{"user", "hello"},
		{"assistant", `{"blocks":[{"type":"text","text":"hi there"}]}`},
	} {
		res, err := env.DB().Exec(
			`INSERT INTO chat_history (project_path, session_id, role, content, backend)
			 VALUES (?, ?, ?, ?, 'codebuddy')`,
			env.ProjectDir, sessionID, m.role, m.content,
		)
		require.NoError(t, err)
		id, err := res.LastInsertId()
		require.NoError(t, err)
		ids = append(ids, id)
	}
	return sessionID, ids
}

// createSessionShareViaAPI creates a conversation share and returns the token.
func createSessionShareViaAPI(t *testing.T, env *testEnv, sessionID string, messageIDs []int64) string {
	t.Helper()
	body := map[string]any{"sessionId": sessionID}
	if messageIDs != nil {
		body["messageIds"] = messageIDs
	}
	req := newRequest(t, http.MethodPost, "/api/share/session", body)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionShareManage, req)
	assertOK(t, w)

	var resp sessionShareResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.Token)
	assert.Equal(t, "/share/"+resp.Token, resp.Path)
	return resp.Token
}

// ─── Management endpoint ─────────────────────────────────────────────────────

func TestSessionShareManage_CreateStatusRevoke(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, _ := seedShareSession(t, env, "sess-1")
	token := createSessionShareViaAPI(t, env, sessionID, nil)

	// GET status → token present, and the message list is returned in the same call.
	req := newRequest(t, http.MethodGet, "/api/share/session?session_id="+sessionID, nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionShareManage, req)
	assertOK(t, w)

	var status sessionShareStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &status))
	assert.Equal(t, token, status.Token)
	assert.Equal(t, 2, status.MessageCount)
	require.Len(t, status.Messages, 2, "the dialog needs the message list from the same request")
	assert.Equal(t, "user", status.Messages[0].Role)
	assert.Equal(t, "hello", status.Messages[0].Preview)

	// DELETE revokes.
	req = newRequest(t, http.MethodDelete, "/api/share/session?session_id="+sessionID, nil)
	withProjectCookie(req, env.ProjectDir)
	w = callHandler(ServeSessionShareManage, req)
	assertOK(t, w)

	// Status now reports no token but still lists messages.
	req = newRequest(t, http.MethodGet, "/api/share/session?session_id="+sessionID, nil)
	withProjectCookie(req, env.ProjectDir)
	w = callHandler(ServeSessionShareManage, req)
	assertOK(t, w)
	status = sessionShareStatusResponse{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &status))
	assert.Empty(t, status.Token)
	assert.Len(t, status.Messages, 2)
}

// The dialog lists in-flight messages so it can show them disabled; the flag has
// to survive the round trip or the UI cannot tell them apart.
func TestSessionShareManage_StatusReportsInFlightFlags(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, _ := seedShareSession(t, env, "sess-1")
	_, err := env.DB().Exec(
		`INSERT INTO chat_history (project_path, session_id, role, content, backend, streaming)
		 VALUES (?, ?, 'assistant', 'typing', 'codebuddy', 1)`,
		env.ProjectDir, sessionID,
	)
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/share/session?session_id="+sessionID, nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionShareManage, req)
	assertOK(t, w)

	var status sessionShareStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &status))
	require.Len(t, status.Messages, 3)
	assert.False(t, status.Messages[0].Streaming)
	assert.True(t, status.Messages[2].Streaming, "streaming flag must reach the dialog")
}

func TestSessionShareManage_CreateRotatesToken(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, _ := seedShareSession(t, env, "sess-1")
	token1 := createSessionShareViaAPI(t, env, sessionID, nil)
	token2 := createSessionShareViaAPI(t, env, sessionID, nil)
	assert.NotEqual(t, token1, token2, "re-sharing rotates the token")

	// The old link is dead.
	req := newRequest(t, http.MethodGet, "/api/share/"+token1+"/session", nil)
	w := callHandler(ServeSharePublic, req)
	assertStatus(t, w, http.StatusNotFound)
}

// A selection naming a streaming message is a client bug; the server rejects so
// the frozen snapshot cannot silently omit what the user selected.
func TestSessionShareManage_RejectsStreamingSelection(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, ids := seedShareSession(t, env, "sess-1")
	res, err := env.DB().Exec(
		`INSERT INTO chat_history (project_path, session_id, role, content, backend, streaming)
		 VALUES (?, ?, 'assistant', 'typing', 'codebuddy', 1)`,
		env.ProjectDir, sessionID,
	)
	require.NoError(t, err)
	streamingID, err := res.LastInsertId()
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/share/session", map[string]any{
		"sessionId":  sessionID,
		"messageIds": []int64{ids[0], streamingID},
	})
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionShareManage, req)
	assertStatus(t, w, http.StatusBadRequest)
}

// Ownership: a session belonging to another project must not be shareable, or a
// cookie holder could freeze someone else's conversation.
func TestSessionShareManage_RejectsForeignSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, _ := seedShareSession(t, env, "sess-other")
	_, err := env.DB().Exec(`UPDATE chat_sessions SET project_path = ? WHERE id = ?`, "/some/other/project", sessionID)
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/share/session", map[string]any{"sessionId": sessionID})
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionShareManage, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestSessionShareManage_UnknownSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/share/session", map[string]any{"sessionId": "nope"})
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionShareManage, req)
	assertStatus(t, w, http.StatusNotFound)
}

func TestSessionShareManage_MissingSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/share/session", map[string]any{})
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionShareManage, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestSessionShareManage_MethodNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPut, "/api/share/session", map[string]any{"sessionId": "x"})
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionShareManage, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

// ─── Public endpoints ────────────────────────────────────────────────────────

func TestShareMeta_ReportsKind(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, _ := seedShareSession(t, env, "sess-1")
	sessionToken := createSessionShareViaAPI(t, env, sessionID, nil)

	absPath := createShareTestFile(t, env, "docs/a.md", "# Hello")
	fileToken := createShareViaAPI(t, env, absPath)

	cases := []struct {
		name  string
		token string
		want  string
	}{
		{"session token", sessionToken, "session"},
		{"file token", fileToken, "file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := newRequest(t, http.MethodGet, "/api/share/"+tc.token+"/meta", nil)
			w := callHandler(ServeSharePublic, req)
			assertOK(t, w)

			var meta shareMetaResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &meta))
			assert.Equal(t, tc.want, meta.Kind)
		})
	}

	t.Run("unknown token is a uniform 404", func(t *testing.T) {
		req := newRequest(t, http.MethodGet, "/api/share/deadbeef/meta", nil)
		w := callHandler(ServeSharePublic, req)
		assertStatus(t, w, http.StatusNotFound)
	})
}

// The dispatch refactor's regression guard: a session token has no file_shares
// row, so looking that up first would 404 every session request before it could
// reach its own branch. Both directions must work.
func TestSharePublic_TokenKindsDoNotCrossContaminate(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, _ := seedShareSession(t, env, "sess-1")
	sessionToken := createSessionShareViaAPI(t, env, sessionID, nil)
	absPath := createShareTestFile(t, env, "docs/a.md", "# Hello")
	fileToken := createShareViaAPI(t, env, absPath)

	t.Run("session token serves the snapshot", func(t *testing.T) {
		req := newRequest(t, http.MethodGet, "/api/share/"+sessionToken+"/session", nil)
		w := callHandler(ServeSharePublic, req)
		assertOK(t, w)
		assert.Contains(t, w.Body.String(), `"messages"`)
		assert.Contains(t, w.Body.String(), "hi there")
	})

	t.Run("session token is not a file", func(t *testing.T) {
		req := newRequest(t, http.MethodGet, "/api/share/"+sessionToken+"/file", nil)
		w := callHandler(ServeSharePublic, req)
		assertStatus(t, w, http.StatusNotFound)
	})

	t.Run("file token has no session payload", func(t *testing.T) {
		req := newRequest(t, http.MethodGet, "/api/share/"+fileToken+"/session", nil)
		w := callHandler(ServeSharePublic, req)
		assertStatus(t, w, http.StatusNotFound)
	})

	t.Run("file token still serves its file", func(t *testing.T) {
		req := newRequest(t, http.MethodGet, "/api/share/"+fileToken+"/file", nil)
		w := callHandler(ServeSharePublic, req)
		assertOK(t, w)
	})
}

func TestSharePublic_SessionPayloadRevokedAfterDelete(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, _ := seedShareSession(t, env, "sess-1")
	token := createSessionShareViaAPI(t, env, sessionID, nil)

	// Live before revoke.
	req := newRequest(t, http.MethodGet, "/api/share/"+token+"/session", nil)
	w := callHandler(ServeSharePublic, req)
	assertOK(t, w)

	req = newRequest(t, http.MethodDelete, "/api/share/session?session_id="+sessionID, nil)
	withProjectCookie(req, env.ProjectDir)
	w = callHandler(ServeSessionShareManage, req)
	assertOK(t, w)

	// Dead after revoke — and the meta endpoint agrees.
	req = newRequest(t, http.MethodGet, "/api/share/"+token+"/session", nil)
	w = callHandler(ServeSharePublic, req)
	assertStatus(t, w, http.StatusNotFound)

	req = newRequest(t, http.MethodGet, "/api/share/"+token+"/meta", nil)
	w = callHandler(ServeSharePublic, req)
	assertStatus(t, w, http.StatusNotFound)
}

// Hard-deleting a session must revoke its share: the snapshot is a copy of the
// messages being deleted, so leaving the link readable would defeat the delete.
func TestSessionShare_RevokedOnHardDelete(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, _ := seedShareSession(t, env, "sess-1")
	token := createSessionShareViaAPI(t, env, sessionID, nil)

	require.NoError(t, service.HardDeleteSession(sessionID))

	req := newRequest(t, http.MethodGet, "/api/share/"+token+"/session", nil)
	w := callHandler(ServeSharePublic, req)
	assertStatus(t, w, http.StatusNotFound)
}

// Archiving keeps the messages, so the frozen snapshot stays valid and the link
// must keep working.
func TestSessionShare_SurvivesArchive(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, _ := seedShareSession(t, env, "sess-1")
	token := createSessionShareViaAPI(t, env, sessionID, nil)

	_, err := env.DB().Exec(`UPDATE chat_sessions SET archived = 1 WHERE id = ?`, sessionID)
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/share/"+token+"/session", nil)
	w := callHandler(ServeSharePublic, req)
	assertOK(t, w)
}

// The public payload endpoint must not require auth, and must not expose the
// creator's absolute paths.
func TestSharePublic_SessionPayloadIsSanitized(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, _ := seedShareSession(t, env, "sess-1")
	token := createSessionShareViaAPI(t, env, sessionID, nil)

	req := newRequest(t, http.MethodGet, "/api/share/"+token+"/session", nil)
	// No cookie, no auth.
	w := callHandler(ServeSharePublic, req)
	assertOK(t, w)

	body := w.Body.String()
	assert.NotContains(t, body, env.ProjectDir, "the project root must not leak to an anonymous viewer")
	assert.Contains(t, body, `"version"`)
}

// A session with no finalized messages cannot be shared at all.
func TestSessionShareManage_NoShareableContent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	_, err := env.DB().Exec(
		`INSERT INTO chat_sessions (id, project_path, backend, title) VALUES ('empty', ?, 'codebuddy', 'Empty')`,
		env.ProjectDir,
	)
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/share/session", map[string]any{"sessionId": "empty"})
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionShareManage, req)
	assertStatus(t, w, http.StatusBadRequest)
}

// ─── Route registration ──────────────────────────────────────────────────────

// /api/share/session must be matched as an exact route ahead of the public
// /api/share/ subtree, and must stay auth-protected.
func TestSessionShareRoute_RegisteredAuthenticated(t *testing.T) {
	// routeTable is only populated by RegisterRoutes, so build a fresh mux first.
	mux := http.NewServeMux()
	RegisterRoutes(mux)

	var found *Route
	for i := range routeTable {
		if routeTable[i].Pattern == "/api/share/session" {
			found = &routeTable[i]
			break
		}
	}
	require.NotNil(t, found, "/api/share/session must be registered")
	assert.True(t, found.Authenticated, "management endpoints must require auth")

	var publicFound *Route
	for i := range routeTable {
		if routeTable[i].Pattern == "/api/share/" {
			publicFound = &routeTable[i]
			break
		}
	}
	require.NotNil(t, publicFound)
	assert.False(t, publicFound.Authenticated, "the token subtree stays public")
}

// helper: the in-memory DB backing the test env.
func (e *testEnv) DB() *sql.DB { return e.OrigDB }

// ─── List endpoint (shared-conversations drawer) ─────────────────────────────

// listSessionSharesViaAPI calls the list endpoint and decodes the envelope.
func listSessionSharesViaAPI(t *testing.T, env *testEnv) []sessionShareListItem {
	t.Helper()
	req := newRequest(t, http.MethodGet, "/api/share/session/list", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionShareList, req)
	assertOK(t, w)

	var resp struct {
		Shares []sessionShareListItem `json:"shares"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp.Shares
}

// moveSessionToProject repoints a seeded session (and its messages) at another
// project, so a share can be created "as" that project.
func moveSessionToProject(t *testing.T, env *testEnv, sessionID, projectPath string) {
	t.Helper()
	_, err := env.DB().Exec("UPDATE chat_sessions SET project_path = ? WHERE id = ?", projectPath, sessionID)
	require.NoError(t, err)
	_, err = env.DB().Exec("UPDATE chat_history SET project_path = ? WHERE session_id = ?", projectPath, sessionID)
	require.NoError(t, err)
}

// createSessionShareForProject creates a share while presenting projectPath in
// the cookie, returning the token.
func createSessionShareForProject(t *testing.T, env *testEnv, projectPath, sessionID string, messageIDs []int64) string {
	t.Helper()
	body := map[string]any{"sessionId": sessionID, "messageIds": messageIDs}
	req := newRequest(t, http.MethodPost, "/api/share/session", body)
	withProjectCookie(req, projectPath)
	w := callHandler(ServeSessionShareManage, req)
	assertOK(t, w)

	var resp sessionShareResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Token)
	return resp.Token
}

func TestSessionShareList_EmptyIsAnEmptyArray(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// No shares yet: the drawer relies on this decoding to [] rather than null.
	assert.Empty(t, listSessionSharesViaAPI(t, env))
}

func TestSessionShareList_ReturnsCreatedShare(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, ids := seedShareSession(t, env, "sess-list")
	token := createSessionShareViaAPI(t, env, sessionID, ids)

	shares := listSessionSharesViaAPI(t, env)
	require.Len(t, shares, 1)
	assert.Equal(t, token, shares[0].Token)
	assert.Equal(t, sessionID, shares[0].SessionID)
	assert.Equal(t, "Shared chat", shares[0].Title)
	assert.Equal(t, "codebuddy", shares[0].Backend)
	assert.Equal(t, 2, shares[0].MessageCount)
	assert.NotEmpty(t, shares[0].CreatedAt)
	assert.False(t, shares[0].Archived)
}

// A conversation title is private content, so the list must not leak another
// project's shares into this one.
func TestSessionShareList_IsProjectScoped(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	otherProject := t.TempDir()
	sessionID, ids := seedShareSession(t, env, "sess-other")
	moveSessionToProject(t, env, sessionID, otherProject)
	createSessionShareForProject(t, env, otherProject, sessionID, ids)

	// Listing from env.ProjectDir must not see it.
	assert.Empty(t, listSessionSharesViaAPI(t, env), "another project's share must not be listed")

	// ...but the owning project does see it.
	shares, err := service.ListSessionShares(otherProject)
	require.NoError(t, err)
	require.Len(t, shares, 1)
	assert.Equal(t, sessionID, shares[0].SessionID)
}

// Archiving keeps the share alive, and an archived session is not addressable by
// id — so the list is the ONLY place the link can be revoked from. It must
// therefore still return archived shares, flagged.
func TestSessionShareList_IncludesArchivedAndCanRevokeIt(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, ids := seedShareSession(t, env, "sess-archived")
	token := createSessionShareViaAPI(t, env, sessionID, ids)

	_, err := env.DB().Exec("UPDATE chat_sessions SET archived = 1 WHERE id = ?", sessionID)
	require.NoError(t, err)

	shares := listSessionSharesViaAPI(t, env)
	require.Len(t, shares, 1, "an archived session's share must stay listed")
	assert.True(t, shares[0].Archived, "the flag lets the UI hide open-conversation")

	// Revoking by token must work even though the session id is unaddressable.
	req := newRequest(t, http.MethodDelete, "/api/share/session/list", map[string]any{"token": token})
	withProjectCookie(req, env.ProjectDir)
	assertOK(t, callHandler(ServeSessionShareList, req))

	assert.Empty(t, listSessionSharesViaAPI(t, env), "revoke must have removed it")
}

func TestSessionShareList_RevokeByToken(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, ids := seedShareSession(t, env, "sess-revoke")
	token := createSessionShareViaAPI(t, env, sessionID, ids)
	require.Len(t, listSessionSharesViaAPI(t, env), 1)

	req := newRequest(t, http.MethodDelete, "/api/share/session/list", map[string]any{"token": token})
	withProjectCookie(req, env.ProjectDir)
	assertOK(t, callHandler(ServeSessionShareList, req))

	assert.Empty(t, listSessionSharesViaAPI(t, env))

	// The link itself must stop resolving.
	_, _, _, ok, err := service.GetSessionShareByToken(token)
	require.NoError(t, err)
	assert.False(t, ok, "the revoked token must no longer resolve")
}

func TestSessionShareList_RevokeAllIsProjectScoped(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Two shares in this project.
	s1, ids1 := seedShareSession(t, env, "sess-a")
	createSessionShareViaAPI(t, env, s1, ids1)
	s2, ids2 := seedShareSession(t, env, "sess-b")
	createSessionShareViaAPI(t, env, s2, ids2)
	require.Len(t, listSessionSharesViaAPI(t, env), 2)

	// One share in another project, which clear-all must NOT touch.
	otherProject := t.TempDir()
	s3, ids3 := seedShareSession(t, env, "sess-c")
	moveSessionToProject(t, env, s3, otherProject)
	createSessionShareForProject(t, env, otherProject, s3, ids3)

	req := newRequest(t, http.MethodDelete, "/api/share/session/list", map[string]any{"all": true})
	withProjectCookie(req, env.ProjectDir)
	assertOK(t, callHandler(ServeSessionShareList, req))

	assert.Empty(t, listSessionSharesViaAPI(t, env), "this project must be cleared")

	// The other project keeps its share.
	shares, err := service.ListSessionShares(otherProject)
	require.NoError(t, err)
	assert.Len(t, shares, 1, "clear-all must not touch another project")
}

// An unknown token and another project's token must be indistinguishable: both
// 404, neither disclosing whether the other exists.
func TestSessionShareList_RevokeUnknownOrForeignTokenIs404(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	otherProject := t.TempDir()
	sessionID, ids := seedShareSession(t, env, "sess-foreign")
	moveSessionToProject(t, env, sessionID, otherProject)
	foreignToken := createSessionShareForProject(t, env, otherProject, sessionID, ids)

	cases := map[string]string{
		"unknown":         "00000000000000000000000000000000",
		"other project's": foreignToken,
	}
	for name, token := range cases {
		req := newRequest(t, http.MethodDelete, "/api/share/session/list", map[string]any{"token": token})
		withProjectCookie(req, env.ProjectDir)
		w := callHandler(ServeSessionShareList, req)
		assert.Equal(t, http.StatusNotFound, w.Code, "%s token must 404", name)
	}

	// The foreign share must survive both attempts.
	shares, err := service.ListSessionShares(otherProject)
	require.NoError(t, err)
	assert.Len(t, shares, 1, "a foreign project's share must not be revoked")
}

func TestSessionShareList_MissingTokenIs400(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/share/session/list", map[string]any{})
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionShareList, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSessionShareList_BadMethodIs405(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPut, "/api/share/session/list", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionShareList, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
