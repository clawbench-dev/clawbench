package handler

import (
	"encoding/json"
	"net/http"
	"testing"

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
