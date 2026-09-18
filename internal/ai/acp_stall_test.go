package ai

import (
	"context"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"

	"clawbench/internal/model"
)

func TestACPConn_EffectiveStallTimeout(t *testing.T) {
	conn := NewACPConnForTest(&model.Agent{ID: "test"}, "sid")

	// Zero uses the default.
	conn.stallTimeout = 0
	assert.Equal(t, defaultACPStallTimeout, conn.effectiveStallTimeout())

	// Negative disables (0).
	conn.stallTimeout = -1
	assert.Equal(t, time.Duration(0), conn.effectiveStallTimeout())

	// Positive is honored.
	conn.stallTimeout = 90 * time.Second
	assert.Equal(t, 90*time.Second, conn.effectiveStallTimeout())
}

func TestACPConn_IsStalled(t *testing.T) {
	conn := NewACPConnForTest(&model.Agent{ID: "test"}, "sid")

	t.Run("disabled timeout is never stalled", func(t *testing.T) {
		assert.False(t, conn.isStalled(0), "zero timeout disables the watchdog")
		assert.False(t, conn.isStalled(-1), "negative timeout disables the watchdog")
	})

	t.Run("fresh activity is not stalled", func(t *testing.T) {
		conn.SetToolInFlight(false)
		conn.TouchModelProgress()
		assert.False(t, conn.isStalled(30*time.Minute))
	})

	t.Run("old activity with no tool is stalled", func(t *testing.T) {
		conn.SetToolInFlight(false)
		conn.lastModelProgress.Store(time.Now().Add(-10 * time.Minute).UnixNano())
		assert.True(t, conn.isStalled(30*time.Second))
	})

	t.Run("in-flight tool suppresses stall regardless of staleness", func(t *testing.T) {
		conn.lastModelProgress.Store(time.Now().Add(-10 * time.Minute).UnixNano())
		conn.SetToolInFlight(true)
		assert.False(t, conn.isStalled(30*time.Second),
			"an in-flight tool must keep the stream alive (e.g. a long-running `sleep`)")
	})

	t.Run("no SessionUpdate yet but fresh connection is not stalled", func(t *testing.T) {
		conn.SetToolInFlight(false)
		conn.lastModelProgress.Store(0) // no model output received yet
		conn.lastUsed = time.Now()      // but the connection was just created/used
		assert.False(t, conn.isStalled(30*time.Second), "a brand-new connection must not be killed immediately")
	})

	t.Run("housekeeping notifications do not count as progress", func(t *testing.T) {
		// Regression: the stall watchdog must key off MODEL progress, not any
		// notification. CodeBuddy re-emits AvailableCommandsUpdate every ~8
		// minutes on plugin refresh; counting that as progress kept a hung turn
		// (upstream empty stream) alive for 7h37m until the user cancelled.
		conn.SetToolInFlight(false)
		conn.lastModelProgress.Store(time.Now().Add(-45 * time.Minute).UnixNano())
		// The agent keeps talking, but only housekeeping — no model output.
		conn.TouchSessionUpdate()
		assert.True(t, conn.isStalled(30*time.Minute),
			"a heartbeat must not mask a turn whose model produced nothing")
	})

	t.Run("permission wait is protected by the in-flight tool, not the heartbeat", func(t *testing.T) {
		// The main legitimate long pause: a permission request awaiting user
		// approval. The tool's ToolCall lands before RequestPermission blocks,
		// so toolInFlight stays true for the entire wait (observed: an
		// ExitPlanMode approval card sat in-flight for 45s). If this ever
		// regresses, a user who leaves the approval card open past the stall
		// window would have their session killed.
		conn.lastModelProgress.Store(time.Now().Add(-2 * time.Hour).UnixNano())
		conn.SetToolInFlight(true)
		assert.False(t, conn.isStalled(30*time.Minute),
			"a user answering an approval card late must not lose the session")
	})
}

func TestACPConn_SetToolInFlight_ViaSessionUpdate(t *testing.T) {
	conn := NewACPConnForTest(&model.Agent{ID: "test"}, "sid")
	ch := make(chan StreamEvent, 16)
	ctx := context.Background()

	// A tool_call starts the tool — should mark in-flight.
	mapACPSessionUpdate(acp.SessionUpdate{
		ToolCall: &acp.SessionUpdateToolCall{
			ToolCallId:    "call_1",
			Title:         "Bash",
			SessionUpdate: "tool_call",
		},
	}, ch, ctx, conn, nil)
	assert.True(t, conn.toolInFlight.Load(), "tool_call should set toolInFlight")

	// An in-progress update keeps it in-flight.
	progress := acp.ToolCallStatusInProgress
	mapACPSessionUpdate(acp.SessionUpdate{
		ToolCallUpdate: &acp.SessionToolCallUpdate{
			ToolCallId:    "call_1",
			SessionUpdate: "tool_call_update",
			Status:        &progress,
		},
	}, ch, ctx, conn, nil)
	assert.True(t, conn.toolInFlight.Load())

	// A completed update clears it.
	completed := acp.ToolCallStatusCompleted
	mapACPSessionUpdate(acp.SessionUpdate{
		ToolCallUpdate: &acp.SessionToolCallUpdate{
			ToolCallId:    "call_1",
			SessionUpdate: "tool_call_update",
			Status:        &completed,
		},
	}, ch, ctx, conn, nil)
	assert.False(t, conn.toolInFlight.Load(), "completed tool_call_update should clear toolInFlight")
}

func TestACPConn_StartStallWatchdog_CancelsOnStall(t *testing.T) {
	conn := NewACPConnForTest(&model.Agent{ID: "test"}, "sid")
	conn.stallTimeout = 200 * time.Millisecond
	// No activity for a while, no tool in flight → stalled.
	conn.lastModelProgress.Store(time.Now().Add(-10 * time.Minute).UnixNano())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stalled := make(chan struct{})
	stop := conn.startStallWatchdog(ctx, func() { close(stalled) })
	defer stop()

	select {
	case <-stalled:
		// Watchdog fired — good.
	case <-time.After(3 * time.Second):
		t.Fatal("watchdog did not fire on a stalled prompt")
	}
}

func TestACPConn_StartStallWatchdog_DoesNotFireWithToolInFlight(t *testing.T) {
	conn := NewACPConnForTest(&model.Agent{ID: "test"}, "sid")
	conn.stallTimeout = 200 * time.Millisecond
	conn.lastModelProgress.Store(time.Now().Add(-10 * time.Minute).UnixNano())
	conn.SetToolInFlight(true) // agent is running a long tool (e.g. `sleep`)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fired := make(chan struct{})
	stop := conn.startStallWatchdog(ctx, func() { close(fired) })
	defer stop()

	// Wait well past the stall window; the watchdog must NOT fire while a
	// tool is in flight.
	select {
	case <-fired:
		t.Fatal("watchdog fired despite an in-flight tool")
	case <-time.After(600 * time.Millisecond):
		// Not fired — good.
	}

	// Once the tool completes (not in flight), it becomes stalled and fires.
	conn.SetToolInFlight(false)
	select {
	case <-fired:
		// Fired after the tool completed — good.
	case <-time.After(3 * time.Second):
		t.Fatal("watchdog did not fire after the in-flight tool completed")
	}
}

func TestIsModelProgressUpdate(t *testing.T) {
	text := &acp.ContentBlockText{Text: "hi"}
	status := acp.ToolCallStatusInProgress

	t.Run("model-driven updates count as progress", func(t *testing.T) {
		cases := map[string]acp.SessionUpdate{
			"agent text":     {AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{Content: acp.ContentBlock{Text: text}}},
			"agent thinking": {AgentThoughtChunk: &acp.SessionUpdateAgentThoughtChunk{Content: acp.ContentBlock{Text: text}}},
			"tool call":      {ToolCall: &acp.SessionUpdateToolCall{ToolCallId: "c1"}},
			"tool update":    {ToolCallUpdate: &acp.SessionToolCallUpdate{ToolCallId: "c1", Status: &status}},
			"plan":           {Plan: &acp.SessionUpdatePlan{}},
			"plan update":    {PlanUpdate: &acp.SessionPlanUpdate{}},
			"plan removed":   {PlanRemoved: &acp.SessionUpdatePlanRemoved{}},
		}
		for name, u := range cases {
			assert.True(t, isModelProgressUpdate(u), "%s should count as model progress", name)
		}
	})

	t.Run("housekeeping updates do not count as progress", func(t *testing.T) {
		cases := map[string]acp.SessionUpdate{
			// The one that caused the 7h37m hang.
			"available commands": {AvailableCommandsUpdate: &acp.SessionAvailableCommandsUpdate{}},
			"current mode":       {CurrentModeUpdate: &acp.SessionCurrentModeUpdate{}},
			"config option":      {ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{}},
			"session info":       {SessionInfoUpdate: &acp.SessionSessionInfoUpdate{}},
			"usage":              {UsageUpdate: &acp.SessionUsageUpdate{}},
			"user message echo":  {UserMessageChunk: &acp.SessionUpdateUserMessageChunk{Content: acp.ContentBlock{Text: text}}},
		}
		for name, u := range cases {
			assert.False(t, isModelProgressUpdate(u), "%s must not count as model progress", name)
		}
	})
}

func TestClawBenchACPClient_SessionUpdate_ModelProgressVsHeartbeat(t *testing.T) {
	// End-to-end wiring: a model-output notification must advance the stall
	// watchdog's clock, while a housekeeping heartbeat must not.
	agent := &model.Agent{ID: "test-progress-agent", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-progress")
	c := NewClawBenchACPClient()
	c.connRef = conn

	ctx := context.Background()
	notif := func(u acp.SessionUpdate) acp.SessionNotification {
		return acp.SessionNotification{SessionId: acp.SessionId("sess-progress"), Update: u}
	}

	// Housekeeping must NOT move the model-progress clock.
	conn.lastModelProgress.Store(time.Now().Add(-10 * time.Minute).UnixNano())
	before := conn.lastModelProgress.Load()
	err := c.SessionUpdate(ctx, notif(acp.SessionUpdate{
		AvailableCommandsUpdate: &acp.SessionAvailableCommandsUpdate{},
	}))
	assert.NoError(t, err)
	assert.Equal(t, before, conn.lastModelProgress.Load(),
		"AvailableCommandsUpdate must not count as model progress")

	// Agent text MUST move it.
	err = c.SessionUpdate(ctx, notif(acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "real output"}},
		},
	}))
	assert.NoError(t, err)
	assert.Greater(t, conn.lastModelProgress.Load(), before,
		"agent text must count as model progress")
}

func TestACPConn_KillAndMarkDead_PreservesAcpSID(t *testing.T) {
	conn := NewACPConnForTest(&model.Agent{ID: "test"}, "sid")
	conn.acpSID = "acp-session-123"

	conn.killAndMarkDead()

	assert.False(t, conn.alive, "connection should be dead after killAndMarkDead")
	assert.Equal(t, "acp-session-123", conn.acpSID,
		"acpSID must be preserved so ensureAliveWithSession can recover via LoadSession/ResumeSession")
	assert.Nil(t, conn.conn, "ACP connection should be nil")
	assert.Nil(t, conn.client, "ACP client should be nil")
}

func TestACPConn_KillAndMarkDeadLocked_NoDeadlock(t *testing.T) {
	// Regression test: ensureAliveWithSession holds c.mu and calls
	// killAndMarkDeadLocked (not killAndMarkDead). The old code called
	// killAndMarkDead which does c.mu.Lock() internally — causing a
	// self-deadlock that hung the session goroutine forever.
	conn := NewACPConnForTest(&model.Agent{ID: "test"}, "sid")
	conn.acpSID = "acp-session-deadlock-test"
	conn.alive = true
	conn.conn = nil
	conn.client = nil
	conn.cmd = nil // no real subprocess needed for this test

	// Call under c.mu — the exact pattern used by ensureAliveWithSession.
	// The old killAndMarkDead() would deadlock here.
	done := make(chan struct{})
	go func() {
		conn.mu.Lock()
		conn.killAndMarkDeadLocked()
		conn.mu.Unlock()
		close(done)
	}()

	select {
	case <-done:
		// No deadlock — good.
	case <-time.After(5 * time.Second):
		t.Fatal("killAndMarkDeadLocked deadlocked when called under c.mu")
	}

	assert.False(t, conn.alive, "connection should be dead after killAndMarkDeadLocked")
	assert.Equal(t, "acp-session-deadlock-test", conn.acpSID,
		"acpSID must be preserved for ResumeSession recovery")
	assert.Nil(t, conn.conn, "ACP connection should be nil")
	assert.Nil(t, conn.client, "ACP client should be nil")
}

func TestACPConn_Close_ClearsAcpSID(t *testing.T) {
	conn := NewACPConnForTest(&model.Agent{ID: "test"}, "sid")
	conn.acpSID = "acp-session-456"

	conn.close()

	assert.False(t, conn.alive, "connection should be dead after close")
	assert.Empty(t, conn.acpSID, "close() must clear acpSID (unlike killAndMarkDead)")
}

func TestACPConn_StallWatchdog_UsesKillAndMarkDead(t *testing.T) {
	// Verify that the stall watchdog preserves acpSID by using killAndMarkDead
	// (not close), ensuring LoadSession/ResumeSession recovery on next prompt.
	conn := NewACPConnForTest(&model.Agent{ID: "test"}, "sid")
	conn.stallTimeout = 200 * time.Millisecond
	conn.acpSID = "acp-session-789"
	conn.lastModelProgress.Store(time.Now().Add(-10 * time.Minute).UnixNano())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stalled := make(chan struct{})
	stop := conn.startStallWatchdog(ctx, func() {
		conn.killAndMarkDead()
		close(stalled)
	})
	defer stop()

	select {
	case <-stalled:
		// Watchdog fired — verify acpSID is preserved.
		assert.False(t, conn.alive, "connection should be dead")
		assert.Equal(t, "acp-session-789", conn.acpSID,
			"stall watchdog must preserve acpSID for session recovery")
	case <-time.After(3 * time.Second):
		t.Fatal("watchdog did not fire")
	}
}
