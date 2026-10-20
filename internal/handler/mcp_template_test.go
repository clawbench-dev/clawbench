package handler

import (
	"strings"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// renderMCPTemplate must substitute every placeholder it advertises: a raw
// "{{ENDPOINTS}}" reaching the AI would make the command useless, and a missing
// auth block would 401 on every call.
func TestRenderMCPTemplate_SubstitutesPlaceholders(t *testing.T) {
	out, err := renderMCPTemplate("/tmp/proj")
	require.NoError(t, err)
	assert.NotContains(t, out, "{{ENDPOINTS}}")
	assert.NotContains(t, out, "{{BASE_URL}}")
	assert.NotContains(t, out, "{{PROJECT_COOKIE}}")
	assert.NotContains(t, out, "{{PROJECT_PATH}}")
	assert.Contains(t, out, "/tmp/proj")
	assert.NotEmpty(t, strings.TrimSpace(out))
}

// explicitAgentRejection names the reason an agent cannot be used here, and
// returns "" for the cases the caller handles itself (no id, unknown id) so the
// two layers do not report two different errors for one request.
func TestExplicitAgentRejection(t *testing.T) {
	defer setupAgentTestEnv(t)()

	assert.Empty(t, explicitAgentRejection("", "/tmp/p"), "no id is the caller's concern")
	assert.Empty(t, explicitAgentRejection("nope", "/tmp/p"), "unknown id is the caller's concern")

	agent := model.GetAgent("claude")
	require.NotNil(t, agent)
	agent.Disabled = true
	t.Cleanup(func() { agent.Disabled = false })
	assert.Equal(t, "AgentDisabled", explicitAgentRejection("claude", "/tmp/p"))

	agent.Disabled = false
	other := int64(999999) // some other project
	agent.ProjectID = &other
	t.Cleanup(func() { agent.ProjectID = nil })
	assert.Equal(t, "AgentNotInProject", explicitAgentRejection("claude", "/tmp/p"))

	agent.ProjectID = nil
	assert.Empty(t, explicitAgentRejection("claude", "/tmp/p"), "a global agent is always usable")
}
