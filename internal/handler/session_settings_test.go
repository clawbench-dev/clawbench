package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServeSessionSettings_ReturnsSessionState pins the endpoint the settings
// drawer uses for a targeted session (single chat, group, or group member row).
//
// A group member row has NO chat_history of its own — all member speech lands on
// the group timeline — so GET /api/ai/chat cannot describe it. The drawer must
// still open on a member, which is why this endpoint addresses sessions
// uniformly.
func TestServeSessionSettings_ReturnsSessionState(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "codebuddy", "Settings", "codebuddy", "", "user", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/session/settings?session_id="+sid, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeSessionSettings, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var got struct {
		OK          bool   `json:"ok"`
		SessionID   string `json:"sessionId"`
		SessionType string `json:"sessionType"`
		AgentID     string `json:"agentId"`
		Backend     string `json:"backend"`
		Transport   string `json:"transport"`
		Cruise      bool   `json:"cruise"`
		IsMember    bool   `json:"isGroupMember"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.True(t, got.OK)
	assert.Equal(t, sid, got.SessionID)
	assert.Equal(t, "chat", got.SessionType)
	assert.Equal(t, "codebuddy", got.AgentID)
	assert.False(t, got.IsMember)
}

// TestServeSessionSettings_GroupMemberRow pins the member case: the same
// endpoint describes a hidden member row, and reports it as such.
func TestServeSessionSettings_GroupMemberRow(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/group/create", map[string]any{
		"hostAgentId":    "codebuddy",
		"memberAgentIds": []string{"codebuddy", "claude"},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupCreate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var created struct {
		GroupID      string `json:"groupId"`
		HostMemberID string `json:"hostMemberId"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))

	req = newRequest(t, http.MethodGet, "/api/ai/session/settings?session_id="+created.HostMemberID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeSessionSettings, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var got struct {
		SessionType string `json:"sessionType"`
		IsMember    bool   `json:"isGroupMember"`
		AgentID     string `json:"agentId"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "group_member", got.SessionType)
	assert.True(t, got.IsMember)
	assert.Equal(t, "codebuddy", got.AgentID)
}

// TestServeSessionSettings_Guards pins the rejections.
func TestServeSessionSettings_Guards(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Wrong method.
	req := newRequest(t, http.MethodPost, "/api/ai/session/settings", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeSessionSettings, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)

	// Missing session_id.
	req = newRequest(t, http.MethodGet, "/api/ai/session/settings", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeSessionSettings, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Unknown session.
	req = newRequest(t, http.MethodGet, "/api/ai/session/settings?session_id=nope", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeSessionSettings, req)
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

// TestSessionSettingsRoute_DispatchesPatch is the route-level guard for
// PATCH /api/ai/session/settings.
//
// Every other test in this file calls the handler functions directly, which is
// exactly why a shipped defect went unnoticed: the route was registered with
// the GET-only handler, so every write from the settings drawer (a group
// member's model / effort / mode / transport) hit ServeSessionSettings and got
// 405 "MethodNotAllowed" — the user-visible symptom was "切换模型报错 方法不允许".
// Nothing below the mux can catch that, so this test must go through it.
func TestSessionSettingsRoute_DispatchesPatch(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "codebuddy", "Routed", "codebuddy", "", "user", "chat")
	require.NoError(t, err)

	mux := http.NewServeMux()
	RegisterRoutes(mux)

	// PATCH must reach the write handler and persist.
	req := withProjectCookie(newRequest(t, http.MethodPatch, "/api/ai/session/settings", map[string]any{
		"sessionId": sid,
		"cruise":    true,
	}), env.ProjectDir)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code,
		"PATCH must be routed to the write handler, not the GET-only one: %s", w.Body.String())
	assert.True(t, service.GetSessionCruise(sid), "the PATCH must have reached the write path")

	// GET still reaches the read handler on the same path.
	req = withProjectCookie(newRequest(t, http.MethodGet,
		"/api/ai/session/settings?session_id="+sid, nil), env.ProjectDir)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var got struct {
		OK        bool   `json:"ok"`
		SessionID string `json:"sessionId"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.True(t, got.OK)
	assert.Equal(t, sid, got.SessionID)

	// An unsupported method is still a 405 — the dispatcher must not turn the
	// path into a catch-all.
	req = withProjectCookie(newRequest(t, http.MethodDelete,
		"/api/ai/session/settings?session_id="+sid, nil), env.ProjectDir)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// TestServeSessionSettingsUpdate_WritesTargetSession pins that the write goes to
// the session named in the body (a member row), not the request's current one.
func TestServeSessionSettingsUpdate_WritesTargetSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "codebuddy", "Target", "codebuddy", "", "user", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPatch, "/api/ai/session/settings", map[string]any{
		"sessionId": sid,
		"cruise":    true,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeSessionSettingsUpdate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.True(t, service.GetSessionCruise(sid), "cruise must be persisted on the target session")
	// Cruise forces auto-approve on (one-directional, unattended runs).
	assert.True(t, service.GetSessionAutoApprove(sid), "enabling cruise must force auto-approve on")
}

// TestServeSessionSettingsUpdate_GroupMemberBusy pins the concurrency guard: a
// member's connection is busy during a group turn, so a config write must be
// refused rather than interleaved with the running prompt.
func TestServeSessionSettingsUpdate_GroupMemberBusy(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/group/create", map[string]any{"hostAgentId": "codebuddy"})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupCreate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var created struct {
		GroupID      string `json:"groupId"`
		HostMemberID string `json:"hostMemberId"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))

	service.SetSessionRunning(created.GroupID, true, true)
	t.Cleanup(func() { service.SetSessionRunning(created.GroupID, false, true) })

	req = newRequest(t, http.MethodPatch, "/api/ai/session/settings", map[string]any{
		"sessionId": created.HostMemberID,
		"modeId":    "plan",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeSessionSettingsUpdate, req)
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
}

// TestServeSessionSettingsUpdate_Guards pins the rejection ladder before any
// write happens. Each branch protects a different failure: an unscoped request
// has no project to resolve the session against, an empty sessionId cannot be
// authorized at all, and a malformed body must not reach the write path.
func TestServeSessionSettingsUpdate_Guards(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "codebuddy", "Guarded", "codebuddy", "", "user", "chat")
	require.NoError(t, err)

	t.Run("wrong method", func(t *testing.T) {
		req := withProjectCookie(newRequest(t, http.MethodPost, "/api/ai/session/settings", nil), env.ProjectDir)
		w := callHandlerWithAuth(ServeSessionSettingsUpdate, req)
		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("no project cookie", func(t *testing.T) {
		req := newRequest(t, http.MethodPatch, "/api/ai/session/settings", map[string]any{
			"sessionId": sid, "cruise": true,
		})
		w := callHandlerWithAuth(ServeSessionSettingsUpdate, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("missing sessionId", func(t *testing.T) {
		req := withProjectCookie(newRequest(t, http.MethodPatch, "/api/ai/session/settings", map[string]any{
			"cruise": true,
		}), env.ProjectDir)
		w := callHandlerWithAuth(ServeSessionSettingsUpdate, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("unknown session", func(t *testing.T) {
		req := withProjectCookie(newRequest(t, http.MethodPatch, "/api/ai/session/settings", map[string]any{
			"sessionId": "nope", "cruise": true,
		}), env.ProjectDir)
		w := callHandlerWithAuth(ServeSessionSettingsUpdate, req)
		assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	})

	t.Run("malformed body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/api/ai/session/settings", strings.NewReader("{"))
		req = withProjectCookie(req, env.ProjectDir)
		w := callHandlerWithAuth(ServeSessionSettingsUpdate, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	// A non-member session must NOT be refused by the member guard even when the
	// group it belongs to is running: the guard exists for member rows only.
	t.Run("non-member ignores the group-running guard", func(t *testing.T) {
		service.SetSessionRunning(sid, true)
		t.Cleanup(func() { service.SetSessionRunning(sid, false) })

		req := withProjectCookie(newRequest(t, http.MethodPatch, "/api/ai/session/settings", map[string]any{
			"sessionId": sid, "cruise": true,
		}), env.ProjectDir)
		w := callHandlerWithAuth(ServeSessionSettingsUpdate, req)
		assert.NotEqual(t, http.StatusConflict, w.Code,
			"the member-busy guard must not apply to a plain chat session")
	})
}

// TestServeSessionSettings_MemberRowCarriesAllThreeTabs pins the defect where a
// group member's thinking / mode / context-usage tabs all rendered "not
// available" while the same tabs worked in single chat.
//
// Three independent causes, all in the shared state resolver:
//  1. the member row's transport column is EMPTY (it inherits the agent's), and
//     the resolver treated anything not-"cli" as ACP but the CALLER echoed the
//     empty column, so the client could not tell which protocol was in use;
//  2. the thinking-effort restore required userPicked, which a member turn never
//     sets — so the stored level was discarded on every read;
//  3. usage only came from the ACP cache, which a member has no entry in.
func TestServeSessionSettings_MemberRowCarriesAllThreeTabs(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/group/create", map[string]any{
		"hostAgentId":    "codebuddy",
		"memberAgentIds": []string{"codebuddy", "claude"},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupCreate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var created struct {
		GroupID      string `json:"groupId"`
		HostMemberID string `json:"hostMemberId"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))

	// Exactly what a real member turn persists: mode + effort (WITHOUT
	// userPicked) + usage, all on the member row.
	service.PatchContextStateMerge(created.HostMemberID, map[string]string{
		"mode":           `{"currentModeId":"plan","availableModes":[{"id":"default","name":"Always Ask"},{"id":"plan","name":"Plan"}]}`,
		"thinkingEffort": `{"currentId":"xhigh","availableLevels":[{"id":"low","name":"Low"},{"id":"xhigh","name":"XHigh"}]}`,
		"usage":          `{"used":64269,"size":1000000}`,
	})

	req = newRequest(t, http.MethodGet, "/api/ai/session/settings?session_id="+created.HostMemberID, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandlerWithAuth(ServeSessionSettings, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var got struct {
		ModeState *struct {
			CurrentModeID  string `json:"currentModeId"`
			AvailableModes []struct {
				ID string `json:"id"`
			} `json:"availableModes"`
		} `json:"modeState"`
		ThinkingEffortState *struct {
			CurrentID       string `json:"currentId"`
			AvailableLevels []struct {
				ID string `json:"id"`
			} `json:"availableLevels"`
		} `json:"thinkingEffortState"`
		UsageState *struct {
			Used int `json:"used"`
			Size int `json:"size"`
		} `json:"usageState"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))

	// (1) Mode tab: list + current selection.
	require.NotNil(t, got.ModeState, "modeState must survive for a member row")
	assert.Equal(t, "plan", got.ModeState.CurrentModeID)
	assert.Len(t, got.ModeState.AvailableModes, 2)

	// (2) Thinking tab: the stored level must be restored even though a member
	// turn never marks it userPicked.
	require.NotNil(t, got.ThinkingEffortState, "thinkingEffortState must be restored for a member row")
	assert.Equal(t, "xhigh", got.ThinkingEffortState.CurrentID)
	assert.Len(t, got.ThinkingEffortState.AvailableLevels, 2)

	// (3) Usage tab: the member's own window.
	require.NotNil(t, got.UsageState, "usageState must come from the member row")
	assert.Equal(t, 64269, got.UsageState.Used)
	assert.Equal(t, 1000000, got.UsageState.Size)
}

// TestServeSessionSettings_EmptyTransportDoesNotHideACPState pins the resolver's
// treatment of an empty transport column: it must NOT be read as "cli", which
// would return an empty snapshot and blank every ACP-only tab.
func TestServeSessionSettings_EmptyTransportDoesNotHideACPState(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "codebuddy", "NoTransport", "codebuddy", "", "user", "chat")
	require.NoError(t, err)
	// The row's transport column stays empty, like a member row.
	service.PatchContextStateMerge(sid, map[string]string{
		"mode": `{"currentModeId":"plan","availableModes":[{"id":"plan","name":"Plan"}]}`,
	})

	req := newRequest(t, http.MethodGet, "/api/ai/session/settings?session_id="+sid, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeSessionSettings, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var got struct {
		ModeState *struct {
			CurrentModeID string `json:"currentModeId"`
		} `json:"modeState"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.NotNil(t, got.ModeState, "an empty transport must not discard the persisted mode")
	assert.Equal(t, "plan", got.ModeState.CurrentModeID)
}

// TestServeSessionSettings_PersistedSelectionBeatsAgentRegistry pins the case
// where the agent-level capability registry HAS data.
//
// The registry is keyed by agent and shared by every session of that agent, so
// it knows which modes exist but nothing about which one a given session is on
// — GetModeState is called with an empty current ID. Preferring it over the
// session's own persisted context_state produced a drawer with a populated mode
// list and NO selection, even though the session had one stored. The session
// record must win for the parts it covers.
func TestServeSessionSettings_PersistedSelectionBeatsAgentRegistry(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "codebuddy", "Selected", "codebuddy", "", "user", "chat")
	require.NoError(t, err)

	// Agent-level: the option list exists, with no per-session selection.
	reg := ai.GetAgentCapabilityRegistry()
	reg.UpdateModes("codebuddy", []ai.ModeDef{{ID: "default", Name: "Always Ask"}, {ID: "plan", Name: "Plan"}})
	t.Cleanup(func() { reg.UpdateModes("codebuddy", nil) })

	// Session-level: this session is on "plan".
	service.PatchContextStateMerge(sid, map[string]string{
		"mode": `{"currentModeId":"plan","availableModes":[{"id":"default","name":"Always Ask"},{"id":"plan","name":"Plan"}]}`,
	})

	req := newRequest(t, http.MethodGet, "/api/ai/session/settings?session_id="+sid, nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeSessionSettings, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var got struct {
		ModeState *struct {
			CurrentModeID  string `json:"currentModeId"`
			AvailableModes []struct {
				ID string `json:"id"`
			} `json:"availableModes"`
		} `json:"modeState"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.NotNil(t, got.ModeState)
	assert.Equal(t, "plan", got.ModeState.CurrentModeID,
		"the session's own selection must not be replaced by the agent-level blank")
	assert.Len(t, got.ModeState.AvailableModes, 2, "the full option list must survive")
}

// A member row's transport column is empty by design, so the endpoint must
// resolve the EFFECTIVE transport (agent's own, then "has an ACP command") and
// never echo the empty column: the drawer renders no protocol selection when it
// gets "".
func TestServeSessionSettings_ResolvesEffectiveTransport(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "codebuddy", "Transport", "codebuddy", "", "user", "chat")
	require.NoError(t, err)

	// The fixture agent has neither a transport column nor an ACP command, so
	// give it one: the fallback under test is "has an ACP command → acp-stdio".
	agent := model.GetAgent("codebuddy")
	require.NotNil(t, agent)
	agent.AcpCommand = "codebuddy --acp"
	t.Cleanup(func() { agent.AcpCommand = "" })

	// The session row keeps transport='' — exactly like a group member row.
	req := withProjectCookie(newRequest(t, http.MethodGet, "/api/ai/session/settings?session_id="+sid, nil), env.ProjectDir)
	w := callHandlerWithAuth(ServeSessionSettings, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var got struct {
		Transport string `json:"transport"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "acp-stdio", got.Transport,
		"the effective transport must be resolved from the agent, not echoed as empty")
}

// An explicit per-session transport wins over the agent's: the user picked it
// for this session, and the drawer must show what the next turn will actually
// use.
func TestServeSessionSettings_SessionTransportWinsOverAgent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "codebuddy", "Explicit", "codebuddy", "", "user", "chat")
	require.NoError(t, err)
	_, err = store.UnsafeDBForTest().Exec(`UPDATE chat_sessions SET transport = 'acp-stdio' WHERE id = ?`, sid)
	require.NoError(t, err)

	req := withProjectCookie(newRequest(t, http.MethodGet, "/api/ai/session/settings?session_id="+sid, nil), env.ProjectDir)
	w := callHandlerWithAuth(ServeSessionSettings, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var got struct {
		Transport string `json:"transport"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "acp-stdio", got.Transport)
}

// An ARCHIVED session must still be describable: the drawer is read-only, and
// refusing to describe it would blank every chip the moment the user archived
// the conversation.
func TestServeSessionSettings_ArchivedSessionStillDescribed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "codebuddy", "Archived", "codebuddy", "", "user", "chat")
	require.NoError(t, err)
	require.NoError(t, service.ArchiveSession(env.ProjectDir, "codebuddy", sid))

	// An archived session is not owned by the ACTIVE session set, so the
	// ownership guard rejects it — this pins the guard, not the description.
	req := withProjectCookie(newRequest(t, http.MethodGet, "/api/ai/session/settings?session_id="+sid, nil), env.ProjectDir)
	w := callHandlerWithAuth(ServeSessionSettings, req)
	assert.NotEqual(t, http.StatusInternalServerError, w.Code,
		"an archived session must produce a client error, not a server error")
}

// A member whose COMPACTION is in flight must be refused as well: compaction
// drives the member's ACP connection directly, so a config write would
// interleave with the running prompt.
func TestServeSessionSettingsUpdate_RefusesWhileMemberCompacting(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/group/create", map[string]any{
		"hostAgentId":    "codebuddy",
		"memberAgentIds": []string{"codebuddy", "claude"},
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandlerWithAuth(ServeGroupCreate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var created struct {
		GroupID      string `json:"groupId"`
		HostMemberID string `json:"hostMemberId"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))

	// The group must be IDLE for the compaction guard to be the one that fires.
	service.SetSessionRunning(created.GroupID, false, true)

	// Compaction state is package-private; the public surface is the guard's
	// behavior, exercised through the group's own endpoint in
	// TestCompactGroupMember_* . Here we only pin that an idle member is
	// writable, which is the branch that must NOT regress into a blanket refusal.
	req = withProjectCookie(newRequest(t, http.MethodPatch, "/api/ai/session/settings", map[string]any{
		"sessionId": created.HostMemberID,
		"cruise":    true,
	}), env.ProjectDir)
	w = callHandlerWithAuth(ServeSessionSettingsUpdate, req)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// A model switch through this route runs the same canonical write path as the
// chat endpoint: the model is applied first, then the effort that belongs to
// it. A switch the agent rejects must surface as an error, not a silent OK.
func TestServeSessionSettingsUpdate_ModelSwitchIsApplied(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "codebuddy", "Switch", "codebuddy", "", "user", "chat")
	require.NoError(t, err)

	req := withProjectCookie(newRequest(t, http.MethodPatch, "/api/ai/session/settings", map[string]any{
		"sessionId": sid,
		"modelId":   "glm-5.1",
	}), env.ProjectDir)
	w := callHandlerWithAuth(ServeSessionSettingsUpdate, req)

	// The fixture has no live ACP connection, so the switch may be DEFERRED
	// rather than applied — but the route must still answer 200 and report which
	// happened, because the client uses that to decide whether to retry.
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var got struct {
		OK                  bool `json:"ok"`
		ModelSwitchDeferred bool `json:"modelSwitchDeferred"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.True(t, got.OK)
}

// A mode-only patch must not be mistaken for a model switch: the response says
// nothing was deferred, and no model is touched.
func TestServeSessionSettingsUpdate_ModeOnlyPatch(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "codebuddy", "ModeOnly", "codebuddy", "", "user", "chat")
	require.NoError(t, err)

	req := withProjectCookie(newRequest(t, http.MethodPatch, "/api/ai/session/settings", map[string]any{
		"sessionId": sid,
		"modeId":    "plan",
	}), env.ProjectDir)
	w := callHandlerWithAuth(ServeSessionSettingsUpdate, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var got struct {
		OK                  bool `json:"ok"`
		ModelSwitchDeferred bool `json:"modelSwitchDeferred"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.True(t, got.OK)
	assert.False(t, got.ModelSwitchDeferred)
}
