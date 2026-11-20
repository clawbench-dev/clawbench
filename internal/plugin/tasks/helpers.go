package tasks

import (
	"net/http"

	"clawbench/internal/httpx"
	"clawbench/internal/model"
)

// jsonMaxCount 与 handler 的同名常量一致（API JSON key）。
const jsonMaxCount = "MaxCount"

// rejectIfTasksDisabled writes 409 TasksDisabled and reports true when the
// feature is off. Callers return immediately on true.
//
// It survives the move to the router gate because the tasks switch is not a
// whole-prefix rule: while tasks are off the list, detail, execution history and
// cancel/delete must keep working, and the /api/tasks/{id} action endpoint
// carries "resume" and "trigger" in the same POST body as "cancel".
func rejectIfTasksDisabled(w http.ResponseWriter, r *http.Request) bool {
	if model.TasksEnabled {
		return false
	}
	httpx.WriteLocalizedErrorf(w, r, http.StatusConflict, "TasksDisabled")
	return true
}

// setSessionID points the browser at a session.
func setSessionID(w http.ResponseWriter, _ *http.Request, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     model.ScopedCookieName("chat_session_id"),
		Value:    sessionID,
		Path:     "/",
		MaxAge:   86400 * 30,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}
