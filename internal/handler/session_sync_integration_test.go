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

type mockMsg struct {
	Role string `json:"role"` // "user" or "assistant"
	Text string `json:"text"`
}

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
// ACP process. It also points the mock's data dir at a temp dir so each test can
// pre-populate the external session's history.
func newMockSyncAgent(t *testing.T) (agentID, dataDir string) {
	t.Helper()
	mockPath := locateACPMock(t)
	dataDir = t.TempDir()
	os.Setenv("ACP_MOCK_DATA_DIR", dataDir)
	t.Cleanup(func() { os.Unsetenv("ACP_MOCK_DATA_DIR") })

	agentID = "acp-mock-sync-agent"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "ACP Mock", Backend: "claude", Transport: "acp-stdio", AcpCommand: mockPath},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}
	return agentID, dataDir
}

// writeMockHistory pre-populates the external ACP session's persisted history
// (as if the conversation happened on the agent side, possibly through a
// separate CLI client), so LoadSession will replay it.
func writeMockHistory(t *testing.T, dataDir, acpSessionID string, msgs []mockMsg) {
	t.Helper()
	raw, err := json.Marshal(msgs)
	require.NoError(t, err)
	path := filepath.Join(dataDir, acpSessionID+".json")
	require.NoError(t, os.WriteFile(path, raw, 0o644))
}

// syncViaRealAgent runs ServeACPSyncSession with the REAL getOrCreateConnForLoad
// (spawning acp-mock) and returns the number of added messages.
func syncViaRealAgent(t *testing.T, agentID, sid string) int {
	t.Helper()
	req := newRequest(t, http.MethodPost, "/api/ai/session/acp-sync", map[string]string{
		"agentId":   agentID,
		"sessionId": sid,
	})
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

func countContent(t *testing.T, sid, substr string) int {
	t.Helper()
	var n int
	err := service.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND content LIKE ?", sid, "%"+substr+"%",
	).Scan(&n)
	require.NoError(t, err)
	return n
}

// TestACPSync_RealAgent_TwoTurnExternalHistory reproduces the real-world usage:
//  1. A ClawBench session chats one turn ("你叫什么名字" → reply).
//  2. The SAME ACP session is then resumed externally (e.g. a CLI client) and
//     chats a second turn ("你几岁了" → reply), so the external session now has
//     2 turns / 4 messages while the local ClawBench session only has turn 1.
//  3. ACP sync is triggered.
//  4. The final session must have 2 rounds = 4 messages: turn 1 unchanged (not
//     duplicated) plus turn 2 synced in from the external chat records.
func TestACPSync_RealAgent_TwoTurnExternalHistory(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID, dataDir := newMockSyncAgent(t)
	extSID := "mock-sess-two-turn"

	// External ACP session state: 2 turns = 4 messages.
	writeMockHistory(t, dataDir, extSID, []mockMsg{
		{Role: "user", Text: "你叫什么名字"},
		{Role: "assistant", Text: "我叫 CodeBuddy Code，你的 AI 编程助手。"},
		{Role: "user", Text: "你几岁了"},
		{Role: "assistant", Text: "我没有年龄的概念——我是一个 AI 助手。"},
	})

	// Current ClawBench session only contains turn 1 (live chat, empty
	// external_message_id).
	sid, err := service.CreateSession(env.ProjectDir, "claude", "Test", agentID, "", "default", "chat")
	require.NoError(t, err)
	service.UpdateExternalSessionID(sid, extSID)
	_, err = service.WriteExec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, external_message_id) VALUES (?, 'claude', ?, 'user', '你叫什么名字', '')",
		env.ProjectDir, sid,
	)
	require.NoError(t, err)
	_, err = service.WriteExec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, external_message_id) VALUES (?, 'claude', ?, 'assistant', '我叫 CodeBuddy Code，你的 AI 编程助手。', '')",
		env.ProjectDir, sid,
	)
	require.NoError(t, err)

	// Sync: only turn 2 (the 2 external messages the local session lacks) should
	// be added; turn 1 must NOT be duplicated.
	added := syncViaRealAgent(t, agentID, sid)
	assert.Equal(t, 2, added, "should add only the 2 external turn-2 messages")

	// Final session = 2 turns = 4 messages.
	var total int
	_ = service.ReadDB().QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ?", sid).Scan(&total)
	assert.Equal(t, 4, total, "final session should have 2 rounds (4 messages)")

	// Turn 1 appears exactly once (not duplicated); turn 2 present exactly once.
	assert.Equal(t, 1, countContent(t, sid, "你叫什么名字"), "turn-1 user message must not be duplicated")
	assert.Equal(t, 1, countContent(t, sid, "我叫 CodeBuddy Code"), "turn-1 assistant message must not be duplicated")
	assert.Equal(t, 1, countContent(t, sid, "你几岁了"), "turn-2 user message must be synced")
	assert.Equal(t, 1, countContent(t, sid, "我没有年龄"), "turn-2 assistant message must be synced")

	ai.GetACPConnManager().CloseConn(sid)
}
