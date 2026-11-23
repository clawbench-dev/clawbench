package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"

	"clawbench/internal/store"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"

	"clawbench/internal/ai/acptest"
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
			MessageId: acptest.MsgID(u1), Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "hi"}},
		}}},
		{SessionId: "s", Update: acp.SessionUpdate{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			MessageId: acptest.MsgID(a1), Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "hello"}},
		}}},
		{SessionId: "s", Update: acp.SessionUpdate{UserMessageChunk: &acp.SessionUpdateUserMessageChunk{
			MessageId: acptest.MsgID(u2), Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "again"}},
		}}},
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

func TestGroupLoadSessionReplay_UserChunkNestedJSONUnwrapped(t *testing.T) {
	// A user chunk whose text field is itself an ACP notification JSON (as seen
	// in dirty data from early sync versions) must be unwrapped to the real
	// user text — never stored as a literal JSON string.
	client := ai.NewClawBenchACPClient()
	nested := `{"content":{"text":"这是真实的用户消息","type":"text"},"messageId":"m1","sessionUpdate":"user_message_chunk"}`
	client.SetLoadSessionBufForTest([]acp.SessionNotification{
		{SessionId: "s", Update: acp.SessionUpdate{UserMessageChunk: &acp.SessionUpdateUserMessageChunk{
			MessageId: acptest.MsgID("m1"), Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: nested}},
		}}},
	})

	msgs := groupLoadSessionReplay(client)
	require.Len(t, msgs, 1)
	assert.Equal(t, "user", msgs[0].role)
	assert.NotContains(t, msgs[0].content, "user_message_chunk", "stored content must not contain the raw ACP notification")
	// The final stored blocks must be exactly one text block with the unwrapped
	// text — no empty blocks, no JSON leakage.
	var parsed struct {
		Blocks []model.ContentBlock `json:"blocks"`
	}
	require.NoError(t, json.Unmarshal([]byte(msgs[0].content), &parsed))
	require.Len(t, parsed.Blocks, 1)
	assert.Equal(t, "text", parsed.Blocks[0].Type)
	assert.Equal(t, "这是真实的用户消息", parsed.Blocks[0].Text)
}

func TestGroupLoadSessionReplay_UserChunkContentArrayUnwrapped(t *testing.T) {
	// A user chunk whose text field is a serialized content array.
	client := ai.NewClawBenchACPClient()
	nested := `[{"type":"text","text":"数组里的用户消息"}]`
	client.SetLoadSessionBufForTest([]acp.SessionNotification{
		{SessionId: "s", Update: acp.SessionUpdate{UserMessageChunk: &acp.SessionUpdateUserMessageChunk{
			MessageId: acptest.MsgID("m2"), Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: nested}},
		}}},
	})

	msgs := groupLoadSessionReplay(client)
	require.Len(t, msgs, 1)
	var parsed struct {
		Blocks []model.ContentBlock `json:"blocks"`
	}
	require.NoError(t, json.Unmarshal([]byte(msgs[0].content), &parsed))
	require.Len(t, parsed.Blocks, 1)
	assert.Equal(t, "text", parsed.Blocks[0].Type)
	assert.Equal(t, "数组里的用户消息", parsed.Blocks[0].Text)
}

func TestGroupLoadSessionReplay_EmptyUserChunkDoesNotCreateEmptyBlock(t *testing.T) {
	// A user chunk with empty text (e.g. an image-only message) must not create
	// an empty text block nor leak JSON; the message may be skipped entirely
	// rather than produce a junk block.
	client := ai.NewClawBenchACPClient()
	client.SetLoadSessionBufForTest([]acp.SessionNotification{
		{SessionId: "s", Update: acp.SessionUpdate{UserMessageChunk: &acp.SessionUpdateUserMessageChunk{
			MessageId: acptest.MsgID("m-empty"), Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: ""}},
		}}},
		{SessionId: "s", Update: acp.SessionUpdate{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			MessageId: acptest.MsgID("a1"), Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "assistant reply"}},
		}}},
	})

	msgs := groupLoadSessionReplay(client)
	// The empty user chunk produces no block; the assistant reply still lands
	// as its own message (role grouping not corrupted by the empty chunk).
	require.Len(t, msgs, 1)
	assert.Equal(t, "assistant", msgs[0].role)
	assert.Contains(t, msgs[0].content, "assistant reply")
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
	_, err = store.WriteExec(
		"INSERT INTO chat_history (project_id, backend, session_id, role, content, external_message_id) VALUES (?, 'claude', ?, 'user', 'existing', 'm1')",
		env.ProjectDir, sid,
	)
	require.NoError(t, err)

	restore := newSyncReplayConn(t, model.Agents[agentID], []acp.SessionNotification{
		{SessionId: "acp-1", Update: acp.SessionUpdate{UserMessageChunk: &acp.SessionUpdateUserMessageChunk{
			MessageId: acptest.MsgID("m1"), Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "existing"}},
		}}},
		{SessionId: "acp-1", Update: acp.SessionUpdate{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			MessageId: acptest.MsgID("m2"), Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "newly added"}},
		}}},
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
	var resp struct {
		Added int `json:"added"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, 1, resp.Added)

	var cnt int
	_ = store.ReadDB().QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ?", sid).Scan(&cnt)
	assert.Equal(t, 2, cnt)
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

func TestHistorySyncWaitsForActiveTurn(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()
	synctest.Test(t, func(t *testing.T) {
		const sid = "sync-waits"
		run, ok := service.TryClaimSessionRun(sid)
		require.True(t, ok)
		acquired := make(chan func(), 1)
		go func() {
			_, release, err := claimHistorySync(t.Context(), sid)
			if err == nil {
				acquired <- release
			}
		}()
		synctest.Wait()
		require.Empty(t, acquired)
		require.NoError(t, run.Err())
		service.FinishSessionRun(sid)
		release := <-acquired
		require.True(t, service.IsSessionRunning(sid))
		release()
		require.False(t, service.IsSessionRunning(sid))
	})
}
