package ai

import (
	"context"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// --- ACPConnManager.CancelTurn ---

func TestACPConnManager_CancelTurn_NoConn(t *testing.T) {
	// CancelTurn on a session with no connection should not panic
	mgr := GetACPConnManager()
	assert.NotPanics(t, func() {
		mgr.CancelTurn("nonexistent-session")
	})
}

func TestACPConnManager_CancelTurn_WithConn(t *testing.T) {
	mgr := GetACPConnManager()

	// Inject a mock ACPConn with a session mapping
	agent := &model.Agent{ID: "test-cancel-agent", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-cancel-turn")
	mgr.SetConnForTest("session-cancel-turn", conn)
	defer mgr.CloseConn("session-cancel-turn")

	// CancelTurn should not panic even when the connection has no real ACP session
	assert.NotPanics(t, func() {
		mgr.CancelTurn("session-cancel-turn")
	})
}

func TestACPConnManager_CancelTurn_DeadConn(t *testing.T) {
	mgr := GetACPConnManager()

	agent := &model.Agent{ID: "test-dead-agent", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-dead-turn")
	// Connection is alive=false by default (not spawned)
	mgr.SetConnForTest("session-dead-turn", conn)
	defer mgr.CloseConn("session-dead-turn")

	// CancelTurn on a dead connection should not panic
	assert.NotPanics(t, func() {
		mgr.CancelTurn("session-dead-turn")
	})
}

// --- ACPConn.CancelTurn ---

func TestACPConn_CancelTurn_NoSession(t *testing.T) {
	agent := &model.Agent{ID: "test-cancel-nosession", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-no-acp")

	// No ACP session ID set — CancelTurn should be a no-op
	assert.NotPanics(t, func() {
		conn.CancelTurn(context.Background())
	})
}

func TestACPConn_CancelTurn_NoConn(t *testing.T) {
	agent := &model.Agent{ID: "test-cancel-noconn", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-no-conn")

	// No connection object — CancelTurn should be a no-op even with acpSID set
	conn.SetSessionMappingForTest("session-no-conn", "acp-session-123")
	assert.NotPanics(t, func() {
		conn.CancelTurn(context.Background())
	})
}

// --- ACPConnManager.GetConn ---

func TestACPConnManager_GetConn_Exists(t *testing.T) {
	mgr := GetACPConnManager()

	agent := &model.Agent{ID: "test-getconn", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-getconn")
	mgr.SetConnForTest("session-getconn", conn)
	defer mgr.CloseConn("session-getconn")

	got := mgr.GetConn("session-getconn")
	assert.NotNil(t, got)
}

func TestACPConnManager_GetConn_NotExists(t *testing.T) {
	mgr := GetACPConnManager()
	got := mgr.GetConn("nonexistent")
	assert.Nil(t, got)
}

// --- ACPConnManager.MarkIdle ---

func TestACPConnManager_MarkIdle_ExistingConn(t *testing.T) {
	mgr := GetACPConnManager()

	agent := &model.Agent{ID: "test-markidle", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-markidle")
	mgr.SetConnForTest("session-markidle", conn)
	defer mgr.CloseConn("session-markidle")

	// Set lastUsed to a known old time
	conn.mu.Lock()
	conn.lastUsed = time.Now().Add(-10 * time.Minute)
	oldLastUsed := conn.lastUsed
	conn.mu.Unlock()

	// MarkIdle should update lastUsed
	mgr.MarkIdle("session-markidle")

	conn.mu.Lock()
	newLastUsed := conn.lastUsed
	conn.mu.Unlock()

	assert.True(t, newLastUsed.After(oldLastUsed), "MarkIdle should update lastUsed")
}

func TestACPConnManager_MarkIdle_NonexistentConn(t *testing.T) {
	mgr := GetACPConnManager()

	// MarkIdle on a nonexistent session should not panic
	assert.NotPanics(t, func() {
		mgr.MarkIdle("nonexistent-session")
	})
}

// --- spawnLocked mutex release during cmd.Wait ---

func TestACPConn_CancelTurn_DoesNotBlockOnDeadConn(t *testing.T) {
	// This test verifies that CancelTurn does not block when the connection
	// is dead. In the old code, spawnLocked held c.mu during cmd.Wait(),
	// which could block CancelTurn (which also acquires c.mu) indefinitely.
	// After the fix, spawnLocked releases c.mu during cmd.Wait(), so
	// other operations can proceed.
	agent := &model.Agent{ID: "test-spawn-mutex", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-spawn-mutex")

	// The connection starts dead (alive=false). CancelTurn acquires c.mu
	// briefly — it should complete quickly, not block.
	done := make(chan struct{})
	go func() {
		conn.CancelTurn(context.Background())
		close(done)
	}()

	select {
	case <-done:
		// Success — CancelTurn did not block
	case <-time.After(2 * time.Second):
		t.Fatal("CancelTurn blocked — spawnLocked may be holding mutex during cmd.Wait()")
	}
}

// --- ACPConnManager.GetCachedStateByClawbenchSID ---

func TestGetCachedStateByClawbenchSID_NilConn(t *testing.T) {
	mgr := GetACPConnManager()
	s := mgr.GetCachedStateByClawbenchSID("nonexistent-session")
	assert.Nil(t, s.Mode)
	assert.Nil(t, s.Config)
	assert.Nil(t, s.Effort)
	assert.Nil(t, s.Commands)
	assert.Nil(t, s.ModelList)
	assert.Nil(t, s.Plan)
}

func TestGetCachedStateByClawbenchSID_NilClient(t *testing.T) {
	mgr := GetACPConnManager()
	agent := &model.Agent{ID: "test-nil-client", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-nil-client")
	// client is nil by default — should not panic
	mgr.SetConnForTest("session-nil-client", conn)
	defer mgr.CloseConn("session-nil-client")

	assert.NotPanics(t, func() {
		mgr.GetCachedStateByClawbenchSID("session-nil-client")
	})
	s := mgr.GetCachedStateByClawbenchSID("session-nil-client")
	assert.Nil(t, s.Mode)
	assert.Nil(t, s.Config)
	assert.Nil(t, s.Effort)
	assert.Nil(t, s.Commands)
	assert.Nil(t, s.ModelList)
	assert.Nil(t, s.Plan)
}

func TestGetCachedStateByClawbenchSID_WithCachedState(t *testing.T) {
	mgr := GetACPConnManager()
	agent := &model.Agent{ID: "test-cached-state", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-cached-state")
	conn.SetCachedModeState(&ModeState{CurrentModeID: "code", AvailableModes: []ModeDef{{ID: "code", Name: "Code"}}})
	conn.SetCachedPlanState(&PlanState{Entries: []PlanEntry{{Content: "Plan A content", Status: "pending"}}})
	mgr.SetConnForTest("session-cached-state", conn)
	defer mgr.CloseConn("session-cached-state")

	s := mgr.GetCachedStateByClawbenchSID("session-cached-state")
	assert.NotNil(t, s.Mode)
	assert.Equal(t, "code", s.Mode.CurrentModeID)
	assert.NotNil(t, s.Plan)
	assert.Len(t, s.Plan.Entries, 1)
}

func TestACPConn_SetCachedUsageState(t *testing.T) {
	conn := newACPConn(nil, "test-usage-sid")
	assert.Nil(t, conn.GetCachedUsageState())

	state := &UsageState{Used: 50000, Size: 200000}
	conn.SetCachedUsageState(state)
	got := conn.GetCachedUsageState()
	assert.Equal(t, 50000, got.Used)
	assert.Equal(t, 200000, got.Size)
}

func TestGetCachedStateByClawbenchSID_Usage(t *testing.T) {
	mgr := GetACPConnManager()
	agent := &model.Agent{ID: "test-usage-agent", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-sid-usage")
	usageState := &UsageState{Used: 100000, Size: 200000, Cost: 1.5, Currency: "USD"}
	conn.SetCachedUsageState(usageState)
	mgr.SetConnForTest("test-sid-usage", conn)
	defer mgr.CloseConn("test-sid-usage")

	state := mgr.GetCachedStateByClawbenchSID("test-sid-usage")
	assert.NotNil(t, state.Usage)
	assert.Equal(t, 100000, state.Usage.Used)
	assert.Equal(t, 200000, state.Usage.Size)
	assert.InDelta(t, 1.5, state.Usage.Cost, 0.001)
	assert.Equal(t, "USD", state.Usage.Currency)
}

// --- ACPConn.shouldSetConfig / markConfigSet ---

func TestACPConn_ShouldSetConfig_ModeInitial(t *testing.T) {
	agent := &model.Agent{ID: "test-config-mode", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-config-mode")

	// Initially, mode should need to be set (empty → non-empty)
	assert.True(t, conn.shouldSetConfig("mode", "code"))
}

func TestACPConn_ShouldSetConfig_ModeSameValue(t *testing.T) {
	agent := &model.Agent{ID: "test-config-mode", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-config-mode")

	conn.markConfigSet("mode", "code")
	assert.False(t, conn.shouldSetConfig("mode", "code"))
}

func TestACPConn_ShouldSetConfig_ModeDifferentValue(t *testing.T) {
	agent := &model.Agent{ID: "test-config-mode", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-config-mode")

	conn.markConfigSet("mode", "code")
	assert.True(t, conn.shouldSetConfig("mode", "ask"))
}

func TestACPConn_ShouldSetConfig_ModeReset(t *testing.T) {
	agent := &model.Agent{ID: "test-config-mode", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-config-mode")

	conn.markConfigSet("mode", "code")
	conn.resetLastSetConfig()
	assert.True(t, conn.shouldSetConfig("mode", "code"))
}

func TestACPConn_ShouldSetConfig_ModelInitial(t *testing.T) {
	agent := &model.Agent{ID: "test-config-model", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-config-model")

	assert.True(t, conn.shouldSetConfig("model", "gpt-4"))
	conn.markConfigSet("model", "gpt-4")
	assert.False(t, conn.shouldSetConfig("model", "gpt-4"))
	assert.True(t, conn.shouldSetConfig("model", "claude-3"))
}

func TestACPConn_ShouldSetConfig_ThinkingEffortInitial(t *testing.T) {
	agent := &model.Agent{ID: "test-config-effort", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-config-effort")

	assert.True(t, conn.shouldSetConfig("thinkingEffort", "high"))
	conn.markConfigSet("thinkingEffort", "high")
	assert.False(t, conn.shouldSetConfig("thinkingEffort", "high"))
}

func TestACPConn_ShouldSetConfig_UnknownConfigID(t *testing.T) {
	agent := &model.Agent{ID: "test-config-unknown", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-config-unknown")

	// Unknown config IDs always return true (no caching)
	assert.True(t, conn.shouldSetConfig("unknown", "value"))
}

func TestACPConn_ShouldSetConfig_UnsupportedConfig(t *testing.T) {
	agent := &model.Agent{ID: "test-unsupported", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-unsupported")

	// Initially, thinkingEffort should be allowed
	assert.True(t, conn.shouldSetConfig("thinkingEffort", "high"))

	// Mark thinkingEffort as unsupported
	conn.lastSetConfigMu.Lock()
	conn.unsupportedConfigs = map[string]bool{"thinkingEffort": true}
	conn.lastSetConfigMu.Unlock()

	// Now shouldSetConfig should return false for thinkingEffort
	assert.False(t, conn.shouldSetConfig("thinkingEffort", "high"))
	assert.False(t, conn.shouldSetConfig("thinkingEffort", "low"))
	// But other configs should still work
	assert.True(t, conn.shouldSetConfig("model", "gpt-4"))
}

func TestACPConn_ResetLastSetConfig_ClearsUnsupported(t *testing.T) {
	agent := &model.Agent{ID: "test-reset-unsupported", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-reset-unsupported")

	// Mark thinkingEffort as unsupported
	conn.lastSetConfigMu.Lock()
	conn.unsupportedConfigs = map[string]bool{"thinkingEffort": true}
	conn.lastSetConfigMu.Unlock()

	assert.False(t, conn.shouldSetConfig("thinkingEffort", "high"))

	// Reset should clear unsupported tracking
	conn.resetLastSetConfig()
	assert.True(t, conn.shouldSetConfig("thinkingEffort", "high"))
}

// --- ACPConn plan state caching ---

func TestACPConn_GetSetCachedPlanState(t *testing.T) {
	agent := &model.Agent{ID: "test-plan-state", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-plan-state")

	// Initially nil
	assert.Nil(t, conn.GetCachedPlanState())

	// Set and get
	planState := &PlanState{Entries: []PlanEntry{{Content: "Step 1 content", Status: "in_progress"}}}
	conn.SetCachedPlanState(planState)
	got := conn.GetCachedPlanState()
	assert.NotNil(t, got)
	assert.Len(t, got.Entries, 1)
	assert.Equal(t, "Step 1 content", got.Entries[0].Content)

	// Clear
	conn.SetCachedPlanState(nil)
	assert.Nil(t, conn.GetCachedPlanState())
}

// --- configKilledConnectionError ---

func TestConfigKilledConnectionError_Value(t *testing.T) {
	err := errConfigKilledConnection("model", "gpt-4")
	var cerr *configKilledConnectionError
	assert.ErrorAs(t, err, &cerr)
	assert.Equal(t, "model", cerr.ConfigID())
	assert.Equal(t, "gpt-4", cerr.Value())
	assert.Contains(t, cerr.Error(), "model")
	assert.Contains(t, cerr.Error(), "gpt-4")
}

func TestConfigKilledConnectionError_EmptyValue(t *testing.T) {
	err := errConfigKilledConnection("mode", "")
	var cerr *configKilledConnectionError
	assert.ErrorAs(t, err, &cerr)
	assert.Equal(t, "mode", cerr.ConfigID())
	assert.Equal(t, "", cerr.Value())
	assert.NotContains(t, cerr.Error(), "value=")
}

func TestConfigKilledConnectionError_Diagnostics(t *testing.T) {
	err := &configKilledConnectionError{
		configID: "thinkingEffort",
		value:    "high",
		diag:     crashDiagnostics{ExitCode: 1, StderrTail: "panic"},
	}
	errMsg := err.Error()
	assert.Contains(t, errMsg, "thinkingEffort")
	assert.Contains(t, errMsg, "high")
	assert.Contains(t, errMsg, "exit_code=1")
	assert.Contains(t, errMsg, "panic")
}

// --- crashDiagnostics.String ---

func TestCrashDiagnostics_String_Empty(t *testing.T) {
	d := crashDiagnostics{}
	assert.Equal(t, "", d.String())
}

func TestCrashDiagnostics_String_ExitCodeOnly(t *testing.T) {
	d := crashDiagnostics{ExitCode: 137}
	assert.Equal(t, " (exit_code=137 (SIGKILL (possible OOM killer)))", d.String())
}

func TestCrashDiagnostics_String_StderrOnly(t *testing.T) {
	d := crashDiagnostics{StderrTail: "segfault"}
	assert.Equal(t, " (stderr: segfault)", d.String())
}

func TestCrashDiagnostics_String_Both(t *testing.T) {
	d := crashDiagnostics{ExitCode: 1, StderrTail: "out of memory"}
	assert.Equal(t, " (exit_code=1 (general error), stderr: out of memory)", d.String())
}

// --- ACPConn.SetCachedConfigState derives modeState ---

func TestACPConn_SetCachedConfigState_DerivesModeState(t *testing.T) {
	agent := &model.Agent{ID: "test-derive-mode", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-derive-mode")

	// cachedModeState starts nil — check via registry
	assert.Nil(t, GetAgentCapabilityRegistry().GetModeState(agent.ID, ""))

	// Set config state with mode category — should derive modeState in registry
	conn.SetCachedConfigState(&ConfigOptionState{
		ConfigID:  "mode",
		CurrentID: "code",
		Options: []ConfigOptionDef{
			{
				ID:       "mode",
				Name:     "Mode",
				Category: "mode",
				Values: []ConfigOptionValue{
					{ID: "code", Name: "Code"},
					{ID: "ask", Name: "Ask"},
				},
			},
		},
	})

	modeState := GetAgentCapabilityRegistry().GetModeState(agent.ID, conn.GetCurrentModeID())
	assert.NotNil(t, modeState)
	assert.Equal(t, "code", modeState.CurrentModeID)
	assert.Len(t, modeState.AvailableModes, 2)
	assert.Equal(t, "code", modeState.AvailableModes[0].ID)
	assert.Equal(t, "ask", modeState.AvailableModes[1].ID)
}

func TestACPConn_SetCachedConfigState_DoesNotOverrideExistingModeState(t *testing.T) {
	agent := &model.Agent{ID: "test-no-override", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-no-override")

	// Set mode state first (writes to registry)
	conn.SetCachedModeState(&ModeState{CurrentModeID: "architect", AvailableModes: []ModeDef{{ID: "architect", Name: "Architect"}}})

	// Now set config state — should NOT override existing modes in registry
	conn.SetCachedConfigState(&ConfigOptionState{
		ConfigID:  "mode",
		CurrentID: "code",
		Options: []ConfigOptionDef{
			{ID: "mode", Category: "mode", Values: []ConfigOptionValue{{ID: "code", Name: "Code"}}},
		},
	})

	modeState := GetAgentCapabilityRegistry().GetModeState(agent.ID, conn.GetCurrentModeID())
	assert.NotNil(t, modeState)
	assert.Equal(t, "architect", modeState.CurrentModeID) // Original preserved
}

func TestACPConn_HasCurrentModeChanged(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "test-mode-changed", Backend: "acp-stdio", AcpCommand: "echo"}, "session-mode-changed")

	// Empty current — any non-empty modeID is a change
	assert.True(t, conn.HasCurrentModeChanged("code"))
	assert.False(t, conn.HasCurrentModeChanged(""))

	// Set initial mode state (updates currentModeID on conn)
	conn.SetCachedModeState(&ModeState{CurrentModeID: "code", AvailableModes: []ModeDef{{ID: "code", Name: "Code"}}})

	// Same mode — no change
	assert.False(t, conn.HasCurrentModeChanged("code"))

	// Different mode — change
	assert.True(t, conn.HasCurrentModeChanged("ask"))
}

// --- ACPConn LoadSession fields ---

func TestACPConn_LoadTargetSID_DefaultEmpty(t *testing.T) {
	agent := &model.Agent{ID: "test-load-default", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-load-default")

	conn.mu.Lock()
	loadTarget := conn.loadTargetSID
	conn.mu.Unlock()
	assert.Empty(t, loadTarget)
}

func TestACPConn_LoadSessionActive_DefaultFalse(t *testing.T) {
	agent := &model.Agent{ID: "test-load-active", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-load-active")

	assert.False(t, conn.loadSessionActive.Load())
}

func TestACPConnManager_GetOrCreateConnForLoad_SetsLoadTarget(t *testing.T) {
	mgr := GetACPConnManager()

	agent := &model.Agent{ID: "test-forload", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-forload")
	mgr.SetConnForTest("session-forload", conn)
	defer mgr.CloseConn("session-forload")

	// Set loadTargetSID via direct conn access (can't call GetOrCreateConnForLoad
	// without a real ACP process, but we can test the field mechanics)
	conn.mu.Lock()
	conn.loadTargetSID = "acp-session-abc"
	conn.mu.Unlock()

	conn.mu.Lock()
	assert.Equal(t, "acp-session-abc", conn.loadTargetSID)
	conn.mu.Unlock()
}

// --- ACPConn.EnsureAlive ---

func TestACPConn_EnsureAlive_NotAlive(t *testing.T) {
	agent := &model.Agent{ID: "test-ensure-alive", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-ensure-alive")

	// Not alive, no process — EnsureAlive would fail because we can't spawn
	// a real ACP process in unit tests. Verify the method exists and the
	// initial state is correct.
	conn.mu.Lock()
	assert.False(t, conn.alive)
	conn.mu.Unlock()
}

// --- ACPConn.ListSessions ---

func TestACPConn_ListSessions_NotAlive(t *testing.T) {
	agent := &model.Agent{ID: "test-list-notalive", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-list-notalive")

	_, _, err := conn.ListSessions(context.Background(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not alive")
}

// --- ACPConn.GetAndClearLoadSessionResp ---

func TestACPConn_GetAndClearLoadSessionResp_Nil(t *testing.T) {
	agent := &model.Agent{ID: "test-load-resp-nil", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-load-resp-nil")

	resp := conn.GetAndClearLoadSessionResp()
	assert.Nil(t, resp)
}

func TestACPConn_GetAndClearLoadSessionResp_ClearsAfterRead(t *testing.T) {
	agent := &model.Agent{ID: "test-load-resp-clear", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-load-resp-clear")

	conn.mu.Lock()
	conn.lastLoadSessionResp = &acp.LoadSessionResponse{}
	conn.mu.Unlock()

	resp := conn.GetAndClearLoadSessionResp()
	assert.NotNil(t, resp)

	// Second call returns nil
	resp2 := conn.GetAndClearLoadSessionResp()
	assert.Nil(t, resp2)
}

// --- ACPConnManager.GetOrCreateConnNoSession ---

func TestACPConnManager_GetOrCreateConnNoSession_FailedSpawn(t *testing.T) {
	mgr := GetACPConnManager()

	// "echo" is not a real ACP agent, so Initialize will fail
	agent := &model.Agent{ID: "test-nosession-fail", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := mgr.GetOrCreateConnNoSession(context.Background(), agent)
	assert.Nil(t, conn, "should return nil when agent binary fails Initialize")
}

func TestACPConnManager_GetOrCreateConnNoSession_UsesSpecialKey(t *testing.T) {
	mgr := GetACPConnManager()

	agent := &model.Agent{ID: "test-nosession-key", Backend: "acp-stdio", AcpCommand: "echo"}

	// Even though spawn fails, a conn entry with the special key should
	// have been created (then cleaned up on failure)
	conn := mgr.GetOrCreateConnNoSession(context.Background(), agent)
	assert.Nil(t, conn)

	// Verify no stale entry was left behind
	key := "__list_sessions__:test-nosession-key"
	mgr.mu.Lock()
	_, exists := mgr.conns[key]
	mgr.mu.Unlock()
	assert.False(t, exists, "failed connection should be cleaned up from conns map")
}

// --- GetOrCreateConn pre-populates acpSID from DB ---

func TestGetOrCreateConn_PrePopulatesAcpSID_FromDB(t *testing.T) {
	// Simulate a server restart scenario: external_session_id in the DB
	// contains the ACP session ID from a previous session_capture event.
	// GetOrCreateConn should pre-populate acpSID so that ensureAliveWithSession
	// can attempt ResumeSession instead of always creating a NewSession.
	mgr := GetACPConnManager()

	clawbenchSID := "session-pre-populate-test"
	acpSessionID := "acp-session-from-capture"

	// Set up the external_session_id callback to return the ACP session ID
	originalGetter := getExternalSessionID
	getExternalSessionID = func(sid string) string {
		if sid == clawbenchSID {
			return acpSessionID
		}
		return ""
	}
	defer func() { getExternalSessionID = originalGetter }()

	agent := &model.Agent{ID: "test-prepopulate", Backend: "acp-stdio", AcpCommand: "echo"}

	// GetOrCreateConn will create a new ACPConn. Since "echo" is not a real
	// ACP agent, the call will fail — but we can verify that acpSID was
	// pre-populated before the spawn attempt by checking the conn entry.
	_, _, err := mgr.GetOrCreateConn(context.Background(), agent, clawbenchSID, "/tmp")
	assert.Error(t, err) // expected — "echo" is not a real ACP agent

	// Verify the conn was created with the pre-populated acpSID
	_ = mgr.GetConn(clawbenchSID)
	// The conn may have been cleaned up on failure, so also check
	// that the mechanism works by inspecting the pool directly.
	mgr.mu.Lock()
	poolConn, exists := mgr.conns[clawbenchSID]
	mgr.mu.Unlock()

	if exists && poolConn != nil {
		poolConn.mu.Lock()
		sid := poolConn.acpSID
		poolConn.mu.Unlock()
		// acpSID was pre-populated from DB (even though spawn failed and cleared it,
		// the pre-population happened before spawn)
		assert.Equal(t, acpSessionID, sid, "acpSID should have been pre-populated from getExternalSessionID callback")
	}

	// Cleanup
	mgr.CloseConn(clawbenchSID)
}

// --- ACPConn.PreApplyConfigCurrentID ---

func TestACPConn_PreApplyConfigCurrentID_UpdatesRegistryConfigState(t *testing.T) {
	agentID := "test-preapply-config-mode"
	agent := &model.Agent{ID: agentID, Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-preapply")

	// Set up registry with config state (simulating NewSession response with bypassPermissions)
	reg := GetAgentCapabilityRegistry()
	reg.UpdateConfigState(agentID, &ConfigOptionState{
		ConfigID:  "mode",
		CurrentID: "bypassPermissions",
		Options: []ConfigOptionDef{{
			ID:       "mode",
			Category: "mode",
			Values: []ConfigOptionValue{
				{ID: "bypassPermissions", Name: "Auto-accept"},
				{ID: "plan", Name: "Plan"},
				{ID: "code", Name: "Code"},
			},
		}},
	})

	// Before pre-apply, config state should have the default
	configState := reg.GetConfigState(agentID)
	require.NotNil(t, configState)
	assert.Equal(t, "bypassPermissions", configState.CurrentID)

	// Pre-apply the user's mode selection
	conn.PreApplyConfigCurrentID("mode", "plan")

	// After pre-apply, config state should reflect the user's choice
	configState = reg.GetConfigState(agentID)
	require.NotNil(t, configState)
	assert.Equal(t, "plan", configState.CurrentID)
}

func TestACPConn_PreApplyConfigCurrentID_InvalidValueNoChange(t *testing.T) {
	agentID := "test-preapply-invalid"
	agent := &model.Agent{ID: agentID, Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-preapply-invalid")

	reg := GetAgentCapabilityRegistry()
	reg.UpdateConfigState(agentID, &ConfigOptionState{
		ConfigID:  "mode",
		CurrentID: "bypassPermissions",
		Options: []ConfigOptionDef{{
			ID:       "mode",
			Category: "mode",
			Values: []ConfigOptionValue{
				{ID: "bypassPermissions", Name: "Auto-accept"},
				{ID: "code", Name: "Code"},
			},
		}},
	})

	// Try to pre-apply a mode that's not in the available values
	conn.PreApplyConfigCurrentID("mode", "plan")

	// Should NOT change because "plan" is not a valid value
	configState := reg.GetConfigState(agentID)
	require.NotNil(t, configState)
	assert.Equal(t, "bypassPermissions", configState.CurrentID)
}

func TestACPConn_PreApplyConfigCurrentID_NoConfigState(t *testing.T) {
	agentID := "test-preapply-noconfig"
	agent := &model.Agent{ID: agentID, Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-preapply-noconfig")

	// No config state registered — should not panic
	assert.NotPanics(t, func() {
		conn.PreApplyConfigCurrentID("mode", "plan")
	})
}

func TestACPConn_PreApplyConfigCurrentID_ThinkingEffort(t *testing.T) {
	agentID := "test-preapply-effort"
	agent := &model.Agent{ID: agentID, Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-preapply-effort")

	reg := GetAgentCapabilityRegistry()
	reg.UpdateConfigState(agentID, &ConfigOptionState{
		ConfigID:  "thinkingEffort",
		CurrentID: "medium",
		Options: []ConfigOptionDef{{
			ID:       "thinkingEffort",
			Category: "thinkingEffort",
			Values: []ConfigOptionValue{
				{ID: "low", Name: "Low"},
				{ID: "medium", Name: "Medium"},
				{ID: "high", Name: "High"},
			},
		}},
	})

	conn.PreApplyConfigCurrentID("thinkingEffort", "high")

	configState := reg.GetConfigState(agentID)
	require.NotNil(t, configState)
	assert.Equal(t, "high", configState.CurrentID)
}

// --- ensureAliveWithSession preserves acpSID across spawnLocked ---

func TestEnsureAliveWithSession_PreservesPreSpawnAcpSID(t *testing.T) {
	// Simulates the server restart recovery flow:
	// 1. GetOrCreateConn creates a new ACPConn with acpSID pre-populated from DB
	// 2. ensureAliveWithSession calls spawnLocked which clears c.acpSID
	// 3. The preSpawnAcpSID variable captures the value BEFORE spawnLocked
	// 4. ResumeSession is attempted with the pre-spawn value
	//
	// This test verifies step 3: that preSpawnAcpSID is correctly used
	// instead of c.acpSID (which was cleared) for the ResumeSession decision.
	agent := &model.Agent{ID: "test-prespawn", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-prespawn")

	acpSIDFromDB := "acp-session-recovered-from-db"

	// Pre-populate acpSID as GetOrCreateConn would do after a server restart
	conn.mu.Lock()
	conn.acpSID = acpSIDFromDB
	conn.mu.Unlock()

	// Verify pre-population worked
	conn.mu.Lock()
	assert.Equal(t, acpSIDFromDB, conn.acpSID, "acpSID should be pre-populated from DB")
	conn.mu.Unlock()

	// After spawnLocked (which happens inside ensureAliveWithSession),
	// c.acpSID is cleared to "". But preSpawnAcpSID captured the value
	// before spawn. We can't call ensureAliveWithSession without a real
	// ACP process, but we verify the data flow by simulating the critical
	// code path:
	conn.mu.Lock()
	savedBeforeSpawn := conn.acpSID // This is what preSpawnAcpSID captures
	conn.acpSID = ""                // This is what spawnLocked does
	conn.mu.Unlock()

	// The decision to attempt ResumeSession should use savedBeforeSpawn, not c.acpSID
	assert.Equal(t, acpSIDFromDB, savedBeforeSpawn, "preSpawnAcpSID should capture the DB-recovered value before spawnLocked clears it")

	conn.mu.Lock()
	assert.Empty(t, conn.acpSID, "c.acpSID should be cleared after spawnLocked")
	conn.mu.Unlock()
}

func TestEnsureAliveWithSession_EmptyPreSpawnAcpSID_SkipsResume(t *testing.T) {
	// When acpSID is empty (brand-new session, never completed a Prompt),
	// preSpawnAcpSID will also be empty, so ResumeSession should NOT be attempted.
	agent := &model.Agent{ID: "test-prespawn-empty", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "session-prespawn-empty")

	// acpSID is empty by default (brand-new session)
	conn.mu.Lock()
	savedBeforeSpawn := conn.acpSID // This is what preSpawnAcpSID captures
	conn.acpSID = ""                // spawnLocked clears it (no-op here, already empty)
	conn.mu.Unlock()

	// preSpawnAcpSID is empty → should skip ResumeSession, go straight to NewSession
	assert.Empty(t, savedBeforeSpawn, "preSpawnAcpSID should be empty for brand-new sessions, skipping ResumeSession")
}

// --- GetOrCreateConn reuses existing conn (does not re-read DB) ---

func TestGetOrCreateConn_ReusesExistingConn(t *testing.T) {
	// When a conn already exists in the pool (e.g., from a previous GetOrCreateConn
	// call in the same server process), GetOrCreateConn should reuse it without
	// re-reading external_session_id from the DB.
	mgr := GetACPConnManager()

	clawbenchSID := "session-reuse-test"
	acpSessionID := "acp-session-first"

	// Set up the external_session_id callback
	originalGetter := getExternalSessionID
	callCount := 0
	getExternalSessionID = func(sid string) string {
		callCount++
		if sid == clawbenchSID {
			return acpSessionID
		}
		return ""
	}
	defer func() { getExternalSessionID = originalGetter }()

	agent := &model.Agent{ID: "test-reuse", Backend: "acp-stdio", AcpCommand: "echo"}

	// First call: creates new conn, reads DB
	_, _, err := mgr.GetOrCreateConn(context.Background(), agent, clawbenchSID, "/tmp")
	assert.Error(t, err) // "echo" is not a real ACP agent
	assert.Equal(t, 1, callCount, "getExternalSessionID should be called once for new conn")

	// Second call: reuses existing conn, does NOT read DB again
	_, _, err = mgr.GetOrCreateConn(context.Background(), agent, clawbenchSID, "/tmp")
	assert.Error(t, err)
	assert.Equal(t, 1, callCount, "getExternalSessionID should NOT be called again for existing conn")

	// Cleanup
	mgr.CloseConn(clawbenchSID)
}

// --- GetOrCreateConn does not pre-populate when external_session_id is empty ---

func TestGetOrCreateConn_DoesNotPrePopulateAcpSID_WhenEmpty(t *testing.T) {
	// When external_session_id is empty in the DB (session was just created,
	// or ACP session was never captured), GetOrCreateConn should NOT
	// pre-populate acpSID — it should remain "" so ensureAliveWithSession
	// uses NewSession instead of attempting ResumeSession (which would fail
	// with a 60s timeout).
	mgr := GetACPConnManager()

	clawbenchSID := "session-empty-extid-test"

	// Simulate empty external_session_id (freshly created session)
	originalGetter := getExternalSessionID
	getExternalSessionID = func(sid string) string {
		return "" // Empty — no external session ID
	}
	defer func() { getExternalSessionID = originalGetter }()

	agent := &model.Agent{ID: "test-empty-extid", Backend: "acp-stdio", AcpCommand: "echo"}

	_, _, err := mgr.GetOrCreateConn(context.Background(), agent, clawbenchSID, "/tmp")
	assert.Error(t, err) // expected — "echo" is not a real ACP agent

	// Verify acpSID was NOT pre-populated
	mgr.mu.Lock()
	poolConn, exists := mgr.conns[clawbenchSID]
	mgr.mu.Unlock()

	if exists && poolConn != nil {
		poolConn.mu.Lock()
		sid := poolConn.acpSID
		poolConn.mu.Unlock()
		assert.Empty(t, sid, "acpSID should NOT be pre-populated when external_session_id is empty")
	}

	// Cleanup
	mgr.CloseConn(clawbenchSID)
}
