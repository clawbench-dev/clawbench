package ai

import (
	"context"
	"errors"
	"testing"

	"clawbench/internal/model"

	acp "github.com/coder/acp-go-sdk"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// opencode recovery cwd
//
// opencode can move an existing session into another directory from inside the
// conversation (its session_move tool, typically into a git worktree). Its ACP
// layer then rejects a resume that carries the old directory with
// "session ... does not belong to cwd", which used to make the conversation
// unrecoverable after a ClawBench restart. Recovery now asks session/list for
// the directory opencode itself reports. These tests pin that behavior without
// spawning an agent.
// ---------------------------------------------------------------------------

// stubCwdRecoverySpecs installs the minimal backend specs these tests need:
// opencode declares ACPRecoversByReportedCwd, claude does not. The real specs
// live in the backend sub-packages, which this package cannot import (cycle),
// so the capability bit has to be declared here explicitly.
func stubCwdRecoverySpecs(t *testing.T) {
	t.Helper()
	restore := model.SetBackendRegistryForTest([]model.BackendSpec{
		{Backend: "opencode", ACPRecoversByReportedCwd: true},
		{Backend: "claude"},
	})
	t.Cleanup(restore)
}

func TestOpenCodeRecoveryCwd_AdoptsAgentReportedDirectory(t *testing.T) {
	stubCwdRecoverySpecs(t)
	requested := t.TempDir()
	moved := t.TempDir()

	conn := newACPConn(&model.Agent{ID: "opencode", Backend: "opencode"}, "sid")
	conn.alive = true
	conn.SetListSessionsFnForTest(func(context.Context, *string) ([]acp.SessionInfo, *string, error) {
		return []acp.SessionInfo{
			{SessionId: "another-session", Cwd: requested},
			{SessionId: "acp-sid", Cwd: moved},
		}, nil, nil
	})

	var resumedCwd string
	conn.SetRecoveryRPCsFnForTest(
		func(_ context.Context, _ string, cwd string) (*acp.ResumeSessionResponse, error) {
			resumedCwd = cwd
			return &acp.ResumeSessionResponse{}, nil
		},
		nil,
	)

	require.NoError(t, conn.recoverExistingSession(context.Background(), requested, "acp-sid", cachedConfigSnapshot{}))
	assert.Equal(t, moved, resumedCwd,
		"recovery must resume at the directory opencode reports, not the stale project root")
}

func TestOpenCodeRecoveryCwd_OnlyAppliesWhenBackendDeclaresIt(t *testing.T) {
	stubCwdRecoverySpecs(t)
	requested := t.TempDir()
	moved := t.TempDir()

	conn := newACPConn(&model.Agent{ID: "claude", Backend: "claude"}, "sid")
	conn.alive = true
	listCalled := false
	conn.SetListSessionsFnForTest(func(context.Context, *string) ([]acp.SessionInfo, *string, error) {
		listCalled = true
		return []acp.SessionInfo{{SessionId: "acp-sid", Cwd: moved}}, nil, nil
	})

	var resumedCwd string
	conn.SetRecoveryRPCsFnForTest(
		func(_ context.Context, _ string, cwd string) (*acp.ResumeSessionResponse, error) {
			resumedCwd = cwd
			return &acp.ResumeSessionResponse{}, nil
		},
		nil,
	)

	require.NoError(t, conn.recoverExistingSession(context.Background(), requested, "acp-sid", cachedConfigSnapshot{}))
	assert.Equal(t, requested, resumedCwd, "a backend without ACPRecoversByReportedCwd must keep resuming with the requested cwd")
	assert.False(t, listCalled, "a backend without ACPRecoversByReportedCwd must not pay for the extra session/list call")
}

func TestOpenCodeRecoveryCwd_PagesUntilSessionFound(t *testing.T) {
	stubCwdRecoverySpecs(t)
	requested := t.TempDir()
	moved := t.TempDir()

	conn := newACPConn(&model.Agent{ID: "opencode", Backend: "opencode"}, "sid")
	conn.alive = true
	var cursors []string
	conn.SetListSessionsFnForTest(func(_ context.Context, cursor *string) ([]acp.SessionInfo, *string, error) {
		cursors = append(cursors, derefString(cursor))
		if cursor == nil {
			next := "page-2"
			return []acp.SessionInfo{{SessionId: "not-it", Cwd: requested}}, &next, nil
		}
		return []acp.SessionInfo{{SessionId: "acp-sid", Cwd: moved}}, nil, nil
	})

	var resumedCwd string
	conn.SetRecoveryRPCsFnForTest(
		func(_ context.Context, _ string, cwd string) (*acp.ResumeSessionResponse, error) {
			resumedCwd = cwd
			return &acp.ResumeSessionResponse{}, nil
		},
		nil,
	)

	require.NoError(t, conn.recoverExistingSession(context.Background(), requested, "acp-sid", cachedConfigSnapshot{}))
	assert.Equal(t, moved, resumedCwd)
	assert.Equal(t, []string{"", "page-2"}, cursors, "recovery must follow nextCursor across pages")
}

func TestOpenCodeRecoveryCwd_KeepsRequestedWhenLookupCannotHelp(t *testing.T) {
	requested := t.TempDir()

	tests := []struct {
		name  string
		setup func(t *testing.T, conn *ACPConn, requested string)
	}{
		{
			name: "list fails",
			setup: func(t *testing.T, conn *ACPConn, _ string) {
				conn.SetListSessionsFnForTest(func(context.Context, *string) ([]acp.SessionInfo, *string, error) {
					return nil, nil, errors.New("list unavailable")
				})
			},
		},
		{
			name: "session not listed",
			setup: func(t *testing.T, conn *ACPConn, _ string) {
				conn.SetListSessionsFnForTest(func(context.Context, *string) ([]acp.SessionInfo, *string, error) {
					return []acp.SessionInfo{{SessionId: "someone-else", Cwd: t.TempDir()}}, nil, nil
				})
			},
		},
		{
			name: "reported directory is gone",
			setup: func(t *testing.T, conn *ACPConn, _ string) {
				conn.SetListSessionsFnForTest(func(context.Context, *string) ([]acp.SessionInfo, *string, error) {
					return []acp.SessionInfo{{SessionId: "acp-sid", Cwd: "/nonexistent/openCode/moved"}}, nil, nil
				})
			},
		},
		{
			name: "reported directory is empty",
			setup: func(t *testing.T, conn *ACPConn, _ string) {
				conn.SetListSessionsFnForTest(func(context.Context, *string) ([]acp.SessionInfo, *string, error) {
					return []acp.SessionInfo{{SessionId: "acp-sid", Cwd: ""}}, nil, nil
				})
			},
		},
		{
			name: "reported directory equals requested",
			setup: func(t *testing.T, conn *ACPConn, requested string) {
				conn.SetListSessionsFnForTest(func(context.Context, *string) ([]acp.SessionInfo, *string, error) {
					return []acp.SessionInfo{{SessionId: "acp-sid", Cwd: requested}}, nil, nil
				})
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stubCwdRecoverySpecs(t)
			conn := newACPConn(&model.Agent{ID: "opencode", Backend: "opencode"}, "sid")
			conn.alive = true
			tc.setup(t, conn, requested)

			var resumedCwd string
			conn.SetRecoveryRPCsFnForTest(
				func(_ context.Context, _ string, cwd string) (*acp.ResumeSessionResponse, error) {
					resumedCwd = cwd
					return &acp.ResumeSessionResponse{}, nil
				},
				nil,
			)

			require.NoError(t, conn.recoverExistingSession(context.Background(), requested, "acp-sid", cachedConfigSnapshot{}))
			assert.Equal(t, requested, resumedCwd,
				"recovery must fall back to the requested cwd when session/list cannot supply a usable one")
		})
	}
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
