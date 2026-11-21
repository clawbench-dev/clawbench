package forge

import (
	"fmt"
	"log/slog"

	"clawbench/internal/service"
)

// ForgeDisableEffect clears every project's repository binding when the forge
// integration is switched off.
//
// Bindings are what make the server talk to GitHub/GitLab, so "off" has to mean
// "stop talking to them", not just "hide the entry". The poller also checks the
// switch every cycle (second line of defense, covering the window before this
// runs). Timing is owned by the caller: after config.yaml was written
// successfully.
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
