package handler

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── etagMatches ─────────────────────────────────────────────────────────────

// The comparison must accept the forms RFC 9110 allows. A plain string equality
// (what file_thumb.go does) silently fails all of them, which turns every
// revalidation into a full re-download.
func TestETagMatches(t *testing.T) {
	const etag = `"abc123"`

	cases := []struct {
		name        string
		ifNoneMatch string
		want        bool
	}{
		{"empty header", "", false},
		{"exact", etag, true},
		{"weak prefix", `W/` + etag, true},
		// RFC 9110 allows optional whitespace between W/ and the quoted value,
		// and some proxies emit it. Missing this only costs a 304, but it is a
		// one-line fix so there is no reason to keep failing it.
		{"weak prefix with space", `W/ ` + etag, true},
		{"list containing it", `"other", ` + etag, true},
		{"list with whitespace", `"other" ,  ` + etag + ` `, true},
		{"weak entry in a list", `W/"other", W/` + etag, true},
		{"star", "*", true},
		{"different value", `"nope"`, false},
		{"different value in a list", `"nope", "also-nope"`, false},
		{"unquoted value does not match", `abc123`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, etagMatches(tc.ifNoneMatch, etag))
		})
	}

	// An empty current etag can never match — including against "*", which would
	// otherwise claim a representation exists when none was computed.
	assert.False(t, etagMatches("*", ""))
}

// Two different bodies must produce different validators.
func TestBodyETag_ChangesWithBody(t *testing.T) {
	a := bodyETag([]byte(`{"a":1}`))
	b := bodyETag([]byte(`{"a":2}`))
	assert.NotEqual(t, a, b)
	assert.Equal(t, a, bodyETag([]byte(`{"a":1}`)), "the same body must hash the same")
	assert.Regexp(t, `^"[0-9a-f]+"$`, a, "a strong validator is quoted hex")
}

// ─── Public payload conditional requests ─────────────────────────────────────

func TestSharePublic_SessionPayloadReturns304(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, ids := seedShareSession(t, env, "sess-1")
	token := createSessionShareViaAPI(t, env, sessionID, ids)

	first := callHandler(ServeSharePublic, newRequest(t, http.MethodGet, "/api/share/"+token+"/session", nil))
	assertOK(t, first)
	etag := first.Header().Get("ETag")
	require.NotEmpty(t, etag)

	req := newRequest(t, http.MethodGet, "/api/share/"+token+"/session", nil)
	req.Header.Set("If-None-Match", etag)
	second := callHandler(ServeSharePublic, req)

	assertStatus(t, second, http.StatusNotModified)
	assert.Empty(t, second.Body.String(), "a 304 carries no body")
	assert.Equal(t, etag, second.Header().Get("ETag"), "a 304 must still carry the validator")
}

// no-store (what middleware.NoCache sets on every route) and 304 are
// contradictory: with no-store the browser never sends the conditional request.
// The handler must override it.
func TestSharePublic_SessionPayloadOverridesNoStore(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, ids := seedShareSession(t, env, "sess-1")
	token := createSessionShareViaAPI(t, env, sessionID, ids)

	w := callHandler(ServeSharePublic, newRequest(t, http.MethodGet, "/api/share/"+token+"/session", nil))
	assertOK(t, w)
	assert.Equal(t, "private, no-cache, max-age=0", w.Header().Get("Cache-Control"))
}

// A summary that lands after the share was created changes the assembled body,
// so the ETag must change with it. This is the case a cheap SQL fingerprint
// would miss (summaries are written asynchronously, with second-granularity
// timestamps).
func TestSharePublic_SessionPayloadETagTracksLateSummary(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, ids := seedShareSession(t, env, "sess-1")
	token := createSessionShareViaAPI(t, env, sessionID, ids)

	before := callHandler(ServeSharePublic, newRequest(t, http.MethodGet, "/api/share/"+token+"/session", nil))
	assertOK(t, before)

	_, err := env.DB().Exec(
		`INSERT INTO summaries (target_type, target_id, summary) VALUES ('chat_message', ?, 'a late summary')`,
		ids[1],
	)
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/share/"+token+"/session", nil)
	req.Header.Set("If-None-Match", before.Header().Get("ETag"))
	after := callHandler(ServeSharePublic, req)

	assertOK(t, after)
	assert.NotEqual(t, before.Header().Get("ETag"), after.Header().Get("ETag"))
	assert.Contains(t, after.Body.String(), "a late summary")
}

// Revocation must beat the conditional request: a caller holding a valid ETag
// still gets 404 once the link is gone.
func TestSharePublic_RevokedTokenBeatsConditionalRequest(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, ids := seedShareSession(t, env, "sess-1")
	token := createSessionShareViaAPI(t, env, sessionID, ids)

	first := callHandler(ServeSharePublic, newRequest(t, http.MethodGet, "/api/share/"+token+"/session", nil))
	assertOK(t, first)
	etag := first.Header().Get("ETag")

	req := newRequest(t, http.MethodDelete, "/api/share/session", map[string]any{"sessionId": sessionID})
	withProjectCookie(req, env.ProjectDir)
	assertOK(t, callHandler(ServeSessionShareManage, req))

	conditional := newRequest(t, http.MethodGet, "/api/share/"+token+"/session", nil)
	conditional.Header.Set("If-None-Match", etag)
	assertStatus(t, callHandler(ServeSharePublic, conditional), http.StatusNotFound)
}

// Every selected message deleted (a rewind) leaves nothing to show, which is a
// 404 — not a 200 with an empty messages array, which the SPA would render as a
// title above nothing.
func TestSharePublic_AllSharedMessagesDeletedIs404(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, ids := seedShareSession(t, env, "sess-1")
	token := createSessionShareViaAPI(t, env, sessionID, ids)

	_, err := env.DB().Exec("DELETE FROM chat_history WHERE session_id = ?", sessionID)
	require.NoError(t, err)

	assertStatus(t, callHandler(ServeSharePublic, newRequest(t, http.MethodGet, "/api/share/"+token+"/session", nil)),
		http.StatusNotFound)
}

// Partially deleted selection: the survivors render.
func TestSharePublic_DeletedMessagesAreSkipped(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, ids := seedShareSession(t, env, "sess-1")
	token := createSessionShareViaAPI(t, env, sessionID, ids)

	_, err := env.DB().Exec("DELETE FROM chat_history WHERE id = ?", ids[1])
	require.NoError(t, err)

	w := callHandler(ServeSharePublic, newRequest(t, http.MethodGet, "/api/share/"+token+"/session", nil))
	assertOK(t, w)
	assert.Contains(t, w.Body.String(), "hello")
	assert.NotContains(t, w.Body.String(), "hi there")
}
