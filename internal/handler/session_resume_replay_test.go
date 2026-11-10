package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"clawbench/internal/store"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/ws"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newACPReplayConn builds an ACPConn whose client carries a load-session replay
// buffer containing the given notifications, plus a getOrCreateConnForLoad
// override that returns it.
func newACPReplayConn(t *testing.T, agent *model.Agent, buf []acp.SessionNotification) func() {
	t.Helper()
	conn := ai.NewACPConnForTest(agent, "dummy-clawbench-sid")
	client := ai.NewClawBenchACPClient()
	client.SetLoadSessionBufForTest(buf)
	conn.SetClientForTest(client)

	orig := getOrCreateConnForLoad
	getOrCreateConnForLoad = func(_ context.Context, _ *model.Agent, _, _, _ string) (*ai.ACPConn, error) {
		return conn, nil
	}
	return func() { getOrCreateConnForLoad = orig }
}

// setupACPReplayAgent registers an ACP-capable agent that supports LoadSession
// (claude backend spec has ACPLoadSession=true).
func setupACPReplayAgent(t *testing.T) string {
	t.Helper()
	agentID := "acp-replay-agent"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "ACP", Backend: "claude", Transport: "acp-stdio", AcpCommand: "echo"},
	}
	model.AgentList = []*model.Agent{model.Agents[agentID]}
	return agentID
}

// TestServeACPLoadSession_ReplayWriteFail drops chat_history so the replay
// INSERT fails, exercising the continue-on-write-error branch in the replay
// goroutine (session_resume.go).
func TestServeACPLoadSession_ReplayWriteFail(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Force chat_history INSERTs to fail so the goroutine hits the continue branch.
	_, err := store.WriteExec("DROP TABLE chat_history")
	require.NoError(t, err)

	agentID := setupACPReplayAgent(t)
	restore := newACPReplayConn(t, model.Agents[agentID], []acp.SessionNotification{
		{
			SessionId: "acp-1",
			Update: acp.SessionUpdate{
				AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
					Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "hello replay"}},
				},
			},
		},
	})
	defer restore()

	req := newRequest(t, http.MethodPost, "/api/ai/session/acp-load", map[string]string{
		"agentId":      agentID,
		"acpSessionId": "acp-1",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPLoadSession(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// The replay goroutine sleeps ~500ms before persisting; give it time to run.
	time.Sleep(1200 * time.Millisecond)
}

// TestServeACPLoadSession_BroadcastsDerivedTitle covers the "restored session
// has no name" symptom: the title is derived in the async replay tail, i.e.
// AFTER the frontend has already listed the new session (the HTTP response
// returns first, and the row renders the blank title the session was created
// with). Nothing else re-reads the session list when a title lands, so the
// handler must broadcast it exactly like the automatic rename does — otherwise
// the restored session keeps showing no name until an unrelated reload.
func TestServeACPLoadSession_BroadcastsDerivedTitle(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	origMgr := ws.GetManager()
	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	defer ws.SetManagerForTest(origMgr)

	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "title-broadcast-client", "")
	require.NotNil(t, sub)

	agentID := setupACPReplayAgent(t)
	restore := newACPReplayConn(t, model.Agents[agentID], []acp.SessionNotification{
		{
			SessionId: "acp-title",
			Update: acp.SessionUpdate{
				UserMessageChunk: &acp.SessionUpdateUserMessageChunk{
					Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "帮我排查登录失败"}},
				},
			},
		},
	})
	defer restore()

	req := newRequest(t, http.MethodPost, "/api/ai/session/acp-load", map[string]string{
		"agentId":      agentID,
		"acpSessionId": "acp-title",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPLoadSession(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// The replay goroutine sleeps ~500ms before deriving + persisting the title.
	assert.Eventually(t, func() bool {
		for _, msg := range sub.GetBufferedEvents() {
			if msg.Event != "session_title_update" {
				continue
			}
			b, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			if strings.Contains(string(b), "帮我排查登录失败") {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond,
		"expected a session_title_update carrying the title derived from the replay")
}

// TestServeACPLoadSession_ReplayToolCallPersistFail drops chat_tool_calls so
// the UpsertToolCall write fails, exercising the warn-and-continue branch for
// replay tool-call persistence (session_resume.go).
func TestServeACPLoadSession_ReplayToolCallPersistFail(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Keep chat_history (replay message INSERT succeeds) but drop chat_tool_calls
	// so UpsertToolCall fails.
	_, err := store.WriteExec("DROP TABLE chat_tool_calls")
	require.NoError(t, err)

	agentID := setupACPReplayAgent(t)
	restore := newACPReplayConn(t, model.Agents[agentID], []acp.SessionNotification{
		{
			SessionId: "acp-1",
			Update: acp.SessionUpdate{
				ToolCall: &acp.SessionUpdateToolCall{
					ToolCallId: "tc-replay-1",
					Title:      "Read",
					Kind:       acp.ToolKindRead,
					RawInput:   map[string]any{"file_path": "/tmp/x.go"},
				},
			},
		},
	})
	defer restore()

	req := newRequest(t, http.MethodPost, "/api/ai/session/acp-load", map[string]string{
		"agentId":      agentID,
		"acpSessionId": "acp-1",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := httptest.NewRecorder()
	ServeACPLoadSession(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// The replay goroutine sleeps ~500ms before persisting; give it time to run.
	time.Sleep(1200 * time.Millisecond)
}
