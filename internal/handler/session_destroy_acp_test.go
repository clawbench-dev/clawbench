package handler

import (
	"net/http"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// session.delete_acp_on_destroy (default true) decides whether a permanent
// delete also asks the agent to drop its own transcript. These tests observe
// the teardown seam, so they prove the handler reads the setting and routes to
// session/delete instead of a plain close — no live agent process involved.

func destroySessionWithTeardownFlag(t *testing.T, deleteNative bool) bool {
	t.Helper()

	env, teardown := setupTestEnv(t)
	defer teardown()

	origAgents := model.Agents
	t.Cleanup(func() { model.Agents = origAgents })
	model.Agents = map[string]*model.Agent{
		"acp-test-agent": {
			ID:         "acp-test-agent",
			Backend:    "claude",
			Transport:  "cli",
			AcpCommand: "claude --acp",
		},
	}

	origCfg := model.ConfigInstance
	t.Cleanup(func() { model.ConfigInstance = origCfg })
	model.ConfigInstance.Session.DeleteACPOnDestroy = deleteNative

	origTeardown := acpTeardown
	t.Cleanup(func() { acpTeardown = origTeardown })
	seen := make(chan bool, 4)
	acpTeardown = func(_ string, wantDelete bool) { seen <- wantDelete }

	sessionID, err := service.CreateSession(env.ProjectDir, "claude", "destroy-me", "acp-test-agent", "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodDelete, "/api/ai/session/destroy?session_id="+sessionID, nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(DestroySession, req)
	assertOK(t, w)

	select {
	case got := <-seen:
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("destroy did not tear down the agent connection")
		return false
	}
}

func TestDestroySession_DeletesAgentSessionWhenSettingOn(t *testing.T) {
	assert.True(t, destroySessionWithTeardownFlag(t, true),
		"with the setting on, destroy must route to ACP session/delete")
}

func TestDestroySession_OnlyClosesAgentConnWhenSettingOff(t *testing.T) {
	assert.False(t, destroySessionWithTeardownFlag(t, false),
		"with the setting off, destroy must only close the connection")
}

// The default matters more than either branch: an install that never touches
// the setting must get the delete behavior.
func TestApplyDefaults_DeleteACPOnDestroyDefaultsTrue(t *testing.T) {
	cfg := model.Config{}
	model.ApplyDefaults(&cfg, map[string]bool{})
	assert.True(t, cfg.Session.DeleteACPOnDestroy, "omitted key must default to true")

	// An explicit `false` in config.yaml arrives as a PRESENT key with the zero
	// value. ApplyDefaults must leave it alone rather than re-defaulting it.
	cfg = model.Config{}
	model.ApplyDefaults(&cfg, map[string]bool{"session.delete_acp_on_destroy": true})
	assert.False(t, cfg.Session.DeleteACPOnDestroy, "an explicit false must survive")
}
