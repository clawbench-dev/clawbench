//go:build integration

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The acp-mock agent replays a FIXED two-message history on every LoadSession:
//   user:      "Hello, this is a replayed user message from the loaded session."
//   assistant: "Hello! This is a replayed assistant response from the loaded session."
const (
	mockReplayUserText      = "Hello, this is a replayed user message from the loaded session."
	mockReplayAssistantText = "Hello! This is a replayed assistant response from the loaded session."
)

// locateACPMock returns the path to a built acp-mock binary, building it into a
// temp dir if the pre-built one is absent. Skips the test if it cannot be built.
func locateACPMock(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	root := filepath.Dir(filepath.Dir(cwd)) // internal/handler -> repo root
	prebuilt := filepath.Join(root, "acp-mock")
	if _, err := os.Stat(prebuilt); err == nil {
		return prebuilt
	}
	tmp := filepath.Join(t.TempDir(), "acp-mock")
	cmd := exec.Command("go", "build", "-o", tmp, "./cmd/acp-mock")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("acp-mock not available and build failed: %v\n%s", err, out)
	}
	return tmp
}

// newMockSyncAgent registers a claude-backend agent (ACPLoadSession=true) whose
// ACP command is the acp-mock binary, so ServeACPSyncSession spawns a REAL
// ACP process whose LoadSession replays the fixed two-message history.
func newMockSyncAgent(t *testing.T) string {
	t.Helper()
	mockPath := locateACPMock(t)
	agentID := "acp-mock-sync-agent"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "ACP Mock", Backend: "claude", Transport: "acp-stdio", AcpCommand: mockPath},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}
	return agentID
}

func syncViaRealAgent(t *testing.T, agentID, sid string) int {
	t.Helper()
	req := newRequest(t, http.MethodPost, "/api/ai/session/acp-sync", map[string]string{
		"agentId":   agentID,
		"sessionId": sid,
	})
	// ServeACPSyncSession derives the project from the cookie; set it to a path
	// that equals the session's project_path.
	req = withProjectCookie(req, sessionProjectPath(t, sid))
	w := httptest.NewRecorder()
	ServeACPSyncSession(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct{ Added int `json:"added"` }
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return resp.Added
}

func sessionProjectPath(t *testing.T, sid string) string {
	t.Helper()
	var p string
	err := service.ReadDB().QueryRow("SELECT project_path FROM chat_sessions WHERE id = ?", sid).Scan(&p)
	require.NoError(t, err)
	return p
}

// TestACPSync_RealAgent_EndToEnd exercises the FULL sync path against a real
// acp-mock process: real spawn, real LoadSession replay, real buffer capture
// with condition-based waiting, and the incremental dedup. It verifies the two
// bugs fixed:
//  1. Syncing into an empty session adds the replayed history (new messages ARE
//     captured — the fixed 500ms wait no longer truncates the replay).
//  2. Syncing when the local session already contains the replayed messages
//     does NOT duplicate them (live messages without external_message_id are
//     deduped by content continuation).
func TestACPSync_RealAgent_EndToEnd(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := newMockSyncAgent(t)

	// Scenario A: empty local session -> sync pulls the full 2-message replay.
	{
		sid, err := service.CreateSession(env.ProjectDir, "claude", "Test", agentID, "", "default", "chat")
		require.NoError(t, err)
		service.UpdateExternalSessionID(sid, "mock-sess-a")

		added := syncViaRealAgent(t, agentID, sid)
		assert.Equal(t, 2, added, "empty session should pull both replayed messages")

		var userCnt, asstCnt int
		_ = service.ReadDB().QueryRow(
			"SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND role = 'user' AND content LIKE ?",
			sid, "%"+mockReplayUserText+"%",
		).Scan(&userCnt)
		_ = service.ReadDB().QueryRow(
			"SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND role = 'assistant' AND content LIKE ?",
			sid, "%"+mockReplayAssistantText+"%",
		).Scan(&asstCnt)
		assert.Equal(t, 1, userCnt, "replayed user message should be present exactly once")
		assert.Equal(t, 1, asstCnt, "replayed assistant message should be present exactly once")

		ai.GetACPConnManager().CloseConn(sid)
	}

	// Scenario B: local already contains the exact replayed messages (created as
	// if by live chat, external_message_id empty) -> sync must NOT duplicate them.
	{
		sid, err := service.CreateSession(env.ProjectDir, "claude", "Test", agentID, "", "default", "chat")
		require.NoError(t, err)
		service.UpdateExternalSessionID(sid, "mock-sess-b")
		_, err = service.WriteExec(
			"INSERT INTO chat_history (project_path, backend, session_id, role, content, external_message_id) VALUES (?, 'claude', ?, 'user', ?, '')",
			env.ProjectDir, sid, mockReplayUserText,
		)
		require.NoError(t, err)
		_, err = service.WriteExec(
			"INSERT INTO chat_history (project_path, backend, session_id, role, content, external_message_id) VALUES (?, 'claude', ?, 'assistant', ?, '')",
			env.ProjectDir, sid, mockReplayAssistantText,
		)
		require.NoError(t, err)

		added := syncViaRealAgent(t, agentID, sid)
		assert.Equal(t, 0, added, "already-present replayed messages must not be duplicated")

		var cnt int
		_ = service.ReadDB().QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ?", sid).Scan(&cnt)
		assert.Equal(t, 2, cnt, "session should still have exactly the 2 original messages")

		ai.GetACPConnManager().CloseConn(sid)
	}
}
