package ai

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// "The agent no longer has this session" — classification
// ---------------------------------------------------------------------------

// TestIsSessionGoneError pins the shapes that mean "this session does not exist
// any more" against the ones that must NOT be read that way.
//
// The distinction decides whether the conversation may continue on a fresh
// session (issue #669) or must keep the old mapping for a retry, so a false
// positive silently loses the agent's context and a false negative strands the
// session forever.
func TestIsSessionGoneError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			// The exact shape opencode's ACP server answers after a restart:
			// {"code":-32602,"message":"Invalid params: session not found"}
			name: "opencode_invalid_params_session_not_found",
			err:  &acp.RequestError{Code: -32602, Message: "Invalid params: session not found"},
			want: true,
		},
		{
			name: "invalid_params_with_session_in_data",
			err: &acp.RequestError{
				Code:    -32602,
				Message: "Invalid params",
				Data:    map[string]any{"details": "session not found"},
			},
			want: true,
		},
		{
			name: "canonical_resource_not_found",
			err:  &acp.RequestError{Code: -32002, Message: "Resource not found"},
			want: true,
		},
		{
			// openclaw's bridge answers a foreign id this way.
			name: "internal_error_session_not_found",
			err:  &acp.RequestError{Code: -32603, Message: "Session 8f2c not found"},
			want: true,
		},
		{
			name: "wrapped_in_recovery_error",
			err: fmt.Errorf("acp: session x ResumeSession failed: %w",
				&acp.RequestError{Code: -32602, Message: "Invalid params: session not found"}),
			want: true,
		},
		{
			name: "plain_error_text",
			err:  errors.New("acp: session not found"),
			want: true,
		},
		{
			// The wrapper recoveryFailure adds always contains the word
			// "session", so a wrapped unrelated "not found" must NOT count.
			name: "wrapped_missing_directory",
			err: fmt.Errorf("acp: ResumeSession failed for session abc: %w",
				errors.New("cwd /work not found")),
			want: false,
		},
		{
			name: "wrapped_missing_config_file",
			err: fmt.Errorf("acp: session/load: %w",
				errors.New("config file not found")),
			want: false,
		},
		{
			// -32002 is generic; only the canonical wording may mean the session.
			name: "resource_not_found_with_unrelated_message",
			err:  &acp.RequestError{Code: -32002, Message: "MCP server 'fs' not found"},
			want: false,
		},
		{
			// A session-scoped resource is not the session itself.
			name: "internal_error_session_resource_not_found",
			err:  &acp.RequestError{Code: -32603, Message: "session cwd /work not found"},
			want: false,
		},
		{
			// Some agents report a lost session with the generic resource code
			// and their own message.
			name: "resource_code_with_session_message",
			err:  &acp.RequestError{Code: -32002, Message: "Session 8f2c not found"},
			want: true,
		},
		{
			name: "short_session_id",
			err:  &acp.RequestError{Code: -32603, Message: "Session x not found"},
			want: true,
		},
		{
			// A genuinely malformed request must not be read as a lost session:
			// starting fresh would throw the context away for nothing.
			name: "invalid_params_about_something_else",
			err:  &acp.RequestError{Code: -32602, Message: "Invalid params: unknown mode"},
			want: false,
		},
		{
			name: "method_not_found",
			err:  &acp.RequestError{Code: -32601, Message: "Method not found"},
			want: false,
		},
		{
			name: "generic_internal_error",
			err:  &acp.RequestError{Code: -32603, Message: "Internal error"},
			want: false,
		},
		{
			name: "internal_error_about_a_file",
			err:  &acp.RequestError{Code: -32603, Message: "Internal error: config file not found"},
			want: false,
		},
		{
			name: "plain_error_about_a_file",
			err:  errors.New("open /tmp/x: file not found"),
			want: false,
		},
		{
			name: "deadline",
			err:  context.DeadlineExceeded,
			want: false,
		},
		{
			name: "nil",
			err:  nil,
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isSessionGoneError(tc.err))
		})
	}
}

// ---------------------------------------------------------------------------
// recoveryFailure — drop the stale mapping only when the session is gone
// ---------------------------------------------------------------------------

// TestRecoveryFailure_DropsMappingWhenSessionIsGone pins that the persisted id
// is cleared when the agent says the session does not exist.
//
// Without the DB clear the same dead id is re-read on the next server start
// (GetOrCreateConn pre-populates acpSID from external_session_id) and every turn
// fails identically — the reported "group member can never speak again".
func TestRecoveryFailure_DropsMappingWhenSessionIsGone(t *testing.T) {
	var cleared []string
	SetExternalSessionIDClearer(func(sid string) { cleared = append(cleared, sid) })
	t.Cleanup(func() { SetExternalSessionIDClearer(func(string) {}) })

	conn := newACPConn(&model.Agent{ID: "opencode", Backend: "opencode"}, "clawbench-sid")
	conn.acpSID = "acp-sid"

	err := conn.recoveryFailure("ResumeSession", "acp-sid", resumeSessionTimeout,
		&acp.RequestError{Code: -32602, Message: "Invalid params: session not found"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ResumeSession", "the failing RPC must stay named")
	assert.True(t, errors.Is(err, errSessionGone),
		"the caller needs the sentinel to decide on a fresh session, got %v", err)
	assert.True(t, isSessionGoneError(err), "the original business error must stay in the chain")
	assert.Empty(t, conn.acpSID, "the in-memory mapping must be dropped")
	assert.Equal(t, []string{"clawbench-sid"}, cleared,
		"the persisted external_session_id must be cleared, or the next restart retries the dead id")
}

// TestRecoveryFailure_KeepsMappingOnOtherFailures is the counterpart: a
// malformed-params refusal and a timeout both keep the mapping, because a later
// attempt may still recover the same session.
func TestRecoveryFailure_KeepsMappingOnOtherFailures(t *testing.T) {
	cases := []struct {
		name  string
		cause error
	}{
		{name: "invalid_params_unrelated", cause: &acp.RequestError{Code: -32602, Message: "Invalid params: unknown mode"}},
		{name: "method_not_found", cause: &acp.RequestError{Code: -32601, Message: "Method not found"}},
		{name: "timeout", cause: context.DeadlineExceeded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var cleared int
			SetExternalSessionIDClearer(func(string) { cleared++ })
			t.Cleanup(func() { SetExternalSessionIDClearer(func(string) {}) })

			conn := newACPConn(&model.Agent{ID: "claude", Backend: "claude"}, "clawbench-sid")
			conn.acpSID = "acp-sid"

			err := conn.recoveryFailure("ResumeSession", "acp-sid", resumeSessionTimeout, tc.cause)
			require.Error(t, err)
			assert.Equal(t, "acp-sid", conn.acpSID, "the mapping must survive a retryable/ambiguous failure")
			assert.Zero(t, cleared, "the persisted id must not be dropped when a retry can still work")
			assert.False(t, errors.Is(err, errSessionGone),
				"an ambiguous failure must not look like a lost session")
		})
	}
}

// TestRecoveryFailure_KeepsAConcurrentlyEstablishedSession pins the
// compare-and-clear guard: killAndMarkDeadLocked releases c.mu while reaping, so
// another caller can establish a session in that window. Wiping its id (and the
// persisted one) would destroy a live session.
func TestRecoveryFailure_KeepsAConcurrentlyEstablishedSession(t *testing.T) {
	var cleared []string
	SetExternalSessionIDClearer(func(sid string) { cleared = append(cleared, sid) })
	t.Cleanup(func() { SetExternalSessionIDClearer(func(string) {}) })

	conn := newACPConn(&model.Agent{ID: "opencode", Backend: "opencode"}, "clawbench-sid")
	conn.acpSID = "newer-session" // established while the failed one was being reaped

	err := conn.recoveryFailure("ResumeSession", "acp-sid", resumeSessionTimeout,
		&acp.RequestError{Code: -32602, Message: "Invalid params: session not found"})

	require.Error(t, err)
	assert.Equal(t, "newer-session", conn.acpSID, "a session established in the unlock window must survive")
	assert.Empty(t, cleared, "the persisted id of the live session must not be dropped")
}

// TestRecoverExistingSession_SessionNotFoundDropsMapping drives the real
// recovery path (ResumeSession → business error) so the classifier, the log and
// the mapping drop are proven wired together, not just individually correct.
func TestRecoverExistingSession_SessionNotFoundDropsMapping(t *testing.T) {
	restore := installTestBackendSpec(t, model.BackendSpec{
		ID:      "session-gone-test",
		Backend: "session-gone-test",
	})
	defer restore()

	var cleared []string
	SetExternalSessionIDClearer(func(sid string) { cleared = append(cleared, sid) })
	t.Cleanup(func() { SetExternalSessionIDClearer(func(string) {}) })

	conn := newACPConn(&model.Agent{ID: "oc", Backend: "session-gone-test"}, "clawbench-sid")
	conn.alive = true
	conn.acpSID = "acp-sid"
	conn.SetRecoveryRPCsFnForTest(
		func(context.Context, string, string) (*acp.ResumeSessionResponse, error) {
			return nil, &acp.RequestError{Code: -32602, Message: "Invalid params: session not found"}
		},
		func(context.Context, string, string) (*acp.LoadSessionResponse, error) {
			t.Fatal("a business error is not -32601, so session/load must not be attempted")
			return nil, nil
		},
	)

	err := conn.recoverExistingSession(context.Background(), "/tmp", "acp-sid", cachedConfigSnapshot{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, errSessionGone), "got %v", err)
	assert.Empty(t, conn.acpSID)
	assert.Equal(t, []string{"clawbench-sid"}, cleared)
}

// TestSessionResetNotice pins what the user is told when a turn continues on a
// fresh session, and — more importantly — the reason it carries.
//
// ReasonSessionReset is the whole point of the new reason code: ReasonBackendExit
// (what this notice used to use) is in the retryable set, so a turn that ran
// perfectly well on the new session would be re-sent by auto-continue and, in a
// group, would not advance the member's seen_cursor.
func TestSessionResetNotice(t *testing.T) {
	notice := sessionResetNotice()
	assert.Equal(t, "warning", notice.Type)
	assert.Equal(t, ReasonSessionReset, notice.Reason)
	assert.NotEqual(t, ReasonBackendExit, notice.Reason)
	assert.NotEmpty(t, notice.Content, "the user must be told the previous context was lost")
}

// TestFreshSessionTurnResendsSystemPrompt pins both halves of the rule: the
// helper's semantics, and the call sites that must use it.
//
// The call sites are a source guard rather than a behavioral test because
// reaching them needs a live ACP agent process: the fallback path spawns the
// backend for real, which this package's tests deliberately never do. The
// contract is exactly "every point that can hand back a fresh session applies
// the rule".
func TestFreshSessionTurnResendsSystemPrompt(t *testing.T) {
	req := ChatRequest{Resume: true}
	applyFreshSessionResumeRule(&req, true)
	assert.False(t, req.Resume, "a fresh session must re-send the system prompt")

	kept := ChatRequest{Resume: true}
	applyFreshSessionResumeRule(&kept, false)
	assert.True(t, kept.Resume, "a recovered (not fresh) session must keep Resume")

	src, err := os.ReadFile("acp_backend.go")
	require.NoError(t, err)
	assert.Regexp(t, `applyFreshSessionResumeRule\(&req, isNew\)`, string(src),
		"ExecuteStream must apply the rule to the first connection")
	assert.Regexp(t, `applyFreshSessionResumeRule\(&req, isNew2\)`, string(src),
		"the prompt-retry respawn can also produce a fresh session and must apply the rule")
}

// TestSessionResetNoticeIsEmitted is a source guard for the notice's call site:
// the reason it carries is what keeps auto-continue from re-sending the turn and
// what lets a group member's seen_cursor advance, so silently reverting to an
// inline event (or to ReasonBackendExit) must fail a test.
func TestSessionResetNoticeIsEmitted(t *testing.T) {
	src, err := os.ReadFile("acp_backend.go")
	require.NoError(t, err)
	assert.Contains(t, string(src), "forwardACPEvent(ch, sessionResetNotice())",
		"the recovery fallback must emit the session-reset notice")
}

// ---------------------------------------------------------------------------
// shouldStartFreshSession — when a failed recovery may be rebuilt
// ---------------------------------------------------------------------------

// TestShouldStartFreshSession pins the rule that keeps amnesia prevention intact
// while unblocking a session the agent has definitively lost.
func TestShouldStartFreshSession(t *testing.T) {
	sessionGone := fmt.Errorf("acp: session x ResumeSession failed: %w: %w",
		errSessionGone, &acp.RequestError{Code: -32602, Message: "Invalid params: session not found"})
	disconnect := errors.New("acp: prompt: peer disconnected")

	assert.True(t, shouldStartFreshSession(sessionGone, true),
		"a session the agent no longer has must be rebuilt even when the conversation has history")
	assert.True(t, shouldStartFreshSession(sessionGone, false))

	assert.False(t, shouldStartFreshSession(disconnect, true),
		"a mere disconnect on a session WITH history must never rebuild (amnesia prevention)")
	assert.True(t, shouldStartFreshSession(disconnect, false),
		"a disconnect on an empty session may rebuild")
	assert.False(t, shouldStartFreshSession(nil, false))

	// Only a RECOVERY RPC can prove the session is gone. The same code coming
	// from session/new (an unrelated missing resource) must not drop a session
	// that still exists.
	rawBusinessError := &acp.RequestError{Code: -32602, Message: "Invalid params: session not found"}
	assert.False(t, shouldStartFreshSession(fmt.Errorf("acp: session/new: %w", rawBusinessError), false),
		"an unclassified -32602 must not be treated as a lost session")
}
