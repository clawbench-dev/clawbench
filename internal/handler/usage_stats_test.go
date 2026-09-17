package handler

import (
	"net/http"
	"net/url"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedUsageStatsData inserts a session+history+metadata triple directly.
// The backend/agent_id column is fixed to "codebuddy" — callers only vary
// model/timestamp, so those are the only behavioral knobs exposed.
func seedUsageStatsData(t *testing.T, projectPath, sessionID, model, createdAt string, total int64) {
	t.Helper()
	db := service.UnsafeDBForTest()

	_, err := db.Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id) VALUES (?, ?, 'codebuddy', 't', 'codebuddy')",
		sessionID, projectPath,
	)
	require.NoError(t, err)

	res, err := db.Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming, created_at) VALUES (?, 'codebuddy', ?, 'assistant', '{}', 0, ?)",
		projectPath, sessionID, createdAt,
	)
	require.NoError(t, err)
	msgID, _ := res.LastInsertId()

	_, err = db.Exec(
		`INSERT INTO chat_metadata (message_id, model, input_tokens, output_tokens, total_tokens,
			cache_hit_tokens, cache_miss_tokens, credit, cost_usd, created_at,
			project_path, backend, agent_id, clawbench_session_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'codebuddy', 'codebuddy', ?)`,
		msgID, model, total, 0, total, 0, 0, 0, 0, createdAt, projectPath, sessionID,
	)
	require.NoError(t, err)
}

func usageStatsURL(params map[string]string) string {
	q := url.Values{}
	for k, v := range params {
		q.Add(k, v)
	}
	return "/api/usage/stats?" + q.Encode()
}

func TestServeUsageStats_ValidResponse(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	projectPath := env.ProjectDir
	seedUsageStatsData(t, projectPath, "sess-1", "glm-5.1", "2026-01-10 10:00:00", 150)

	req := withProjectCookie(newRequest(t, http.MethodGet, usageStatsURL(map[string]string{
		"start":   "2026-01-01T00:00:00Z",
		"end":     "2026-02-01T00:00:00Z",
		"dims":    "model",
		"metrics": "total",
		"sort":    "total",
		"order":   "desc",
	}), nil), projectPath)

	w := callHandler(ServeUsageStats, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Totals struct {
			Total int64 `json:"total"`
		} `json:"totals"`
		Rows []map[string]any `json:"rows"`
	}
	decodeRespJSON(t, w.Body, &body)
	assert.Equal(t, int64(150), body.Totals.Total)
	require.Len(t, body.Rows, 1)
}

func TestServeUsageStats_ValidationErrors(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	projectPath := env.ProjectDir

	cases := []struct {
		name   string
		params map[string]string
	}{
		{"missing start", map[string]string{"end": "2026-02-01T00:00:00Z", "dims": "model", "metrics": "total"}},
		{"invalid end", map[string]string{"start": "2026-01-01T00:00:00Z", "end": "garbage", "dims": "model", "metrics": "total"}},
		{"missing dims", map[string]string{"start": "2026-01-01T00:00:00Z", "end": "2026-02-01T00:00:00Z", "metrics": "total"}},
		{"bad dim", map[string]string{"start": "2026-01-01T00:00:00Z", "end": "2026-02-01T00:00:00Z", "dims": "bogus", "metrics": "total"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := withProjectCookie(newRequest(t, http.MethodGet, usageStatsURL(tc.params), nil), projectPath)
			w := callHandler(ServeUsageStats, req)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}

	// Validation failures carry a stable machine-readable code in detail —
	// never the server's English Error() text.
	t.Run("bad dim detail is a code not English text", func(t *testing.T) {
		req := withProjectCookie(newRequest(t, http.MethodGet, usageStatsURL(map[string]string{
			"start": "2026-01-01T00:00:00Z", "end": "2026-02-01T00:00:00Z",
			"dims": "bogus", "metrics": "total",
		}), nil), projectPath)
		w := callHandler(ServeUsageStats, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)

		var body struct {
			Detail map[string]any `json:"detail"`
		}
		decodeRespJSON(t, w.Body, &body)
		assert.Equal(t, "invalid_dim", body.Detail["reason"])
	})
}

func TestServeUsageStats_TrendResponse(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	projectPath := env.ProjectDir
	seedUsageStatsData(t, projectPath, "sess-t1", "glm-5.1", "2026-01-10 10:00:00", 100)
	seedUsageStatsData(t, projectPath, "sess-t2", "glm-5.1", "2026-01-11 09:00:00", 50)

	req := withProjectCookie(newRequest(t, http.MethodGet, usageStatsURL(map[string]string{
		"start":   "2026-01-01T00:00:00Z",
		"end":     "2026-02-01T00:00:00Z",
		"dims":    "model",
		"metrics": "total",
		"trend":   "1",
	}), nil), projectPath)

	w := callHandler(ServeUsageStats, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Totals struct {
			Total int64 `json:"total"`
		} `json:"totals"`
		Rows  []map[string]any `json:"rows"`
		Trend []map[string]any `json:"trend"`
	}
	decodeRespJSON(t, w.Body, &body)
	assert.Equal(t, int64(150), body.Totals.Total)
	// Trend responses carry empty (not null) rows and day-bucketed trend rows.
	require.Empty(t, body.Rows)
	require.Len(t, body.Trend, 2)
	assert.Equal(t, "2026-01-10", body.Trend[0]["day"])
	assert.Equal(t, "2026-01-11", body.Trend[1]["day"])
}

func TestServeUsageStats_MissingProjectCookie(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, usageStatsURL(map[string]string{
		"start": "2026-01-01T00:00:00Z", "end": "2026-02-01T00:00:00Z",
		"dims": "model", "metrics": "total",
	}), nil)
	w := callHandler(ServeUsageStats, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeUsageStats_DBFailureReturns500(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	projectPath := env.ProjectDir

	// Force the read pool (used by UsageStats) to fail so the aggregates query
	// errors instead of returning data.
	closedDB, err := service.InitInMemoryDB()
	require.NoError(t, err)
	_ = closedDB.Close()
	cleanup := service.SetDBForTest(service.UnsafeDBForTest(), closedDB)
	defer cleanup()

	req := withProjectCookie(newRequest(t, http.MethodGet, usageStatsURL(map[string]string{
		"start": "2026-01-01T00:00:00Z", "end": "2026-02-01T00:00:00Z",
		"dims": "model", "metrics": "total",
	}), nil), projectPath)
	w := callHandler(ServeUsageStats, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var body struct {
		MsgKey string `json:"msgKey"`
	}
	decodeRespJSON(t, w.Body, &body)
	assert.Equal(t, "InternalError", body.MsgKey)
}

func TestServeUsageStats_MethodNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := withProjectCookie(newRequest(t, http.MethodPost, usageStatsURL(map[string]string{
		"start": "2026-01-01T00:00:00Z", "end": "2026-02-01T00:00:00Z",
		"dims": "model", "metrics": "total",
	}), nil), env.ProjectDir)
	w := callHandler(ServeUsageStats, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeUsageStats_QueryParamPassthrough(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	projectPath := env.ProjectDir

	seedUsageStatsData(t, projectPath, "sess-p1", "glm-5.1", "2026-01-10 10:00:00", 150)
	seedUsageStatsData(t, projectPath, "sess-p2", "glm-5.2", "2026-01-11 09:00:00", 50)

	// asc order + explicit limit/top parses — the request must still succeed
	// and exercise the sort/limit/top parameter parsing branches.
	req := withProjectCookie(newRequest(t, http.MethodGet, usageStatsURL(map[string]string{
		"start":   "2026-01-01T00:00:00Z",
		"end":     "2026-02-01T00:00:00Z",
		"dims":    "model",
		"metrics": "total",
		"sort":    "total",
		"order":   "asc",
		"limit":   "5",
		"trend":   "1",
		"top":     "3",
	}), nil), projectPath)

	w := callHandler(ServeUsageStats, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestServeUsageStats_InvalidLimitIgnored(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	projectPath := env.ProjectDir

	// A non-numeric limit or top must be ignored (defaults used), not 400.
	req := withProjectCookie(newRequest(t, http.MethodGet, usageStatsURL(map[string]string{
		"start":   "2026-01-01T00:00:00Z",
		"end":     "2026-02-01T00:00:00Z",
		"dims":    "model",
		"metrics": "total",
		"limit":   "abc",
		"top":     "-2",
	}), nil), projectPath)

	w := callHandler(ServeUsageStats, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// --- scope=all (cross-project) ---

// TestServeUsageStats_ScopeAllWithAIToken is the positive cross-project case: a
// local AI token with no project cookie may aggregate every project.
func TestServeUsageStats_ScopeAllWithAIToken(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	model.CookieToken = "instance-key"
	seedUsageStatsData(t, env.ProjectDir, "sess-1", "glm-5.1", "2026-01-10 10:00:00", 150)

	req := newRequest(t, http.MethodGet, usageStatsURL(map[string]string{
		"start": "2026-01-01T00:00:00Z", "end": "2026-02-01T00:00:00Z",
		"dims": "project", "metrics": "total", "scope": "all",
	}), nil)
	req.RemoteAddr = "127.0.0.1:12345"
	withAIToken(req)

	w := callHandler(ServeUsageStats, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Totals struct {
			Total int64 `json:"total"`
		} `json:"totals"`
		Rows []map[string]any `json:"rows"`
	}
	decodeRespJSON(t, w.Body, &body)
	assert.Equal(t, int64(150), body.Totals.Total)
	require.Len(t, body.Rows, 1)
	key, ok := body.Rows[0]["key"].(map[string]any)
	require.True(t, ok, "row must carry a key object")
	assert.Equal(t, env.ProjectDir, key["project"])
}

// TestServeUsageStats_ScopeAllLoopbackWithoutTokenDenied isolates the handler's
// own gate by calling it directly (no auth middleware in front). The exemption
// must come from the token, not the loopback address — otherwise any local
// process could read every project's usage. The paired positive case is
// TestServeUsageStats_ScopeAllWithAIToken.
func TestServeUsageStats_ScopeAllLoopbackWithoutTokenDenied(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.CookieToken = "instance-key"

	req := newRequest(t, http.MethodGet, usageStatsURL(map[string]string{
		"start": "2026-01-01T00:00:00Z", "end": "2026-02-01T00:00:00Z",
		"dims": "project", "metrics": "total", "scope": "all",
	}), nil)
	req.RemoteAddr = "127.0.0.1:12345" // loopback, but no token

	w := callHandler(ServeUsageStats, req)
	assert.Equal(t, http.StatusForbidden, w.Code,
		"loopback without an AI token must not get the cross-project exemption")

	var body struct {
		MsgKey string `json:"msgKey"`
	}
	decodeRespJSON(t, w.Body, &body)
	assert.Equal(t, "AccessDenied", body.MsgKey)
}

// TestServeUsageStats_ScopeAllRemoteDenied covers a remote caller that somehow
// holds a valid token signature: the loopback half of IsAITokenRequest must
// still refuse it.
func TestServeUsageStats_ScopeAllRemoteDenied(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.CookieToken = "instance-key"

	req := newRequest(t, http.MethodGet, usageStatsURL(map[string]string{
		"start": "2026-01-01T00:00:00Z", "end": "2026-02-01T00:00:00Z",
		"dims": "project", "metrics": "total", "scope": "all",
	}), nil)
	// Default RemoteAddr is non-loopback; still send a valid token.
	withAIToken(req)

	w := callHandler(ServeUsageStats, req)
	assert.Equal(t, http.StatusForbidden, w.Code,
		"a token replayed from off-machine must not get cross-project reads")
}

// TestServeUsageStats_ScopeAllWithProjectCookieRejected pins the contradiction
// rule: scope=all plus a project cookie is refused rather than resolved to one
// of the two meanings. A caller sending both believes it is getting the
// narrower answer, so silently aggregating everything would leak more than it
// asked for.
func TestServeUsageStats_ScopeAllWithProjectCookieRejected(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	model.CookieToken = "instance-key"

	req := withProjectCookie(newRequest(t, http.MethodGet, usageStatsURL(map[string]string{
		"start": "2026-01-01T00:00:00Z", "end": "2026-02-01T00:00:00Z",
		"dims": "project", "metrics": "total", "scope": "all",
	}), nil), env.ProjectDir)
	req.RemoteAddr = "127.0.0.1:12345"
	withAIToken(req)

	w := callHandler(ServeUsageStats, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	var body struct {
		Detail map[string]any `json:"detail"`
	}
	decodeRespJSON(t, w.Body, &body)
	assert.Equal(t, "conflicting_scope", body.Detail["reason"])
}

// TestServeUsageStats_InvalidScopeRejected keeps an unknown scope from silently
// falling back to the single-project path, which would make a typo look like a
// successful narrower query.
func TestServeUsageStats_InvalidScopeRejected(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := withProjectCookie(newRequest(t, http.MethodGet, usageStatsURL(map[string]string{
		"start": "2026-01-01T00:00:00Z", "end": "2026-02-01T00:00:00Z",
		"dims": "model", "metrics": "total", "scope": "everything",
	}), nil), env.ProjectDir)

	w := callHandler(ServeUsageStats, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	var body struct {
		Detail map[string]any `json:"detail"`
	}
	decodeRespJSON(t, w.Body, &body)
	assert.Equal(t, "scope", body.Detail["reason"])
}

// TestServeUsageStats_ScopeProjectStillRequiresCookie guards the default path
// against the scope refactor: an explicit scope=project must behave exactly
// like no scope at all, including rejecting a missing cookie. This is the
// browser-panel contract, and it must not be reachable without a cookie just
// because the handler grew an exemption branch.
func TestServeUsageStats_ScopeProjectStillRequiresCookie(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.CookieToken = "instance-key"

	req := newRequest(t, http.MethodGet, usageStatsURL(map[string]string{
		"start": "2026-01-01T00:00:00Z", "end": "2026-02-01T00:00:00Z",
		"dims": "model", "metrics": "total", "scope": "project",
	}), nil)
	req.RemoteAddr = "127.0.0.1:12345"
	withAIToken(req) // even a valid token must not bypass the cookie here

	w := callHandler(ServeUsageStats, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// TestServeUsageStats_ScopeAllEndToEndThroughAuth exercises the production
// layering (middleware.Auth in front of the handler) rather than the handler
// alone. The two gates must compose: the token gets the request past Auth, and
// the handler's own check then decides the scope. A regression that moved the
// exemption to the loopback address would pass the handler-only test above but
// fail here, because Auth would reject the token-less request first.
func TestServeUsageStats_ScopeAllEndToEndThroughAuth(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	model.CookieToken = "instance-key"
	seedUsageStatsData(t, env.ProjectDir, "sess-1", "glm-5.1", "2026-01-10 10:00:00", 150)

	t.Run("token gets through and aggregates all projects", func(t *testing.T) {
		req := newRequest(t, http.MethodGet, usageStatsURL(map[string]string{
			"start": "2026-01-01T00:00:00Z", "end": "2026-02-01T00:00:00Z",
			"dims": "project", "metrics": "total", "scope": "all",
		}), nil)
		req.RemoteAddr = "127.0.0.1:12345"
		withAIToken(req)

		w := callHandlerWithAuth(ServeUsageStats, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("loopback without a token is rejected by Auth before the handler", func(t *testing.T) {
		req := newRequest(t, http.MethodGet, usageStatsURL(map[string]string{
			"start": "2026-01-01T00:00:00Z", "end": "2026-02-01T00:00:00Z",
			"dims": "project", "metrics": "total", "scope": "all",
		}), nil)
		req.RemoteAddr = "127.0.0.1:12345"

		w := callHandlerWithAuth(ServeUsageStats, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code,
			"Auth is the outer gate; no token means 401, not 403")
	})
}
