package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/model"

	_ "clawbench/internal/ai/backends/builtin"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// patchAgent sends a PATCH /api/agents body through the real handler.
func patchAgent(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequest(t, http.MethodPatch, "/api/agents", json.RawMessage(body))
	req = withAuthCookie(req, model.SessionToken)
	rec := httptest.NewRecorder()
	ServeAgents(rec, req)
	return rec
}

// TestServeAgentsPatch_ToolPolicy pins the settings-page path end to end.
//
// The body is what the Vue editor sends; the stored policy must be the closure
// of what it asked for, because exec can write files whether or not fs.write
// was ticked.
func TestServeAgentsPatch_ToolPolicy(t *testing.T) {
	defer setupAgentTestEnv(t)()

	rec := patchAgent(t, `{"id":"claude","tool_policy":{"allow":["exec"],"maxRounds":5}}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	agent := model.GetAgent("claude")
	require.NotNil(t, agent)
	assert.Equal(t, []string{model.CapFSRead, model.CapFSWrite, model.CapExec}, agent.ToolPolicy.Allow,
		"the stored grant must be the capability closure, not the raw list")
	assert.Equal(t, 5, agent.ToolPolicy.MaxRounds)
}

// TestServeAgentsPatch_ToolPolicyRejectsBadBudget pins that an out-of-range
// budget is a 400 instead of a silently clamped value.
func TestServeAgentsPatch_ToolPolicyRejectsBadBudget(t *testing.T) {
	defer setupAgentTestEnv(t)()

	rec := patchAgent(t, `{"id":"claude","tool_policy":{"allow":[],"maxRounds":-1}}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestServeAgentsPatch_ToolPolicyRejectsBadTimeout pins the upper bound: a
// timeout measured in days would let one command outlive the turn.
func TestServeAgentsPatch_ToolPolicyRejectsBadTimeout(t *testing.T) {
	defer setupAgentTestEnv(t)()

	rec := patchAgent(t, `{"id":"claude","tool_policy":{"allow":[],"bashTimeoutSec":999999}}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestServeAgentsPatch_ToolPolicyClearsGrant pins that an empty list really
// clears the grant (rather than being treated as "leave alone").
func TestServeAgentsPatch_ToolPolicyClearsGrant(t *testing.T) {
	defer setupAgentTestEnv(t)()

	require.Equal(t, http.StatusOK, patchAgent(t, `{"id":"claude","tool_policy":{"allow":["fs.read"]}}`).Code)
	require.Equal(t, http.StatusOK, patchAgent(t, `{"id":"claude","tool_policy":{"allow":[]}}`).Code)

	agent := model.GetAgent("claude")
	require.NotNil(t, agent)
	assert.Empty(t, agent.ToolPolicy.Allow)
}

// TestServeAgentsGet_ReportsBuiltinBackend pins the flag the settings page uses
// to hide the CLI command, install command and MCP section.
func TestServeAgentsGet_ReportsBuiltinBackend(t *testing.T) {
	defer setupAgentTestEnv(t)()

	// The fixture's agent set may not include the built-in backend; add one so
	// the flag is exercised on both a built-in and an external backend.
	builtin := *model.GetAgent("claude")
	builtin.ID = "builtin"
	builtin.Name = "Builtin"
	builtin.Backend = "builtin"
	builtin.BuiltinBackend = true
	model.Agents["builtin"] = &builtin
	model.AgentList = append(model.AgentList, &builtin)

	req := newRequest(t, http.MethodGet, "/api/agents", nil)
	req = withAuthCookie(req, model.SessionToken)
	rec := httptest.NewRecorder()
	ServeAgents(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Agents []struct {
			ID             string `json:"id"`
			BuiltinBackend bool   `json:"builtinBackend"`
		} `json:"agents"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	got := make(map[string]bool, len(resp.Agents))
	for _, a := range resp.Agents {
		got[a.ID] = a.BuiltinBackend
	}
	assert.True(t, got["builtin"], "the built-in agent must report builtinBackend")
	assert.False(t, got["claude"], "an external backend must not report builtinBackend")
}

// TestBuiltinBackendIsRegisteredWithTheExpectedSpec pins the registration flags
// the UI and the model loader depend on.
//
// The blank import is required: backends register themselves from init(), and
// the handler package does not otherwise pull the built-in one in. Without it
// this test would assert on an empty registry and pass vacuously.
func TestBuiltinBackendIsRegisteredWithTheExpectedSpec(t *testing.T) {
	spec := model.FindSpecByBackend("builtin")
	require.NotNil(t, spec, "the built-in backend must be registered")
	assert.True(t, spec.Builtin)
	assert.True(t, spec.NoCLI, "there is no binary to probe")
	assert.Empty(t, spec.AcpCommand, "this backend never spawns a process")
	assert.False(t, spec.AutoLoadsNativeSkills, "it has no skill directory of its own")
}
