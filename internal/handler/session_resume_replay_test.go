package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"

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
	_, err := service.WriteExec("DROP TABLE chat_history")
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

// TestServeACPLoadSession_ReplayToolCallPersistFail drops chat_tool_calls so
// the UpsertToolCall write fails, exercising the warn-and-continue branch for
// replay tool-call persistence (session_resume.go).
func TestServeACPLoadSession_ReplayToolCallPersistFail(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Keep chat_history (replay message INSERT succeeds) but drop chat_tool_calls
	// so UpsertToolCall fails.
	_, err := service.WriteExec("DROP TABLE chat_tool_calls")
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
