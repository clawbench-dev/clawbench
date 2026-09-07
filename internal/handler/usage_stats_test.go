package handler

import (
	"net/http"
	"net/url"
	"testing"

	"clawbench/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedUsageStatsData inserts a session+history+metadata triple directly.
func seedUsageStatsData(t *testing.T, projectPath, sessionID, agentID, backend, model, createdAt string, total int64) {
	t.Helper()
	db := service.UnsafeDBForTest()

	_, err := db.Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id) VALUES (?, ?, ?, 't', ?)",
		sessionID, projectPath, backend, agentID,
	)
	require.NoError(t, err)

	res, err := db.Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming, created_at) VALUES (?, ?, ?, 'assistant', '{}', 0, ?)",
		projectPath, backend, sessionID, createdAt,
	)
	require.NoError(t, err)
	msgID, _ := res.LastInsertId()

	_, err = db.Exec(
		`INSERT INTO chat_metadata (message_id, model, input_tokens, output_tokens, total_tokens,
			cache_hit_tokens, cache_miss_tokens, credit, cost_usd, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msgID, model, total, 0, total, 0, 0, 0, 0, createdAt,
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
	seedUsageStatsData(t, projectPath, "sess-1", "codebuddy", "codebuddy", "glm-5.1", "2026-01-10 10:00:00", 150)

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
	seedUsageStatsData(t, projectPath, "sess-t1", "codebuddy", "codebuddy", "glm-5.1", "2026-01-10 10:00:00", 100)
	seedUsageStatsData(t, projectPath, "sess-t2", "codebuddy", "codebuddy", "glm-5.1", "2026-01-11 09:00:00", 50)

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

	seedUsageStatsData(t, projectPath, "sess-p1", "codebuddy", "codebuddy", "glm-5.1", "2026-01-10 10:00:00", 150)
	seedUsageStatsData(t, projectPath, "sess-p2", "codebuddy", "codebuddy", "glm-5.2", "2026-01-11 09:00:00", 50)

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
