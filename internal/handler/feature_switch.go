package handler

import (
	"net/http"

	"clawbench/internal/model"
)

// The tasks / share master switches are package globals (model.TasksEnabled /
// model.ShareEnabled) rather than direct ConfigInstance reads, so a test that
// resets ConfigInstance cannot silently switch a feature off. See the field
// comments in internal/model/config.go.
//
// Mail has no such global: its switch has always been read straight from
// ConfigInstance (internal/service/mail_worker.go does the same).
//
// Where the gate lives: the router's feature gate (internal/plugins.Gate, wired
// in RegisterRoutes) refuses a whole prefix while its feature is off. That
// covers mail, forge, share, the tasks create/update routes, and the terminal.
// The helpers below are only for rules a prefix cannot express — see
// rejectIfTasksDisabled.

// tasksFeatureEnabled reports whether scheduled tasks may be created or run.
func tasksFeatureEnabled() bool { return model.TasksEnabled }

// shareFeatureEnabled reports whether new share links may be created.
func shareFeatureEnabled() bool { return model.ShareEnabled }

// rejectIfTasksDisabled writes 409 TasksDisabled and reports true when the
// feature is off. Callers return immediately on true.
//
// It survives the move to the router gate because the tasks switch is not a
// whole-prefix rule: while tasks are off the list, detail, execution history and
// cancel/delete must keep working (that is how a user inspects or cleans up
// after the switch cancelled everything), and the /api/tasks/{id} action
// endpoint carries "resume" and "trigger" — which must be refused — in the same
// POST body as "cancel", which must not. So POST /api/tasks/ is GateExempt and
// these two actions check here.
//
// 409 (not 403): the caller is allowed to use the API, the feature is simply
// switched off. It mirrors the ForgeDisabled answer so clients handle both the
// same way.
func rejectIfTasksDisabled(w http.ResponseWriter, r *http.Request) bool {
	if tasksFeatureEnabled() {
		return false
	}
	writeLocalizedErrorf(w, r, http.StatusConflict, "TasksDisabled")
	return true
}
