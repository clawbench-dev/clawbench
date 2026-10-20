package handler

import (
	"log/slog"
	"net/http"
	"time"

	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/service"
)

// ServeUsageMeter handles GET /api/usage/meter.
//
// Returns the configured rule plus the computed state: cycle bounds, how much
// may have been used by now (the linear baseline), how much actually was, and
// the cumulative curve. The header capsule and the popover both read this one
// response, so the numbers they show cannot disagree.
//
// There is no scope parameter: the meter is deliberately instance-wide (a budget
// answers "how much did I spend in total", not "in the project I happen to be
// looking at"). Requiring authentication is therefore the same gate the stats
// panel's scope=all uses — a logged-in session can already read every project's
// usage one at a time.
//
// A disabled rule (quota <= 0) returns 200 with {"enabled": false} rather than
// 404: "the feature is off" is a valid state the client renders by hiding the
// capsule, not an error.
func ServeUsageMeter(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if !middleware.IsAuthenticated(r) {
		writeLocalizedErrorf(w, r, http.StatusForbidden, "AccessDenied")
		return
	}

	status, err := service.ComputeUsageMeter(r.Context(), time.Now())
	if err != nil {
		slog.Warn("handler: ServeUsageMeter — compute failed", slog.String("err", err.Error()))
		writeLocalizedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// PersistUsageMeterState stores the usage meter's internal notification state.
//
// Called by the service layer after a notification is emitted. This lives in the
// handler package because config.yaml writing (configMutex + atomic rename) is
// owned here; the service must not reach into that.
//
// Only the two bookkeeping fields are written. The rule itself is written from
// whatever is currently in memory, never from values captured earlier: a PATCH
// may have changed the quota while the aggregate query was running, and writing
// a stale quota back would silently undo the user's edit.
func PersistUsageMeterState(cycleStart, stage string) error {
	configMutex.Lock()
	defer configMutex.Unlock()

	snapshot := model.ConfigInstance
	um := &model.ConfigInstance.UsageMeter
	um.NotifiedCycleStart = cycleStart
	um.NotifiedStage = stage

	patch := map[string]any{
		"usage_meter": map[string]any{
			"notified_cycle_start": um.NotifiedCycleStart,
			"notified_stage":       um.NotifiedStage,
		},
	}
	if err := writeConfigYAML(patch); err != nil {
		model.ConfigInstance = snapshot
		slog.Warn("usage meter: failed to persist notification state", "error", err)
		return err
	}
	return nil
}
