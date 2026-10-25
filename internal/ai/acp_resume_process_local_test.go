package ai

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"clawbench/internal/model"

	acp "github.com/coder/acp-go-sdk"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ACPResumeIsProcessLocal — recovery must go straight to session/load
// ---------------------------------------------------------------------------

// TestRecoverExistingSession_UsesLoadWhenResumeIsProcessLocal pins the branch
// that keeps an openclaw session alive across a bridge restart.
//
// The generic path tries session/resume first and only falls back to
// session/load on -32601. A process-scoped resume answers a BUSINESS error
// (-32603 "Session ... not found"), which that path must treat as
// unrecoverable (it must never silently start a fresh session and lose the
// conversation). So without this branch, every restart strands the session —
// and the failure looks like a normal "session is unrecoverable" error, which
// is why it needs its own test rather than relying on the fallback's coverage.
func TestRecoverExistingSession_UsesLoadWhenResumeIsProcessLocal(t *testing.T) {
	// A backend id registered with the flag set. The registry is populated by
	// the backends package's init(), which this package does not import, so the
	// test installs a minimal spec through the same lookup the production code
	// uses.
	restore := installTestBackendSpec(t, model.BackendSpec{
		ID:                      "process-local-resume-test",
		Backend:                 "process-local-resume-test",
		ACPResumeIsProcessLocal: true,
	})
	defer restore()

	conn := newACPConn(&model.Agent{ID: "plr", Backend: "process-local-resume-test"}, "sid")
	conn.alive = true

	var resumeCalled, loadCalled bool
	conn.SetRecoveryRPCsFnForTest(
		func(context.Context, string, string) (*acp.ResumeSessionResponse, error) {
			resumeCalled = true
			return nil, &acp.RequestError{Code: -32603, Message: "Session x not found"}
		},
		func(context.Context, string, string) (*acp.LoadSessionResponse, error) {
			loadCalled = true
			return &acp.LoadSessionResponse{}, nil
		},
	)

	err := conn.recoverExistingSession(context.Background(), "/tmp", "acp-sid", cachedConfigSnapshot{})
	require.NoError(t, err)
	assert.True(t, loadCalled, "session/load must be used for a process-local-resume backend")
	assert.False(t, resumeCalled,
		"session/resume must not be attempted: it cannot resolve ids from a previous process, "+
			"and its business error would be reported as unrecoverable")
}

// TestRecoverExistingSession_UsesResumeByDefault makes sure the new branch does
// not change behavior for every other backend.
func TestRecoverExistingSession_UsesResumeByDefault(t *testing.T) {
	restore := installTestBackendSpec(t, model.BackendSpec{
		ID:      "normal-resume-test",
		Backend: "normal-resume-test",
	})
	defer restore()

	conn := newACPConn(&model.Agent{ID: "nr", Backend: "normal-resume-test"}, "sid")
	conn.alive = true

	var loadCalled bool
	conn.SetRecoveryRPCsFnForTest(
		func(context.Context, string, string) (*acp.ResumeSessionResponse, error) {
			return &acp.ResumeSessionResponse{}, nil
		},
		func(context.Context, string, string) (*acp.LoadSessionResponse, error) {
			loadCalled = true
			return &acp.LoadSessionResponse{}, nil
		},
	)

	err := conn.recoverExistingSession(context.Background(), "/tmp", "acp-sid", cachedConfigSnapshot{})
	require.NoError(t, err)
	assert.False(t, loadCalled,
		"a backend without the flag must keep using session/resume (load replays the whole "+
			"conversation and can exceed the timeout)")
}

// TestRecoverExistingSession_ProcessLocalLoadFailureIsReported pins that a
// failing load on the new branch surfaces as an error rather than falling
// through to "start a new session".
func TestRecoverExistingSession_ProcessLocalLoadFailureIsReported(t *testing.T) {
	restore := installTestBackendSpec(t, model.BackendSpec{
		ID:                      "process-local-load-fail-test",
		Backend:                 "process-local-load-fail-test",
		ACPResumeIsProcessLocal: true,
	})
	defer restore()

	conn := newACPConn(&model.Agent{ID: "plf", Backend: "process-local-load-fail-test"}, "sid")
	conn.alive = true
	conn.SetRecoveryRPCsFnForTest(
		func(context.Context, string, string) (*acp.ResumeSessionResponse, error) {
			t.Fatal("session/resume must not be attempted when ACPResumeIsProcessLocal is set")
			return nil, nil
		},
		func(context.Context, string, string) (*acp.LoadSessionResponse, error) {
			return nil, &acp.RequestError{Code: -32603, Message: "Session x not found"}
		},
	)

	err := conn.recoverExistingSession(context.Background(), "/tmp", "acp-sid", cachedConfigSnapshot{})
	require.Error(t, err, "a failed load must not be swallowed into a fresh session")
	assert.Contains(t, err.Error(), "LoadSession")
}

// ---------------------------------------------------------------------------
// session/load deadline + timeout-vs-refusal classification
// ---------------------------------------------------------------------------

// TestLoadSessionTimeout_ProcessLocalResumeGetsMoreTime pins the deadline split.
//
// For a process-local-resume backend, session/load is the ONLY recovery path
// (resume cannot resolve an id from a previous process), so every bridge
// restart replays the whole conversation. Its cost scales with history length,
// so the generic 60s deadline turned long sessions into permanent failures.
func TestLoadSessionTimeout_ProcessLocalResumeGetsMoreTime(t *testing.T) {
	restore := installTestBackendSpec(t, model.BackendSpec{
		ID:                      "process-local-deadline-test",
		Backend:                 "process-local-deadline-test",
		ACPResumeIsProcessLocal: true,
	})
	defer restore()

	processLocal := newACPConn(&model.Agent{ID: "pld", Backend: "process-local-deadline-test"}, "sid")
	assert.Equal(t, loadSessionTimeoutProcessLocalResume, processLocal.loadSessionTimeout(),
		"a backend that can only recover via session/load needs the long deadline")

	// Every other backend keeps the short one: load is only the -32601
	// fallback there, and a genuinely hung agent must not block for 5 minutes.
	restoreNormal := installTestBackendSpec(t, model.BackendSpec{
		ID:      "normal-deadline-test",
		Backend: "normal-deadline-test",
	})
	defer restoreNormal()

	normal := newACPConn(&model.Agent{ID: "nd", Backend: "normal-deadline-test"}, "sid")
	assert.Equal(t, loadSessionTimeoutDefault, normal.loadSessionTimeout(),
		"the generic backend must keep the 60s deadline")
	assert.Less(t, loadSessionTimeoutDefault, loadSessionTimeoutProcessLocalResume,
		"the generic deadline must stay the shorter of the two")
}

// TestRecoverExistingSession_TimeoutIsRetryableNotUnrecoverable pins the
// distinction between "we stopped waiting" and "the conversation is gone".
//
// A timed-out load is retryable: killAndMarkDeadLocked preserves acpSID, so the
// next turn re-attempts the same recovery. Reporting it as unrecoverable told
// the user their history was lost when it was not.
func TestRecoverExistingSession_TimeoutIsRetryableNotUnrecoverable(t *testing.T) {
	restore := installTestBackendSpec(t, model.BackendSpec{
		ID:                      "process-local-timeout-test",
		Backend:                 "process-local-timeout-test",
		ACPResumeIsProcessLocal: true,
	})
	defer restore()

	conn := newACPConn(&model.Agent{ID: "plt", Backend: "process-local-timeout-test"}, "sid")
	conn.alive = true
	conn.acpSID = "acp-sid"
	conn.SetRecoveryRPCsFnForTest(
		func(context.Context, string, string) (*acp.ResumeSessionResponse, error) {
			t.Fatal("session/resume must not be attempted when ACPResumeIsProcessLocal is set")
			return nil, nil
		},
		func(context.Context, string, string) (*acp.LoadSessionResponse, error) {
			// What the ACP SDK returns when the timeout fires: a -32603 whose
			// data carries the original context error.
			return nil, &acp.RequestError{
				Code:    -32603,
				Message: "Internal error",
				Data:    map[string]any{"error": "context deadline exceeded"},
			}
		},
	)

	err := conn.recoverExistingSession(context.Background(), "/tmp", "acp-sid", cachedConfigSnapshot{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, errSessionRecoveryTimeout),
		"a timed-out recovery must be marked retryable, got %v", err)
	assert.Contains(t, err.Error(), "retryable")
	assert.NotContains(t, err.Error(), "unrecoverable")
	// The session mapping must survive so the next prompt can retry it.
	assert.Equal(t, "acp-sid", conn.acpSID, "the session id must be preserved for a retry")
	// Deliberately NOT wrapping context.DeadlineExceeded: the transport layer
	// treats that as a peer disconnect and would silently re-run the whole
	// recovery (another multi-minute replay) inside one turn.
	assert.False(t, errors.Is(err, context.DeadlineExceeded),
		"the timeout marker must not look like a transport deadline, or recovery would be re-run")
}

// TestRecoverExistingSession_RefusalStaysUnrecoverable is the counterpart: an
// agent that ANSWERS "no such session" is not retryable, and the error must
// keep saying so.
func TestRecoverExistingSession_RefusalStaysUnrecoverable(t *testing.T) {
	restore := installTestBackendSpec(t, model.BackendSpec{
		ID:                      "process-local-refusal-test",
		Backend:                 "process-local-refusal-test",
		ACPResumeIsProcessLocal: true,
	})
	defer restore()

	conn := newACPConn(&model.Agent{ID: "plr2", Backend: "process-local-refusal-test"}, "sid")
	conn.alive = true
	conn.acpSID = "acp-sid"
	conn.SetRecoveryRPCsFnForTest(
		func(context.Context, string, string) (*acp.ResumeSessionResponse, error) {
			t.Fatal("session/resume must not be attempted when ACPResumeIsProcessLocal is set")
			return nil, nil
		},
		func(context.Context, string, string) (*acp.LoadSessionResponse, error) {
			return nil, &acp.RequestError{Code: -32002, Message: "Resource not found"}
		},
	)

	err := conn.recoverExistingSession(context.Background(), "/tmp", "acp-sid", cachedConfigSnapshot{})
	require.Error(t, err)
	assert.False(t, errors.Is(err, errSessionRecoveryTimeout),
		"a refusal must NOT be reported as a retryable timeout, got %v", err)
	assert.NotContains(t, err.Error(), "retryable")
}

// TestRecoverExistingSession_ResumeTimeoutIsAlsoRetryable covers the other RPC:
// the classification is a property of the failure, not of session/load.
func TestRecoverExistingSession_ResumeTimeoutIsAlsoRetryable(t *testing.T) {
	restore := installTestBackendSpec(t, model.BackendSpec{
		ID:      "resume-timeout-test",
		Backend: "resume-timeout-test",
	})
	defer restore()

	conn := newACPConn(&model.Agent{ID: "rt", Backend: "resume-timeout-test"}, "sid")
	conn.alive = true
	conn.acpSID = "acp-sid"
	conn.SetRecoveryRPCsFnForTest(
		func(context.Context, string, string) (*acp.ResumeSessionResponse, error) {
			return nil, context.DeadlineExceeded
		},
		func(context.Context, string, string) (*acp.LoadSessionResponse, error) {
			t.Fatal("a deadline is not -32601, so session/load must not be attempted")
			return nil, nil
		},
	)

	err := conn.recoverExistingSession(context.Background(), "/tmp", "acp-sid", cachedConfigSnapshot{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, errSessionRecoveryTimeout), "got %v", err)
	assert.Equal(t, "acp-sid", conn.acpSID)
}

// ---------------------------------------------------------------------------
// WARN-010: an unregistered backend must be visible in the logs
// ---------------------------------------------------------------------------

// TestRecoveryFailure_LogsDistinguishTimeoutFromRefusal pins the OPERATIONAL
// half of the timeout/refusal split.
//
// The internal marker only helps code. An operator reading the log has to be
// able to tell "we gave up waiting" from "the agent says the session is gone",
// because only the first one is worth retrying. Both branches must therefore
// carry distinct wording (and the timeout branch must name the deadline), not
// just a different Go error value.
func TestRecoveryFailure_LogsDistinguishTimeoutFromRefusal(t *testing.T) {
	restore := installTestBackendSpec(t, model.BackendSpec{
		ID:      "log-distinction-test",
		Backend: "log-distinction-test",
	})
	defer restore()

	capture := func(run func(*ACPConn)) string {
		var buf bytes.Buffer
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
		defer slog.SetDefault(prev)
		conn := newACPConn(&model.Agent{ID: "ld", Backend: "log-distinction-test"}, "sid")
		run(conn)
		return buf.String()
	}

	timedOut := capture(func(c *ACPConn) {
		c.recoveryFailure("LoadSession", "acp-sid", loadSessionTimeoutProcessLocalResume, context.DeadlineExceeded)
	})
	assert.Contains(t, timedOut, "timed out",
		"a timeout must say so in words, got %q", timedOut)
	assert.Contains(t, timedOut, "timeout=5m0s",
		"the deadline must be logged so a slow-but-healthy replay is identifiable, got %q", timedOut)
	assert.Contains(t, timedOut, "preserved",
		"the log must state that the session mapping survives, or it reads like data loss, got %q", timedOut)
	assert.NotContains(t, timedOut, "unrecoverable",
		"a timeout is retryable and must not be logged as unrecoverable, got %q", timedOut)

	refused := capture(func(c *ACPConn) {
		c.recoveryFailure("LoadSession", "acp-sid", loadSessionTimeoutDefault, &acp.RequestError{Code: -32002, Message: "Resource not found"})
	})
	assert.Contains(t, refused, "unrecoverable",
		"a refusal must be logged as unrecoverable, got %q", refused)
	assert.NotContains(t, refused, "timed out",
		"a refusal must not be logged as a timeout, got %q", refused)
}

// TestRecoverExistingSession_UnregisteredBackendWarns pins the diagnostic that
// makes "the registry was not populated yet" distinguishable from "this backend
// genuinely has no spec".
//
// backendSpec() resolves through model.FindSpecByBackend, whose registry is
// filled by the backend subpackages' init(). internal/ai does not import them
// (that would be a cycle), so in this package's tests — and in any early
// startup path — the lookup can return nil even for a real backend such as
// openclaw. Recovery then silently takes the resume-first path, where
// openclaw's -32603 reads as "unrecoverable". Without a log line the two cases
// are indistinguishable, which is what this test guards.
func TestRecoverExistingSession_UnregisteredBackendWarns(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	conn := newACPConn(&model.Agent{ID: "ghost", Backend: "definitely-not-registered"}, "sid")
	conn.alive = true
	conn.acpSID = "acp-sid"
	conn.SetRecoveryRPCsFnForTest(
		func(context.Context, string, string) (*acp.ResumeSessionResponse, error) {
			return &acp.ResumeSessionResponse{}, nil
		},
		func(context.Context, string, string) (*acp.LoadSessionResponse, error) {
			return &acp.LoadSessionResponse{}, nil
		},
	)

	require.NoError(t, conn.recoverExistingSession(context.Background(), "/tmp", "acp-sid", cachedConfigSnapshot{}))

	logged := buf.String()
	assert.Contains(t, logged, "no registered spec",
		"an unregistered backend must be reported: it is the only signal that a real backend's "+
			"capability flags were invisible, got %q", logged)
	assert.Contains(t, logged, "definitely-not-registered",
		"the log must name the backend so the cause can be identified")
}

// TestRecoverExistingSession_RegisteredBackendDoesNotWarn is the counterpart:
// the warning must not fire on the normal path, or it becomes noise every
// restart and the diagnostic is worthless.
func TestRecoverExistingSession_RegisteredBackendDoesNotWarn(t *testing.T) {
	restore := installTestBackendSpec(t, model.BackendSpec{
		ID:      "registered-warn-test",
		Backend: "registered-warn-test",
	})
	defer restore()

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	conn := newACPConn(&model.Agent{ID: "rw", Backend: "registered-warn-test"}, "sid")
	conn.alive = true
	conn.acpSID = "acp-sid"
	conn.SetRecoveryRPCsFnForTest(
		func(context.Context, string, string) (*acp.ResumeSessionResponse, error) {
			return &acp.ResumeSessionResponse{}, nil
		},
		func(context.Context, string, string) (*acp.LoadSessionResponse, error) {
			return &acp.LoadSessionResponse{}, nil
		},
	)

	require.NoError(t, conn.recoverExistingSession(context.Background(), "/tmp", "acp-sid", cachedConfigSnapshot{}))
	assert.NotContains(t, buf.String(), "no registered spec")
}

// installTestBackendSpec temporarily prepends a spec to the backend registry and
// returns a restore func. Needed because internal/ai does not import the backends
// package (that would be a cycle), so no specs are registered in this package's
// tests. GetBackendRegistry() returns the exported var after its sync.Once, so
// assigning it directly is enough.
func installTestBackendSpec(t *testing.T, spec model.BackendSpec) func() {
	t.Helper()
	prev := model.GetBackendRegistry()
	model.BackendRegistry = append([]model.BackendSpec{spec}, prev...)
	return func() { model.BackendRegistry = prev }
}
