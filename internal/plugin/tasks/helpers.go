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
// It exists alongside the router gate because the tasks switch is not a
// whole-prefix rule: while tasks are off, the list, detail, execution history and
// cancel/delete must keep working (that is how a user inspects or cleans up after
// the switch cancelled everything), while "resume" and "trigger" must be
// refused — and all of those arrive as actions in the same POST body. The prefix
// is therefore GateExempt and these call sites decide.
//
// 409 (not 403): the caller may use the API, the feature is simply off. It
// mirrors the ForgeDisabled answer so clients handle both the same way.
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
