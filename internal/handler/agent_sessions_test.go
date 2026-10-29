package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serveAgentSessions issues GET /api/agents/{id}/sessions against the live mux
// so the sub-route dispatch (not just the handler) is exercised.
func serveAgentSessions(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestServeAgentSessions_ShapeAndTotal(t *testing.T) {
	cleanup := setupAgentTestEnv(t)
	defer cleanup()

	_, err := store.WriteExec(
		`INSERT INTO chat_sessions (id, agent_id, session_type, title, updated_at)
		 VALUES ('c1', 'codebuddy', 'chat', 'one', '2026-01-01 10:00:00')`,
	)
	require.NoError(t, err)
	_, err = store.WriteExec(
		`INSERT INTO chat_sessions (id, agent_id, session_type, title, updated_at)
		 VALUES ('g1', 'codebuddy', 'group', 'grp', '2026-01-02 10:00:00')`,
	)
	require.NoError(t, err)
	_, err = store.WriteExec(
		`INSERT INTO chat_sessions (id, agent_id, session_type, group_id, updated_at)
		 VALUES ('m1', 'codebuddy', 'group_member', 'g1', '2026-01-02 10:00:00')`,
	)
	require.NoError(t, err)

	rec := serveAgentSessions(t, "/api/agents/codebuddy/sessions")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Sessions     []map[string]any `json:"sessions"`
		HasMore      bool             `json:"hasMore"`
		NextCursor   string           `json:"nextCursor"`
		NextCursorID string           `json:"nextCursorId"`
		Total        int              `json:"total"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	// 1 chat + 1 membership. The group row must NOT be added again.
	assert.Equal(t, 2, resp.Total)
	require.Len(t, resp.Sessions, 2)
	assert.False(t, resp.HasMore)

	// Field names are part of the client contract — pin them so a rename cannot
	// silently blank the list.
	first := resp.Sessions[0]
	for _, key := range []string{"entryId", "sessionId", "title", "projectPath", "sessionType", "role", "memberName", "archived", "updatedAt"} {
		assert.Contains(t, first, key, "response field %s must be present", key)
	}
}

func TestServeAgentSessions_UnknownAgent(t *testing.T) {
	cleanup := setupAgentTestEnv(t)
	defer cleanup()

	rec := serveAgentSessions(t, "/api/agents/no-such-agent/sessions")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// An invalid id must be rejected before any query. (A `..` segment is a poor
// probe: ServeMux canonicalises the path and answers 307 before the handler
// runs, so this uses a character the id grammar forbids instead.)
func TestServeAgentSessions_InvalidAgentID(t *testing.T) {
	cleanup := setupAgentTestEnv(t)
	defer cleanup()

	rec := serveAgentSessions(t, "/api/agents/bad%20id/sessions")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestServeAgentSessions_MethodNotAllowed(t *testing.T) {
	cleanup := setupAgentTestEnv(t)
	defer cleanup()

	mux := http.NewServeMux()
	RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/agents/codebuddy/sessions", http.NoBody)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code,
		"a wrong method must answer 405, not 404 — the route exists")
}

// Pagination contract: the response's nextCursor/nextCursorId must be usable
// verbatim as the next request's cursor/cursor_id, and every entry appears
// exactly once across the pages.
func TestServeAgentSessions_PaginationRoundTrip(t *testing.T) {
	cleanup := setupAgentTestEnv(t)
	defer cleanup()

	// Five memberships of ONE group ⇒ identical updated_at, which is exactly the
	// case a timestamp-only cursor would break.
	_, err := store.WriteExec(
		`INSERT INTO chat_sessions (id, agent_id, session_type, title, updated_at)
		 VALUES ('g', 'other', 'group', 'grp', '2026-01-05 10:00:00')`,
	)
	require.NoError(t, err)
	for _, id := range []string{"m1", "m2", "m3", "m4", "m5"} {
		_, err = store.WriteExec(
			`INSERT INTO chat_sessions (id, agent_id, session_type, group_id, updated_at)
			 VALUES (?, 'codebuddy', 'group_member', 'g', '2026-01-05 10:00:00')`, id,
		)
		require.NoError(t, err)
	}

	seen := map[string]bool{}
	cursor, cursorID := "", ""
	for range 10 {
		path := "/api/agents/codebuddy/sessions?limit=2"
		if cursor != "" {
			// The cursor is a timestamp containing a space; a real client
			// percent-encodes it, so the test must too (an unescaped space is
			// not even a legal request line).
			path += "&cursor=" + url.QueryEscape(cursor) + "&cursor_id=" + url.QueryEscape(cursorID)
		}
		rec := serveAgentSessions(t, path)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var resp struct {
			Sessions     []struct{ EntryID string } `json:"sessions"`
			HasMore      bool                       `json:"hasMore"`
			NextCursor   string                     `json:"nextCursor"`
			NextCursorID string                     `json:"nextCursorId"`
			Total        int                        `json:"total"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, 5, resp.Total, "total is page-independent")

		for _, s := range resp.Sessions {
			require.Falsef(t, seen[s.EntryID], "entry %s returned twice", s.EntryID)
			seen[s.EntryID] = true
		}
		if !resp.HasMore {
			break
		}
		cursor, cursorID = resp.NextCursor, resp.NextCursorID
		require.NotEmpty(t, cursor, "hasMore must carry a cursor")
		require.NotEmpty(t, cursorID, "hasMore must carry a cursor id")
	}
	assert.Len(t, seen, 5)
}

// The cap must be APPLIED, not merely accepted: with more rows than the cap, a
// page may never exceed it. Asserting only "200 OK" would pass even if the limit
// were ignored entirely.
func TestServeAgentSessions_LimitIsCapped(t *testing.T) {
	cleanup := setupAgentTestEnv(t)
	defer cleanup()

	const rows = maxAgentSessionsPageSize + 5
	for i := range rows {
		_, err := store.WriteExec(
			`INSERT INTO chat_sessions (id, agent_id, session_type, updated_at)
			 VALUES (?, 'codebuddy', 'chat', '2026-01-01 10:00:00')`,
			fmt.Sprintf("cap-%03d", i),
		)
		require.NoError(t, err)
	}

	rec := serveAgentSessions(t, "/api/agents/codebuddy/sessions?limit=100000")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Sessions []map[string]any `json:"sessions"`
		HasMore  bool             `json:"hasMore"`
		Total    int              `json:"total"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Len(t, resp.Sessions, maxAgentSessionsPageSize,
		"an oversized limit must be clamped to the cap")
	assert.True(t, resp.HasMore, "rows beyond the cap must be reported as more")
	assert.Equal(t, rows, resp.Total, "total counts everything, not just the page")
}

// The default page size is what a client that sends no limit gets.
func TestServeAgentSessions_DefaultPageSize(t *testing.T) {
	cleanup := setupAgentTestEnv(t)
	defer cleanup()

	const rows = defaultAgentSessionsPageSize + 1
	for i := range rows {
		_, err := store.WriteExec(
			`INSERT INTO chat_sessions (id, agent_id, session_type, updated_at)
			 VALUES (?, 'codebuddy', 'chat', '2026-01-01 10:00:00')`,
			fmt.Sprintf("def-%03d", i),
		)
		require.NoError(t, err)
	}

	rec := serveAgentSessions(t, "/api/agents/codebuddy/sessions")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Sessions []map[string]any `json:"sessions"`
		HasMore  bool             `json:"hasMore"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Len(t, resp.Sessions, defaultAgentSessionsPageSize)
	assert.True(t, resp.HasMore)
}

// The response keys must be camelCase like every other endpoint (the Go struct
// has json tags precisely so the wire shape is not the field name).
func TestServeAgentSessions_CamelCaseKeys(t *testing.T) {
	cleanup := setupAgentTestEnv(t)
	defer cleanup()

	_, err := store.WriteExec(
		`INSERT INTO chat_sessions (id, agent_id, session_type, title, updated_at)
		 VALUES ('cc1', 'codebuddy', 'chat', 'x', '2026-01-01 10:00:00')`,
	)
	require.NoError(t, err)

	rec := serveAgentSessions(t, "/api/agents/codebuddy/sessions")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Sessions []map[string]any `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Sessions, 1)
	for _, key := range []string{"entryId", "sessionId", "projectPath", "sessionType", "memberName", "updatedAt"} {
		assert.Contains(t, resp.Sessions[0], key, "response key %s must be present", key)
	}
	assert.NotContains(t, resp.Sessions[0], "EntryID", "Go field names must not leak onto the wire")
}
