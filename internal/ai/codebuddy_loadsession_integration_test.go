//go:build integration

package ai

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// ===========================================================================
// CodeBuddy LoadSession Support — Empirical Verification
// ===========================================================================
//
// Background (docs/spec/core/ai-backend.md): some ACP agents (e.g. CodeBuddy)
// report LoadSession=true in the ACP Initialize response but do NOT actually
// implement the session/load RPC. ClawBench therefore deliberately treats
// BackendSpec.ACPLoadSession (not the Initialize report) as the authoritative
// source of LoadSession capability.
//
// This test empirically verifies whether CodeBuddy truly supports LoadSession
// by calling the session/load RPC against the real CLI. It does NOT skip on
// failure — it reports the actual outcome so the BackendSpec flag stays accurate.

// codebuddyACPAgent returns a model.Agent configured for CodeBuddy ACP transport.
func codebuddyACPAgent() *model.Agent {
	return &model.Agent{
		ID:                   "codebuddy-acp-loadsession-test",
		Name:                 "CodeBuddy ACP LoadSession Test",
		Backend:              "codebuddy",
		Transport:            "acp-stdio",
		AcpCommand:           "codebuddy --acp",
		Models:                []model.AgentModel{{ID: "glm-4-plus", Name: "glm-4-plus", Default: true}},
		ThinkingEffortLevels: []string{"low", "medium", "high"},
	}
}

// requireCodebuddyACPAvailable skips the test if the CodeBuddy CLI is not installed.
func requireCodebuddyACPAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("codebuddy"); err != nil {
		t.Skip("codebuddy CLI not available, skipping CodeBuddy LoadSession integration test")
	}
}

// TestCodebuddyACP_LoadSession_Capability verifies whether CodeBuddy truly
// supports the session/load RPC. It:
//  1. Spawns a real CodeBuddy ACP process and creates a session
//  2. Captures the ACP session ID
//  3. Closes the connection and calls LoadSession (session/load) on the real CLI
//  4. Reports whether session/load succeeded or failed (and the error)
func TestCodebuddyACP_LoadSession_Capability(t *testing.T) {
	requireCodebuddyACPAvailable(t)

	agent := codebuddyACPAgent()
	env := setupACPTestEnvForAgent(t, agent)
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	// Step 1: Establish a real session with a prompt that plants a recallable fact.
	sessionID := acpSessionID()
	defer env.closeConn(t, sessionID)
	events := sendACPPrompt(t, backend, sessionID, "记住数字：927，只回复'好的'", 90*time.Second)
	requireDoneEvent(t, events)
	require.NotEmpty(t, concatACPContent(events), "should receive content from CodeBuddy")

	// Step 2: Capture the ACP session ID.
	acpSID := extractACPCaptureID(t, events)
	require.NotEmpty(t, acpSID, "should capture an ACP session ID")

	// Step 3: Close the connection so LoadSession spawns a fresh process.
	env.closeConn(t, sessionID)

	// Step 4: Call the real session/load RPC via GetOrCreateConnForLoad.
	loadSessionID := acpSessionID()
	defer env.closeConn(t, loadSessionID)
	ctx, cancel := contextWithTimeout(t, 90*time.Second)
	defer cancel()

	conn, err := env.mgr.GetOrCreateConnForLoad(ctx, agent, loadSessionID, acpSID, acpTestWorkDir())

	// Report the empirical result. Do NOT skip — the point is to surface the truth.
	if err != nil {
		t.Logf("RESULT: CodeBuddy session/load FAILED: %v", err)
		t.Logf("  → CodeBuddy does NOT support LoadSession (or it errored for session %q)", acpSID)
		t.Logf("  → BackendSpec.ACPLoadSession should be reverted to false for codebuddy")
		// LoadSession support is expected (verified empirically). A failure here
		// signals a regression worth surfacing loudly.
		require.NoError(t, err, "CodeBuddy session/load should succeed (it genuinely supports LoadSession)")
		return
	}
	require.NotNil(t, conn, "should have a connection after LoadSession")

	// session/load succeeded — read the replay buffer to confirm real replay.
	time.Sleep(500 * time.Millisecond)
	client := conn.GetClient()
	require.NotNil(t, client, "should have an ACP client after LoadSession")
	buf := client.GetAndClearLoadSessionBuf()

	// Step 5: Verify LoadSession FUNCTIONALLY restored context by asking the AI
	// to recall the planted fact on the freshly-loaded connection. If it recalls
	// "927", LoadSession genuinely restored the conversation — not just the RPC.
	events2 := sendACPPrompt(t, backend, loadSessionID, "我之前让你记住的数字是什么？只回答数字", 90*time.Second)
	requireDoneEvent(t, events2)
	content2 := concatACPContent(events2)

	t.Logf("RESULT: CodeBuddy session/load SUCCEEDED (replayed %d notifications)", len(buf))
	t.Logf("  → After LoadSession, recall response: %q", content2)
	if strings.Contains(content2, "927") {
		t.Logf("  → CodeBuddy LoadSession FUNCTIONALLY restores context (recalled 927)")
		t.Logf("  → BackendSpec.ACPLoadSession=true is CORRECT for codebuddy")
	} else {
		t.Logf("  → CodeBuddy session/load RPC succeeds but did NOT restore context (did not recall 927)")
		t.Logf("  → Check whether LoadSession truly restores context for the current codebuddy version")
	}
}

// ===========================================================================
// CodeBuddy ListSessions — Empirical Verification
// ===========================================================================
//
// Verifies whether CodeBuddy truly supports the session/list RPC. The
// Initialize SessionCapabilities.List report is read into the capability
// registry (unlike LoadSession, which is BackendSpec-driven). This test calls
// the real session/list RPC and reports whether it returns sessions.

// TestCodebuddyACP_ListSessions_Capability empirically verifies CodeBuddy's
// session/list support by:
//  1. Spawning a real CodeBuddy ACP process and creating a session
//  2. Calling ListSessions via the alive connection
//  3. Reporting the number of sessions returned and the List capability flag
func TestCodebuddyACP_ListSessions_Capability(t *testing.T) {
	requireCodebuddyACPAvailable(t)

	agent := codebuddyACPAgent()
	env := setupACPTestEnvForAgent(t, agent)
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	// Step 1: Establish a session so there is something to list.
	sessionID := acpSessionID()
	defer env.closeConn(t, sessionID)
	events := sendACPPrompt(t, backend, sessionID, "回复一个字：好", 90*time.Second)
	requireDoneEvent(t, events)
	require.NotEmpty(t, concatACPContent(events), "should receive content from CodeBuddy")

	// Step 2: Check the Initialize-driven ListSessions capability flag.
	reg := GetAgentCapabilityRegistry()
	listSessions := reg.GetListSessions(env.agent.ID)
	t.Logf("codebuddy Initialize SessionCapabilities.List = %v", listSessions)

	// Step 3: Call the real session/list RPC on the alive connection.
	conn := env.mgr.GetConn(sessionID)
	require.NotNil(t, conn, "should have a connection after prompt")

	ctx, cancel := contextWithTimeout(t, 30*time.Second)
	defer cancel()
	sessions, nextCursor, listErr := conn.ListSessions(ctx, nil)

	if listErr != nil {
		t.Logf("RESULT: CodeBuddy session/list RPC FAILED: %v", listErr)
		t.Logf("  → CodeBuddy does NOT support ListSessions (SessionCapabilities.List=false is CORRECT)")
		return
	}

	t.Logf("RESULT: CodeBuddy session/list SUCCEEDED (returned %d sessions, nextCursor=%v)", len(sessions), nextCursor)
	for i, s := range sessions {
		t.Logf("  session[%d]: id=%s cwd=%q", i, s.SessionId, s.Cwd)
	}

	// If the List capability flag is false but the RPC works, the Initialize
	// parse is wrong — surface it. Otherwise confirm the flag is accurate.
	if !listSessions {
		t.Logf("  → NOTE: Initialize reported List=false but session/list RPC works — Initialize parse may be inaccurate")
	} else {
		t.Logf("  → CodeBuddy supports ListSessions (flag=true matches RPC result)")
	}
}
