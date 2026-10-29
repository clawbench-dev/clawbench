package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// acpRefreshFixture registers an ACP agent and clears its capability state, so
// the refresh path can be exercised without spawning a real agent process.
func acpRefreshFixture(t *testing.T, agent *model.Agent) {
	t.Helper()
	if agent.Transport == "" {
		agent.Transport = "acp-stdio"
	}
	model.Agents[agent.ID] = agent
	model.AgentList = append(model.AgentList, agent)
	model.RegisterModelSource(model.StaticSource(agent.Backend, "", nil))

	// The probe's throwaway session is NOT a real connection: nothing else may
	// claim the ACP lists this fixture writes.
	reg := ai.GetAgentCapabilityRegistry()
	t.Cleanup(func() {
		reg.UpdateModels(agent.ID, nil)
		reg.UpdateModes(agent.ID, nil)
		reg.UpdateThinkingEfforts(agent.ID, nil)
		reg.UpdateCommands(agent.ID, nil)
	})
}

// A refresh on an ACP agent must ask the agent (not the CLI discovery
// pipeline), apply its answer, and report the model list resolved the same way
// GET /api/agents does.
func TestAgentRefreshModels_ACPProbesAgentAndReturnsState(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	_ = env

	agent := &model.Agent{
		ID:         "acp-refresh",
		Name:       "ACP Refresh",
		Backend:    "claude",
		AcpCommand: "claude --acp",
		Models:     []model.AgentModel{{ID: "cli-a", Name: "CLI A"}, {ID: "cli-b", Name: "CLI B"}},
	}
	acpRefreshFixture(t, agent)

	var gotCwd string
	restore := ai.SetACPStateProbeForTest(func(_ context.Context, a *model.Agent, cwd string) (*ai.ACPStateProbeResult, error) {
		gotCwd = cwd
		return &ai.ACPStateProbeResult{
			Models: []model.AgentModel{
				{ID: "acp-a", Name: "ACP A"},
				{ID: "acp-b", Name: "ACP B"},
			},
			Modes:           []ai.ModeDef{{ID: "plan", Name: "Plan"}},
			Efforts:         []ai.ThinkingEffortDef{{ID: "high", Name: "High"}},
			Commands:        []ai.AvailableCommandInfo{{Name: "/review", Description: "Review"}},
			CurrentModelID:  "acp-a",
			CurrentModeID:   "plan",
			CurrentEffortID: "high",
			SessionDeleted:  true,
		}, nil
	})
	defer restore()

	req := newRequest(t, http.MethodPost, "/api/agents/acp-refresh/refresh-models", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAgentRefreshModels, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		Models    []model.AgentModel `json:"models"`
		CLIModels []model.AgentModel `json:"cliModels"`
		ACPState  struct {
			Commands       []ai.AvailableCommandInfo `json:"commands"`
			ModeState      *ai.ModeState             `json:"modeState"`
			ThinkingEffort *ai.ThinkingEffortState   `json:"thinkingEffortState"`
		} `json:"acpState"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	// The probe ran with the request's project directory — session/new requires
	// an absolute cwd, and the agent's answers can depend on it.
	assert.Equal(t, env.ProjectDir, gotCwd)

	// ACP membership is authoritative: the agent's list wins, and the CLI list is
	// still reported for a later transport switch.
	assert.Equal(t, []string{"acp-a", "acp-b"}, modelIDs(resp.Models))
	assert.Equal(t, []string{"cli-a", "cli-b"}, modelIDs(resp.CLIModels))

	// The other lists must ride along, or the drawer's mode/thinking/`/` menu
	// would keep showing the pre-refresh state.
	require.Len(t, resp.ACPState.Commands, 1)
	assert.Equal(t, "/review", resp.ACPState.Commands[0].Name)
	require.NotNil(t, resp.ACPState.ModeState)
	assert.Equal(t, "plan", resp.ACPState.ModeState.CurrentModeID)
	require.NotNil(t, resp.ACPState.ThinkingEffort)
	assert.Equal(t, "high", resp.ACPState.ThinkingEffort.AvailableLevels[0].ID)

	// The agent row keeps the CLI list: overwriting it with the ACP list would
	// make a later switch to CLI show models that transport cannot run.
	assert.Equal(t, []string{"cli-a", "cli-b"}, modelIDs(model.GetAgent("acp-refresh").Models))
}

// An ACP agent that reports no models must answer with an EMPTY list, never the
// CLI skeleton: the client writes `models` straight onto an acp-stdio agent, so
// a fallback would present models the agent cannot run.
func TestAgentRefreshModels_ACPEmptyProbeDoesNotFallBackToCLI(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agent := &model.Agent{
		ID:         "acp-empty",
		Name:       "ACP Empty",
		Backend:    "claude",
		AcpCommand: "claude --acp",
		Models:     []model.AgentModel{{ID: "cli-a", Name: "CLI A"}},
	}
	acpRefreshFixture(t, agent)

	restore := ai.SetACPStateProbeForTest(func(context.Context, *model.Agent, string) (*ai.ACPStateProbeResult, error) {
		return &ai.ACPStateProbeResult{SessionDeleted: true}, nil
	})
	defer restore()

	req := newRequest(t, http.MethodPost, "/api/agents/acp-empty/refresh-models", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAgentRefreshModels, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		Models    []model.AgentModel `json:"models"`
		CLIModels []model.AgentModel `json:"cliModels"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Empty(t, resp.Models, "the CLI skeleton must never be reported as runnable over ACP")
	assert.Equal(t, []string{"cli-a"}, modelIDs(resp.CLIModels))
}

// A failed probe is a failure — the CLI list must not be substituted for it.
func TestAgentRefreshModels_ACPProbeFailureIsReported(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agent := &model.Agent{
		ID:         "acp-broken",
		Name:       "ACP Broken",
		Backend:    "claude",
		AcpCommand: "claude --acp",
		Models:     []model.AgentModel{{ID: "cli-a", Name: "CLI A"}},
	}
	acpRefreshFixture(t, agent)

	restore := ai.SetACPStateProbeForTest(func(context.Context, *model.Agent, string) (*ai.ACPStateProbeResult, error) {
		return nil, errors.New("agent did not start")
	})
	defer restore()

	req := newRequest(t, http.MethodPost, "/api/agents/acp-broken/refresh-models", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAgentRefreshModels, req)

	require.Equal(t, http.StatusInternalServerError, w.Code)

	var resp struct {
		MsgKey string         `json:"msgKey"`
		Detail map[string]any `json:"detail"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ModelDiscoveryFailed", resp.MsgKey)
	assert.Equal(t, "agent did not start", resp.Detail["detail"])
}

// The probe needs a cwd (session/new validates it), so a request without a
// project must be refused before spawning anything.
func TestAgentRefreshModels_ACPRequiresProject(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	agent := &model.Agent{
		ID:         "acp-noproject",
		Name:       "ACP NoProject",
		Backend:    "claude",
		AcpCommand: "claude --acp",
	}
	acpRefreshFixture(t, agent)

	probed := false
	restore := ai.SetACPStateProbeForTest(func(context.Context, *model.Agent, string) (*ai.ACPStateProbeResult, error) {
		probed = true
		return &ai.ACPStateProbeResult{}, nil
	})
	defer restore()

	req := newRequest(t, http.MethodPost, "/api/agents/acp-noproject/refresh-models", nil)
	w := callHandler(ServeAgentRefreshModels, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.False(t, probed, "no project means no cwd, so nothing may be spawned")
}

// A CLI-transport agent must keep using discovery — the probe would spawn an
// agent process the user never asked for.
func TestAgentRefreshModels_CLITransportDoesNotProbe(t *testing.T) {
	defer setupAgentTestEnv(t)()

	// codebuddy has an AcpCommand in some registries but transport "cli" here.
	cb := model.Agents["codebuddy"]
	require.NotNil(t, cb)
	cb.Transport = "cli"
	t.Cleanup(func() { cb.Transport = "" })

	probed := false
	restore := ai.SetACPStateProbeForTest(func(context.Context, *model.Agent, string) (*ai.ACPStateProbeResult, error) {
		probed = true
		return &ai.ACPStateProbeResult{}, nil
	})
	defer restore()

	origDiscover := model.DiscoverWithDetail
	model.DiscoverWithDetail = func(string) ([]model.AgentModel, string) {
		return []model.AgentModel{{ID: "glm-6", Name: "GLM 6", Default: true}}, ""
	}
	defer func() { model.DiscoverWithDetail = origDiscover }()

	req := newRequest(t, http.MethodPost, "/api/agents/codebuddy/refresh-models", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgentRefreshModels, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.False(t, probed)
}

func modelIDs(models []model.AgentModel) []string {
	out := make([]string, 0, len(models))
	for _, m := range models {
		out = append(out, m.ID)
	}
	return out
}
