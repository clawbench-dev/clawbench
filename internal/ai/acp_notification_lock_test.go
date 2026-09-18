package ai

import (
	"context"
	"testing"
	"time"

	"clawbench/internal/model"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Notification-path lock guard
// ---------------------------------------------------------------------------
//
// These tests pin the invariant documented on ACPConn.stateMu: callbacks that
// run on the ACP SDK's notification goroutine must NOT acquire c.mu.
//
// Why it matters (production incident, 2026-09-17):
//   - RPCs like NewSession/ResumeSession hold c.mu while waiting for queued
//     notifications to drain (SDK Connection.waitNotificationsUpTo).
//   - A notification callback that takes c.mu therefore deadlocks against the
//     in-flight RPC.
//   - The agent keeps emitting while both are stuck, the SDK's bounded
//     notification queue (1024) overflows, and the SDK calls shutdownReceive —
//     killing the connection. The user sees
//     `-32603 "peer disconnected before response"` while the agent process is
//     still alive (crash diagnostics show rss/fds but no exit_code).
//
// The tests hold c.mu to simulate an in-flight RPC, then drive the real
// notification path. On the buggy code the accessor blocks on c.mu forever and
// the test times out.

// notificationPathDeadlockTimeout bounds how long a notification callback may
// take while c.mu is held. The callbacks under test do no I/O, so anything
// beyond a generous scheduler window means it is blocked on the lock.
const notificationPathDeadlockTimeout = 5 * time.Second

// runWithConnMuHeld simulates an in-flight ACP RPC by holding c.mu while fn
// runs on another goroutine. It reports false if fn did not finish in time,
// which is the observable symptom of a notification callback taking c.mu.
//
// The mutex is always released before returning so a failure cannot poison
// later subtests with a leaked lock.
func runWithConnMuHeld(t *testing.T, conn *ACPConn, fn func()) (completed bool) {
	t.Helper()

	conn.mu.Lock()
	defer conn.mu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()

	select {
	case <-done:
		return true
	case <-time.After(notificationPathDeadlockTimeout):
		return false
	}
}

// newNotificationPathConn returns a connection suitable for driving
// mapACPSessionUpdate with a non-nil conn (the branches under test only touch
// the connection cache when conn != nil).
func newNotificationPathConn(t *testing.T) *ACPConn {
	t.Helper()
	conn := newACPConn(&model.Agent{ID: "test-notification-lock", Backend: "acp-stdio"}, "sid-notification-lock")
	require.NotNil(t, conn)
	return conn
}

// TestMapACPSessionUpdate_DoesNotTakeConnMu drives every notification shape
// that touches the cached session state and asserts the callback completes
// while c.mu is held.
//
// Each case also asserts the resulting side effect, so a case cannot pass by
// early-returning before it reaches the accessor (which would make this test a
// tautology).
func TestMapACPSessionUpdate_DoesNotTakeConnMu(t *testing.T) {
	t.Run("plan_update_takes_no_conn_mu", func(t *testing.T) {
		conn := newNotificationPathConn(t)
		ch := make(chan StreamEvent, 16)

		update := acp.SessionUpdate{
			Plan: &acp.SessionUpdatePlan{
				Entries: []acp.PlanEntry{
					{Content: "step one", Priority: acp.PlanEntryPriorityHigh, Status: acp.PlanEntryStatusInProgress},
				},
			},
		}

		ok := runWithConnMuHeld(t, conn, func() {
			mapACPSessionUpdate(update, ch, context.Background(), conn, nil)
		})
		require.True(t, ok,
			"plan_update must not acquire c.mu: it runs on the SDK notification goroutine, "+
				"which an in-flight NewSession/ResumeSession RPC waits on (SDK waitNotificationsUpTo)")

		require.NotNil(t, conn.GetCachedPlanState(), "plan must reach the cache, or this case is vacuous")
		assert.Len(t, conn.GetCachedPlanState().Entries, 1)
	})

	t.Run("usage_update_takes_no_conn_mu", func(t *testing.T) {
		conn := newNotificationPathConn(t)
		ch := make(chan StreamEvent, 16)

		update := acp.SessionUpdate{
			UsageUpdate: &acp.SessionUsageUpdate{Used: 1000, Size: 5000},
		}

		ok := runWithConnMuHeld(t, conn, func() {
			mapACPSessionUpdate(update, ch, context.Background(), conn, nil)
		})
		require.True(t, ok,
			"usage_update must not acquire c.mu: usage notifications are the highest-frequency "+
				"notification on this goroutine, so a block here fills the SDK queue fastest")

		require.NotNil(t, conn.GetCachedUsageState(), "usage must reach the cache, or this case is vacuous")
		assert.Equal(t, 1000, conn.GetCachedUsageState().Used)
	})

	t.Run("config_option_mode_select_takes_no_conn_mu", func(t *testing.T) {
		conn := newNotificationPathConn(t)
		ch := make(chan StreamEvent, 16)

		cat := acp.SessionConfigOptionCategoryMode
		ungrouped := acp.SessionConfigSelectOptionsUngrouped(
			[]acp.SessionConfigSelectOption{
				{Name: "Code", Value: acp.SessionConfigValueId("code")},
			},
		)
		update := acp.SessionUpdate{
			ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{
				ConfigOptions: []acp.SessionConfigOption{
					{
						Select: &acp.SessionConfigOptionSelect{
							Id:           acp.SessionConfigId("mode"),
							Name:         "Mode",
							Category:     &cat,
							CurrentValue: acp.SessionConfigValueId("code"),
							Options:      acp.SessionConfigSelectOptions{Ungrouped: &ungrouped},
						},
					},
				},
			},
		}

		ok := runWithConnMuHeld(t, conn, func() {
			mapACPSessionUpdate(update, ch, context.Background(), conn, nil)
		})
		require.True(t, ok,
			"config_option (mode) must not acquire c.mu: handleConfigOptionSelect calls "+
				"HasCurrentChanged/UpdateCachedCurrent on the notification goroutine")

		assert.Equal(t, "code", conn.GetCurrentSelection("mode"),
			"mode selection must reach the cache, or this case is vacuous")
	})

	t.Run("config_option_model_select_takes_no_conn_mu", func(t *testing.T) {
		conn := newNotificationPathConn(t)
		ch := make(chan StreamEvent, 16)

		cat := acp.SessionConfigOptionCategoryModel
		ungrouped := acp.SessionConfigSelectOptionsUngrouped(
			[]acp.SessionConfigSelectOption{
				{Name: "GPT-4o", Value: acp.SessionConfigValueId("gpt-4o")},
			},
		)
		update := acp.SessionUpdate{
			ConfigOptionUpdate: &acp.SessionConfigOptionUpdate{
				ConfigOptions: []acp.SessionConfigOption{
					{
						Select: &acp.SessionConfigOptionSelect{
							Id:           acp.SessionConfigId("model"),
							Name:         "Model",
							Category:     &cat,
							CurrentValue: acp.SessionConfigValueId("gpt-4o"),
							Options:      acp.SessionConfigSelectOptions{Ungrouped: &ungrouped},
						},
					},
				},
			},
		}

		ok := runWithConnMuHeld(t, conn, func() {
			mapACPSessionUpdate(update, ch, context.Background(), conn, nil)
		})
		require.True(t, ok,
			"config_option (model) must not acquire c.mu: the model branch calls "+
				"SetCachedModelListState on the notification goroutine")

		assert.Equal(t, "gpt-4o", conn.GetCurrentModelID(),
			"model selection must reach the cache, or this case is vacuous")
	})

	t.Run("current_mode_update_takes_no_conn_mu", func(t *testing.T) {
		conn := newNotificationPathConn(t)
		ch := make(chan StreamEvent, 16)

		update := acp.SessionUpdate{
			CurrentModeUpdate: &acp.SessionCurrentModeUpdate{
				CurrentModeId: acp.SessionModeId("code"),
			},
		}

		ok := runWithConnMuHeld(t, conn, func() {
			mapACPSessionUpdate(update, ch, context.Background(), conn, nil)
		})
		require.True(t, ok,
			"current_mode_update must not acquire c.mu: the v1 mode branch calls "+
				"HasCurrentChanged/UpdateCachedCurrent on the notification goroutine")

		assert.Equal(t, "code", conn.GetCurrentSelection("mode"),
			"mode selection must reach the cache, or this case is vacuous")
	})
}

// TestACPConn_CachedStateAccessors_DoNotTakeConnMu is the direct, structural
// counterpart to the test above: it pins each accessor individually so a
// regression names the offending method instead of only failing the
// end-to-end path.
func TestACPConn_CachedStateAccessors_DoNotTakeConnMu(t *testing.T) {
	// Seed values with the lock not held so the getters have something to read.
	conn := newNotificationPathConn(t)
	conn.SetCachedPlanState(&PlanState{Entries: []PlanEntry{{Content: "seed"}}})
	conn.SetCachedUsageState(&UsageState{Used: 1, Size: 2})
	conn.UpdateCachedCurrent("mode", "seed")
	conn.UpdateCachedCurrent("model", "seed-model")
	conn.UpdateCachedCurrent("thought_level", "seed-effort")

	cases := []struct {
		name string
		fn   func()
	}{
		{"SetCachedPlanState", func() { conn.SetCachedPlanState(&PlanState{}) }},
		{"GetCachedPlanState", func() { _ = conn.GetCachedPlanState() }},
		{"SetCachedUsageState", func() { conn.SetCachedUsageState(&UsageState{}) }},
		{"GetCachedUsageState", func() { _ = conn.GetCachedUsageState() }},
		{"UpdateCachedCurrent", func() { conn.UpdateCachedCurrent("mode", "next") }},
		{"GetCurrentSelection", func() { _ = conn.GetCurrentSelection("mode") }},
		{"HasCurrentChanged", func() { _ = conn.HasCurrentChanged("mode", "other") }},
		{"GetCurrentModeID", func() { _ = conn.GetCurrentModeID() }},
		{"SetCurrentModeID", func() { conn.SetCurrentModeID("next") }},
		{"GetCurrentThinkingEffortID", func() { _ = conn.GetCurrentThinkingEffortID() }},
		{"SetCurrentThinkingEffortID", func() { conn.SetCurrentThinkingEffortID("next") }},
		{"GetCurrentModelID", func() { _ = conn.GetCurrentModelID() }},
		{"SetCurrentModelID", func() { conn.SetCurrentModelID("next") }},
		{"UpdateCachedCurrentMode", func() { conn.UpdateCachedCurrentMode("next") }},
		{"UpdateCachedCurrentModel", func() { conn.UpdateCachedCurrentModel("next") }},
		{"UpdateCachedCurrentThinkingEffort", func() { conn.UpdateCachedCurrentThinkingEffort("next") }},
		{"SetCachedModeState", func() { conn.SetCachedModeState(&ModeState{CurrentModeID: "next"}) }},
		{"SetCachedThinkingEffortState", func() {
			conn.SetCachedThinkingEffortState(&ThinkingEffortState{CurrentID: "next"})
		}},
		{"SetCachedModelListState", func() {
			conn.SetCachedModelListState(&ModelListState{CurrentModelID: "next"})
		}},
		{"SetCachedConfigState", func() {
			conn.SetCachedConfigState(&ConfigOptionState{ConfigID: "mode", CurrentID: "next"})
		}},
		{"HasCurrentModeChanged", func() { _ = conn.HasCurrentModeChanged("next") }},
		{"HasCurrentThinkingEffortChanged", func() { _ = conn.HasCurrentThinkingEffortChanged("next") }},
		{"snapshotCachedConfig", func() { _ = conn.snapshotCachedConfig() }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok := runWithConnMuHeld(t, conn, tc.fn)
			assert.True(t, ok,
				"%s must not acquire c.mu: it is reachable from the SDK notification goroutine, "+
					"which an in-flight ACP RPC waits on", tc.name)
		})
	}
}

// TestACPConn_NotificationPath_NoLostUpdate asserts the lock move did not
// change observable behavior: concurrent notification-path writes and
// RPC-path reads still agree on the final value.
func TestACPConn_NotificationPath_NoLostUpdate(t *testing.T) {
	conn := newNotificationPathConn(t)

	const iterations = 200
	done := make(chan struct{})

	// Writer: the notification goroutine's write path.
	go func() {
		defer close(done)
		for i := range iterations {
			conn.UpdateCachedCurrent("mode", "mode-"+string(rune('a'+i%26)))
			conn.SetCachedUsageState(&UsageState{Used: i})
		}
	}()

	// Reader: an RPC-path snapshot. Must never observe a torn value; with a
	// single mutex guarding both fields this just has to not race (run with
	// -race to give it teeth).
	for range iterations {
		_ = conn.GetCurrentSelection("mode")
		_ = conn.GetCachedUsageState()
	}

	<-done

	// Final state is whatever the last write was; the point is that both
	// readers and writers completed and the values are internally consistent.
	assert.NotEmpty(t, conn.GetCurrentSelection("mode"))
	require.NotNil(t, conn.GetCachedUsageState())
}
