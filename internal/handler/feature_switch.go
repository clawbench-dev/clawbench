package handler

import (
	"fmt"
	"log/slog"
	"net/http"

	"clawbench/internal/model"
	"clawbench/internal/service"
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

// ForgeDisableEffect clears every project's repository binding when the forge
// integration is switched off.
//
// Bindings are what actually drives polling, so this is what makes "disabled"
// mean "no outbound requests" rather than just "hidden". The poller also checks
// the flag each cycle (belt and braces for the window before this runs).
//
// It is the forge plugin's OnDisable, wired in cmd/server. The timing — after
// writeConfigYAML succeeded — is enforced by serveConfigPatch; doing it first
// and then failing to persist would destroy the user's bindings while the
// switch stayed on.
//
// A failure is reported as a WARNING, not a 500: the config is already
// persisted, so answering 500 would make the client roll `forge.enabled` back
// to true while the server is disabled. The stale bindings are harmless (the
// poller checks the flag first) and the next disable retries the cleanup.
func ForgeDisableEffect() []string {
	removed, err := service.ClearAllProjectForges()
	if err != nil {
		slog.Error("forge: clear bindings after disable failed", "err", err)
		return []string{fmt.Sprintf("forge disabled, but clearing repository bindings failed: %v", err)}
	}
	if removed > 0 {
		slog.Info("forge: integration disabled, bindings cleared", "removed", removed)
	}
	return nil
}

// ShareDisableEffect revokes every issued share link when sharing is switched
// off (file, conversation and meeting links all at once).
//
// Links are server-issued capability URLs, so "off" has to mean they stop
// working everywhere — which one browser cannot do for another. Re-enabling does
// NOT restore them: that is the only honest reading of "revoke".
//
// Same warning semantics as ForgeDisableEffect: the switch already took effect.
func ShareDisableEffect() []string {
	res, err := service.RevokeAllShares()
	if err != nil {
		slog.Error("share: revoke all links after disable failed", "err", err)
		return []string{fmt.Sprintf("share disabled, but revoking existing links failed: %v", err)}
	}
	slog.Info(
		"share: master switch off, links revoked",
		"file", res.FileShares,
		"session", res.SessionShares,
		"meeting", res.MeetingShares,
	)
	return nil
}
