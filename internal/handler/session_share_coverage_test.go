package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Coverage for the session-share management handler's guard branches: bad
// bodies, missing ids, and the DB failure paths. These are the paths that turn
// a client mistake into a 4xx and a storage fault into a 5xx; without them a
// regression could silently return 200 with an empty body.

// ─── Request decoding ────────────────────────────────────────────────────────

// A malformed JSON body must be a 400, not a silent empty request.
func TestSessionShareManage_MalformedBodyIs400(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/share/session", nil)
	req.Body = http.NoBody
	req.ContentLength = 8
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionShareManage, req)
	assertStatus(t, w, http.StatusBadRequest)
}

// GET without session_id is a 400.
func TestSessionShareManage_StatusRequiresSessionID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/share/session", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionShareManage, req)
	assertStatus(t, w, http.StatusBadRequest)
}

// A request with no project cookie is rejected before any session lookup.
func TestSessionShareManage_RequiresProjectCookie(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/share/session", map[string]any{"sessionId": "x"})
	w := callHandler(ServeSessionShareManage, req)
	assertStatus(t, w, http.StatusForbidden)
}

// ─── Query-string fallbacks ──────────────────────────────────────────────────

// A body-less request can carry the session id and selection in the query
// string, which is what makes scripted share creation possible.
func TestSessionShareManage_BodylessQueryParamsWork(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, ids := seedShareSession(t, env, "sess-q")

	req := newRequest(t, http.MethodPost,
		"/api/share/session?session_id="+sessionID+"&message_ids="+strconv.FormatInt(ids[0], 10)+","+strconv.FormatInt(ids[1], 10), nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionShareManage, req)
	assertOK(t, w)

	var resp sessionShareResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 2, resp.MessageCount)
}

// Unparseable, empty and non-positive entries are skipped rather than rejected.
func TestSessionShareManage_UnparseableMessageIDsAreSkipped(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, ids := seedShareSession(t, env, "sess-bad")

	raw := "abc,,0,-5," + strconv.FormatInt(ids[0], 10)
	req := newRequest(t, http.MethodPost, "/api/share/session?session_id="+sessionID+"&message_ids="+raw, nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionShareManage, req)
	assertOK(t, w)

	var resp sessionShareResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.MessageCount, "only the one valid id is selected")
}

// ─── Revoke-by-token request decoding ────────────────────────────────────────

// The token may arrive as a query param when there is no body.
func TestSessionShareList_RevokeByQueryToken(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, ids := seedShareSession(t, env, "sess-qtoken")
	token := createSessionShareViaAPI(t, env, sessionID, ids)

	req := newRequest(t, http.MethodDelete, "/api/share/session/list?token="+token, nil)
	withProjectCookie(req, env.ProjectDir)
	assertOK(t, callHandler(ServeSessionShareList, req))

	assert.Empty(t, listSessionSharesViaAPI(t, env))
}

// all=1 in the query string is equivalent to {"all":true}.
func TestSessionShareList_RevokeAllByQueryFlag(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, ids := seedShareSession(t, env, "sess-qall")
	createSessionShareViaAPI(t, env, sessionID, ids)

	req := newRequest(t, http.MethodDelete, "/api/share/session/list?all=1", nil)
	withProjectCookie(req, env.ProjectDir)
	assertOK(t, callHandler(ServeSessionShareList, req))

	assert.Empty(t, listSessionSharesViaAPI(t, env))
}

// A malformed DELETE body is a 400 rather than a no-op revoke.
func TestSessionShareList_MalformedDeleteBodyIs400(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/share/session/list", nil)
	req.Body = http.NoBody
	req.ContentLength = 5
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionShareList, req)
	assertStatus(t, w, http.StatusBadRequest)
}

// The list endpoints require a project cookie.
func TestSessionShareList_RequiresProjectCookie(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	w := callHandler(ServeSessionShareList, newRequest(t, http.MethodGet, "/api/share/session/list", nil))
	assertStatus(t, w, http.StatusForbidden)
}

// ─── Public payload / meta failure paths ─────────────────────────────────────

// An unknown token is a uniform 404 on both public endpoints.
func TestSharePublic_UnknownTokenIs404(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	for _, path := range []string{"/api/share/deadbeef/session", "/api/share/deadbeef/meta"} {
		w := callHandler(ServeSharePublic, newRequest(t, http.MethodGet, path, nil))
		assertStatus(t, w, http.StatusNotFound)
	}
}

// A non-GET method on the public subtree is a 405.
func TestSharePublic_NonGetIs405(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	w := callHandler(ServeSharePublic, newRequest(t, http.MethodPost, "/api/share/deadbeef/session", nil))
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

// ─── DB failure paths ────────────────────────────────────────────────────────

// Every management handler must turn a storage fault into a 500 instead of a
// misleading 200. The tables are dropped so the failure is a real driver error.
func TestSessionShareManage_StorageFailuresAre500(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		body   map[string]any
		drop   string
	}{
		{"status list messages", http.MethodGet, "/api/share/session", nil, "chat_history"},
		{"status lookup", http.MethodGet, "/api/share/session", nil, "session_shares"},
		{"create payload build", http.MethodPost, "/api/share/session", map[string]any{"sessionId": "sess-1"}, "chat_history"},
		{"create upsert", http.MethodPost, "/api/share/session", map[string]any{"sessionId": "sess-1"}, "session_shares"},
		{"revoke", http.MethodDelete, "/api/share/session", nil, "session_shares"},
		{"list", http.MethodGet, "/api/share/session/list", nil, "session_shares"},
		{"revoke by token", http.MethodDelete, "/api/share/session/list", map[string]any{"token": "deadbeef"}, "session_shares"},
		{"revoke all", http.MethodDelete, "/api/share/session/list", map[string]any{"all": true}, "session_shares"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, teardown := setupTestEnv(t)
			defer teardown()

			seedShareSession(t, env, "sess-1")
			_, err := env.DB().Exec("DROP TABLE " + tc.drop)
			require.NoError(t, err)

			// Pick the handler first: the body can only be read once, so the
			// request must go to exactly one of them.
			h := ServeSessionShareManage
			path := tc.path
			if tc.path == "/api/share/session/list" {
				h = ServeSessionShareList
			} else if tc.body == nil {
				path += "?session_id=sess-1"
			}
			req := newRequest(t, tc.method, path, tc.body)
			withProjectCookie(req, env.ProjectDir)

			assertStatus(t, callHandler(h, req), http.StatusInternalServerError)
		})
	}
}

// The public payload endpoint turns a storage fault into a 500 too.
func TestSharePublic_StorageFailureIs500(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, ids := seedShareSession(t, env, "sess-1")
	token := createSessionShareViaAPI(t, env, sessionID, ids)

	_, err := env.DB().Exec("DROP TABLE session_shares")
	require.NoError(t, err)

	for _, path := range []string{"/api/share/" + token + "/session", "/api/share/" + token + "/meta"} {
		w := callHandler(ServeSharePublic, newRequest(t, http.MethodGet, path, nil))
		assertStatus(t, w, http.StatusInternalServerError)
	}
}

// The payload is stored verbatim; the public read must not re-encode it.
func TestSharePublic_SessionPayloadIsByteIdentical(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, ids := seedShareSession(t, env, "sess-1")
	token := createSessionShareViaAPI(t, env, sessionID, ids)

	stored, _, _, ok, err := service.GetSessionShareByToken(token)
	require.NoError(t, err)
	require.True(t, ok)

	w := callHandler(ServeSharePublic, newRequest(t, http.MethodGet, "/api/share/"+token+"/session", nil))
	assertOK(t, w)
	assert.Equal(t, stored, w.Body.String())
}
