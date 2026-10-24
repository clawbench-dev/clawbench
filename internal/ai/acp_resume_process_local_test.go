package ai

import (
	"context"
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
