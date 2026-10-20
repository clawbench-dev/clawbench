package handler

import (
	"net/http"
	"strconv"
	"time"

	"clawbench/internal/service"
	"clawbench/internal/store"
)

// jsonTotal is the response key for the filtered (unpaged) row count. Declared
// locally so the two responses in this file cannot drift.
const jsonTotal = "total"

// ServeNotificationsOrClear dispatches /api/notifications by method: GET lists,
// DELETE clears. The path is one resource, so it registers one handler.
func ServeNotificationsOrClear(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ServeNotifications(w, r)
	case http.MethodDelete:
		ServeNotificationsClear(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{strError: "method not allowed"})
	}
}

// ServeNotifications lists the notification-center log.
//
// GET /api/notifications
//
// Query parameters (all optional):
//
//	project  — a project PATH; resolves to its id. Absent means all projects.
//	kind     — "session" | "task" | "forge" | "usage". Absent means all kinds.
//	range    — "today" | "7d" | "30d". Absent/unknown means no time lower bound.
//	before_at / before_id — keyset cursor from the previous page's last row.
//	limit    — page size (default 50, capped at 200).
//
// Each row carries `sourceMissing`, set when the session/task it points at no
// longer exists. Forge rows are never marked: they live upstream and have no
// local delete semantics.
//
// The project filter is by PATH (the frontend's natural key, matching every
// other project-scoped endpoint) and is resolved with ProjectIDByPath — a read
// must not register a project as a side effect, and an unknown project is
// "no data" rather than an error.
func ServeNotifications(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	q := r.URL.Query()

	filter := service.NotificationFilter{
		Kind: q.Get("kind"),
	}
	if projectPath := q.Get("project"); projectPath != "" {
		id, ok, err := store.ProjectIDByPath(projectPath)
		if err != nil {
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
			return
		}
		if !ok {
			// An unknown project has no notifications. Returning empty (rather
			// than erroring) keeps the panel's empty state reachable when the
			// user filters by a project that has never produced an event.
			writeJSON(w, http.StatusOK, map[string]any{
				jsonItems: []service.Notification{}, jsonTotal: 0,
			})
			return
		}
		filter.ProjectID = id
	}
	filter.Since = notificationRangeSince(q.Get("range"))

	// Keyset cursor. Both halves are required: applying before_at alone would
	// leave BeforeID at 0, and the tiebreaker `id < 0` would then silently drop
	// every row sharing that timestamp. A partial/unparsable cursor is ignored
	// entirely (fall back to the newest page) — the safe direction is to show
	// more, never to hide rows.
	if beforeAt := q.Get("before_at"); beforeAt != "" {
		if id, err := strconv.ParseInt(q.Get("before_id"), 10, 64); err == nil {
			filter.BeforeCreatedAt = beforeAt
			filter.BeforeID = id
		}
	}
	if l, err := strconv.Atoi(q.Get("limit")); err == nil {
		filter.Limit = l
	}

	items, err := service.ListNotifications(filter)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	service.MarkMissingSources(items)

	// The count reflects the filter (project/kind/range) but ignores paging, so
	// the panel can show "N items" for the whole filtered set.
	total, err := service.CountNotifications(filter)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	// hasMore: a full page implies there may be another. Compare against the
	// page size actually requested (the service applies its own default/cap, so
	// mirror that here rather than guessing).
	pageSize := filter.Limit
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}
	hasMore := len(items) >= pageSize

	writeJSON(w, http.StatusOK, map[string]any{
		jsonItems:   items,
		jsonTotal:   total,
		jsonHasMore: hasMore,
	})
}

// ServeNotificationsClear deletes the notification log.
//
// DELETE /api/notifications
//
// Clears the WHOLE log (not a filtered subset): the panel's "clear" action is a
// destructive reset, and a filtered clear would leave the user unable to reason
// about what remains. Returns the number of rows removed.
func ServeNotificationsClear(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodDelete) {
		return
	}
	n, err := service.ClearNotifications()
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": n})
}

// notificationRangeSince maps the `range` token to a created_at lower bound,
// using the same tokens the panel's time filter offers.
func notificationRangeSince(token string) string {
	return service.NotificationRangeSince(token, time.Now())
}
