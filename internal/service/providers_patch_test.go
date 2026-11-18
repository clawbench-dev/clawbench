package service

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

// The backend and edition fields are only meaningful for the private protocol,
// but a patch must still carry them into the merged row: validation runs on the
// merged provider, not per field.
func TestApplyProviderPatch_DeploymentAndBackend(t *testing.T) {
	deployment := model.ProviderDeploymentSelfhosted
	backend := "codebuddy"

	got := applyProviderPatch(model.Provider{}, ProviderPatch{
		Deployment: &deployment,
		Backend:    &backend,
	})
	assert.Equal(t, deployment, got.Deployment)
	assert.Equal(t, backend, got.Backend)
}
