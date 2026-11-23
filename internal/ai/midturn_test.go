package ai

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"clawbench/internal/model"
)

// stubInjector records whether it was consulted.
type stubInjector struct {
	called bool
	res    MidTurnInjectResult
}

func (s *stubInjector) Inject(context.Context, RawRPCTransport, MidTurnInjectRequest) (MidTurnInjectResult, error) {
	s.called = true
	return s.res, nil
}

// stubInProcessInjector records whether it was consulted, and can be told to
// fail so the error path is exercised.
type stubInProcessInjector struct {
	called bool
	res    MidTurnInjectResult
	err    error
}

func (s *stubInProcessInjector) InjectInProcess(context.Context, MidTurnInjectRequest) (MidTurnInjectResult, error) {
	s.called = true
	return s.res, s.err
}

// TestInjectMidTurn_NoLookupFnIsSafe verifies core flow degrades quietly when
// nothing wires the seam (e.g. a test binary or a build without backends).
func TestInjectMidTurn_NoLookupFnIsSafe(t *testing.T) {
	orig := LookupMidTurnInjectorFn
	LookupMidTurnInjectorFn = nil
	t.Cleanup(func() { LookupMidTurnInjectorFn = orig })

	res := InjectMidTurn(context.Background(), "codebuddy", "sess", "agent", "hi", "q1")
	assert.False(t, res.Injected, "must decline rather than panic when unwired")
}

// TestInjectMidTurn_UnknownBackendDeclines verifies a backend with no policy
// falls back to queueing.
func TestInjectMidTurn_UnknownBackendDeclines(t *testing.T) {
	orig := LookupMidTurnInjectorFn
	LookupMidTurnInjectorFn = func(string) MidTurnInjector { return nil }
	t.Cleanup(func() { LookupMidTurnInjectorFn = orig })

	res := InjectMidTurn(context.Background(), "claude", "sess", "agent", "hi", "q1")
	assert.False(t, res.Injected)
}

// TestInjectMidTurn_NoLiveConnectionDeclines verifies the policy is never
// consulted when the session has no ACP connection (nothing to inject into).
func TestInjectMidTurn_NoLiveConnectionDeclines(t *testing.T) {
	stub := &stubInjector{res: MidTurnInjectResult{Injected: true}}

	orig := LookupMidTurnInjectorFn
	LookupMidTurnInjectorFn = func(string) MidTurnInjector { return stub }
	t.Cleanup(func() { LookupMidTurnInjectorFn = orig })

	// A session id that certainly has no pooled connection.
	res := InjectMidTurn(context.Background(), "codebuddy", "no-such-session-xyz", "agent", "hi", "q1")

	assert.False(t, res.Injected)
	assert.False(t, stub.called, "policy must not run without a live connection")
}

// TestInjectMidTurn_InProcessAnswersFirst verifies an in-process backend is
// served by its own policy and never touches the ACP path. There is no
// connection to hand over, so reaching the ACP lookup would decline a backend
// that can actually inject.
func TestInjectMidTurn_InProcessAnswersFirst(t *testing.T) {
	inproc := &stubInProcessInjector{res: MidTurnInjectResult{Injected: true}}

	origInProc, origACP := LookupInProcessMidTurnInjectorFn, LookupMidTurnInjectorFn
	LookupInProcessMidTurnInjectorFn = func(id string) InProcessMidTurnInjector {
		if id == "builtin" {
			return inproc
		}
		return nil
	}
	acpStub := &stubInjector{res: MidTurnInjectResult{Injected: true}}
	LookupMidTurnInjectorFn = func(string) MidTurnInjector { return acpStub }
	t.Cleanup(func() {
		LookupInProcessMidTurnInjectorFn = origInProc
		LookupMidTurnInjectorFn = origACP
	})

	res := InjectMidTurn(context.Background(), "builtin", "sess", "agent", "hi", "q1")

	assert.True(t, res.Injected)
	assert.True(t, inproc.called, "the in-process policy must be consulted")
	assert.False(t, acpStub.called, "the ACP policy must not run for an in-process backend")
}

// TestInjectMidTurn_InProcessErrorIsNotFatal verifies an in-process failure
// degrades to "queue it" instead of surfacing an error to the caller, matching
// the ACP path's contract.
func TestInjectMidTurn_InProcessErrorIsNotFatal(t *testing.T) {
	inproc := &stubInProcessInjector{err: assertErr{}}

	origInProc, origACP := LookupInProcessMidTurnInjectorFn, LookupMidTurnInjectorFn
	LookupInProcessMidTurnInjectorFn = func(string) InProcessMidTurnInjector { return inproc }
	LookupMidTurnInjectorFn = nil
	t.Cleanup(func() {
		LookupInProcessMidTurnInjectorFn = origInProc
		LookupMidTurnInjectorFn = origACP
	})

	res := InjectMidTurn(context.Background(), "builtin", "sess", "agent", "hi", "q1")

	assert.False(t, res.Injected, "an error must not report a successful injection")
	assert.Equal(t, "error", res.Reason)
}

// TestACPConn_SatisfiesRawRPCTransport is a compile-time guarantee that the
// transport handed to policies is the real connection type.
func TestACPConn_SatisfiesRawRPCTransport(t *testing.T) {
	var _ RawRPCTransport = (*ACPConn)(nil)
}

// TestACPConn_CallRawWithoutConnection verifies a dead/absent connection returns
// the sentinel error instead of panicking — CallRaw is reachable from user
// input paths, so it must be safe on a zero-value connection.
func TestACPConn_CallRawWithoutConnection(t *testing.T) {
	c := &ACPConn{}
	_, err := c.CallRaw(context.Background(), "session/steer", map[string]any{"sessionId": "s"})
	assert.ErrorIs(t, err, errRawConnClosed)
}

// TestACPConn_AcpSessionIDWithoutLock verifies the accessor used by policies is
// safe on a connection with no session yet.
func TestACPConn_AcpSessionIDWithoutLock(t *testing.T) {
	c := &ACPConn{}
	assert.Empty(t, c.AcpSessionID())
}

// TestMidTurnInjectResult_ZeroValueMeansQueue documents the contract: the zero
// value is the "queue it" answer, so a policy that forgets to set Injected
// degrades safely rather than claiming success.
func TestMidTurnInjectResult_ZeroValueMeansQueue(t *testing.T) {
	var res MidTurnInjectResult
	assert.False(t, res.Injected)
	assert.Empty(t, res.Reason)

	// Guard the JSON shape used on the raw wire is not accidentally coupled here.
	_ = json.RawMessage(nil)
}

// TestInjectMidTurn_ACPInjectsThroughLiveConnections covers the ACP success
// path: with a live connection the policy must receive THAT transport, and its
// result must come back unchanged. Previously only the decline and error paths
// were exercised, so a wrong transport could go unnoticed.
func TestInjectMidTurn_ACPInjectsThroughLiveConnections(t *testing.T) {
	agent := &model.Agent{ID: "midturn-acp-agent", Backend: "codebuddy", Name: "midturn-acp"}
	conn := NewACPConnForTest(agent, "midturn-acp-session")
	const sid = "midturn-acp-session"
	GetACPConnManager().SetConnForTest(sid, conn)
	t.Cleanup(func() { GetACPConnManager().CloseConn(sid) })

	var gotTransport RawRPCTransport
	injector := &recordingInjector{res: MidTurnInjectResult{Injected: true, OwnerRequestID: "owner-1"}}
	injector.onInject = func(tr RawRPCTransport) { gotTransport = tr }

	origACPLookup, origInProc := LookupMidTurnInjectorFn, LookupInProcessMidTurnInjectorFn
	LookupMidTurnInjectorFn = func(string) MidTurnInjector { return injector }
	LookupInProcessMidTurnInjectorFn = nil
	t.Cleanup(func() {
		LookupMidTurnInjectorFn = origACPLookup
		LookupInProcessMidTurnInjectorFn = origInProc
	})

	res := InjectMidTurn(context.Background(), "codebuddy", sid, "agent", "hi", "q1")

	assert.True(t, res.Injected)
	assert.Equal(t, "owner-1", res.OwnerRequestID, "the policy's result must come back unchanged")
	assert.Same(t, conn, gotTransport, "the policy must be handed the session's live connection")
}

// recordingInjector remembers the transport it was given.
type recordingInjector struct {
	res      MidTurnInjectResult
	onInject func(RawRPCTransport)
}

func (r *recordingInjector) Inject(_ context.Context, tr RawRPCTransport, _ MidTurnInjectRequest) (MidTurnInjectResult, error) {
	if r.onInject != nil {
		r.onInject(tr)
	}
	return r.res, nil
}
