package handler

import (
	"net/http"

	"clawbench/internal/model"
)

// The tasks / share master switches are package globals (model.TasksEnabled /
// model.ShareEnabled) rather than direct ConfigInstance reads, so a test that
// resets ConfigInstance cannot silently switch a feature off. See the field
// comments in internal/model/config.go.

// tasksFeatureEnabled reports whether scheduled tasks may be created or run.
func tasksFeatureEnabled() bool { return model.TasksEnabled }

// shareFeatureEnabled reports whether new share links may be created.
func shareFeatureEnabled() bool { return model.ShareEnabled }

// rejectIfTasksDisabled writes 409 TasksDisabled and reports true when the
// feature is off. Callers return immediately on true.
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

// rejectIfShareDisabled writes 409 ShareDisabled and reports true when the
// feature is off.
func rejectIfShareDisabled(w http.ResponseWriter, r *http.Request) bool {
	if shareFeatureEnabled() {
		return false
	}
	writeLocalizedErrorf(w, r, http.StatusConflict, "ShareDisabled")
	return true
}
