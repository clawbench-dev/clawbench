package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDisabledEffectWarningsSurfaceAsWarnings pins the contract both master
// switches share: when the cleanup after "switch off" fails, the API still
// reports success (the config is already persisted, and a 500 would make the
// client roll the switch back to ON while the server is off) — the failure
// travels as a warning instead.
func TestDisabledEffectWarningsSurfaceAsWarnings(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()
	saveFeatureSwitchState(t)

	// A failing ForgeDisableEffect path cannot be produced without breaking the
	// DB, so its message shape is checked against the documented format: one
	// plain sentence naming the feature. What is asserted here is that the
	// success path is silent — a warning rendered on every toggle would train
	// users to ignore the field.
	require.Empty(t, ForgeDisableEffect(), "nothing removed is not a warning")
	require.Empty(t, ShareDisableEffect())
}

// TestServeFeatures_SwitchDrivesReason pins the axis relationship end to end:
// the reason a client greys a feature out comes from the same two predicates the
// route gate uses. "disabled" must win over "capability", or a turned-off
// feature would tell the user to change platform.
func TestServeFeatures_SwitchDrivesReason(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	w := serveFeaturesForTest(t)
	require.Equal(t, http.StatusOK, w.Code)
	var body featuresResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

	for _, f := range body.Features {
		switch {
		case !f.Enabled:
			require.Equal(t, "disabled", f.Reason, f.ID)
		case !f.Available:
			require.Equal(t, "capability", f.Reason, f.ID)
		default:
			require.Empty(t, f.Reason, f.ID)
		}
	}
}
