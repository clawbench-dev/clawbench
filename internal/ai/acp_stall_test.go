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
		conn.TouchSessionUpdate()
		assert.False(t, conn.isStalled(30*time.Minute))
	})

	t.Run("old activity with no tool is stalled", func(t *testing.T) {
		conn.SetToolInFlight(false)
		conn.lastSessionUpdate.Store(time.Now().Add(-10 * time.Minute).UnixNano())
		assert.True(t, conn.isStalled(30*time.Second))
	})

	t.Run("in-flight tool suppresses stall regardless of staleness", func(t *testing.T) {
		conn.lastSessionUpdate.Store(time.Now().Add(-10 * time.Minute).UnixNano())
		conn.SetToolInFlight(true)
		assert.False(t, conn.isStalled(30*time.Second),
			"an in-flight tool must keep the stream alive (e.g. a long-running `sleep`)")
	})

	t.Run("no SessionUpdate yet but fresh connection is not stalled", func(t *testing.T) {
		conn.SetToolInFlight(false)
		conn.lastSessionUpdate.Store(0) // no SessionUpdate received yet
		conn.lastUsed = time.Now()      // but the connection was just created/used
		assert.False(t, conn.isStalled(30*time.Second), "a brand-new connection must not be killed immediately")
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
	conn.lastSessionUpdate.Store(time.Now().Add(-10 * time.Minute).UnixNano())

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
	conn.lastSessionUpdate.Store(time.Now().Add(-10 * time.Minute).UnixNano())
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
	conn.lastSessionUpdate.Store(time.Now().Add(-10 * time.Minute).UnixNano())

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
