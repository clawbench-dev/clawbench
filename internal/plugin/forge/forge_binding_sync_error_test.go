package forge

import (
	"errors"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bindingView feeds the panel's binding card. A repository whose polling is
// failing still answers live reads, so the only way the user learns about the
// failure is this field. Without it the panel looked healthy while event tasks
// silently never fired.
func TestBindingView_SurfacesSyncFailure(t *testing.T) {
	_, teardown := setupForgeEnv(t)
	defer teardown()

	pf := &service.ProjectForge{
		Platform: "github",
		Host:     "github.com",
		Owner:    "acme",
		Repo:     "widgets",
		Source:   "manual",
	}

	view := bindingView(pf)
	_, has := view["syncError"]
	assert.False(t, has, "a healthy repository must not report a sync error")

	service.RecordForgeSyncFailure(pf.RepoKey(), errors.New("401 Bad credentials"))

	view = bindingView(pf)
	raw, has := view["syncError"]
	require.True(t, has, "a recorded failure must reach the panel")

	syncErr, ok := raw.(map[string]any)
	require.True(t, ok, "syncError must be an object, got %T", raw)
	assert.Equal(t, "401 Bad credentials", syncErr[strReqError])
	assert.NotNil(t, syncErr["occurredAt"])

	// A later success clears it, so the panel goes back to looking healthy.
	service.ClearForgeSyncFailure(pf.RepoKey())
	view = bindingView(pf)
	_, has = view["syncError"]
	assert.False(t, has, "clearing the failure must remove the field")
}

// The failure is per repository: another repo's bad token must not make this
// binding look broken.
func TestBindingView_SyncFailureIsPerRepository(t *testing.T) {
	_, teardown := setupForgeEnv(t)
	defer teardown()

	failing := &service.ProjectForge{Platform: "github", Host: "github.com", Owner: "acme", Repo: "broken", Source: "manual"}
	healthy := &service.ProjectForge{Platform: "github", Host: "github.com", Owner: "acme", Repo: "fine", Source: "manual"}

	service.RecordForgeSyncFailure(failing.RepoKey(), errors.New("boom"))

	_, hasFailing := bindingView(failing)["syncError"]
	_, hasHealthy := bindingView(healthy)["syncError"]
	assert.True(t, hasFailing)
	assert.False(t, hasHealthy)
}
