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

// normalizeAsyncTaskUpdate is the wire shim: Claude Code's adapter reports
// background work as `async_task_spawned` / `async_task_state_update`, which the
// SDK drops. Anything it does not recognize must be passed through untouched —
// a rewritten frame the SDK cannot decode is worse than the original.
func TestNormalizeAsyncTaskUpdate(t *testing.T) {
	frame := func(kind, id, state string) string {
		return `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s1","update":{` +
			`"sessionUpdate":"` + kind + `","asyncTaskId":"` + id + `","taskType":"shell",` +
			`"state":"` + state + `","description":"npm test","summary":"ok"}}}`
	}

	t.Run("spawn becomes a started background event", func(t *testing.T) {
		out := normalizeAsyncTaskUpdate([]byte(frame("async_task_spawned", "a1", "")))
		require.NotEqual(t, frame("async_task_spawned", "a1", ""), string(out))
		var got struct {
			Method string `json:"method"`
			Params struct {
				SID    string `json:"sessionId"`
				Update struct {
					Kind string         `json:"sessionUpdate"`
					Meta map[string]any `json:"_meta"`
				} `json:"update"`
			} `json:"params"`
		}
		require.NoError(t, json.Unmarshal(out, &got))
		require.Equal(t, "session/update", got.Method)
		require.Equal(t, "s1", got.Params.SID)
		require.Equal(t, "session_info_update", got.Params.Update.Kind)
		event, ok := got.Params.Update.Meta[backgroundEventKey].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "a1", event["id"])
		require.Equal(t, "shell", event["kind"])
		require.Equal(t, "started", event["state"])
	})

	t.Run("terminal state passes through", func(t *testing.T) {
		for _, state := range []string{"completed", "failed", "stopped"} {
			out := normalizeAsyncTaskUpdate([]byte(frame("async_task_state_update", "a1", state)))
			require.Contains(t, string(out), backgroundEventKey, state)
		}
	})

	t.Run("non-terminal state is left alone", func(t *testing.T) {
		in := frame("async_task_state_update", "a1", "running")
		require.Equal(t, in, string(normalizeAsyncTaskUpdate([]byte(in))))
	})

	t.Run("frames it does not own are left alone", func(t *testing.T) {
		for _, in := range []string{
			`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s1","update":{"sessionUpdate":"agent_message_chunk"}}}`,
			`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"","update":{"sessionUpdate":"async_task_spawned","asyncTaskId":"a1"}}}`,
			`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s1","update":{"sessionUpdate":"async_task_spawned","asyncTaskId":""}}}`,
			`{"jsonrpc":"2.0","method":"other","params":{"sessionId":"s1","update":{"sessionUpdate":"async_task_spawned","asyncTaskId":"a1"}}}`,
			`not json at all`,
		} {
			require.Equal(t, in, string(normalizeAsyncTaskUpdate([]byte(in))), in)
		}
	})
}

// The background task renders as a tool card: a call while it runs, a result
// once it settles. A failed/stopped task must read as an error, and the summary
// is what the user actually needs to see.
func TestBackgroundTaskCard(t *testing.T) {
	started := backgroundTaskCard("a1", "started", "npm test", "")
	require.Equal(t, "tool_use", started.Type)
	require.Equal(t, "BackgroundTask", started.Tool.Name)
	require.False(t, started.Tool.Done)
	require.Contains(t, started.Tool.Input, "npm test")

	done := backgroundTaskCard("a1", "completed", "npm test", "all green")
	require.Equal(t, "tool_result", done.Type)
	require.True(t, done.Tool.Done)
	require.Equal(t, "success", done.Tool.Status)
	require.Equal(t, "completed: all green", done.Tool.Output)

	failed := backgroundTaskCard("a1", "failed", "npm test", "")
	require.Equal(t, "error", failed.Tool.Status)
	require.Equal(t, "failed", failed.Tool.Output)
}

func TestHasDetachedTask(t *testing.T) {
	w := newPromptBackgroundWork()
	require.False(t, w.hasDetachedTask())

	w.active["turn"] = "turn"
	require.False(t, w.hasDetachedTask(), "the autonomous turn itself is not detached work")

	w.active["shell"] = "shell"
	require.True(t, w.hasDetachedTask())
}
