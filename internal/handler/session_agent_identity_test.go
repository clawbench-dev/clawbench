package handler

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setScopedTestAgent registers an extra agent scoped to projectID and returns a
// teardown that removes it from the in-memory registry.
//
// The scenario under test only exists for a PROJECT-SCOPED agent: a global one
// is visible in every project, so GET /api/agents would have returned it and the
// client could resolve the name itself.
func setScopedTestAgent(t *testing.T, id, name, backend, avatar string, projectID int64) {
	t.Helper()
	agent := &model.Agent{
		ID:        id,
		Name:      name,
		Backend:   backend,
		Avatar:    avatar,
		ProjectID: &projectID,
	}
	origAgents := model.Agents
	origList := model.AgentList

	model.Agents[id] = agent
	model.AgentList = append(append([]*model.Agent{}, model.AgentList...), agent)

	t.Cleanup(func() {
		delete(model.Agents, id)
		model.Agents = origAgents
		model.AgentList = origList
	})
}

// TestServeSessionsOverview_carriesAgentIdentityForScopedAgents covers the
// cross-project pane's degradation: a session in ANOTHER project may run an
// agent scoped to that project, which GET /api/agents (scoped to the caller's
// project) never returns. Without the identity on the row the client fell back
// to the raw agent id and an initial-letter icon.
func TestServeSessionsOverview_carriesAgentIdentityForScopedAgents(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// The session lives in project B and runs an agent scoped to project B.
	projectB := canonPath(filepath.Join(env.WatchDir, "project-b"))
	projectBID := store.ProjectIDForTest(t, projectB)
	setScopedTestAgent(t, "scoped-b", "视觉制作", "codebuddy", "<svg>avatar-b</svg>", projectBID)

	sessionID, err := service.CreateSession(projectB, "codebuddy", "B scoped", "scoped-b", "", "default", "chat")
	require.NoError(t, err)
	service.SetSessionRunning(sessionID, true)
	t.Cleanup(func() { service.SetSessionRunning(sessionID, false) })

	// The caller is in project A: the scoped agent is invisible to it, which is
	// exactly the state the client's own lookup is in.
	req := newRequest(t, http.MethodGet, "/api/ai/sessions/overview", nil)
	req = withAuthCookie(req, model.SessionToken)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionsOverview, req)
	assertOK(t, w)

	var result struct {
		Projects []struct {
			Name     string `json:"name"`
			Sessions []struct {
				ID          string `json:"id"`
				AgentID     string `json:"agentId"`
				AgentName   string `json:"agentName"`
				AgentAvatar string `json:"agentAvatar"`
			} `json:"sessions"`
		} `json:"projects"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))

	var found bool
	for _, p := range result.Projects {
		for _, s := range p.Sessions {
			if s.ID != sessionID {
				continue
			}
			found = true
			assert.Equal(t, "scoped-b", s.AgentID)
			assert.Equal(t, "视觉制作", s.AgentName,
				"the name must ride on the row: the caller's /api/agents does not contain this agent")
			assert.Equal(t, "<svg>avatar-b</svg>", s.AgentAvatar,
				"the avatar is only shipped when the caller cannot resolve the agent itself")
		}
	}
	assert.True(t, found, "the running session must appear in the overview")
}

// TestServeSessionsOverview_omitsAvatarForVisibleAgents pins the payload rule:
// an agent the caller CAN resolve (a global one) ships its name but NOT its
// avatar — the avatar is a multi-KB SVG and the client already has it.
func TestServeSessionsOverview_omitsAvatarForVisibleAgents(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// "claude" is a global test agent; give it an avatar to make the assertion
	// meaningful (empty stays empty for the wrong reason otherwise).
	origAvatar := model.Agents["claude"].Avatar
	model.Agents["claude"].Avatar = "<svg>global-avatar</svg>"
	t.Cleanup(func() { model.Agents["claude"].Avatar = origAvatar })

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "A global", "claude", "", "default", "chat")
	require.NoError(t, err)
	service.SetSessionRunning(sessionID, true)
	t.Cleanup(func() { service.SetSessionRunning(sessionID, false) })

	req := newRequest(t, http.MethodGet, "/api/ai/sessions/overview", nil)
	req = withAuthCookie(req, model.SessionToken)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessionsOverview, req)
	assertOK(t, w)

	var result struct {
		Projects []struct {
			Sessions []struct {
				ID          string `json:"id"`
				AgentName   string `json:"agentName"`
				AgentAvatar string `json:"agentAvatar"`
			} `json:"sessions"`
		} `json:"projects"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))

	var found bool
	for _, p := range result.Projects {
		for _, s := range p.Sessions {
			if s.ID != sessionID {
				continue
			}
			found = true
			assert.Equal(t, "Claude", s.AgentName)
			assert.Empty(t, s.AgentAvatar,
				"a resolvable agent must not ship its avatar — the client already has it")
		}
	}
	assert.True(t, found)
}

// TestServeSessions_carriesAgentIdentity verifies the same identity rides on
// the project-scoped session list. Its agents are normally resolvable (the list
// is filtered to the caller's project), but a DELETED agent's sessions still
// exist — the row keeps a human name instead of falling back to the id.
func TestServeSessions_carriesAgentIdentity(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "A1", "claude", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/sessions", nil)
	req = withAuthCookie(req, model.SessionToken)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result struct {
		Sessions []struct {
			ID          string `json:"id"`
			AgentName   string `json:"agentName"`
			AgentAvatar string `json:"agentAvatar"`
		} `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))

	var found bool
	for _, s := range result.Sessions {
		if s.ID != sessionID {
			continue
		}
		found = true
		assert.Equal(t, "Claude", s.AgentName)
	}
	assert.True(t, found, "the created session must be listed")
}

// TestServeSessions_overviewUnknownAgentLeavesFieldsEmpty pins the fallback: an
// id the registry no longer knows (deleted agent) must not invent a name, so
// the client's own "unknown agent" handling still applies.
func TestServeSessions_overviewUnknownAgentLeavesFieldsEmpty(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	_, err := store.UnsafeDBForTest().Exec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, agent_source, model, session_type, archived)
		 VALUES (?, ?, 'claude', 'ghost', 'deleted-agent', 'default', '', 'chat', 0)`,
		"ghost-session", store.ProjectIDForTest(t, env.ProjectDir),
	)
	require.NoError(t, err)
	_, err = store.UnsafeDBForTest().Exec(
		`INSERT INTO chat_history (project_id, role, content, session_id, backend, streaming)
		 VALUES (?, 'assistant', 'unread', 'ghost-session', 'claude', 0)`,
		store.ProjectIDForTest(t, env.ProjectDir),
	)
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/ai/sessions", nil)
	req = withAuthCookie(req, model.SessionToken)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessions, req)
	assertOK(t, w)

	var result struct {
		Sessions []struct {
			ID          string `json:"id"`
			AgentName   string `json:"agentName"`
			AgentAvatar string `json:"agentAvatar"`
		} `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))

	for _, s := range result.Sessions {
		if s.ID != "ghost-session" {
			continue
		}
		assert.Empty(t, s.AgentName, "an unknown agent id must not invent a display name")
		assert.Empty(t, s.AgentAvatar)
	}
}
