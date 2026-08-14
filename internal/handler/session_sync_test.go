package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupLoadSessionReplay_CapturesMessageID(t *testing.T) {
	client := ai.NewClawBenchACPClient()
	u1 := "uuid-user-1"
	u2 := "uuid-user-2"
	a1 := "uuid-assistant-1"
	client.SetLoadSessionBufForTest([]acp.SessionNotification{
		{SessionId: "s", Update: acp.SessionUpdate{UserMessageChunk: &acp.SessionUpdateUserMessageChunk{
			MessageId: &u1, Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "hi"}}}}},
		{SessionId: "s", Update: acp.SessionUpdate{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			MessageId: &a1, Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "hello"}}}}},
		{SessionId: "s", Update: acp.SessionUpdate{UserMessageChunk: &acp.SessionUpdateUserMessageChunk{
			MessageId: &u2, Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "again"}}}}},
	})

	msgs := groupLoadSessionReplay(client)
	require.Len(t, msgs, 3)
	assert.Equal(t, "user", msgs[0].role)
	assert.Equal(t, u1, msgs[0].extMsgID)
	assert.Equal(t, "assistant", msgs[1].role)
	assert.Equal(t, a1, msgs[1].extMsgID)
	assert.Equal(t, "user", msgs[2].role)
	assert.Equal(t, u2, msgs[2].extMsgID)
}

func TestServeACPSyncSession_NoAcpSession(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-sync-agent"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "ACP", Backend: "claude", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}

	// 创建会话但 external_session_id 为空、无活动连接
	sid, err := service.CreateSession(env.ProjectDir, "claude", "Test", agentID, "", "default", "chat")
	require.NoError(t, err)

	req := newRequest(t, http.MethodPost, "/api/ai/session/acp-sync", map[string]string{
		"agentId":   agentID,
		"sessionId": sid,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSyncSession(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	assert.Equal(t, "NoAcpSession", body["msgKey"])
}

func TestServeACPSyncSession_ProjectMismatch(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := "acp-sync-agent"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "ACP", Backend: "claude", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}
	sid, err := service.CreateSession(env.ProjectDir, "claude", "Test", agentID, "", "default", "chat")
	require.NoError(t, err)
	service.UpdateExternalSessionID(sid, "acp-xyz")

	req := newRequest(t, http.MethodPost, "/api/ai/session/acp-sync", map[string]string{
		"agentId":   agentID,
		"sessionId": sid,
	})
	req = withProjectCookie(req, "/other/project") // 不同项目
	w := httptest.NewRecorder()
	ServeACPSyncSession(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestServeACPSyncSession_IncrementalMerge(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := setupACPReplayAgent(t)
	sid, err := service.CreateSession(env.ProjectDir, "claude", "Test", agentID, "", "default", "chat")
	require.NoError(t, err)
	service.UpdateExternalSessionID(sid, "acp-1")
	_, err = service.WriteExec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, external_message_id) VALUES (?, 'claude', ?, 'user', 'existing', 'm1')",
		env.ProjectDir, sid,
	)
	require.NoError(t, err)

	restore := newSyncReplayConn(t, model.Agents[agentID], []acp.SessionNotification{
		{SessionId: "acp-1", Update: acp.SessionUpdate{UserMessageChunk: &acp.SessionUpdateUserMessageChunk{
			MessageId: strPtr("m1"), Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "existing"}}}}},
		{SessionId: "acp-1", Update: acp.SessionUpdate{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			MessageId: strPtr("m2"), Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "newly added"}}}}},
	})
	defer restore()

	req := newRequest(t, http.MethodPost, "/api/ai/session/acp-sync", map[string]string{
		"agentId":   agentID,
		"sessionId": sid,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSyncSession(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct{ Added int `json:"added"` }
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, 1, resp.Added)

	var cnt int
	_ = service.ReadDB().QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ?", sid).Scan(&cnt)
	assert.Equal(t, 2, cnt)
}

func strPtr(s string) *string { return &s }

// TestServeACPSyncSession_NoDuplicateOfLiveMessages reproduces the reported bug:
// a session whose earlier messages were created via LIVE chat (no external
// message_id) and whose external history then grew. Sync must NOT duplicate the
// live messages (even though their external_message_id is empty), and must add
// only the genuinely new external messages.
func TestServeACPSyncSession_NoDuplicateOfLiveMessages(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agentID := setupACPReplayAgent(t)
	sid, err := service.CreateSession(env.ProjectDir, "claude", "Test", agentID, "", "default", "chat")
	require.NoError(t, err)
	service.UpdateExternalSessionID(sid, "acp-1")

	// Live-created messages: EMPTY external_message_id.
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

	// External replay: the two live messages (user one now carries an injected
	// system-instructions prefix) + two genuinely new messages.
	restore := newSyncReplayConn(t, model.Agents[agentID], []acp.SessionNotification{
		{SessionId: "acp-1", Update: acp.SessionUpdate{UserMessageChunk: &acp.SessionUpdateUserMessageChunk{
			MessageId: strPtr("x1"), Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "[System Instructions: ...] 你叫什么名字"}}}}},
		{SessionId: "acp-1", Update: acp.SessionUpdate{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			MessageId: strPtr("y1"), Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "我叫 CodeBuddy Code，你的 AI 编程助手。"}}}}},
		{SessionId: "acp-1", Update: acp.SessionUpdate{UserMessageChunk: &acp.SessionUpdateUserMessageChunk{
			MessageId: strPtr("z1"), Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "你几岁了"}}}}},
		{SessionId: "acp-1", Update: acp.SessionUpdate{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			MessageId: strPtr("w1"), Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "我没有年龄的概念——我是一个 AI 助手。"}}}}},
	})
	defer restore()

	req := newRequest(t, http.MethodPost, "/api/ai/session/acp-sync", map[string]string{
		"agentId":   agentID,
		"sessionId": sid,
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPSyncSession(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct{ Added int `json:"added"` }
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	// Only the 2 new messages should be added (no duplication of live messages).
	assert.Equal(t, 2, resp.Added)

	// Total = 2 live + 2 new = 4 (no duplicates).
	var cnt int
	_ = service.ReadDB().QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ?", sid).Scan(&cnt)
	assert.Equal(t, 4, cnt)

	// The new messages are present.
	var hasNew int
	_ = service.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND content LIKE '%你几岁了%'", sid,
	).Scan(&hasNew)
	assert.Equal(t, 1, hasNew)
}

// newSyncReplayConn 同 newACPReplayConn，但预置 loadSessionActive=true 以跳过
// SyncLoadSession 的真实 RPC，直接使用预填充的回放缓冲。
func newSyncReplayConn(t *testing.T, agent *model.Agent, buf []acp.SessionNotification) (restore func()) {
	t.Helper()
	conn := ai.NewACPConnForTest(agent, "dummy-clawbench-sid")
	client := ai.NewClawBenchACPClient()
	client.SetLoadSessionBufForTest(buf)
	conn.SetClientForTest(client)
	conn.SetLoadSessionActiveForTest(true)

	orig := getOrCreateConnForLoad
	getOrCreateConnForLoad = func(_ context.Context, _ *model.Agent, _, _, _ string) (*ai.ACPConn, error) {
		return conn, nil
	}
	return func() { getOrCreateConnForLoad = orig }
}
