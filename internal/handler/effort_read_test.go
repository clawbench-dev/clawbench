package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/service"
)

// TestAIChat_GET_AgentReportedEffortNotPresentedAsCurrent pins the read path of
// issue #552.
//
// context_state holds BOTH the user's explicit pick and the value the agent
// reports for itself. Only the former may be returned as the session's current
// effort; presenting the agent's per-model default ("low" for most OpenCode
// models) as the current selection is what made every new session look stuck on
// low, and the client then had no reason to fall back to the configured default.
func TestAIChat_GET_AgentReportedEffortNotPresentedAsCurrent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "opencode", "Effort read", "opencode", "glm-5.3", "user", "chat")
	require.NoError(t, err)

	// The agent reported "low" for itself — NOT a user pick.
	service.PatchContextStateMerge(sid, map[string]string{
		"thinkingEffort": `{"currentId":"low"}`,
	})

	req := newRequest(t, http.MethodGet, "/api/ai/chat?session_id="+sid+"&limit=1", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChat, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	state, _ := body["thinkingEffortState"].(map[string]any)
	assert.Nil(t, state,
		"an agent-reported default must not be returned as the session's current effort")
}

// TestAIChat_GET_UserPickedEffortIsReturned is the positive control: a real
// user pick must still be restored, or reloading the page would lose it.
func TestAIChat_GET_UserPickedEffortIsReturned(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	sid, err := service.CreateSession(env.ProjectDir, "opencode", "Effort read", "opencode", "glm-5.3", "user", "chat")
	require.NoError(t, err)

	service.PersistThinkingEffortPick(sid, "max", "glm-5.3")

	req := newRequest(t, http.MethodGet, "/api/ai/chat?session_id="+sid+"&limit=1", nil)
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(AIChat, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	state, ok := body["thinkingEffortState"].(map[string]any)
	require.True(t, ok, "a user pick must be restored")
	assert.Equal(t, "max", state["currentId"])
	assert.Equal(t, true, state["userPicked"],
		"the client needs the flag to tell a user pick from the agent's default")
}
