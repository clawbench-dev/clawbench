package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"
)

// extraConfigRecorder captures every set_config_option the handler forwards.
// The forward is asynchronous, so positive assertions poll and the negative one
// waits on the channel with a timeout.
type extraConfigRecorder struct {
	ch chan [2]string
}

// installExtraConfigRecorder wires a fake ACP connection onto the session and
// returns the recorder.
func installExtraConfigRecorder(t *testing.T, agent *model.Agent, sessionID string) *extraConfigRecorder {
	t.Helper()
	conn := ai.NewACPConnForTest(agent, sessionID)
	conn.SetAliveForTest()
	conn.SetSessionMappingForTest(sessionID, "acp-sid-extra-"+sessionID)
	// The handler only accepts ids the agent advertised for THIS session, so the
	// fixture must look like a codex connection that offered Fast mode.
	conn.SetExtraConfigOptions([]ai.ExtraConfigOption{
		{
			ConfigID: "fast-mode", Name: "Fast mode", Category: "model_config", CurrentID: "off",
			Values: []ai.ConfigOptionValue{{ID: "off", Name: "Off"}, {ID: "on", Name: "On"}},
		},
		{
			ConfigID: "fast-toggle", Name: "Fast toggle", Category: "model_config",
			Type: "boolean", CurrentID: "false",
		},
	})
	ai.GetACPConnManager().SetConnForTest(sessionID, conn)
	t.Cleanup(func() { ai.GetACPConnManager().CloseConn(sessionID) })

	rec := &extraConfigRecorder{ch: make(chan [2]string, 4)}
	conn.SetConfigOptionFnForTest(func(_ context.Context, _, configID, value string) error {
		select {
		case rec.ch <- [2]string{configID, value}:
		default:
		}
		return nil
	})
	return rec
}

// The generic option channel must reach the agent under the id IT advertised
// ("fast-mode"), with the value the user picked. A category-mapped id would be
// rejected as an unknown option and the toggle would silently do nothing.
func TestServeAISessionUpdate_ConfigOptions_ForwardsWireID(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agent := &model.Agent{ID: "extra-cfg-agent", Backend: "codex", Transport: "acp-stdio", AcpCommand: "codex"}
	sessionID, err := service.CreateSession(env.ProjectDir, "codex", "patch-extra-cfg", agent.ID, "", "default", "chat")
	require.NoError(t, err)

	rec := installExtraConfigRecorder(t, agent, sessionID)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"configOptions": map[string]string{"fast-mode": "on"},
	})
	w := callHandler(ServeAISessionUpdate, req)
	assertOK(t, w)

	select {
	case got := <-rec.ch:
		assert.Equal(t, [2]string{"fast-mode", "on"}, got)
	case <-time.After(2 * time.Second):
		t.Fatal("PATCH must forward the generic option to the ACP agent")
	}
}

// The settings endpoint (used by the drawer for group MEMBER rows) must honor
// the same field — otherwise the toggle works in single chat and silently does
// nothing when opened on a member.
func TestServeSessionSettingsUpdate_ConfigOptions_Forwards(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agent := &model.Agent{ID: "extra-cfg-member-agent", Backend: "codex", Transport: "acp-stdio", AcpCommand: "codex"}
	sessionID, err := service.CreateSession(env.ProjectDir, "codex", "settings-extra-cfg", agent.ID, "", "default", "chat")
	require.NoError(t, err)

	rec := installExtraConfigRecorder(t, agent, sessionID)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/settings", map[string]any{
		"sessionId":     sessionID,
		"configOptions": map[string]string{"fast-mode": "on"},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeSessionSettingsUpdate, req)
	assertOK(t, w)

	select {
	case got := <-rec.ch:
		assert.Equal(t, [2]string{"fast-mode", "on"}, got)
	case <-time.After(2 * time.Second):
		t.Fatal("settings PATCH must forward the generic option too")
	}
}

// Empty ids/values are dropped rather than sent: set_config_option with an
// empty id is a protocol error that would surface as a failed turn.
func TestApplyExtraConfigOptions_SkipsEmpty(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agent := &model.Agent{ID: "extra-cfg-empty-agent", Backend: "codex", Transport: "acp-stdio", AcpCommand: "codex"}
	sessionID, err := service.CreateSession(env.ProjectDir, "codex", "extra-cfg-empty", agent.ID, "", "default", "chat")
	require.NoError(t, err)

	rec := installExtraConfigRecorder(t, agent, sessionID)

	applyExtraConfigOptions(sessionID, map[string]string{"": "on", "fast-mode": ""})

	select {
	case got := <-rec.ch:
		t.Fatalf("empty id/value must not reach the agent, got %v", got)
	case <-time.After(100 * time.Millisecond):
	}
}

// The generic channel must not be a back door into the dedicated settings: a
// request naming "mode"/"model" would otherwise reach set_config_option without
// persistence, per-model effort validation, or auto-approve bookkeeping.
func TestApplyExtraConfigOptions_RejectsUnadvertisedIDs(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agent := &model.Agent{ID: "extra-cfg-guard-agent", Backend: "codex", Transport: "acp-stdio", AcpCommand: "codex"}
	sessionID, err := service.CreateSession(env.ProjectDir, "codex", "extra-cfg-guard", agent.ID, "", "default", "chat")
	require.NoError(t, err)

	rec := installExtraConfigRecorder(t, agent, sessionID)

	applyExtraConfigOptions(sessionID, map[string]string{"mode": "bypassPermissions", "model": "gpt-5"})

	select {
	case got := <-rec.ch:
		t.Fatalf("only agent-advertised ids may pass, got %v", got)
	case <-time.After(100 * time.Millisecond):
	}
}

// A boolean option is still carried by the same map[string]string body; the
// handler parses it and forwards it through the boolean variant.
func TestApplyExtraConfigOptions_Boolean(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agent := &model.Agent{ID: "extra-cfg-bool-agent", Backend: "codex", Transport: "acp-stdio", AcpCommand: "codex"}
	sessionID, err := service.CreateSession(env.ProjectDir, "codex", "extra-cfg-bool", agent.ID, "", "default", "chat")
	require.NoError(t, err)

	rec := installExtraConfigRecorder(t, agent, sessionID)

	applyExtraConfigOptions(sessionID, map[string]string{"fast-toggle": "true"})

	select {
	case got := <-rec.ch:
		assert.Equal(t, [2]string{"fast-toggle", "true"}, got)
	case <-time.After(2 * time.Second):
		t.Fatal("boolean option must be forwarded")
	}
}

// A non-boolean string for a boolean option is rejected before the RPC; the
// agent would answer -32602 and the turn would surface a protocol error.
func TestApplyExtraConfigOptions_RejectsNonBooleanValue(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agent := &model.Agent{ID: "extra-cfg-bool-bad-agent", Backend: "codex", Transport: "acp-stdio", AcpCommand: "codex"}
	sessionID, err := service.CreateSession(env.ProjectDir, "codex", "extra-cfg-bool-bad", agent.ID, "", "default", "chat")
	require.NoError(t, err)

	rec := installExtraConfigRecorder(t, agent, sessionID)

	applyExtraConfigOptions(sessionID, map[string]string{"fast-toggle": "maybe"})

	select {
	case got := <-rec.ch:
		t.Fatalf("non-boolean value must not reach the agent, got %v", got)
	case <-time.After(100 * time.Millisecond):
	}
}

// A pick made while the session has NO live connection must be stored, not
// dropped (issue #746). Before this, `applyExtraConfigOptions` returned early on
// a nil connection: the click vanished, the user saw no error, and the value was
// gone on the next reload. Storing it is what lets the next turn apply it.
func TestServeAISessionUpdate_ConfigOptions_NoConnPersists(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agent := &model.Agent{
		ID: "extra-cfg-offline", Backend: "codex", AcpCommand: "codex",
		Models: []model.AgentModel{{ID: "m1", Default: true}},
	}
	model.AddAgent(agent)
	t.Cleanup(func() { model.DeleteAgent(agent.ID) })

	sessionID, err := service.CreateSession(env.ProjectDir, "codex", "offline-extra-cfg", agent.ID, "m1", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/update?session_id="+sessionID, map[string]any{
		"configOptions": map[string]string{"fast-mode": "on"},
	})
	w := callHandler(ServeAISessionUpdate, req)
	assertOK(t, w)

	// Persisted for this session, so the next turn applies it.
	picks := service.GetSessionConfigOptionPicks(sessionID)
	require.Equal(t, "on", picks["fast-mode"])

	// Remembered on the agent per model, so a NEW session on the same model
	// starts with it instead of the agent's own default.
	got := model.GetAgent(agent.ID)
	require.NotNil(t, got)
	assert.Equal(t, "on", got.ConfigOptionsForModel("m1")["fast-mode"])
}

// A new session (never sent a message, so no ACP connection) must already see
// the agent-declared options: that is what makes the "advanced" tab render
// before the first message. A stored pick must win over the value the agent
// reported for itself.
func TestSessionStateSnapshot_NewSessionConfigOptions(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agent := &model.Agent{
		ID: "snap-cfg-agent", Backend: "codex", AcpCommand: "codex",
		Models: []model.AgentModel{{ID: "m1", Default: true}},
	}
	model.AddAgent(agent)
	t.Cleanup(func() { model.DeleteAgent(agent.ID) })

	reg := ai.GetAgentCapabilityRegistry()
	reg.ResetForTest(agent.ID)
	t.Cleanup(func() { reg.ResetForTest(agent.ID) })
	reg.UpdateExtraConfigOptions(agent.ID, "m1", []ai.ExtraConfigOption{
		{
			ConfigID: "fast-mode", Name: "Fast mode", Category: "model_config", Type: "select", CurrentID: "off",
			Values: []ai.ConfigOptionValue{{ID: "off", Name: "Off"}, {ID: "on", Name: "On"}},
		},
	})

	sessionID, err := service.CreateSession(env.ProjectDir, "codex", "snap-extra-cfg", agent.ID, "m1", "default", "chat")
	require.NoError(t, err)

	st := sessionStateSnapshot(sessionID, agent.ID)
	list, ok := st.ConfigOptions.([]ai.ExtraConfigOption)
	require.True(t, ok, "a new session must get the option list from the agent-level cache")
	require.Len(t, list, 1)
	// The registry holds no value; with no pick anywhere nothing is selected.
	assert.Empty(t, list[0].CurrentID)

	// The per-model default remembered on the agent is what the next turn will
	// run, so a new session shows it.
	model.UpdateAgent(agent.ID, func(a *model.Agent) {
		a.SetConfigOptionsForModel("m1", map[string]string{"fast-mode": "on"})
	})
	st = sessionStateSnapshot(sessionID, agent.ID)
	list, ok = st.ConfigOptions.([]ai.ExtraConfigOption)
	require.True(t, ok)
	require.Len(t, list, 1)
	assert.Equal(t, "on", list[0].CurrentID)

	// A pick stored on the SESSION wins over the agent-level default.
	service.PersistConfigOptionPicks(sessionID, map[string]string{"fast-mode": "off"})
	st = sessionStateSnapshot(sessionID, agent.ID)
	list, ok = st.ConfigOptions.([]ai.ExtraConfigOption)
	require.True(t, ok)
	require.Len(t, list, 1)
	assert.Equal(t, "off", list[0].CurrentID, "the drawer must show the session's pick")
}

// The generic channel must never carry an id that has its own channel: "mode"
// here would bypass mode validation and its bookkeeping, and would be stored as
// a session pick that no later code path understands.
func TestApplyExtraConfigOptions_RefusesDedicatedIDs(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agent := &model.Agent{ID: "extra-cfg-dedicated-agent", Backend: "codex", Transport: "acp-stdio", AcpCommand: "codex"}
	sessionID, err := service.CreateSession(env.ProjectDir, "codex", "extra-cfg-dedicated", agent.ID, "", "default", "chat")
	require.NoError(t, err)

	rec := installExtraConfigRecorder(t, agent, sessionID)

	applyExtraConfigOptions(sessionID, map[string]string{"mode": "bypassPermissions", "model": "gpt-5"})

	select {
	case got := <-rec.ch:
		t.Fatalf("dedicated ids must not reach the agent on the generic channel, got %v", got)
	case <-time.After(100 * time.Millisecond):
	}
	assert.Nil(t, service.GetSessionConfigOptionPicks(sessionID))
}

// After a model switch made BEFORE the first message there is no connection to
// ask, so the refreshed option list must come from the agent-level cache for the
// model the session now runs (issue #746). Otherwise the drawer keeps showing the
// previous model's options.
func TestSessionExtraConfigOptions_NoConnUsesRegistryForModel(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agent := &model.Agent{
		ID: "extra-cfg-switch-agent", Backend: "codex", AcpCommand: "codex",
		Models: []model.AgentModel{{ID: "m1", Default: true}, {ID: "m2"}},
	}
	model.AddAgent(agent)
	t.Cleanup(func() { model.DeleteAgent(agent.ID) })

	reg := ai.GetAgentCapabilityRegistry()
	reg.ResetForTest(agent.ID)
	t.Cleanup(func() { reg.ResetForTest(agent.ID) })
	reg.UpdateExtraConfigOptions(agent.ID, "m2", []ai.ExtraConfigOption{
		{ConfigID: "fast-mode", Name: "Fast mode", Category: "model_config", Type: "select", CurrentID: "off"},
	})

	sessionID, err := service.CreateSession(env.ProjectDir, "codex", "extra-cfg-switch", agent.ID, "m2", "default", "chat")
	require.NoError(t, err)

	got := sessionExtraConfigOptions(sessionID)
	require.Len(t, got, 1, "the new model's list must be returned without a connection")
	assert.Equal(t, "fast-mode", got[0].ConfigID)

	// A model whose list was never learned returns nil ("omit the key").
	service.UpdateSessionModel(sessionID, "m1")
	assert.Nil(t, sessionExtraConfigOptions(sessionID))
}

// TestIsDedicatedConfigID pins the id set owned by the dedicated channels. A
// miss here silently opens a hole: the generic channel would then write mode /
// model / effort without their validation, persistence or bookkeeping.
func TestIsDedicatedConfigID(t *testing.T) {
	for _, id := range []string{"mode", "model", "thought_level", "thinkingEffort", "effort"} {
		assert.True(t, isDedicatedConfigID(id), "%q belongs to a dedicated channel", id)
	}
	for _, id := range []string{"fast-mode", "", "Fast-Mode", "models", "efforts"} {
		assert.False(t, isDedicatedConfigID(id), "%q is a generic option", id)
	}
}

// TestMapsEqualString pins the comparison used to skip a no-op agent write.
// Both directions matter: a size mismatch must short-circuit, and a same-size
// map with a different value must not report equal (otherwise a changed pick is
// never persisted).
func TestMapsEqualString(t *testing.T) {
	cases := []struct {
		name string
		a, b map[string]string
		want bool
	}{
		{"both nil", nil, nil, true},
		{"nil vs empty", nil, map[string]string{}, true},
		{"same entries", map[string]string{"a": "1"}, map[string]string{"a": "1"}, true},
		{"different size", map[string]string{"a": "1"}, map[string]string{"a": "1", "b": "2"}, false},
		{"same size different value", map[string]string{"a": "1"}, map[string]string{"a": "2"}, false},
		{"same size different key", map[string]string{"a": "1"}, map[string]string{"b": "1"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, mapsEqualString(tc.a, tc.b))
			assert.Equal(t, tc.want, mapsEqualString(tc.b, tc.a), "must be symmetric")
		})
	}
}

// TestRememberConfigOptionPicks_StoresPerModel covers the mechanism that makes a
// NEW session start with the user's last choice: the pick is merged into the
// agent's per-model map, and a pick under the model the session runs does not
// leak into another model's entry.
func TestRememberConfigOptionPicks_StoresPerModel(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agent := &model.Agent{
		ID: "remember-picks-agent", Backend: "codex", AcpCommand: "codex",
		Models: []model.AgentModel{{ID: "m1", Default: true}, {ID: "m2"}},
	}
	model.AddAgent(agent)
	t.Cleanup(func() { model.DeleteAgent(agent.ID) })

	sessionID, err := service.CreateSession(env.ProjectDir, "codex", "remember-picks", agent.ID, "m2", "default", "chat")
	require.NoError(t, err)

	rememberConfigOptionPicks(sessionID, map[string]string{"fast-mode": "on"})

	a := model.GetAgent(agent.ID)
	require.NotNil(t, a)
	assert.Equal(t, "on", a.ConfigOptionsForModel("m2")["fast-mode"], "stored under the session's model")
	assert.Empty(t, a.ConfigOptionsForModel("m1"), "must not leak into another model's entry")
}

// TestRememberConfigOptionPicks_MergesWithExisting keeps the other options when
// one is updated — the map is a set of picks, not a single value.
func TestRememberConfigOptionPicks_MergesWithExisting(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agent := &model.Agent{
		ID: "remember-merge-agent", Backend: "codex", AcpCommand: "codex",
		Models: []model.AgentModel{{ID: "m1", Default: true}},
	}
	model.AddAgent(agent)
	t.Cleanup(func() { model.DeleteAgent(agent.ID) })
	model.UpdateAgent(agent.ID, func(a *model.Agent) {
		a.SetConfigOptionsForModel("m1", map[string]string{"verbose": "off"})
	})

	sessionID, err := service.CreateSession(env.ProjectDir, "codex", "remember-merge", agent.ID, "", "default", "chat")
	require.NoError(t, err)

	// Empty model on the session: the agent's default model is used.
	rememberConfigOptionPicks(sessionID, map[string]string{"fast-mode": "on"})

	picks := model.GetAgent(agent.ID).ConfigOptionsForModel("m1")
	require.Equal(t, "on", picks["fast-mode"])
	assert.Equal(t, "off", picks["verbose"], "an unrelated existing pick must survive")
}

// TestRememberConfigOptionPicks_UnknownAgentIsNoop makes sure a stale session row
// cannot panic the update path.
func TestRememberConfigOptionPicks_UnknownAgentIsNoop(t *testing.T) {
	assert.NotPanics(t, func() {
		rememberConfigOptionPicks("no-such-session-at-all", map[string]string{"fast-mode": "on"})
	})
}

// TestAdvertisedExtraConfigOptions_NoConnWithoutAgentDisablesValidation pins the
// "nothing to validate against" contract: with no connection and no agent, the
// caller must be told validation is impossible rather than reject every id.
func TestAdvertisedExtraConfigOptions_NoConnWithoutAgentDisablesValidation(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agent := &model.Agent{ID: "advertised-none-agent", Backend: "codex", AcpCommand: "codex"}
	model.AddAgent(agent)
	t.Cleanup(func() { model.DeleteAgent(agent.ID) })

	sessionID, err := service.CreateSession(env.ProjectDir, "codex", "advertised-none", agent.ID, "", "default", "chat")
	require.NoError(t, err)

	list, validate := advertisedExtraConfigOptions(sessionID, nil)
	assert.Nil(t, list)
	assert.False(t, validate, "an unlearned list must not validate")
}

// TestRememberConfigOptionPicks_PersistsToDB covers the durable half: the pick
// must survive a process restart (it is what the settings page and the next
// session read), so the agent row has to actually be written.
func TestRememberConfigOptionPicks_PersistsToDB(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agent := &model.Agent{
		ID: "remember-persist-agent", Backend: "codex", AcpCommand: "codex",
		Models: []model.AgentModel{{ID: "m1", Default: true}},
	}
	require.NoError(t, service.SaveAgent(store.WriteDB(), agent))
	model.AddAgent(agent)
	t.Cleanup(func() { model.DeleteAgent(agent.ID) })

	sessionID, err := service.CreateSession(env.ProjectDir, "codex", "remember-persist", agent.ID, "m1", "default", "chat")
	require.NoError(t, err)

	rememberConfigOptionPicks(sessionID, map[string]string{"fast-mode": "on"})

	// Re-read from the database, not the in-memory agent, to prove durability.
	loaded, err := service.LoadAgentsFromDB()
	require.NoError(t, err)
	var stored *model.Agent
	for _, a := range loaded {
		if a != nil && a.ID == agent.ID {
			stored = a
			break
		}
	}
	require.NotNil(t, stored, "the agent row must exist")
	assert.Equal(t, "on", stored.ConfigOptionsForModel("m1")["fast-mode"],
		"the pick must be persisted, not only published in memory")
}
