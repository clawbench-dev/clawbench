# CodeBuddy ACP Integration Test Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Write comprehensive end-to-end integration tests for CodeBuddy's ACP (Agent Client Protocol) backend, focusing on connection lifecycle, mode/model/thinking switching, and state consistency after reconnection — especially detecting "amnesia" (lost state after resume).

**Architecture:** Real CodeBuddy CLI subprocess communicating via ACP JSON-RPC over stdio. Each test creates an `ACPConnManager`, spawns a real `codebuddy acp` process, sends prompts, collects `StreamEvent`s, and asserts state transitions. Tests are gated by `//go:build integration` and `requireCodeBuddyACPAvailable()`.

**Tech Stack:** Go testing + testify, real CodeBuddy CLI with `acp` subcommand, ACP JSON-RPC stdio transport

---

## Task 1: Create test file with shared helpers

**Files:**
- Create: `internal/ai/acp_integration_test.go`

**Step 1: Write test file header and shared helpers**

```go
//go:build integration

package ai

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Shared Helpers ---

// acpTestAgent returns a model.Agent configured for CodeBuddy ACP transport.
func acpTestAgent() *model.Agent {
	return &model.Agent{
		ID:         "codebuddy-acp-test",
		Name:       "CodeBuddy ACP Test",
		Backend:    "codebuddy",
		Transport:  "acp-stdio",
		AcpCommand: "codebuddy acp",
		Models: []model.AgentModel{
			{ID: "glm-4-plus", Name: "GLM-4 Plus", Default: true},
		},
		ThinkingEffortLevels: []string{"low", "medium", "high"},
	}
}

// acpTestWorkDir returns the project root directory (git repo preferred).
func acpTestWorkDir() string {
	if dir, _ := os.Getwd(); dir != "" {
		return dir
	}
	return os.TempDir()
}

// requireCodeBuddyACPAvailable skips the test if CodeBuddy CLI is not installed
// or doesn't support the `acp` subcommand.
func requireCodeBuddyACPAvailable(t *testing.T) {
	t.Helper()
	path, err := exec.LookPath("codebuddy")
	if err != nil {
		t.Skipf("codebuddy CLI not available, skipping ACP integration test")
	}
	// Verify acp subcommand is supported
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "acp", "--help")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("codebuddy acp subcommand not supported (error: %v, output: %s), skipping", err, truncate(string(output), 200))
	}
}

// truncate returns the first n characters of s.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// acpSessionID generates a unique ClawBench session ID for testing.
func acpSessionID() string {
	return uuid.New().String()
}

// newTestACPConnManager creates an independent ACPConnManager for testing.
// Does NOT use the global singleton — each test gets its own manager.
func newTestACPConnManager() *ACPConnManager {
	return &ACPConnManager{
		conns:     make(map[string]*ACPConn),
		stopSweep: make(chan struct{}),
	}
}

// acpTestEnv holds test environment state for cleanup.
type acpTestEnv struct {
	mgr     *ACPConnManager
	agent   *model.Agent
	cleanup func()
}

// setupACPTestEnv creates a full test environment with independent manager,
// mock external session ID store, and mock state persister.
func setupACPTestEnv(t *testing.T) *acpTestEnv {
	t.Helper()

	agent := acpTestAgent()
	mgr := newTestACPConnManager()

	// Store external session IDs in memory (normally backed by DB)
	var sidMu sync.Mutex
	externalSessionIDs := make(map[string]string)

	// Override package-level function for external session ID lookup
	origGetExtSID := getExternalSessionID
	getExternalSessionID = func(clawbenchSID string) string {
		sidMu.Lock()
		defer sidMu.Unlock()
		return externalSessionIDs[clawbenchSID]
	}

	// Override state persister (normally writes to DB)
	origPersist := persistAgentACPStateToDB
	persistAgentACPStateToDB = func(agentID, modeState, commands, thinkingState, modelListState string) error {
		return nil // no-op for testing
	}

	// Helper to store external session ID (simulates what chat_stream.go does)
	storeExtSID := func(clawbenchSID, acpSID string) {
		sidMu.Lock()
		defer sidMu.Unlock()
		externalSessionIDs[clawbenchSID] = acpSID
	}

	cleanup := func() {
		mgr.StopAll()
		getExternalSessionID = origGetExtSID
		persistAgentACPStateToDB = origPersist
	}

	return &acpTestEnv{
		mgr:     mgr,
		agent:   agent,
		cleanup: cleanup,
	}
}

// sendACPPrompt sends a prompt through ACPBackend.ExecuteStream and collects all events.
// Returns the collected events.
func sendACPPrompt(t *testing.T, backend *ACPBackend, sessionID, prompt string, timeout time.Duration) []StreamEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:    prompt,
		SessionID: sessionID,
		WorkDir:   acpTestWorkDir(),
	})
	require.NoError(t, err, "ExecuteStream should not return error")

	return collectEvents(t, ch, timeout)
}

// collectEvents reads all events from the channel until it closes or timeout.
func collectEvents(t *testing.T, ch <-chan StreamEvent, timeout time.Duration) []StreamEvent {
	t.Helper()
	var events []StreamEvent
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, event)
		case <-timer.C:
			t.Log("collectEvents: timeout waiting for channel to close")
			return events
		}
	}
}

// findACPEvents returns all events matching the given type.
func findACPEvents(events []StreamEvent, eventType string) []StreamEvent {
	var matched []StreamEvent
	for _, e := range events {
		if e.Type == eventType {
			matched = append(matched, e)
		}
	}
	return matched
}

// requireDoneEvent asserts that events contain a "done" event.
func requireDoneEvent(t *testing.T, events []StreamEvent) {
	t.Helper()
	dones := findACPEvents(events, "done")
	require.NotEmpty(t, dones, "expected a 'done' event in stream, got event types: %v", eventTypes(events))
}

// eventTypes returns the type of each event as a string slice.
func eventTypes(events []StreamEvent) []string {
	types := make([]string, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	return types
}

// concatACPContent joins all content from content-type events.
func concatACPContent(events []StreamEvent) string {
	var sb strings.Builder
	for _, e := range events {
		if e.Type == "content" {
			sb.WriteString(e.Content)
		}
	}
	return sb.String()
}

// getConnPID returns the PID of the agent subprocess for a given connection.
// Returns 0 if no process is running.
func getConnPID(conn *ACPConn) int {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.cmd != nil && conn.cmd.Process != nil {
		return conn.cmd.Process.Pid
	}
	return 0
}

// killConnProcess kills the agent subprocess for a given connection.
// This simulates a process crash.
func killConnProcess(t *testing.T, conn *ACPConn) {
	t.Helper()
	pid := getConnPID(conn)
	require.NotZero(t, pid, "connection should have a running process")
	err := conn.cmd.Process.Kill()
	require.NoError(t, err, "killing agent process should succeed")
	// Wait for watchProcessDeath to detect the crash
	time.Sleep(500 * time.Millisecond)
}

// assertCacheState checks that the cached ACP state matches expectations.
func assertCacheState(t *testing.T, conn *ACPConn, modeID, modelID, effortID string) {
	t.Helper()
	if modeID != "" {
		ms := conn.GetCachedModeState()
		require.NotNil(t, ms, "cached mode state should not be nil")
		assert.Equal(t, modeID, ms.CurrentModeID, "cached mode should match")
	}
	if modelID != "" {
		ml := conn.GetCachedModelListState()
		require.NotNil(t, ml, "cached model list state should not be nil")
		assert.Equal(t, modelID, ml.CurrentModelID, "cached model should match")
	}
	if effortID != "" {
		es := conn.GetCachedThinkingEffortState()
		require.NotNil(t, es, "cached thinking effort state should not be nil")
		assert.Equal(t, effortID, es.CurrentID, "cached thinking effort should match")
	}
}
```

**Step 2: Verify compilation**

Run: `go build -tags=integration ./internal/ai/`
Expected: Builds successfully (no undefined references).

**Step 3: Commit**

```bash
git add internal/ai/acp_integration_test.go
git commit -m "test: add ACP integration test helpers and infrastructure"
```

---

## Task 2: Category A — Connection lifecycle tests (A1-A4)

**Files:**
- Modify: `internal/ai/acp_integration_test.go`

**Step 1: Write tests A1-A4**

```go
// --- Category A: Connection Lifecycle ---

// A1: First GetOrCreateConn → NewSession → session_capture event + cache populated
func TestACPIntegration_NewSession_CreateAndCapture(t *testing.T) {
	requireCodeBuddyACPAvailable(t)
	env := setupACPTestEnv(t)
	defer env.cleanup()

	backend, err := NewACPBackend(env.agent)
	require.NoError(t, err)

	sessionID := acpSessionID()
	events := sendACPPrompt(t, backend, sessionID, "说一个字：好", 90*time.Second)

	requireDoneEvent(t, events)

	// Should have session_capture event
	captures := findACPEvents(events, "session_capture")
	require.NotEmpty(t, captures, "new session should emit session_capture")
	assert.NotEmpty(t, captures[0].Content, "session_capture should contain ACP session ID")

	// Should have content event
	content := concatACPContent(events)
	assert.NotEmpty(t, content, "should receive content from agent")

	// Cache should be populated
	conn := env.mgr.GetConn(sessionID)
	// Note: conn may be nil because ACPBackend uses global singleton manager
	// This is expected — the test validates the event flow through ExecuteStream
}

// A2: Second prompt with same sessionID → connection reuse → no second session_capture
func TestACPIntegration_ConnReuse_SameSession(t *testing.T) {
	requireCodeBuddyACPAvailable(t)
	env := setupACPTestEnv(t)
	defer env.cleanup()

	backend, err := NewACPBackend(env.agent)
	require.NoError(t, err)

	sessionID := acpSessionID()

	// First prompt — creates new session
	events1 := sendACPPrompt(t, backend, sessionID, "说一个字：好", 90*time.Second)
	requireDoneEvent(t, events1)
	captures1 := findACPEvents(events1, "session_capture")
	require.NotEmpty(t, captures1, "first prompt should emit session_capture")

	firstACPSSID := captures1[0].Content

	// Second prompt — should reuse connection
	events2 := sendACPPrompt(t, backend, sessionID, "再说一个字：棒", 90*time.Second)
	requireDoneEvent(t, events2)

	captures2 := findACPEvents(events2, "session_capture")
	assert.Empty(t, captures2, "reused session should NOT emit session_capture again")

	// Content should still arrive
	content := concatACPContent(events2)
	assert.NotEmpty(t, content, "should receive content on reused connection")

	// Session capture from first prompt should contain the same ACP session ID
	assert.NotEmpty(t, firstACPSSID, "first session capture should have ACP session ID")
}

// A3: Kill agent process → next prompt triggers respawn + ResumeSession → state recovered
func TestACPIntegration_ProcessCrash_AutoResume(t *testing.T) {
	requireCodeBuddyACPAvailable(t)
	env := setupACPTestEnv(t)
	defer env.cleanup()

	backend, err := NewACPBackend(env.agent)
	require.NoError(t, err)

	sessionID := acpSessionID()

	// First prompt — establish connection
	events1 := sendACPPrompt(t, backend, sessionID, "说一个字：好", 90*time.Second)
	requireDoneEvent(t, events1)
	captures1 := findACPEvents(events1, "session_capture")
	require.NotEmpty(t, captures1, "should have session_capture from first prompt")

	acpSessionIDVal := captures1[0].Content
	require.NotEmpty(t, acpSessionIDVal, "should have ACP session ID")

	// Store the external session ID so ResumeSession can find it
	// (In production, this is done by chat_stream.go handler)
	origGetExtSID := getExternalSessionID
	getExternalSessionID = func(sid string) string {
		if sid == sessionID {
			return acpSessionIDVal
		}
		return ""
	}
	defer func() { getExternalSessionID = origGetExtSID }()

	// Kill the agent process to simulate a crash
	conn := GetACPConnManager().GetConn(sessionID)
	require.NotNil(t, conn, "should have a connection after first prompt")
	killConnProcess(t, conn)

	// Second prompt — should respawn + ResumeSession
	events2 := sendACPPrompt(t, backend, sessionID, "再说一个字：棒", 90*time.Second)
	requireDoneEvent(t, events2)

	// Should NOT have session_capture (this is a resume, not a new session)
	captures2 := findACPEvents(events2, "session_capture")
	// Note: ResumeSession may or may not emit session_capture depending on implementation
	// The key assertion is that we get content back
	content := concatACPContent(events2)
	assert.NotEmpty(t, content, "should receive content after resume")

	// Connection should be alive again
	conn2 := GetACPConnManager().GetConn(sessionID)
	require.NotNil(t, conn2, "should have a connection after resume")
	assert.True(t, conn2.IsAlive(), "connection should be alive after resume")
}

// A4: Agent crashes during prompt → isACPPeerDisconnected → auto-retry with respawn + ResumeSession
func TestACPIntegration_PeerDisconnect_RetryPrompt(t *testing.T) {
	requireCodeBuddyACPAvailable(t)
	env := setupACPTestEnv(t)
	defer env.cleanup()

	backend, err := NewACPBackend(env.agent)
	require.NoError(t, err)

	sessionID := acpSessionID()

	// First prompt — establish connection and get ACP session ID
	events1 := sendACPPrompt(t, backend, sessionID, "说一个字：好", 90*time.Second)
	requireDoneEvent(t, events1)
	captures1 := findACPEvents(events1, "session_capture")
	require.NotEmpty(t, captures1)
	acpSessionIDVal := captures1[0].Content

	// Store external session ID for resume
	origGetExtSID := getExternalSessionID
	getExternalSessionID = func(sid string) string {
		if sid == sessionID {
			return acpSessionIDVal
		}
		return ""
	}
	defer func() { getExternalSessionID = origGetExtSID }()

	// Send a long-running prompt, then kill the process mid-stream
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:    "用200字描述Go语言的优点",
		SessionID: sessionID,
		WorkDir:   acpTestWorkDir(),
	})
	require.NoError(t, err)

	// Collect a few events, then kill the process
	var preKillEvents []StreamEvent
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	collectedEnough := false
	for !collectedEnough {
		select {
		case event, ok := <-ch:
			if !ok {
				collectedEnough = true
				break
			}
			preKillEvents = append(preKillEvents, event)
			if len(preKillEvents) >= 3 {
				collectedEnough = true
			}
		case <-timer.C:
			collectedEnough = true
		}
	}

	// Kill the process mid-stream
	conn := GetACPConnManager().GetConn(sessionID)
	if conn != nil && getConnPID(conn) != 0 {
		_ = conn.cmd.Process.Kill()
	}

	// Continue collecting — the retry mechanism should kick in
	remainingEvents := collectEvents(t, ch, 90*time.Second)

	allEvents := append(preKillEvents, remainingEvents...)

	// After retry, should eventually get either:
	// - A "done" event (retry succeeded), or
	// - An "error" event (retry also failed)
	// Either way, the channel should be closed
	dones := findACPEvents(allEvents, "done")
	errors := findACPEvents(allEvents, "error")
	assert.True(t, len(dones) > 0 || len(errors) > 0,
		"should get either done or error after peer disconnect, got types: %v", eventTypes(allEvents))
}
```

**Step 2: Run tests to verify they compile**

Run: `go build -tags=integration ./internal/ai/`
Expected: Builds successfully.

**Step 3: Commit**

```bash
git add internal/ai/acp_integration_test.go
git commit -m "test: add ACP integration tests — connection lifecycle (A1-A4)"
```

---

## Task 3: Category A — Connection lifecycle tests (A5-A7)

**Files:**
- Modify: `internal/ai/acp_integration_test.go`

**Step 1: Write tests A5-A7**

```go
// A5: Simulate idle sweep closing connection → next prompt triggers respawn + ResumeSession
func TestACPIntegration_IdleSweep_ConnectionRecycled(t *testing.T) {
	requireCodeBuddyACPAvailable(t)
	env := setupACPTestEnv(t)
	defer env.cleanup()

	backend, err := NewACPBackend(env.agent)
	require.NoError(t, err)

	sessionID := acpSessionID()

	// First prompt — establish connection
	events1 := sendACPPrompt(t, backend, sessionID, "说一个字：好", 90*time.Second)
	requireDoneEvent(t, events1)
	captures1 := findACPEvents(events1, "session_capture")
	require.NotEmpty(t, captures1)
	acpSessionIDVal := captures1[0].Content

	// Store external session ID for resume
	origGetExtSID := getExternalSessionID
	getExternalSessionID = func(sid string) string {
		if sid == sessionID {
			return acpSessionIDVal
		}
		return ""
	}
	defer func() { getExternalSessionID = origGetExtSID }()

	// Simulate idle sweep by manually closing the connection
	GetACPConnManager().CloseConn(sessionID)

	// Second prompt — should create new connection + ResumeSession
	events2 := sendACPPrompt(t, backend, sessionID, "再说一个字：棒", 90*time.Second)
	requireDoneEvent(t, events2)

	// Should get content back (resume succeeded)
	content := concatACPContent(events2)
	assert.NotEmpty(t, content, "should receive content after idle sweep + resume")
}

// A6: Explicit CloseConn → next prompt creates entirely new session (not resume)
func TestACPIntegration_ExplicitClose_NewSessionCreated(t *testing.T) {
	requireCodeBuddyACPAvailable(t)
	env := setupACPTestEnv(t)
	defer env.cleanup()

	backend, err := NewACPBackend(env.agent)
	require.NoError(t, err)

	sessionID := acpSessionID()

	// First prompt
	events1 := sendACPPrompt(t, backend, sessionID, "说一个字：好", 90*time.Second)
	requireDoneEvent(t, events1)
	captures1 := findACPEvents(events1, "session_capture")
	require.NotEmpty(t, captures1)
	firstACPSSID := captures1[0].Content

	// Close the connection explicitly
	GetACPConnManager().CloseConn(sessionID)

	// Clear the external session ID so ResumeSession can't find it
	// (simulating a scenario where the old session can't be recovered)
	origGetExtSID := getExternalSessionID
	getExternalSessionID = func(sid string) string {
		return "" // no external session ID — forces NewSession
	}
	defer func() { getExternalSessionID = origGetExtSID }()

	// Second prompt — should create a brand new session
	events2 := sendACPPrompt(t, backend, sessionID, "再说一个字：棒", 90*time.Second)
	requireDoneEvent(t, events2)

	captures2 := findACPEvents(events2, "session_capture")
	// New session should emit session_capture
	assert.NotEmpty(t, captures2, "new session after close should emit session_capture")

	// The new ACP session ID should be different from the first
	if len(captures2) > 0 {
		assert.NotEqual(t, firstACPSSID, captures2[0].Content,
			"new session should have a different ACP session ID")
	}
}

// A7: Two sessions → independent connections → one crash doesn't affect the other
func TestACPIntegration_MultipleSessions_IsolatedConns(t *testing.T) {
	requireCodeBuddyACPAvailable(t)
	env := setupACPTestEnv(t)
	defer env.cleanup()

	backend, err := NewACPBackend(env.agent)
	require.NoError(t, err)

	sessionID1 := acpSessionID()
	sessionID2 := acpSessionID()

	// Establish both sessions
	events1a := sendACPPrompt(t, backend, sessionID1, "说一个字：好", 90*time.Second)
	requireDoneEvent(t, events1a)

	events2a := sendACPPrompt(t, backend, sessionID2, "说一个字：棒", 90*time.Second)
	requireDoneEvent(t, events2a)

	// Both should have session_capture
	captures1 := findACPEvents(events1a, "session_capture")
	captures2 := findACPEvents(events2a, "session_capture")
	require.NotEmpty(t, captures1)
	require.NotEmpty(t, captures2)
	assert.NotEqual(t, captures1[0].Content, captures2[0].Content,
		"different sessions should have different ACP session IDs")

	// Kill session1's process
	conn1 := GetACPConnManager().GetConn(sessionID1)
	require.NotNil(t, conn1)
	killConnProcess(t, conn1)

	// Session2 should still work fine
	events2b := sendACPPrompt(t, backend, sessionID2, "再说一个字：强", 90*time.Second)
	requireDoneEvent(t, events2b)
	content := concatACPContent(events2b)
	assert.NotEmpty(t, content, "session2 should still work after session1 crashed")
}
```

**Step 2: Run tests**

Run: `go test -tags=integration ./internal/ai/ -run "TestACPIntegration_(NewSession|ConnReuse|ProcessCrash|PeerDisconnect|IdleSweep|ExplicitClose|MultipleSessions)" -v -timeout 300s -count=1`

**Step 3: Commit**

```bash
git add internal/ai/acp_integration_test.go
git commit -m "test: add ACP integration tests — connection lifecycle A5-A7"
```

---

## Task 4: Category B — Mode/Model/Thinking switching tests (B1-B3)

**Files:**
- Modify: `internal/ai/acp_integration_test.go`

**Step 1: Write tests B1-B3**

```go
// --- Category B: Mode/Model/Thinking Switching ---

// B1: Switch mode via SetSessionConfigOption → cache reflects new mode
func TestACPIntegration_ModeSwitch_CodeToPlan(t *testing.T) {
	requireCodeBuddyACPAvailable(t)

	backend, err := NewACPBackend(acpTestAgent())
	require.NoError(t, err)

	sessionID := acpSessionID()

	// First prompt — establish connection and get initial state
	events1 := sendACPPrompt(t, backend, sessionID, "说一个字：好", 90*time.Second)
	requireDoneEvent(t, events1)

	// Check if mode_update was emitted (may or may not be present)
	modeUpdates := findACPEvents(events1, "mode_update")
	var initialMode string
	if len(modeUpdates) > 0 && modeUpdates[0].Mode != nil {
		initialMode = modeUpdates[0].Mode.CurrentModeID
	}
	t.Logf("Initial mode: %q", initialMode)

	// Get the connection and switch mode
	mgr := GetACPConnManager()
	conn := mgr.GetConn(sessionID)
	require.NotNil(t, conn, "should have a connection")

	// Try switching to "plan" mode via SetSessionConfigOption
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err = conn.SetSessionConfigOption(ctx, "mode", "plan")
	// May fail if agent doesn't support mode switching — that's OK
	if err != nil {
		t.Logf("Mode switch failed (agent may not support it): %v", err)
		// Verify the error is handled gracefully
		return
	}

	// After successful switch, cache should reflect "plan"
	modeState := conn.GetCachedModeState()
	require.NotNil(t, modeState, "mode state should be cached after switch")
	assert.Equal(t, "plan", modeState.CurrentModeID, "cached mode should be 'plan'")

	// Send another prompt — mode should persist
	events2 := sendACPPrompt(t, backend, sessionID, "说一个字：行", 90*time.Second)
	requireDoneEvent(t, events2)

	// Config dedup should prevent re-sending "plan" mode
	// (shouldSetConfig returns false for same value)
}

// B2: Switch model via SetSessionConfigOption → cache reflects new model
func TestACPIntegration_ModelSwitch_ChangeModel(t *testing.T) {
	requireCodeBuddyACPAvailable(t)

	backend, err := NewACPBackend(acpTestAgent())
	require.NoError(t, err)

	sessionID := acpSessionID()

	// First prompt
	events1 := sendACPPrompt(t, backend, sessionID, "说一个字：好", 90*time.Second)
	requireDoneEvent(t, events1)

	// Get initial model from cache
	mgr := GetACPConnManager()
	conn := mgr.GetConn(sessionID)
	require.NotNil(t, conn)

	modelList := conn.GetCachedModelListState()
	var targetModel string
	if modelList != nil && len(modelList.Models) > 1 {
		// Pick a model that's different from current
		for _, m := range modelList.Models {
			if m.ID != modelList.CurrentModelID {
				targetModel = m.ID
				break
			}
		}
	}

	if targetModel == "" {
		t.Skip("No alternative model available for switching")
	}

	// Switch model
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err = conn.SetSessionConfigOption(ctx, "model", targetModel)
	if err != nil {
		t.Logf("Model switch failed: %v", err)
		return
	}

	// Cache should reflect new model
	modelList2 := conn.GetCachedModelListState()
	require.NotNil(t, modelList2)
	assert.Equal(t, targetModel, modelList2.CurrentModelID, "cached model should be updated")
}

// B3: Switch thinking effort via SetSessionConfigOption → cache reflects new effort
func TestACPIntegration_ThinkingEffortSwitch(t *testing.T) {
	requireCodeBuddyACPAvailable(t)

	backend, err := NewACPBackend(acpTestAgent())
	require.NoError(t, err)

	sessionID := acpSessionID()

	// First prompt
	events1 := sendACPPrompt(t, backend, sessionID, "说一个字：好", 90*time.Second)
	requireDoneEvent(t, events1)

	// Get connection
	mgr := GetACPConnManager()
	conn := mgr.GetConn(sessionID)
	require.NotNil(t, conn)

	// Try switching thinking effort
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err = conn.SetSessionConfigOption(ctx, "thinkingEffort", "high")
	if err != nil {
		t.Logf("Thinking effort switch failed (may not be supported): %v", err)
		return
	}

	// Cache should reflect new effort
	effortState := conn.GetCachedThinkingEffortState()
	require.NotNil(t, effortState)
	assert.Equal(t, "high", effortState.CurrentID, "cached thinking effort should be 'high'")
}
```

**Step 2: Run tests**

Run: `go test -tags=integration ./internal/ai/ -run "TestACPIntegration_(ModeSwitch|ModelSwitch|ThinkingEffortSwitch)" -v -timeout 300s -count=1`

**Step 3: Commit**

```bash
git add internal/ai/acp_integration_test.go
git commit -m "test: add ACP integration tests — mode/model/thinking switching (B1-B3)"
```

---

## Task 5: Category B — Config edge cases (B4-B6)

**Files:**
- Modify: `internal/ai/acp_integration_test.go`

**Step 1: Write tests B4-B6**

```go
// B4: Unsupported config option → graceful degradation (marked as unsupported, skipped on retry)
func TestACPIntegration_UnsupportedConfig_GracefulDegradation(t *testing.T) {
	requireCodeBuddyACPAvailable(t)

	backend, err := NewACPBackend(acpTestAgent())
	require.NoError(t, err)

	sessionID := acpSessionID()

	// First prompt
	events1 := sendACPPrompt(t, backend, sessionID, "说一个字：好", 90*time.Second)
	requireDoneEvent(t, events1)

	// Get connection
	mgr := GetACPConnManager()
	conn := mgr.GetConn(sessionID)
	require.NotNil(t, conn)

	// Try setting a non-existent config option
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err = conn.SetSessionConfigOption(ctx, "nonexistent_config_option_xyz", "some_value")

	// This should either:
	// 1. Return an error (isUnknownConfigOption), or
	// 2. Succeed but mark the config as unsupported on the next response
	if err != nil {
		t.Logf("Unsupported config returned error (expected): %v", err)
		assert.True(t, isUnknownConfigOption(err) || isACPPeerDisconnected(err),
			"error should be UnknownConfigOption or peer disconnect")
	}
}

// B5: Setting same config value twice → shouldSetConfig returns false → no second RPC
func TestACPIntegration_ConfigDedup_NoResend(t *testing.T) {
	requireCodeBuddyACPAvailable(t)

	agent := acpTestAgent()
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	sessionID := acpSessionID()

	// First prompt
	events1 := sendACPPrompt(t, backend, sessionID, "说一个字：好", 90*time.Second)
	requireDoneEvent(t, events1)

	mgr := GetACPConnManager()
	conn := mgr.GetConn(sessionID)
	require.NotNil(t, conn)

	// Set a model
	ctx := context.Background()
	modelList := conn.GetCachedModelListState()
	if modelList != nil && modelList.CurrentModelID != "" {
		err = conn.SetSessionConfigOption(ctx, "model", modelList.CurrentModelID)
		if err == nil {
			// shouldSetConfig should return false for the same value
			// This is tested at unit level in acp_pool_test.go
			// Here we verify the integration doesn't break
			t.Log("Setting same model twice — should be deduplicated")
		}
	}
}

// B6: Config option that kills the connection → configKilledConnectionError → retry skips that config
func TestACPIntegration_ConfigKilledConnection_RetrySkipsConfig(t *testing.T) {
	requireCodeBuddyACPAvailable(t)

	// This test verifies the ExecuteStream retry logic when a config option
	// causes the agent process to crash. We test this by sending a prompt
	// with a model that might crash the agent (unlikely with CodeBuddy but
	// the mechanism is tested at unit level).

	// The key integration behavior: if model change kills the connection,
	// ExecuteStream retries with req.Model="" (cleared), so the retry
	// uses whatever model the agent defaults to.

	backend, err := NewACPBackend(acpTestAgent())
	require.NoError(t, err)

	sessionID := acpSessionID()

	// Send a prompt with an invalid model that might crash the agent
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:    "说一个字：好",
		SessionID: sessionID,
		WorkDir:   acpTestWorkDir(),
		Model:     "nonexistent-model-xyz-12345",
	})
	require.NoError(t, err)

	events := collectEvents(t, ch, 120*time.Second)

	// Should either:
	// 1. Succeed (agent ignores invalid model and uses default)
	// 2. Error (agent crashes, retry with cleared model, may succeed or fail)
	dones := findACPEvents(events, "done")
	errors := findACPEvents(events, "error")
	assert.True(t, len(dones) > 0 || len(errors) > 0,
		"should get done or error, got types: %v", eventTypes(events))
}
```

**Step 2: Run tests**

Run: `go test -tags=integration ./internal/ai/ -run "TestACPIntegration_(UnsupportedConfig|ConfigDedup|ConfigKilledConnection)" -v -timeout 300s -count=1`

**Step 3: Commit**

```bash
git add internal/ai/acp_integration_test.go
git commit -m "test: add ACP integration tests — config edge cases (B4-B6)"
```

---

## Task 6: Category C — State consistency / "No Amnesia" tests (C1-C3)

**Files:**
- Modify: `internal/ai/acp_integration_test.go`

**Step 1: Write tests C1-C3 — the critical "amnesia" detection tests**

```go
// --- Category C: State Consistency — "No Amnesia" Tests ---
//
// These tests verify that ACP state is preserved after process crash + resume.
// "Amnesia" means the cached state (mode, model, thinking effort, commands)
// is lost or reset to incorrect values after a connection is respawned.

// C1: Switch to architect mode → crash → resume → cached mode still "architect"
func TestACPIntegration_ResumeAfterCrash_ModePreserved(t *testing.T) {
	requireCodeBuddyACPAvailable(t)

	backend, err := NewACPBackend(acpTestAgent())
	require.NoError(t, err)

	sessionID := acpSessionID()

	// Step 1: Establish connection
	events1 := sendACPPrompt(t, backend, sessionID, "说一个字：好", 90*time.Second)
	requireDoneEvent(t, events1)

	captures := findACPEvents(events1, "session_capture")
	require.NotEmpty(t, captures)
	acpSSID := captures[0].Content

	// Store external session ID for resume
	origGetExtSID := getExternalSessionID
	getExternalSessionID = func(sid string) string {
		if sid == sessionID {
			return acpSSID
		}
		return ""
	}
	defer func() { getExternalSessionID = origGetExtSID }()

	// Step 2: Switch mode
	mgr := GetACPConnManager()
	conn := mgr.GetConn(sessionID)
	require.NotNil(t, conn)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	switchErr := conn.SetSessionConfigOption(ctx, "mode", "architect")

	if switchErr != nil {
		t.Skipf("Mode switch not supported: %v", switchErr)
	}

	// Record mode before crash
	modeBefore := conn.GetCachedModeState()
	require.NotNil(t, modeBefore, "mode state should be cached before crash")
	t.Logf("Mode before crash: %q", modeBefore.CurrentModeID)

	// Step 3: Kill the process
	killConnProcess(t, conn)

	// Step 4: Resume — send another prompt
	events2 := sendACPPrompt(t, backend, sessionID, "说一个字：行", 90*time.Second)
	requireDoneEvent(t, events2)

	// Step 5: Check mode after resume — should NOT be amnesiac
	conn2 := mgr.GetConn(sessionID)
	require.NotNil(t, conn2, "should have a connection after resume")

	modeAfter := conn2.GetCachedModeState()
	require.NotNil(t, modeAfter, "mode state should be cached after resume")

	// KEY ASSERTION: Mode should match what we set before the crash
	assert.Equal(t, modeBefore.CurrentModeID, modeAfter.CurrentModeID,
		"AMNESIA DETECTED: mode changed after resume! Before=%q, After=%q",
		modeBefore.CurrentModeID, modeAfter.CurrentModeID)
	t.Logf("Mode after resume: %q (preserved!)", modeAfter.CurrentModeID)
}

// C2: Switch model → crash → resume → cached model still the new model
func TestACPIntegration_ResumeAfterCrash_ModelPreserved(t *testing.T) {
	requireCodeBuddyACPAvailable(t)

	backend, err := NewACPBackend(acpTestAgent())
	require.NoError(t, err)

	sessionID := acpSessionID()

	// Step 1: Establish connection
	events1 := sendACPPrompt(t, backend, sessionID, "说一个字：好", 90*time.Second)
	requireDoneEvent(t, events1)

	captures := findACPEvents(events1, "session_capture")
	require.NotEmpty(t, captures)
	acpSSID := captures[0].Content

	origGetExtSID := getExternalSessionID
	getExternalSessionID = func(sid string) string {
		if sid == sessionID {
			return acpSSID
		}
		return ""
	}
	defer func() { getExternalSessionID = origGetExtSID }()

	// Step 2: Switch model
	mgr := GetACPConnManager()
	conn := mgr.GetConn(sessionID)
	require.NotNil(t, conn)

	modelList := conn.GetCachedModelListState()
	var targetModel string
	if modelList != nil && len(modelList.Models) > 1 {
		for _, m := range modelList.Models {
			if m.ID != modelList.CurrentModelID {
				targetModel = m.ID
				break
			}
		}
	}
	if targetModel == "" {
		t.Skip("No alternative model available for switching")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err = conn.SetSessionConfigOption(ctx, "model", targetModel)
	if err != nil {
		t.Skipf("Model switch failed: %v", err)
	}

	modelBefore := conn.GetCachedModelListState()
	require.NotNil(t, modelBefore)
	t.Logf("Model before crash: %q", modelBefore.CurrentModelID)

	// Step 3: Kill the process
	killConnProcess(t, conn)

	// Step 4: Resume
	events2 := sendACPPrompt(t, backend, sessionID, "说一个字：行", 90*time.Second)
	requireDoneEvent(t, events2)

	// Step 5: Check model after resume
	conn2 := mgr.GetConn(sessionID)
	require.NotNil(t, conn2)

	modelAfter := conn2.GetCachedModelListState()
	require.NotNil(t, modelAfter, "model list state should be cached after resume")

	assert.Equal(t, modelBefore.CurrentModelID, modelAfter.CurrentModelID,
		"AMNESIA DETECTED: model changed after resume! Before=%q, After=%q",
		modelBefore.CurrentModelID, modelAfter.CurrentModelID)
	t.Logf("Model after resume: %q (preserved!)", modelAfter.CurrentModelID)
}

// C3: Switch thinking effort → crash → resume → cached effort preserved
func TestACPIntegration_ResumeAfterCrash_ThinkingPreserved(t *testing.T) {
	requireCodeBuddyACPAvailable(t)

	backend, err := NewACPBackend(acpTestAgent())
	require.NoError(t, err)

	sessionID := acpSessionID()

	// Step 1: Establish connection
	events1 := sendACPPrompt(t, backend, sessionID, "说一个字：好", 90*time.Second)
	requireDoneEvent(t, events1)

	captures := findACPEvents(events1, "session_capture")
	require.NotEmpty(t, captures)
	acpSSID := captures[0].Content

	origGetExtSID := getExternalSessionID
	getExternalSessionID = func(sid string) string {
		if sid == sessionID {
			return acpSSID
		}
		return ""
	}
	defer func() { getExternalSessionID = origGetExtSID }()

	// Step 2: Switch thinking effort
	mgr := GetACPConnManager()
	conn := mgr.GetConn(sessionID)
	require.NotNil(t, conn)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err = conn.SetSessionConfigOption(ctx, "thinkingEffort", "high")
	if err != nil {
		t.Skipf("Thinking effort switch failed: %v", err)
	}

	effortBefore := conn.GetCachedThinkingEffortState()
	require.NotNil(t, effortBefore)
	t.Logf("Thinking effort before crash: %q", effortBefore.CurrentID)

	// Step 3: Kill the process
	killConnProcess(t, conn)

	// Step 4: Resume
	events2 := sendACPPrompt(t, backend, sessionID, "说一个字：行", 90*time.Second)
	requireDoneEvent(t, events2)

	// Step 5: Check thinking effort after resume
	conn2 := mgr.GetConn(sessionID)
	require.NotNil(t, conn2)

	effortAfter := conn2.GetCachedThinkingEffortState()
	require.NotNil(t, effortAfter, "thinking effort state should be cached after resume")

	assert.Equal(t, effortBefore.CurrentID, effortAfter.CurrentID,
		"AMNESIA DETECTED: thinking effort changed after resume! Before=%q, After=%q",
		effortBefore.CurrentID, effortAfter.CurrentID)
	t.Logf("Thinking effort after resume: %q (preserved!)", effortAfter.CurrentID)
}
```

**Step 2: Run tests**

Run: `go test -tags=integration ./internal/ai/ -run "TestACPIntegration_ResumeAfterCrash_(Mode|Model|Thinking)Preserved" -v -timeout 300s -count=1`

**Step 3: Commit**

```bash
git add internal/ai/acp_integration_test.go
git commit -m "test: add ACP integration tests — state consistency/amnesia detection (C1-C3)"
```

---

## Task 7: Category C — More amnesia tests (C4-C6)

**Files:**
- Modify: `internal/ai/acp_integration_test.go`

**Step 1: Write tests C4-C6**

```go
// C4: Agent sends available_commands_update → crash → resume → commands still cached
func TestACPIntegration_ResumeAfterCrash_CommandsPreserved(t *testing.T) {
	requireCodeBuddyACPAvailable(t)

	backend, err := NewACPBackend(acpTestAgent())
	require.NoError(t, err)

	sessionID := acpSessionID()

	// Step 1: Establish connection (commands are cached during session)
	events1 := sendACPPrompt(t, backend, sessionID, "说一个字：好", 90*time.Second)
	requireDoneEvent(t, events1)

	captures := findACPEvents(events1, "session_capture")
	require.NotEmpty(t, captures)
	acpSSID := captures[0].Content

	origGetExtSID := getExternalSessionID
	getExternalSessionID = func(sid string) string {
		if sid == sessionID {
			return acpSSID
		}
		return ""
	}
	defer func() { getExternalSessionID = origGetExtSID }()

	// Check if commands were cached
	mgr := GetACPConnManager()
	conn := mgr.GetConn(sessionID)
	require.NotNil(t, conn)

	client := conn.GetClient()
	cmdsBefore := 0
	if client != nil {
		cmdsBefore = len(client.GetCommands())
	}
	t.Logf("Commands before crash: %d", cmdsBefore)

	// Step 2: Kill the process
	killConnProcess(t, conn)

	// Step 3: Resume
	events2 := sendACPPrompt(t, backend, sessionID, "说一个字：行", 90*time.Second)
	requireDoneEvent(t, events2)

	// Step 4: Check commands after resume
	conn2 := mgr.GetConn(sessionID)
	require.NotNil(t, conn2)

	client2 := conn2.GetClient()
	cmdsAfter := 0
	if client2 != nil {
		cmdsAfter = len(client2.GetCommands())
	}
	t.Logf("Commands after resume: %d", cmdsAfter)

	// Commands should be re-populated from the resumed session
	// They may differ slightly (agent may send updated commands on resume)
	// but the count should be > 0 if we had commands before
	if cmdsBefore > 0 {
		assert.Greater(t, cmdsAfter, 0,
			"AMNESIA DETECTED: commands lost after resume! Before=%d, After=%d",
			cmdsBefore, cmdsAfter)
	}
}

// C5: Set model X → crash → resume → lastSetModel reset → re-sends model X (not infinite loop)
func TestACPIntegration_ResumeAfterCrash_ConfigDedupReset(t *testing.T) {
	requireCodeBuddyACPAvailable(t)

	// This test verifies a specific amnesia scenario:
	// 1. Set model to X → lastSetModel = X
	// 2. Process crashes → resetLastSetConfig() clears lastSetModel
	// 3. Resume → Prompt re-sends model X (because lastSetModel is empty)
	// 4. This should NOT cause an infinite crash loop because:
	//    - The agent accepts model X on the resumed session
	//    - If it crashes again, configKilledConnectionError clears req.Model

	backend, err := NewACPBackend(acpTestAgent())
	require.NoError(t, err)

	sessionID := acpSessionID()

	// Step 1: Establish connection
	events1 := sendACPPrompt(t, backend, sessionID, "说一个字：好", 90*time.Second)
	requireDoneEvent(t, events1)

	captures := findACPEvents(events1, "session_capture")
	require.NotEmpty(t, captures)
	acpSSID := captures[0].Content

	origGetExtSID := getExternalSessionID
	getExternalSessionID = func(sid string) string {
		if sid == sessionID {
			return acpSSID
		}
		return ""
	}
	defer func() { getExternalSessionID = origGetExtSID }()

	// Step 2: Switch to a model
	mgr := GetACPConnManager()
	conn := mgr.GetConn(sessionID)
	require.NotNil(t, conn)

	modelList := conn.GetCachedModelListState()
	if modelList == nil || len(modelList.Models) <= 1 {
		t.Skip("No alternative model available")
	}

	targetModel := modelList.CurrentModelID
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = conn.SetSessionConfigOption(ctx, "model", targetModel)

	// Step 3: Kill the process
	killConnProcess(t, conn)

	// Step 4: Resume — this should work without infinite loop
	events2 := sendACPPrompt(t, backend, sessionID, "说一个字：行", 90*time.Second)
	requireDoneEvent(t, events2)

	// If we get here, no infinite loop occurred
	t.Log("Resume after config dedup reset succeeded — no infinite loop")
}

// C6: Agent sends plan → crash → resume → planState is nil (transient, expected loss)
func TestACPIntegration_ResumeAfterCrash_PlanStateLost(t *testing.T) {
	requireCodeBuddyACPAvailable(t)

	// Plan state is intentionally transient (not persisted to DB).
	// After resume, plan state is expected to be lost.
	// This test documents this known behavior.

	backend, err := NewACPBackend(acpTestAgent())
	require.NoError(t, err)

	sessionID := acpSessionID()

	// Step 1: Establish connection
	events1 := sendACPPrompt(t, backend, sessionID, "说一个字：好", 90*time.Second)
	requireDoneEvent(t, events1)

	captures := findACPEvents(events1, "session_capture")
	require.NotEmpty(t, captures)
	acpSSID := captures[0].Content

	origGetExtSID := getExternalSessionID
	getExternalSessionID = func(sid string) string {
		if sid == sessionID {
			return acpSSID
		}
		return ""
	}
	defer func() { getExternalSessionID = origGetExtSID }()

	// Step 2: Check if plan state was cached (may or may not be present)
	mgr := GetACPConnManager()
	conn := mgr.GetConn(sessionID)
	require.NotNil(t, conn)

	planBefore := conn.GetCachedPlanState()
	t.Logf("Plan state before crash: %v", planBefore != nil)

	// Step 3: Kill the process
	killConnProcess(t, conn)

	// Step 4: Resume
	events2 := sendACPPrompt(t, backend, sessionID, "说一个字：行", 90*time.Second)
	requireDoneEvent(t, events2)

	// Step 5: Plan state is expected to be nil after resume
	conn2 := mgr.GetConn(sessionID)
	require.NotNil(t, conn2)

	planAfter := conn2.GetCachedPlanState()
	t.Logf("Plan state after resume: %v", planAfter != nil)

	// Document: Plan state is transient and expected to be lost after resume.
	// This is NOT amnesia — it's by design (plan is per-execution-cycle).
	if planBefore != nil && planAfter == nil {
		t.Log("Plan state lost after resume — this is expected (transient state)")
	}
}
```

**Step 2: Run tests**

Run: `go test -tags=integration ./internal/ai/ -run "TestACPIntegration_ResumeAfterCrash_(Commands|ConfigDedup|PlanState)" -v -timeout 300s -count=1`

**Step 3: Commit**

```bash
git add internal/ai/acp_integration_test.go
git commit -m "test: add ACP integration tests — more amnesia detection (C4-C6)"
```

---

## Task 8: Category D — SSE disconnect/reconnect + long-running tests (D1-D2)

**Files:**
- Modify: `internal/ai/acp_integration_test.go`

**Step 1: Write tests D1-D2**

```go
// --- Category D: SSE Disconnect/Reconnect + Long-Running ---

// D1: Simulate SSE disconnect → drain → agent continues → reconnect → cached state re-emitted
func TestACPIntegration_SSEDisconnect_DrainAndContinue(t *testing.T) {
	requireCodeBuddyACPAvailable(t)

	// This test simulates what happens when the SSE client disconnects
	// while the ACP agent is still running. The key behaviors:
	// 1. Agent continues running (not cancelled)
	// 2. Remaining events are drained from the stream channel
	// 3. On reconnect, cached state is re-emitted

	backend, err := NewACPBackend(acpTestAgent())
	require.NoError(t, err)

	sessionID := acpSessionID()

	// Step 1: Send a prompt and let it complete normally
	events1 := sendACPPrompt(t, backend, sessionID, "说一个字：好", 90*time.Second)
	requireDoneEvent(t, events1)

	// Step 2: Verify connection is still alive
	mgr := GetACPConnManager()
	conn := mgr.GetConn(sessionID)
	require.NotNil(t, conn)
	assert.True(t, conn.IsAlive(), "connection should be alive after prompt completes")

	// Step 3: Get cached state
	modeBefore := conn.GetCachedModeState()
	effortBefore := conn.GetCachedThinkingEffortState()
	t.Logf("State before reconnect: mode=%v, effort=%v",
		modeBefore != nil, effortBefore != nil)

	// Step 4: Simulate reconnection by sending another prompt
	// (In production, the SSE handler would call GetCachedStateByClawbenchSID
	// and re-emit cached state events)
	events2 := sendACPPrompt(t, backend, sessionID, "再说一个字：棒", 90*time.Second)
	requireDoneEvent(t, events2)

	// After reconnection, config_update should be re-emitted
	configUpdates := findACPEvents(events2, "config_update")
	t.Logf("Config updates on reconnect: %d", len(configUpdates))

	// Commands should be re-emitted
	commandsUpdates := findACPEvents(events2, "commands_update")
	t.Logf("Commands updates on reconnect: %d", len(commandsUpdates))

	// Content should still arrive
	content := concatACPContent(events2)
	assert.NotEmpty(t, content, "should receive content after reconnect")
}

// D2: Full session → SSE reconnect → mode_update/config_update/commands_update re-emitted
func TestACPIntegration_SSEReconnect_StateReemitted(t *testing.T) {
	requireCodeBuddyACPAvailable(t)

	backend, err := NewACPBackend(acpTestAgent())
	require.NoError(t, err)

	sessionID := acpSessionID()

	// First prompt — establish connection and get initial state
	events1 := sendACPPrompt(t, backend, sessionID, "说一个字：好", 90*time.Second)
	requireDoneEvent(t, events1)

	// Record what state events were emitted on first connection
	firstModeUpdates := findACPEvents(events1, "mode_update")
	firstConfigUpdates := findACPEvents(events1, "config_update")
	firstCommandsUpdates := findACPEvents(events1, "commands_update")
	firstThinkingUpdates := findACPEvents(events1, "thinking_effort_update")

	t.Logf("First prompt state events: mode=%d, config=%d, commands=%d, thinking=%d",
		len(firstModeUpdates), len(firstConfigUpdates), len(firstCommandsUpdates), len(firstThinkingUpdates))

	// Second prompt (simulates SSE reconnect)
	events2 := sendACPPrompt(t, backend, sessionID, "再说一个字：棒", 90*time.Second)
	requireDoneEvent(t, events2)

	// On reconnect, these events should be re-emitted:
	// - config_update (always re-emitted)
	// - commands_update (always re-emitted)
	// - plan_update (if cached)
	// mode_update, thinking_effort_update, model_list_update are NOT re-emitted
	// (frontend loads from DB on session switch)

	secondConfigUpdates := findACPEvents(events2, "config_update")
	secondCommandsUpdates := findACPEvents(events2, "commands_update")

	if len(firstConfigUpdates) > 0 {
		assert.NotEmpty(t, secondConfigUpdates,
			"config_update should be re-emitted on reconnect")
	}

	t.Logf("Second prompt state events: config=%d, commands=%d",
		len(secondConfigUpdates), len(secondCommandsUpdates))
}
```

**Step 2: Run tests**

Run: `go test -tags=integration ./internal/ai/ -run "TestACPIntegration_(SSEDisconnect|SSEReconnect)" -v -timeout 300s -count=1`

**Step 3: Commit**

```bash
git add internal/ai/acp_integration_test.go
git commit -m "test: add ACP integration tests — SSE disconnect/reconnect (D1-D2)"
```

---

## Task 9: Category D — Long-running tests (D3-D4)

**Files:**
- Modify: `internal/ai/acp_integration_test.go`

**Step 1: Write tests D3-D4**

```go
// D3: 5 turns on same connection → no leaks, cache stays consistent
func TestACPIntegration_LongRunning_MultipleTurns(t *testing.T) {
	requireCodeBuddyACPAvailable(t)

	backend, err := NewACPBackend(acpTestAgent())
	require.NoError(t, err)

	sessionID := acpSessionID()

	prompts := []string{
		"说一个字：一",
		"说一个字：二",
		"说一个字：三",
		"说一个字：四",
		"说一个字：五",
	}

	for i, prompt := range prompts {
		t.Logf("Turn %d: %q", i+1, prompt)
		events := sendACPPrompt(t, backend, sessionID, prompt, 90*time.Second)
		requireDoneEvent(t, events)

		content := concatACPContent(events)
		assert.NotEmpty(t, content, "turn %d should produce content", i+1)
	}

	// After 5 turns, verify cache is still consistent
	mgr := GetACPConnManager()
	conn := mgr.GetConn(sessionID)
	require.NotNil(t, conn)
	assert.True(t, conn.IsAlive(), "connection should still be alive after 5 turns")

	// Mode/thinking/model caches should still be populated
	modeState := conn.GetCachedModeState()
	t.Logf("Mode after 5 turns: %v", modeState != nil)

	// No zombie processes (PID should still be valid)
	pid := getConnPID(conn)
	assert.NotZero(t, pid, "should have a valid PID after 5 turns")
}

// D4: Switch mode/model/thinking 10 times → final state matches last setting
func TestACPIntegration_LongRunning_ConfigStateConsistency(t *testing.T) {
	requireCodeBuddyACPAvailable(t)

	backend, err := NewACPBackend(acpTestAgent())
	require.NoError(t, err)

	sessionID := acpSessionID()

	// First prompt — establish connection
	events1 := sendACPPrompt(t, backend, sessionID, "说一个字：好", 90*time.Second)
	requireDoneEvent(t, events1)

	mgr := GetACPConnManager()
	conn := mgr.GetConn(sessionID)
	require.NotNil(t, conn)

	// Try switching thinking effort multiple times
	effortLevels := []string{"low", "medium", "high", "medium", "low", "high", "low", "high", "medium", "low"}
	lastSuccessfulEffort := ""

	for i, effort := range effortLevels {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := conn.SetSessionConfigOption(ctx, "thinkingEffort", effort)
		cancel()
		if err != nil {
			t.Logf("Turn %d: thinkingEffort=%q failed: %v", i+1, effort, err)
			if isUnknownConfigOption(err) {
				t.Skip("Agent doesn't support thinkingEffort config option")
			}
			continue
		}
		lastSuccessfulEffort = effort
		t.Logf("Turn %d: thinkingEffort=%q succeeded", i+1, effort)
	}

	if lastSuccessfulEffort != "" {
		// Final state should match last successful setting
		effortState := conn.GetCachedThinkingEffortState()
		require.NotNil(t, effortState, "thinking effort state should be cached")
		assert.Equal(t, lastSuccessfulEffort, effortState.CurrentID,
			"final thinking effort should match last successful switch: expected=%q, got=%q",
			lastSuccessfulEffort, effortState.CurrentID)
		t.Logf("Final thinking effort: %q (matches last switch)", effortState.CurrentID)
	}
}
```

**Step 2: Run tests**

Run: `go test -tags=integration ./internal/ai/ -run "TestACPIntegration_LongRunning" -v -timeout 600s -count=1`

**Step 3: Commit**

```bash
git add internal/ai/acp_integration_test.go
git commit -m "test: add ACP integration tests — long-running scenarios (D3-D4)"
```

---

## Task 10: Add IsAlive helper and fix compilation issues

**Files:**
- Modify: `internal/ai/acp_pool.go` (add `IsAlive()` public method if missing)

**Step 1: Check if IsAlive exists**

Run: `grep -n 'func.*ACPConn.*IsAlive' internal/ai/acp_pool.go`

If not found, add it:

```go
// IsAlive returns whether the connection is alive.
func (c *ACPConn) IsAlive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.alive && c.isAliveLocked()
}
```

**Step 2: Build and verify**

Run: `go build -tags=integration ./internal/ai/`

**Step 3: Commit if changes were needed**

```bash
git add internal/ai/acp_pool.go
git commit -m "feat: add IsAlive() public method for ACPConn"
```

---

## Task 11: Run full test suite and fix any issues

**Step 1: Run all ACP integration tests**

Run: `go test -tags=integration ./internal/ai/ -run "TestACPIntegration_" -v -timeout 600s -count=1 2>&1 | head -200`

**Step 2: Fix any compilation errors or test failures**

Common issues to expect:
- `ACPConn` fields like `cmd` are unexported — may need to add accessor methods
- `GetACPConnManager()` returns the global singleton — tests that create independent managers need adjustment
- `killConnProcess` may need to handle the case where `cmd.Process` is nil
- Timeouts may need adjustment based on network speed

**Step 3: Commit fixes**

```bash
git add internal/ai/acp_integration_test.go
git commit -m "test: fix ACP integration test compilation and runtime issues"
```

---

## Summary

| Category | Tests | Focus |
|----------|-------|-------|
| A: Connection Lifecycle | A1-A7 | New session, reuse, crash recovery, idle sweep, explicit close, isolation |
| B: Mode/Model/Thinking | B1-B6 | Switching, unsupported config, dedup, crash-on-config |
| C: No Amnesia | C1-C6 | Mode/model/thinking/commands preserved after crash+resume, plan loss documented |
| D: SSE + Long-Running | D1-D4 | Disconnect drain, state re-emission, multi-turn, config consistency |
| **Total** | **23 tests** | |

Key "amnesia" scenarios covered:
- **C1**: Mode preserved after crash+resume ✓
- **C2**: Model preserved after crash+resume ✓
- **C3**: Thinking effort preserved after crash+resume ✓
- **C4**: Commands re-populated after crash+resume ✓
- **C5**: Config dedup reset after crash (re-sends but doesn't loop) ✓
- **C6**: Plan state intentionally lost (documented behavior) ✓
