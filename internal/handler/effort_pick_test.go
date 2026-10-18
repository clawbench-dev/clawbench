package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// These tests pin the user-visible contract of issue #552 on the PATCH path:
// an explicit effort pick must (a) be stored as a USER pick so later turns can
// replay it, and (b) be remembered per model so a new session or a switch back
// to that model restores it.

// setAgentEffortLevels installs an agent in memory with the given advertised
// effort levels (and model list, for the switch path).
func setAgentEffortLevels(t *testing.T, agentID string, levels []string, models ...string) {
	t.Helper()
	agentModels := make([]model.AgentModel, len(models))
	for i, m := range models {
		agentModels[i] = model.AgentModel{ID: m, Name: m}
	}
	orig := model.Agents
	model.Agents = map[string]*model.Agent{
		agentID: {
			ID: agentID, Name: agentID, Backend: "opencode",
			ThinkingEffortLevels: levels,
			Models:               agentModels,
		},
	}
	t.Cleanup(func() { model.Agents = orig })
}

// TestServeAISessionUpdate_EffortPickRememberedPerModel is the core regression
// test: picking "max" for a model must survive the session, so the next turn
// (and the next session) does not fall back to the agent's own "low" default.
func TestServeAISessionUpdate_EffortPickRememberedPerModel(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	setAgentEffortLevels(t, "opencode", []string{"low", "high", "max"}, "glm-5.3")

	sid, err := service.CreateSession(env.ProjectDir, "opencode", "Effort", "opencode", "glm-5.3", "user", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update", map[string]any{
		"sessionId":      sid,
		"thinkingEffort": "max",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// (a) Stored as a user pick for this model.
	effort, modelID, ok := service.UserThinkingEffortPick(sid)
	assert.True(t, ok, "the pick must be recorded as a user pick")
	assert.Equal(t, "max", effort)
	assert.Equal(t, "glm-5.3", modelID, "the pick must be keyed to the model it was made under")

	// (b) Remembered on the agent for that model.
	agent := model.GetAgent("opencode")
	require.NotNil(t, agent)
	assert.Equal(t, "max", agent.ThinkingEffortForModel("glm-5.3"),
		"a new session on this model must start from the remembered pick")
}

// TestServeAISessionUpdate_ModelSwitchInvalidatesPick pins that a switch to a
// model with no remembered pick CLEARS the session's pick instead of carrying
// the previous model's level over. The old level may not even be offered by the
// new model (OpenCode answers -32602 and the session would keep displaying it).
func TestServeAISessionUpdate_ModelSwitchInvalidatesPick(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	setAgentEffortLevels(t, "opencode", []string{"low", "high", "max"}, "glm-5.3", "deepseek-v4-pro")

	sid, err := service.CreateSession(env.ProjectDir, "opencode", "Switch", "opencode", "glm-5.3", "user", "chat")
	require.NoError(t, err)

	// Pick max for glm-5.3.
	req := newRequest(t, http.MethodPatch, "/api/ai/session/update", map[string]any{
		"sessionId":      sid,
		"thinkingEffort": "max",
	})
	req = withProjectCookie(req, env.ProjectDir)
	require.Equal(t, http.StatusOK, callHandler(ServeAISessionUpdate, req).Code)

	// Switch to a model with no remembered pick.
	req = newRequest(t, http.MethodPatch, "/api/ai/session/update", map[string]any{
		"sessionId": sid,
		"modelId":   "deepseek-v4-pro",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	_, _, ok := service.UserThinkingEffortPick(sid)
	assert.False(t, ok, "the previous model's pick must not survive a model switch")
}

// TestServeAISessionUpdate_ModelSwitchRestoresRememberedPick pins the positive
// case: switching back to a model the user configured restores that level, and
// reports it so the client can show it.
func TestServeAISessionUpdate_ModelSwitchRestoresRememberedPick(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	setAgentEffortLevels(t, "opencode", []string{"low", "high", "max"}, "glm-5.3", "deepseek-v4-pro")

	// Remember a pick for deepseek-v4-pro by selecting it there first.
	sid, err := service.CreateSession(env.ProjectDir, "opencode", "Restore", "opencode", "deepseek-v4-pro", "user", "chat")
	require.NoError(t, err)
	req := newRequest(t, http.MethodPatch, "/api/ai/session/update", map[string]any{
		"sessionId":      sid,
		"thinkingEffort": "high",
	})
	req = withProjectCookie(req, env.ProjectDir)
	require.Equal(t, http.StatusOK, callHandler(ServeAISessionUpdate, req).Code)

	// Move away, then back.
	req = newRequest(t, http.MethodPatch, "/api/ai/session/update", map[string]any{
		"sessionId": sid,
		"modelId":   "glm-5.3",
	})
	req = withProjectCookie(req, env.ProjectDir)
	require.Equal(t, http.StatusOK, callHandler(ServeAISessionUpdate, req).Code)

	req = newRequest(t, http.MethodPatch, "/api/ai/session/update", map[string]any{
		"sessionId": sid,
		"modelId":   "deepseek-v4-pro",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	effort, modelID, ok := service.UserThinkingEffortPick(sid)
	assert.True(t, ok, "switching back must restore the remembered pick")
	assert.Equal(t, "high", effort)
	assert.Equal(t, "deepseek-v4-pro", modelID)

	// The response carries it so the client can update the chip immediately.
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "high", body["thinkingEffort"])
}

// TestServeAISessionUpdate_EffortPickAlongsideModelSwitch keys the pick to the
// NEW model. The session row still holds the previous id when the effort is
// persisted (the model is applied last), so the request's modelId must win.
func TestServeAISessionUpdate_EffortPickAlongsideModelSwitch(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	setAgentEffortLevels(t, "opencode", []string{"low", "high", "max"}, "glm-5.3", "deepseek-v4-pro")

	sid, err := service.CreateSession(env.ProjectDir, "opencode", "Together", "opencode", "glm-5.3", "user", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update", map[string]any{
		"sessionId":      sid,
		"modelId":        "deepseek-v4-pro",
		"thinkingEffort": "max",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	_, modelID, ok := service.UserThinkingEffortPick(sid)
	require.True(t, ok)
	assert.Equal(t, "deepseek-v4-pro", modelID, "the pick belongs to the model being switched TO")

	agent := model.GetAgent("opencode")
	require.NotNil(t, agent)
	assert.Equal(t, "max", agent.ThinkingEffortForModel("deepseek-v4-pro"))
	assert.Equal(t, "", agent.ThinkingEffortForModel("glm-5.3"),
		"the previous model must not inherit the pick")
}

// TestSwitchSessionEffortToModel_DeferredStillPersists pins that a DEFERRED
// model switch still records the new model's effort for the next turn.
//
// The RPC must NOT be sent in that case (the agent is still on the previous
// model) — that half is a wiring invariant with no observable behaviour in a
// test (no live ACP connection), so it is pinned separately in
// chat_session_wiring_test.go.
func TestSwitchSessionEffortToModel_DeferredStillPersists(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	setAgentEffortLevels(t, "opencode", []string{"low", "high", "max"}, "glm-5.3", "deepseek-v4-pro")

	sid, err := service.CreateSession(env.ProjectDir, "opencode", "Deferred", "opencode", "glm-5.3", "user", "chat")
	require.NoError(t, err)

	// The handler reads the pick from the in-memory agent; set it directly so the
	// test does not depend on the DB round trip (covered separately).
	model.Agents["opencode"].ThinkingEffortByModel = map[string]string{"deepseek-v4-pro": "max"}

	got := switchSessionEffortToModel(sid, "deepseek-v4-pro", true)
	assert.Equal(t, "max", got, "the resolved level is still reported to the client")

	// The pick is persisted for the next turn...
	effort, modelID, ok := service.UserThinkingEffortPick(sid)
	require.True(t, ok)
	assert.Equal(t, "max", effort)
	assert.Equal(t, "deepseek-v4-pro", modelID)
}

// TestRememberEffortForModel_RejectsUnknownLevel pins the validation gate: a
// level the agent does not advertise must not be stored as a per-model pick.
//
// Storing it would pin the model to a value the agent rejects on every turn —
// the "display says X, agent runs Y" bug from a different angle. The ACP layer
// no longer records a rejected value, but nothing would remove the stored pick,
// so it would be re-sent forever.
func TestRememberEffortForModel_RejectsUnknownLevel(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	setAgentEffortLevels(t, "opencode", []string{"low", "high", "max"}, "glm-5.3")

	sid, err := service.CreateSession(env.ProjectDir, "opencode", "Invalid", "opencode", "glm-5.3", "user", "chat")
	require.NoError(t, err)

	rememberEffortForModel(sid, "glm-5.3", "ultra", true)

	agent := model.GetAgent("opencode")
	require.NotNil(t, agent)
	assert.Equal(t, "", agent.ThinkingEffortForModel("glm-5.3"),
		"an unadvertised level must not be remembered")
}

// TestRememberEffortForModel_SkipsUnchangedWrite pins the no-op short circuit:
// re-selecting the same level must not take the global write mutex and
// republish the agent pointer.
func TestRememberEffortForModel_SkipsUnchangedWrite(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	setAgentEffortLevels(t, "opencode", []string{"low", "high", "max"}, "glm-5.3")
	model.Agents["opencode"].ThinkingEffortByModel = map[string]string{"glm-5.3": "max"}

	sid, err := service.CreateSession(env.ProjectDir, "opencode", "Same", "opencode", "glm-5.3", "user", "chat")
	require.NoError(t, err)

	before := model.GetAgent("opencode")
	rememberEffortForModel(sid, "glm-5.3", "max", true)
	after := model.GetAgent("opencode")

	assert.Same(t, before, after, "an unchanged pick must not republish the agent")
}
