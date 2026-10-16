package handler

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// insertNotification seeds one row directly, so the handler tests do not depend
// on the emit sites.
func insertNotification(t *testing.T, eventID, kind, eventType, title string, projectID int64, sessionID, taskID string) {
	t.Helper()
	_, err := store.WriteExec(
		`INSERT INTO notifications (event_id, kind, event_type, title, body, project_id, session_id, task_id, created_at)
		 VALUES (?, ?, ?, ?, '', ?, ?, ?, ?)`,
		eventID, kind, eventType, title, projectID, sessionID, taskID,
		// Fixed sub-second timestamp so ordering is deterministic across rows.
		"2026-10-15T12:00:00.000Z",
	)
	require.NoError(t, err)
}

func TestServeNotifications_ListsNewestFirst(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	pid := store.ProjectIDForTest(t, env.ProjectDir)

	insertNotification(t, "e1", service.NotificationKindSession, "completed", "First", pid, "", "")
	insertNotification(t, "e2", service.NotificationKindTask, "failed", "Second", pid, "", "")

	req := withProjectCookie(newRequest(t, http.MethodGet, "/api/notifications", nil), env.ProjectDir)
	w := callHandler(ServeNotifications, req)
	assertStatus(t, w, http.StatusOK)

	var resp struct {
		Items []service.Notification `json:"items"`
		Total int                    `json:"total"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Items, 2)
	assert.Equal(t, 2, resp.Total)
}

func TestServeNotifications_FilterByKind(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	pid := store.ProjectIDForTest(t, env.ProjectDir)

	insertNotification(t, "e1", service.NotificationKindSession, "completed", "S", pid, "", "")
	insertNotification(t, "e2", service.NotificationKindTask, "failed", "T", pid, "", "")

	req := withProjectCookie(newRequest(t, http.MethodGet, "/api/notifications?kind=task", nil), env.ProjectDir)
	w := callHandler(ServeNotifications, req)
	assertStatus(t, w, http.StatusOK)

	var resp struct {
		Items []service.Notification `json:"items"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Items, 1)
	assert.Equal(t, service.NotificationKindTask, resp.Items[0].Kind)
}

func TestServeNotifications_FilterByProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	pid := store.ProjectIDForTest(t, env.ProjectDir)

	insertNotification(t, "e1", service.NotificationKindSession, "completed", "Mine", pid, "", "")
	// A row attributed to no project (the 0 sentinel) must not appear when
	// filtering by a real project.
	insertNotification(t, "e2", service.NotificationKindSession, "completed", "Global", 0, "", "")

	req := withProjectCookie(newRequest(t, http.MethodGet, "/api/notifications?project="+env.ProjectDir, nil), env.ProjectDir)
	w := callHandler(ServeNotifications, req)
	assertStatus(t, w, http.StatusOK)

	var resp struct {
		Items []service.Notification `json:"items"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Items, 1)
	assert.Equal(t, "Mine", resp.Items[0].Title)
}

func TestServeNotifications_UnknownProjectReturnsEmpty(t *testing.T) {
	// A project that has never produced an event is "no data", not an error.
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := withProjectCookie(newRequest(t, http.MethodGet, "/api/notifications?project=/never/seen", nil), env.ProjectDir)
	w := callHandler(ServeNotifications, req)
	assertStatus(t, w, http.StatusOK)

	var resp struct {
		Items []service.Notification `json:"items"`
		Total int                    `json:"total"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.Items)
	assert.Equal(t, 0, resp.Total)
}

func TestServeNotifications_MarksMissingSource(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	pid := store.ProjectIDForTest(t, env.ProjectDir)

	// One notification points at a session that does not exist; another at a
	// task that does not exist; a forge row is never probed.
	insertNotification(t, "e1", service.NotificationKindSession, "completed", "Gone session", pid, "no-such-session", "")
	insertNotification(t, "e2", service.NotificationKindTask, "failed", "Gone task", pid, "", "999")
	insertNotification(t, "e3", service.NotificationKindForge, "merged", "PR", pid, "", "")

	req := withProjectCookie(newRequest(t, http.MethodGet, "/api/notifications", nil), env.ProjectDir)
	w := callHandler(ServeNotifications, req)
	assertStatus(t, w, http.StatusOK)

	var resp struct {
		Items []service.Notification `json:"items"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	byTitle := map[string]service.Notification{}
	for _, n := range resp.Items {
		byTitle[n.Title] = n
	}
	assert.True(t, byTitle["Gone session"].SourceMissing, "deleted session must be flagged")
	assert.True(t, byTitle["Gone task"].SourceMissing, "deleted task must be flagged")
	assert.False(t, byTitle["PR"].SourceMissing, "forge rows are never probed")
}

func TestServeNotifications_ClearDeletesEverything(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	pid := store.ProjectIDForTest(t, env.ProjectDir)

	insertNotification(t, "e1", service.NotificationKindSession, "completed", "A", pid, "", "")
	insertNotification(t, "e2", service.NotificationKindSession, "completed", "B", pid, "", "")

	req := withProjectCookie(newRequest(t, http.MethodDelete, "/api/notifications", nil), env.ProjectDir)
	w := callHandler(ServeNotificationsClear, req)
	assertStatus(t, w, http.StatusOK)

	var resp struct {
		Deleted int64 `json:"deleted"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(2), resp.Deleted)

	// Confirm the log is actually empty afterwards.
	listReq := withProjectCookie(newRequest(t, http.MethodGet, "/api/notifications", nil), env.ProjectDir)
	lw := callHandler(ServeNotifications, listReq)
	var listResp struct {
		Items []service.Notification `json:"items"`
	}
	require.NoError(t, json.Unmarshal(lw.Body.Bytes(), &listResp))
	assert.Empty(t, listResp.Items)
}

func TestServeNotificationsOrClear_MethodDispatch(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// GET lists.
	getReq := withProjectCookie(newRequest(t, http.MethodGet, "/api/notifications", nil), env.ProjectDir)
	assertStatus(t, callHandler(ServeNotificationsOrClear, getReq), http.StatusOK)

	// DELETE clears.
	delReq := withProjectCookie(newRequest(t, http.MethodDelete, "/api/notifications", nil), env.ProjectDir)
	assertStatus(t, callHandler(ServeNotificationsOrClear, delReq), http.StatusOK)

	// Anything else is rejected rather than silently treated as a read.
	postReq := withProjectCookie(newRequest(t, http.MethodPost, "/api/notifications", nil), env.ProjectDir)
	assertStatus(t, callHandler(ServeNotificationsOrClear, postReq), http.StatusMethodNotAllowed)
}

// TestServeNotifications_PartialCursorIsIgnored guards the keyset-cursor parse.
//
// Applying before_at without a parsable before_id would leave BeforeID at 0, and
// the `id < 0` tiebreaker would then drop every row sharing that timestamp. A
// partial cursor must fall back to the newest page (show more, never hide).
func TestServeNotifications_PartialCursorIsIgnored(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	pid := store.ProjectIDForTest(t, env.ProjectDir)

	insertNotification(t, "e1", service.NotificationKindSession, "completed", "Row", pid, "", "")

	// before_at present, before_id missing.
	req := withProjectCookie(
		newRequest(t, http.MethodGet, "/api/notifications?before_at=2026-10-15T12:00:00.000Z", nil),
		env.ProjectDir,
	)
	w := callHandler(ServeNotifications, req)
	assertStatus(t, w, http.StatusOK)

	var resp struct {
		Items []service.Notification `json:"items"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Items, 1, "a partial cursor must not hide rows")
}

// TestServeNotifications_CompleteCursorApplies checks the other half of the
// cursor contract: when BOTH halves are present and parsable the filter is
// actually applied, so paging walks backwards through the log.
func TestServeNotifications_CompleteCursorApplies(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	pid := store.ProjectIDForTest(t, env.ProjectDir)

	insertNotification(t, "e1", service.NotificationKindSession, "completed", "Only", pid, "", "")

	// Cursor at the row's own timestamp with before_id=0: the row's id (>=1) is
	// NOT < 0, and its created_at is not strictly older, so the tiebreaker
	// excludes it. This proves the cursor is applied rather than ignored.
	req := withProjectCookie(
		newRequest(t, http.MethodGet,
			"/api/notifications?before_at=2026-10-15T12:00:00.000Z&before_id=0", nil),
		env.ProjectDir,
	)
	w := callHandler(ServeNotifications, req)
	assertStatus(t, w, http.StatusOK)

	var resp struct {
		Items []service.Notification `json:"items"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.Items, "the id tiebreaker must page past a row sharing the cursor timestamp")

	// Sanity: a cursor whose id is ABOVE the row's id keeps it (same timestamp,
	// id < 999), confirming the two halves are combined, not just before_at.
	keepReq := withProjectCookie(
		newRequest(t, http.MethodGet,
			"/api/notifications?before_at=2026-10-15T12:00:00.000Z&before_id=999", nil),
		env.ProjectDir,
	)
	kw := callHandler(ServeNotifications, keepReq)
	assertStatus(t, kw, http.StatusOK)
	var keepResp struct {
		Items []service.Notification `json:"items"`
	}
	require.NoError(t, json.Unmarshal(kw.Body.Bytes(), &keepResp))
	assert.Len(t, keepResp.Items, 1, "a row older than the cursor id must still be returned")
}

// TestServeNotifications_LimitControlsPageAndHasMore pins the paging response:
// a page filled to the requested limit reports hasMore, and one that is not
// does not. Without this the panel would either stop early or loop forever.
func TestServeNotifications_LimitControlsPageAndHasMore(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	pid := store.ProjectIDForTest(t, env.ProjectDir)

	for _, id := range []string{"e1", "e2", "e3"} {
		insertNotification(t, id, service.NotificationKindSession, "completed", id, pid, "", "")
	}

	// limit=2 against 3 rows → full page, so hasMore must be true.
	req := withProjectCookie(newRequest(t, http.MethodGet, "/api/notifications?limit=2", nil), env.ProjectDir)
	w := callHandler(ServeNotifications, req)
	assertStatus(t, w, http.StatusOK)

	var resp struct {
		Items   []service.Notification `json:"items"`
		Total   int                    `json:"total"`
		HasMore bool                   `json:"hasMore"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Items, 2, "the page must honor the requested limit")
	assert.True(t, resp.HasMore, "a full page implies there may be more")
	// total ignores paging so the panel can show the size of the filtered set.
	assert.Equal(t, 3, resp.Total)

	// limit=10 against 3 rows → partial page, so hasMore must be false.
	bigReq := withProjectCookie(newRequest(t, http.MethodGet, "/api/notifications?limit=10", nil), env.ProjectDir)
	bw := callHandler(ServeNotifications, bigReq)
	assertStatus(t, bw, http.StatusOK)
	var bigResp struct {
		Items   []service.Notification `json:"items"`
		HasMore bool                   `json:"hasMore"`
	}
	require.NoError(t, json.Unmarshal(bw.Body.Bytes(), &bigResp))
	assert.Len(t, bigResp.Items, 3)
	assert.False(t, bigResp.HasMore, "a partial page is the end of the log")
}

// TestServeNotifications_LimitIsCappedAt200 guards the hasMore calculation's
// upper clamp: the service caps the page at 200, so the handler must compare
// against the same figure or a 500-row request would never report hasMore.
func TestServeNotifications_LimitIsCappedAt200(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	pid := store.ProjectIDForTest(t, env.ProjectDir)

	insertNotification(t, "e1", service.NotificationKindSession, "completed", "One", pid, "", "")

	req := withProjectCookie(newRequest(t, http.MethodGet, "/api/notifications?limit=500", nil), env.ProjectDir)
	w := callHandler(ServeNotifications, req)
	assertStatus(t, w, http.StatusOK)

	var resp struct {
		Items   []service.Notification `json:"items"`
		HasMore bool                   `json:"hasMore"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	// One row can never fill a 200-row page, so hasMore must be false even
	// though the raw limit asked for 500.
	assert.False(t, resp.HasMore)
}

// TestServeNotifications_UnparsableLimitFallsBackToDefault verifies a malformed
// limit is ignored rather than turning into a zero/negative page size.
func TestServeNotifications_UnparsableLimitFallsBackToDefault(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	pid := store.ProjectIDForTest(t, env.ProjectDir)

	insertNotification(t, "e1", service.NotificationKindSession, "completed", "Row", pid, "", "")

	req := withProjectCookie(newRequest(t, http.MethodGet, "/api/notifications?limit=abc", nil), env.ProjectDir)
	w := callHandler(ServeNotifications, req)
	assertStatus(t, w, http.StatusOK)

	var resp struct {
		Items []service.Notification `json:"items"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Items, 1, "a bad limit must not drop rows")
}

// TestServeNotifications_RangeFiltersOldRows pins the time-range filter: a row
// older than the window is excluded, a recent one is kept.
func TestServeNotifications_RangeFiltersOldRows(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	pid := store.ProjectIDForTest(t, env.ProjectDir)

	// Insert the recent row at "now" rather than a fixed date, so the assertion
	// stays valid however long after this test was written it runs.
	recent := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	_, err := store.WriteExec(
		`INSERT INTO notifications (event_id, kind, event_type, title, body, project_id, session_id, task_id, created_at)
		 VALUES (?, ?, ?, ?, '', ?, '', '', ?)`,
		"recent", service.NotificationKindSession, "completed", "Recent", pid, recent,
	)
	require.NoError(t, err)
	// A row far in the past: any supported range token must exclude it.
	_, err = store.WriteExec(
		`INSERT INTO notifications (event_id, kind, event_type, title, body, project_id, session_id, task_id, created_at)
		 VALUES (?, ?, ?, ?, '', ?, '', '', ?)`,
		"old", service.NotificationKindSession, "completed", "Ancient", pid,
		"2000-01-01T00:00:00.000Z",
	)
	require.NoError(t, err)

	req := withProjectCookie(newRequest(t, http.MethodGet, "/api/notifications?range=today", nil), env.ProjectDir)
	w := callHandler(ServeNotifications, req)
	assertStatus(t, w, http.StatusOK)

	var resp struct {
		Items []service.Notification `json:"items"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Items, 1, "only the recent row is inside the window")
	assert.Equal(t, "Recent", resp.Items[0].Title)
}

// TestServeNotifications_RejectsNonGet keeps the method guard honest when the
// handler is reached directly rather than through the dispatcher.
func TestServeNotifications_RejectsNonGet(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := withProjectCookie(newRequest(t, http.MethodPost, "/api/notifications", nil), env.ProjectDir)
	assertStatus(t, callHandler(ServeNotifications, req), http.StatusMethodNotAllowed)
}

// TestServeNotificationsClear_RejectsNonDelete is the mirror guard on the clear
// handler: a GET must not wipe the log.
func TestServeNotificationsClear_RejectsNonDelete(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	pid := store.ProjectIDForTest(t, env.ProjectDir)
	insertNotification(t, "e1", service.NotificationKindSession, "completed", "Keep", pid, "", "")

	req := withProjectCookie(newRequest(t, http.MethodGet, "/api/notifications", nil), env.ProjectDir)
	assertStatus(t, callHandler(ServeNotificationsClear, req), http.StatusMethodNotAllowed)

	// The row must still be there.
	listReq := withProjectCookie(newRequest(t, http.MethodGet, "/api/notifications", nil), env.ProjectDir)
	lw := callHandler(ServeNotifications, listReq)
	var listResp struct {
		Items []service.Notification `json:"items"`
	}
	require.NoError(t, json.Unmarshal(lw.Body.Bytes(), &listResp))
	assert.Len(t, listResp.Items, 1, "a rejected method must not have cleared the log")
}
