package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ai/acptest"
)

func TestAsyncTasksAcrossPromptBoundary(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := NewClawBenchACPClient()
		conn := &ACPConn{}
		client.connRef = conn
		client.expectTaskFollowup = true
		events := make(chan StreamEvent, 16)
		w := client.RegisterSession("s", events)
		update := func(sid, id, variant, kind, state string) {
			frame := map[string]any{"method": "session/update", "params": map[string]any{"sessionId": sid, "update": map[string]any{"sessionUpdate": variant, "asyncTaskId": id, "taskType": kind, "state": state}}}
			line, err := json.Marshal(frame)
			require.NoError(t, err)
			var decoded struct{ Params acp.SessionNotification }
			require.NoError(t, json.Unmarshal(normalizeAsyncTaskUpdate(line), &decoded))
			require.NoError(t, client.SessionUpdate(t.Context(), decoded.Params))
		}
		spawn := func(id, kind string) { update("s", id, "async_task_spawned", kind, "") }
		end := func(id string) { update("s", id, "async_task_state_update", "", "completed") }
		spawn("one", "shell")
		spawn("one", "shell")
		spawn("two", "shell")
		done := make(chan error, 1)
		go func() { done <- w.wait(t.Context(), nil) }()
		synctest.Wait()
		time.Sleep(2 * defaultACPStallTimeout)
		require.False(t, conn.isStalled(defaultACPStallTimeout))
		end("one")
		spawn("first-review", "turn")
		end("first-review")
		synctest.Wait()
		require.Empty(t, done)
		end("two")
		synctest.Wait()
		require.Empty(t, done, "last task has ended, but the review hasn't started")
		spawn("review", "turn")
		update("foreign", "review", "async_task_state_update", "", "completed")
		synctest.Wait()
		require.Empty(t, done)
		require.NoError(t, client.SessionUpdate(t.Context(), acp.SessionNotification{SessionId: "s", Update: agentTextUpdate("final answer")}))
		end("review")
		require.NoError(t, <-done)
		var tools []string
		var content string
		for len(events) > 0 {
			e := <-events
			if e.Tool != nil {
				tools = append(tools, e.Type+" "+e.Tool.ID)
			}
			if e.Type == "content" {
				content += e.Content
			}
		}
		require.Equal(t, []string{"tool_use async-task:one", "tool_use async-task:two", "tool_result async-task:one", "tool_result async-task:two"}, tools)
		require.Equal(t, "final answer", content)
		client.UnregisterSession("s")
	})
}

func TestBackgroundWorkStopsWithoutRetry(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cancel bool
	}{{"cancel", true}, {"disconnect", false}} {
		t.Run(tc.name, func(t *testing.T) {
			w := newPromptBackgroundWork()
			w.observe("shell", "shell", "started", "", "", make(chan StreamEvent, 1), nil)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			disconnected := make(chan struct{})
			if tc.cancel {
				cancel()
			} else {
				close(disconnected)
			}
			err := w.wait(ctx, disconnected)
			require.Error(t, err)
			require.False(t, isACPPeerDisconnected(err))
		})
	}
}

func TestBackgroundTaskReplayDoesNotStartWork(t *testing.T) {
	c := NewClawBenchACPClient()
	c.connRef = &ACPConn{}
	w := c.RegisterSession("s", make(chan StreamEvent, 1))
	c.connRef.loadSessionActive.Store(true)
	u := acp.SessionUpdate{SessionInfoUpdate: &acp.SessionSessionInfoUpdate{
		Meta: acptest.PatchMeta(map[string]any{backgroundEventKey: map[string]any{"id": "old", "state": "started", "kind": "shell"}}),
	}}
	require.NoError(t, c.SessionUpdate(t.Context(), acp.SessionNotification{SessionId: "s", Update: u}))
	require.NoError(t, w.wait(t.Context(), nil))
	require.False(t, c.connRef.backgroundTasksRunning.Load())
}

func TestIdleSweepRechecksRetainedRoute(t *testing.T) {
	const sid = "retained"
	conn := &ACPConn{alive: true, lastUsed: time.Now().Add(-2 * idleConnTimeout)}
	conn.client = newConnClient(conn)
	m := &ACPConnManager{conns: map[string]*ACPConn{sid: conn}}
	candidates, _ := m.collectIdleConns()
	require.Len(t, candidates, 1)
	conn.client.RegisterSession(sid, make(chan StreamEvent, 1))
	m.killIdleConns(candidates)
	require.True(t, conn.alive)
	candidates, _ = m.collectIdleConns()
	require.Empty(t, candidates)
	conn.client.UnregisterSession(sid)
	candidates, _ = m.collectIdleConns()
	require.Len(t, candidates, 1)
}

func TestBackgroundTaskOnlyAgentAndDuplicateEdges(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newPromptBackgroundWork()
		events := make(chan StreamEvent, 8)
		w.observe("a", "shell", "started", "work", "", events, nil)
		done := make(chan error, 1)
		go func() { done <- w.wait(t.Context(), nil) }()
		synctest.Wait()
		require.Empty(t, done)
		w.observe("a", "", "completed", "", "ok", events, nil)
		w.observe("a", "shell", "started", "work", "", events, nil)
		require.NoError(t, <-done)
		require.Len(t, events, 2)
	})
}

func TestBackgroundFailureMessageCannotRetryAcceptedPrompt(t *testing.T) {
	err := fmt.Errorf("%w: %s", errBackgroundFollowup, "peer disconnected")
	require.False(t, isACPPeerDisconnected(err))
}
