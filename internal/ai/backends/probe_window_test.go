package backends_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"clawbench/internal/ai"
	_ "clawbench/internal/ai/backends/all"
	"clawbench/internal/model"
)

// The probe window is derived from BackendSpec.CommandsReEmitDelay, so these
// assertions need the real backend specs registered.
// CodeBuddy pushes its plugin/skill commands only after its PluginManager has
// loaded (~4s). A short window would return the built-in list only — i.e. the
// refresh would silently drop exactly the commands that were missing.
func TestProbeCommandWindow_CodeBuddyWaitsForPluginLoad(t *testing.T) {
	maxWait, quiet := ai.ProbeCommandWindowForTest(&model.Agent{ID: "cb", Backend: "codebuddy"})

	assert.GreaterOrEqual(t, quiet, 4*time.Second,
		"the list must be considered settled only after the plugin push window")
	assert.Greater(t, maxWait, quiet,
		"maxWait must leave room beyond the quiet window")
}

func TestProbeCommandWindow_OtherBackendsUseShortWindow(t *testing.T) {
	maxWait, quiet := ai.ProbeCommandWindowForTest(&model.Agent{ID: "claude", Backend: "claude"})

	assert.Less(t, maxWait, 4*time.Second,
		"a non-CodeBuddy agent must not pay the 4s plugin window")
	assert.Less(t, quiet, maxWait)
}
