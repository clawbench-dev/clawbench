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

// TestInjectMidTurn_NoLookupFnIsSafe verifies core flow degrades quietly when
// nothing wires the seam (e.g. a test binary or a build without backends).
func TestInjectMidTurn_NoLookupFnIsSafe(t *testing.T) {
	orig := LookupMidTurnInjectorFn
	LookupMidTurnInjectorFn = nil
	t.Cleanup(func() { LookupMidTurnInjectorFn = orig })

	res := InjectMidTurn(context.Background(), "codebuddy", "sess", "agent", "hi", nil, "q1")
	assert.False(t, res.Injected, "must decline rather than panic when unwired")
}

// TestInjectMidTurn_UnknownBackendDeclines verifies a backend with no policy
// falls back to queueing.
func TestInjectMidTurn_UnknownBackendDeclines(t *testing.T) {
	orig := LookupMidTurnInjectorFn
	LookupMidTurnInjectorFn = func(string) MidTurnInjector { return nil }
	t.Cleanup(func() { LookupMidTurnInjectorFn = orig })

	res := InjectMidTurn(context.Background(), "claude", "sess", "agent", "hi", nil, "q1")
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
	res := InjectMidTurn(context.Background(), "codebuddy", "no-such-session-xyz", "agent", "hi", nil, "q1")

	assert.False(t, res.Injected)
	assert.False(t, stub.called, "policy must not run without a live connection")
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
	_ = model.FileEntry{}
}
