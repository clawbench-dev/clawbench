package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

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
