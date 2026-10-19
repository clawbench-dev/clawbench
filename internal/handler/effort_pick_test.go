package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"
)

// testEffortAgentID is the agent id the effort-pick fixtures install. Every
// test in this file targets the same single agent, so the helpers take it as a
// constant instead of a parameter.
const testEffortAgentID = "opencode"

// These tests pin the user-visible contract of issue #552 on the PATCH path:
// an explicit effort pick must (a) be stored as a USER pick so later turns can
// replay it, and (b) be remembered per model so a new session or a switch back
// to that model restores it.

// setAgentEffortLevels installs an agent in memory with the given advertised
// effort levels (and model list, for the switch path).
//
// The agent is a CLI agent: static levels, model-independent. That is the right
// shape for the picker/validation tests below. Tests that exercise the ACP
// per-model path use setACPAgentEffortLevels instead.
func setAgentEffortLevels(t *testing.T, levels []string, models ...string) {
	t.Helper()
	agentModels := make([]model.AgentModel, len(models))
	for i, m := range models {
		agentModels[i] = model.AgentModel{ID: m, Name: m}
	}
	orig := model.Agents
	model.Agents = map[string]*model.Agent{
		testEffortAgentID: {
			ID: testEffortAgentID, Name: testEffortAgentID, Backend: "opencode",
			ThinkingEffortLevels: levels,
			Models:               agentModels,
		},
	}
	t.Cleanup(func() { model.Agents = orig })
}

// setACPAgentEffortLevels installs an ACP agent. Its levels are reported per
// model at runtime, so static ThinkingEffortLevels are left empty.
func setACPAgentEffortLevels(t *testing.T, models ...string) {
	t.Helper()
	agentModels := make([]model.AgentModel, len(models))
	for i, m := range models {
		agentModels[i] = model.AgentModel{ID: m, Name: m}
	}
	orig := model.Agents
	model.Agents = map[string]*model.Agent{
		testEffortAgentID: {
			ID: testEffortAgentID, Name: testEffortAgentID, Backend: "opencode",
			Transport: "acp-stdio", AcpCommand: "opencode acp",
			Models: agentModels,
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
	setAgentEffortLevels(t, []string{"low", "high", "max"}, "glm-5.3")

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

// TestServeAISessionUpdate_ModelSwitchInvalidatesPick pins that an ACP model
// switch to a model with no remembered pick CLEARS the session's pick instead of
// carrying the previous model's level over. ACP levels are per model, so the old
// level may not even be offered by the new one (OpenCode answers -32602 and the
// session would keep displaying it).
//
// CLI backends are deliberately excluded: their levels come from BackendSpec and
// are identical for every model, so a switch cannot invalidate the pick — see
// TestServeAISessionUpdate_ModelSwitchKeepsPickForCLI.
func TestServeAISessionUpdate_ModelSwitchInvalidatesPick(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	setACPAgentEffortLevels(t, "glm-5.3", "deepseek-v4-pro")

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
	assert.False(t, ok, "the previous model's pick must not survive an ACP model switch")
}

// TestServeAISessionUpdate_ModelSwitchKeepsPickForCLI pins the CLI counterpart:
// a switch must NOT discard the user's pick.
//
// CLI levels are a static BackendSpec list, the same for every model, so the
// pick cannot be invalidated by a switch. Clearing it would silently drop the
// user's choice and make them re-pick — the complaint in issue #552, for every
// CLI backend.
func TestServeAISessionUpdate_ModelSwitchKeepsPickForCLI(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	setAgentEffortLevels(t, []string{"low", "high", "max"}, "glm-5.3", "deepseek-v4-pro")

	sid, err := service.CreateSession(env.ProjectDir, "opencode", "CLI switch", "opencode", "glm-5.3", "user", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update", map[string]any{
		"sessionId":      sid,
		"thinkingEffort": "max",
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

	effort, _, ok := service.UserThinkingEffortPick(sid)
	assert.True(t, ok, "a CLI model switch must not discard the user's pick")
	assert.Equal(t, "max", effort)

	// ...and it is reported back so the chip keeps showing it.
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "max", body["thinkingEffort"])
}

// TestServeAISessionUpdate_ModelSwitchRestoresRememberedPick pins the positive
// case: switching back to a model the user configured restores that level, and
// reports it so the client can show it.
func TestServeAISessionUpdate_ModelSwitchRestoresRememberedPick(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	setACPAgentEffortLevels(t, "glm-5.3", "deepseek-v4-pro")

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
	setAgentEffortLevels(t, []string{"low", "high", "max"}, "glm-5.3", "deepseek-v4-pro")

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
// model) — that half is a wiring invariant with no observable behavior in a
// test (no live ACP connection), so it is pinned separately in
// chat_session_wiring_test.go.
func TestSwitchSessionEffortToModel_DeferredStillPersists(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	setACPAgentEffortLevels(t, "glm-5.3", "deepseek-v4-pro")

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
	setAgentEffortLevels(t, []string{"low", "high", "max"}, "glm-5.3")

	sid, err := service.CreateSession(env.ProjectDir, "opencode", "Invalid", "opencode", "glm-5.3", "user", "chat")
	require.NoError(t, err)

	accepted, remembered := rememberEffortForModel(sid, "glm-5.3", "ultra", true)
	assert.False(t, accepted, "an unadvertised level must be reported as rejected")
	assert.False(t, remembered)

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
	setAgentEffortLevels(t, []string{"low", "high", "max"}, "glm-5.3")
	model.Agents["opencode"].ThinkingEffortByModel = map[string]string{"glm-5.3": "max"}

	sid, err := service.CreateSession(env.ProjectDir, "opencode", "Same", "opencode", "glm-5.3", "user", "chat")
	require.NoError(t, err)

	before := model.GetAgent("opencode")
	accepted, remembered := rememberEffortForModel(sid, "glm-5.3", "max", true)
	assert.True(t, accepted)
	assert.True(t, remembered)
	after := model.GetAgent("opencode")

	assert.Same(t, before, after, "an unchanged pick must not republish the agent")
}

// TestApplySessionConfigSelections_InvalidLevelNotPersisted pins the ordering
// fix: validation happens BEFORE the context_state write.
//
// A level the agent does not advertise must not become a stored user pick. The
// ACP layer no longer records a rejected value (so it stays retryable), which
// means a persisted invalid pick would be re-sent on EVERY turn and returned by
// GET as the user's choice, with nothing in the UI to explain why the level
// never takes effect.
func TestApplySessionConfigSelections_InvalidLevelNotPersisted(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	setAgentEffortLevels(t, []string{"low", "high", "max"}, "glm-5.3")

	sid, err := service.CreateSession(env.ProjectDir, "opencode", "Invalid persist", "opencode", "glm-5.3", "user", "chat")
	require.NoError(t, err)

	applySessionConfigSelections(sid, "", "ultra", "", true)

	_, _, ok := service.UserThinkingEffortPick(sid)
	assert.False(t, ok, "an unadvertised level must not be stored as a user pick")
}

// TestServeAISessionUpdate_RejectedEffortIsReported pins that a level the agent
// refuses is reported to the client, not silently dropped.
//
// The level is deliberately NOT persisted (it would be re-sent every turn), so
// nothing else tells the client: without this flag the chip keeps showing a
// level the next turn will not run, and the local mirror re-sends it as an
// explicit override on the next POST (which is not validated at all).
func TestServeAISessionUpdate_RejectedEffortIsReported(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	setAgentEffortLevels(t, []string{"low", "high", "max"}, "glm-5.3")

	sid, err := service.CreateSession(env.ProjectDir, "opencode", "Rejected", "opencode", "glm-5.3", "user", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update", map[string]any{
		"sessionId":      sid,
		"thinkingEffort": "ultra",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, true, body["thinkingEffortRejected"],
		"the client must be told the level was refused so it can undo the optimistic update")

	_, _, ok := service.UserThinkingEffortPick(sid)
	assert.False(t, ok, "a rejected level must not be stored")
}

// TestSwitchSessionEffortToModel_RegistryRejectsRememberedPick pins the last
// guard on the switch path: a pick remembered for the model can still be absent
// from what the agent advertises (OpenCode's lists differ per model), and
// carrying it over would make every turn fail with -32602.
func TestSwitchSessionEffortToModel_RegistryRejectsRememberedPick(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	// A DEDICATED agent id: the capability registry has no reset API
	// (UpdateThinkingEfforts ignores an empty list), so reusing the shared
	// fixture id would leak these levels into every later test that reads the
	// registry — which is exactly what broke effort_read_test.go.
	const agentID = "opencode-registry-reject"
	setAgentEffortLevels(t, []string{"low", "high", "max"}, "glm-5.3", "deepseek-v4-pro")
	model.Agents[agentID] = model.Agents[testEffortAgentID]
	model.Agents[agentID].ID = agentID
	model.Agents[agentID].Transport = "acp-stdio"
	model.Agents[agentID].AcpCommand = "opencode acp"

	reg := ai.GetAgentCapabilityRegistry()
	reg.UpdateThinkingEfforts(agentID, []ai.ThinkingEffortDef{{ID: "high"}, {ID: "max"}})

	sid, err := service.CreateSession(env.ProjectDir, agentID, "Registry", agentID, "glm-5.3", "user", "chat")
	require.NoError(t, err)

	// The user picked "low" for deepseek-v4-pro, but the agent advertises only
	// high/max. The pick must be cleared rather than replayed.
	model.Agents[agentID].ThinkingEffortByModel = map[string]string{"deepseek-v4-pro": "low"}

	got := switchSessionEffortToModel(sid, "deepseek-v4-pro", false)
	assert.Equal(t, "", got, "a level the agent does not advertise must not be applied")

	_, _, ok := service.UserThinkingEffortPick(sid)
	assert.False(t, ok, "the unusable pick must be cleared so the agent's own default applies")
}

// TestSwitchSessionEffortToModel_UnknownAgentIsANoop pins the nil-agent guard:
// a session whose agent was deleted must not panic the switch path.
func TestSwitchSessionEffortToModel_UnknownAgentIsANoop(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	setAgentEffortLevels(t, []string{"low", "high"}, "glm-5.3")

	sid, err := service.CreateSession(env.ProjectDir, testEffortAgentID, "No agent", testEffortAgentID, "glm-5.3", "user", "chat")
	require.NoError(t, err)

	orig := model.Agents
	model.Agents = map[string]*model.Agent{}
	t.Cleanup(func() { model.Agents = orig })

	assert.Equal(t, "", switchSessionEffortToModel(sid, "glm-5.3", false))
}

// TestSessionUsesACP_TransportOverrideWins pins the precedence: the session
// row's transport override is authoritative over the agent's configured one
// (a user can flip one session to CLI without touching the agent).
func TestSessionUsesACP_TransportOverrideWins(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	setACPAgentEffortLevels(t, "glm-5.3")

	sid, err := service.CreateSession(env.ProjectDir, testEffortAgentID, "Override", testEffortAgentID, "glm-5.3", "user", "chat")
	require.NoError(t, err)

	agent := model.GetAgent(testEffortAgentID)
	require.NotNil(t, agent)
	assert.True(t, sessionUsesACP(sid, agent), "no override: the agent's acp-stdio transport decides")

	require.NoError(t, service.UpdateSessionTransport(sid, "cli"))
	assert.False(t, sessionUsesACP(sid, agent), "a per-session cli override must win")

	require.NoError(t, service.UpdateSessionTransport(sid, "acp-stdio"))
	assert.True(t, sessionUsesACP(sid, agent))
}

// TestRememberEffortForModel_MissingAgentIsANoop pins the deleted-agent guard.
func TestRememberEffortForModel_MissingAgentIsANoop(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	setAgentEffortLevels(t, []string{"low", "high"}, "glm-5.3")

	sid, err := service.CreateSession(env.ProjectDir, testEffortAgentID, "Gone", testEffortAgentID, "glm-5.3", "user", "chat")
	require.NoError(t, err)

	orig := model.Agents
	model.Agents = map[string]*model.Agent{}
	t.Cleanup(func() { model.Agents = orig })

	accepted, remembered := rememberEffortForModel(sid, "glm-5.3", "max", true)
	assert.True(t, accepted, "a deleted agent must not block the pick")
	assert.True(t, remembered, "nothing to persist for a deleted agent, so it counts as remembered")
}

// TestSwitchSessionEffortToModel_DeferredSkipsRegistryCheck pins the deferred
// half of the ordering fix.
//
// A deferred switch means the agent is STILL on the previous model, so the
// registry's level list describes that model. Validating the remembered pick for
// the NEW model against it is wrong in both directions:
//   - the old list is WIDER (glm has "low", deepseek does not): a pick that can
//     never run is kept, and every later turn is rejected;
//   - the old list is NARROWER: a legitimate pick is cleared and the user has to
//     re-choose for no reason.
//
// In the deferred case the pick must be trusted and reported, not judged.
func TestSwitchSessionEffortToModel_DeferredSkipsRegistryCheck(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	const agentID = "opencode-deferred-registry"
	setAgentEffortLevels(t, []string{"low", "high", "max"}, "glm-5.3", "deepseek-v4-pro")
	model.Agents[agentID] = model.Agents[testEffortAgentID]
	model.Agents[agentID].ID = agentID
	model.Agents[agentID].Transport = "acp-stdio"
	model.Agents[agentID].AcpCommand = "opencode acp"

	// The registry still describes the PREVIOUS model (glm: low/high/max) — the
	// agent has not switched yet.
	ai.GetAgentCapabilityRegistry().UpdateThinkingEfforts(agentID, []ai.ThinkingEffortDef{
		{ID: "low"}, {ID: "high"}, {ID: "max"},
	})

	sid, err := service.CreateSession(env.ProjectDir, agentID, "Deferred registry", agentID, "glm-5.3", "user", "chat")
	require.NoError(t, err)

	// The remembered pick for the NEW model is a level the old list happens to
	// also contain; the point is that the check must not run at all. Use "high"
	// here and prove the pick survives — with the bug it survives too, so the
	// discriminating case is the narrower one below.
	model.Agents[agentID].ThinkingEffortByModel = map[string]string{"deepseek-v4-pro": "high"}
	assert.Equal(t, "high", switchSessionEffortToModel(sid, "deepseek-v4-pro", true))

	// Discriminating case: a level the OLD model offers but the new one does not.
	// With validation it would be kept (old list contains it) — that half is not
	// observable here. The reverse is: a level only the NEW model offers must not
	// be cleared just because the old list lacks it.
	model.Agents[agentID].ThinkingEffortByModel = map[string]string{"deepseek-v4-pro": "max"}
	ai.GetAgentCapabilityRegistry().UpdateThinkingEfforts(agentID, []ai.ThinkingEffortDef{
		{ID: "low"}, {ID: "medium"},
	})
	// Simulate "the new model offers max, the old one does not" by making the
	// registry describe the OLD model (low/medium) while the pick is max.
	assert.Equal(t, "max", switchSessionEffortToModel(sid, "deepseek-v4-pro", true),
		"a deferred switch must trust the remembered pick, not judge it against the previous model's list")

	effort, modelID, ok := service.UserThinkingEffortPick(sid)
	require.True(t, ok, "the pick must not be cleared during a deferred switch")
	assert.Equal(t, "max", effort)
	assert.Equal(t, "deepseek-v4-pro", modelID)

	// And the non-deferred path still validates: the same pick is cleared once the
	// agent really is on the new model.
	assert.Equal(t, "", switchSessionEffortToModel(sid, "deepseek-v4-pro", false))
	_, _, ok = service.UserThinkingEffortPick(sid)
	assert.False(t, ok, "a non-deferred switch must still reject a level the agent does not offer")
}

// TestServeAISessionUpdate_PickAlongsideSwitchIsValidatedAfterSwitch pins the
// ordering: the model must be applied BEFORE the pick is validated and pushed.
//
// Applying the pick first validated it against the previous model's level list
// and pushed the new level at the old model. The observable contract here is
// that the pick lands on the NEW model's key and the previous model keeps none.
func TestServeAISessionUpdate_PickAlongsideSwitchIsValidatedAfterSwitch(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	const agentID = "opencode-order-after-switch"
	setAgentEffortLevels(t, []string{"low", "high", "max"}, "glm-5.3", "deepseek-v4-pro")
	model.Agents[agentID] = model.Agents[testEffortAgentID]
	model.Agents[agentID].ID = agentID
	model.Agents[agentID].Transport = "acp-stdio"
	model.Agents[agentID].AcpCommand = "opencode acp"

	ai.GetAgentCapabilityRegistry().UpdateThinkingEfforts(agentID, []ai.ThinkingEffortDef{
		{ID: "high"}, {ID: "max"},
	})

	sid, err := service.CreateSession(env.ProjectDir, agentID, "Order", agentID, "glm-5.3", "user", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update", map[string]any{
		"sessionId":      sid,
		"modelId":        "deepseek-v4-pro",
		"thinkingEffort": "max",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeAISessionUpdate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.NotEqual(t, true, body["thinkingEffortRejected"],
		"a level the agent offers must not be reported as rejected")

	effort, modelID, ok := service.UserThinkingEffortPick(sid)
	require.True(t, ok, "the pick must be persisted for the new model")
	assert.Equal(t, "max", effort)
	assert.Equal(t, "deepseek-v4-pro", modelID)
	assert.Equal(t, "max", model.GetAgent(agentID).ThinkingEffortForModel("deepseek-v4-pro"))
	assert.Equal(t, "", model.GetAgent(agentID).ThinkingEffortForModel("glm-5.3"),
		"the previous model must not inherit the pick")
}
