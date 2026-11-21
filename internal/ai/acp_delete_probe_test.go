package ai

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// probeConnForTest returns a live-looking connection whose raw channel is the
// given hook, so the probe can be exercised without an agent process.
func probeConnForTest(t *testing.T, hook func(ctx context.Context, method string, params any) (json.RawMessage, error)) *ACPConn {
	t.Helper()
	conn := newACPConn(&model.Agent{ID: "delete-probe"}, "delete-probe")
	conn.SetAliveForTest()
	conn.SetRawCallFnForTest(hook)
	return conn
}

// CodeBuddy implements session/delete without advertising it: the only way to
// learn that is to call it. An agent that refuses the made-up session ("not
// found") has clearly taken the call, so the method exists.
func TestProbeDeleteSession_SessionNotFoundMeansSupported(t *testing.T) {
	var gotMethod string
	var gotParams map[string]any
	conn := probeConnForTest(t, func(_ context.Context, method string, params any) (json.RawMessage, error) {
		gotMethod = method
		gotParams, _ = params.(map[string]any)
		return nil, &RawRPCError{Code: -32602, Message: "session not found"}
	})

	confirmed, answered := probeDeleteSession(context.Background(), conn)
	assert.Equal(t, "session/delete", gotMethod)
	assert.True(t, answered, "a JSON-RPC answer is an answer")
	assert.True(t, confirmed, "a refusal about the session proves the method exists")

	sid, _ := gotParams["sessionId"].(string)
	require.NotEmpty(t, sid)
	assert.Contains(t, sid, "__clawbench_capability_probe__",
		"the probe must not be able to delete a real session")
}

// Method not found is the definitive "no".
func TestProbeDeleteSession_MethodNotFoundMeansUnsupported(t *testing.T) {
	conn := probeConnForTest(t, func(_ context.Context, _ string, _ any) (json.RawMessage, error) {
		return nil, &RawRPCError{Code: -32601, Message: "method not found"}
	})

	confirmed, answered := probeDeleteSession(context.Background(), conn)
	assert.True(t, answered)
	assert.False(t, confirmed)
}

// No JSON-RPC answer at all (dead channel, spawn failure) tells us nothing
// about the agent, so it must not be remembered as "unsupported".
func TestProbeDeleteSession_TransportFailureIsNotAnAnswer(t *testing.T) {
	conn := probeConnForTest(t, func(_ context.Context, _ string, _ any) (json.RawMessage, error) {
		return nil, errRawConnClosed
	})

	_, answered := probeDeleteSession(context.Background(), conn)
	assert.False(t, answered)
}

// End to end: the probe result lands in the registry and is what the
// permanent-delete path reads — even though the advertisement said nothing.
func TestRefreshDeleteSessionCapability_CodeBuddyGetsDeleteSessionTrue(t *testing.T) {
	resetGlobalRegistryForTest(t)
	reg := GetAgentCapabilityRegistry()

	// What CodeBuddy's Initialize reports: no sessionCapabilities at all.
	reg.UpdateDeleteSession("cb-agent", false)
	require.False(t, reg.GetDeleteSession("cb-agent"))

	conn := probeConnForTest(t, func(_ context.Context, _ string, _ any) (json.RawMessage, error) {
		return nil, &RawRPCError{Code: -32602, Message: "session not found"}
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		refreshDeleteSessionCapability(context.Background(), conn, "cb-agent")
	}()
	<-done

	require.Eventually(t, func() bool { return reg.GetDeleteSession("cb-agent") },
		time.Second, 10*time.Millisecond,
		"the probe must override the (empty) advertisement")
	assert.False(t, reg.DeleteSessionProbePending("cb-agent"))
}

// An agent that really does not support the call must stay false — no false
// positives from the probe.
func TestRefreshDeleteSessionCapability_UnsupportedStaysFalse(t *testing.T) {
	resetGlobalRegistryForTest(t)
	reg := GetAgentCapabilityRegistry()

	conn := probeConnForTest(t, func(_ context.Context, _ string, _ any) (json.RawMessage, error) {
		return nil, &RawRPCError{Code: -32601, Message: "method not found"}
	})
	refreshDeleteSessionCapability(context.Background(), conn, "plain-agent")

	require.Eventually(t, func() bool { return !reg.DeleteSessionProbePending("plain-agent") },
		time.Second, 10*time.Millisecond, "the probe must record its answer")
	assert.False(t, reg.GetDeleteSession("plain-agent"))
}

// The probe is agent-level (not session-level): a persisted result is picked up
// after a restart, so the probe does not have to run again.
func TestDeleteSessionProbePersistsAcrossRestart(t *testing.T) {
	db := setupCapabilityDB(t)
	origDB := getRegistryDB()
	t.Cleanup(func() { SetRegistryDB(origDB) })
	SetRegistryDB(db)

	_, err := db.Exec("INSERT INTO agents (id, transport) VALUES ('cb-persist', 'acp-stdio')")
	require.NoError(t, err)

	reg := resetGlobalRegistryForTest(t)
	reg.RecordDeleteSessionProbe("cb-persist", true, true)

	require.Eventually(t, func() bool {
		var probed, confirmed bool
		if err := db.QueryRow(
			"SELECT acp_delete_session_probed, acp_delete_session_confirmed FROM agents WHERE id = 'cb-persist'").
			Scan(&probed, &confirmed); err != nil {
			return false
		}
		return probed && confirmed
	}, 2*time.Second, 20*time.Millisecond, "the probe result must be persisted")

	restarted := resetGlobalRegistryForTest(t)
	restarted.LoadFromDB(db)
	assert.True(t, restarted.GetDeleteSession("cb-persist"),
		"the confirmed capability must survive a restart")
}

// Before any probe has answered, an agent that DOES advertise the capability
// must keep it (an install that has never been probed still deletes).
func TestGetDeleteSession_FallsBackToAdvertisementUntilProbed(t *testing.T) {
	resetGlobalRegistryForTest(t)
	reg := GetAgentCapabilityRegistry()

	reg.UpdateDeleteSession("advertised", true)
	assert.True(t, reg.GetDeleteSession("advertised"))
	assert.False(t, reg.DeleteSessionProbePending("advertised"),
		"an advertised yes is nothing to wait for")

	reg.UpdateDeleteSession("silent", false)
	assert.False(t, reg.GetDeleteSession("silent"))
	assert.True(t, reg.DeleteSessionProbePending("silent"),
		"a silent agent's answer may still arrive from the probe")
}

// The advertisement alone can also be WRONG in the other direction: once the
// probe says the method does not exist, an advertised true must not resurrect it.
func TestGetDeleteSession_ProbeBeatsAdvertisement(t *testing.T) {
	resetGlobalRegistryForTest(t)
	reg := GetAgentCapabilityRegistry()

	reg.UpdateDeleteSession("liar", true)
	reg.RecordDeleteSessionProbe("liar", false, true)
	assert.False(t, reg.GetDeleteSession("liar"),
		"the probed fact must win over the advertisement")
}

// A transport failure answers nothing: it must not erase a confirmed true.
func TestRecordDeleteSessionProbe_UnansweredKeepsPreviousResult(t *testing.T) {
	resetGlobalRegistryForTest(t)
	reg := GetAgentCapabilityRegistry()

	reg.RecordDeleteSessionProbe("cb-keep", true, true)
	reg.RecordDeleteSessionProbe("cb-keep", false, false)
	assert.True(t, reg.GetDeleteSession("cb-keep"))
}

// deleteACPSession must actually issue the RPC for an agent that only the
// probe vouched for — this is the behavior issue #707 reported as skipped.
// The RPC itself goes through the SDK (UnstableDeleteSession), so it cannot be
// captured here; what this pins is that the call is REACHED without panicking
// despite the agent advertising nothing (a capability-gated version returns at
// the check instead).
func TestDeleteACPSession_UsesProbedCapability(t *testing.T) {
	resetGlobalRegistryForTest(t)
	reg := GetAgentCapabilityRegistry()
	reg.RecordDeleteSessionProbe("cb-del", true, true)

	agent := &model.Agent{ID: "cb-del", Backend: "codebuddy", AcpCommand: "codebuddy --acp"}
	conn := newACPConn(agent, "sid-cb-del")
	conn.SetAliveForTest()
	conn.SetSessionMappingForTest("sid-cb-del", "acp-cb-1")
	// The raw hook would swallow the call; a pipe-based connection answers
	// -32601, which is the real "reached but refused" path.
	conn.SetRawCallFnForTest(nil)

	assert.NotPanics(t, func() { conn.deleteACPSession() })
	assert.True(t, reg.GetDeleteSession("cb-del"))
}

// The settle window must run BEFORE the capability gate. An agent that
// advertises nothing reads as "not supported" until its probe lands, so a
// gate-first implementation returns immediately and never waits — which is the
// window the delete-right-after-the-first-turn case depends on.
func TestDeleteACPSession_WaitsForInFlightProbeBeforeGating(t *testing.T) {
	resetGlobalRegistryForTest(t)
	reg := GetAgentCapabilityRegistry()
	reg.UpdateDeleteSession("cb-late", false)

	agent := &model.Agent{ID: "cb-late", Backend: "codebuddy", AcpCommand: "codebuddy --acp"}
	conn := newACPConn(agent, "sid-cb-late")
	conn.SetAliveForTest()
	conn.SetSessionMappingForTest("sid-cb-late", "acp-cb-late-1")
	conn.SetRawCallFnForTest(nil)

	// The probe is in flight and answers "no" shortly after the delete starts:
	// the delete must wait for that answer, then honor it.
	go func() {
		time.Sleep(150 * time.Millisecond)
		reg.RecordDeleteSessionProbe("cb-late", false, true)
	}()

	start := time.Now()
	conn.deleteACPSession()
	elapsed := time.Since(start)

	assert.GreaterOrEqual(t, elapsed, 100*time.Millisecond,
		"deleteACPSession must wait for the in-flight probe before gating")
	assert.False(t, reg.GetDeleteSession("cb-late"))
}

// ...and deleteACPSession must stay silent for an agent the probe ruled out,
// no matter what it advertised.
func TestDeleteACPSession_ProbeRefutedSkips(t *testing.T) {
	resetGlobalRegistryForTest(t)
	reg := GetAgentCapabilityRegistry()
	reg.UpdateDeleteSession("no-del", true)
	reg.RecordDeleteSessionProbe("no-del", false, true)

	agent := &model.Agent{ID: "no-del", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "sid-no-del")
	conn.SetAliveForTest()
	conn.SetSessionMappingForTest("sid-no-del", "acp-no-del-1")
	conn.SetRawCallFnForTest(nil)

	// The connection is a pipe, so a call that DID go through would stall for
	// the 10s RPC timeout; returning promptly is the evidence it was skipped.
	start := time.Now()
	conn.deleteACPSession()
	assert.Less(t, time.Since(start), time.Second,
		"the probe said the method does not exist, so the RPC must not be sent")
}

// A successful answer is a confirmation too (it cannot touch a real session:
// the id is made up).
func TestProbeDeleteSession_SuccessMeansSupported(t *testing.T) {
	conn := probeConnForTest(t, func(_ context.Context, _ string, _ any) (json.RawMessage, error) {
		return json.RawMessage(`{}`), nil
	})

	confirmed, answered := probeDeleteSession(context.Background(), conn)
	assert.True(t, answered)
	assert.True(t, confirmed)
}

// Any JSON-RPC error other than "method not found" is still the agent talking
// about the METHOD (unknown session, busy, ...), so the method exists. This is
// the fallback branch, not one of the two named codes.
func TestProbeDeleteSession_OtherRPCErrorMeansSupported(t *testing.T) {
	conn := probeConnForTest(t, func(_ context.Context, _ string, _ any) (json.RawMessage, error) {
		return nil, &RawRPCError{Code: -32000, Message: "agent busy"}
	})

	confirmed, answered := probeDeleteSession(context.Background(), conn)
	assert.True(t, answered, "a JSON-RPC answer is an answer")
	assert.True(t, confirmed, "an error about the method still proves the method exists")
}

// A missing agent id or connection is a no-op, not a panic: the call site runs
// on every spawn, including paths where the id has not been resolved yet.
func TestRefreshDeleteSessionCapability_NoAgentOrConnIsNoop(t *testing.T) {
	resetGlobalRegistryForTest(t)
	reg := GetAgentCapabilityRegistry()
	reg.UpdateDeleteSession("untouched", false)
	require.True(t, reg.DeleteSessionProbePending("untouched"))

	conn := probeConnForTest(t, func(_ context.Context, _ string, _ any) (json.RawMessage, error) {
		t.Fatal("the probe must not run without an agent id")
		return nil, nil
	})

	assert.NotPanics(t, func() {
		refreshDeleteSessionCapability(context.Background(), nil, "some-agent")
		refreshDeleteSessionCapability(context.Background(), conn, "")
	})
	assert.True(t, reg.DeleteSessionProbePending("untouched"),
		"a no-op call must not touch any agent's capability")
}

// A transport failure answers nothing, so nothing is recorded: the registry
// keeps waiting for a real answer instead of caching "unsupported".
func TestRefreshDeleteSessionCapability_UnansweredRecordsNothing(t *testing.T) {
	resetGlobalRegistryForTest(t)
	reg := GetAgentCapabilityRegistry()
	reg.UpdateDeleteSession("flaky", false)
	require.True(t, reg.DeleteSessionProbePending("flaky"))

	conn := probeConnForTest(t, func(_ context.Context, _ string, _ any) (json.RawMessage, error) {
		return nil, errRawConnClosed
	})
	refreshDeleteSessionCapability(context.Background(), conn, "flaky")

	assert.Never(t, func() bool { return !reg.DeleteSessionProbePending("flaky") },
		300*time.Millisecond, 10*time.Millisecond,
		"an unanswered probe must not record a result")
}

// A process respawn replaces the capability snapshot; the probe result is
// learned knowledge and must be carried through (issue #707).
func TestForceUpdate_CarriesDeleteSessionProbeResult(t *testing.T) {
	resetGlobalRegistryForTest(t)
	reg := GetAgentCapabilityRegistry()
	reg.RecordDeleteSessionProbe("cb-force", true, true)

	applied := reg.ForceUpdateIfNeeded("cb-force", nil, nil, nil, nil, nil, false, false, false, false)
	require.True(t, applied)
	assert.True(t, reg.GetDeleteSession("cb-force"),
		"a handshake refresh must not drop what the probe learned")
}
