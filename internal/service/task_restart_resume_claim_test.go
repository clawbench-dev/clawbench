package service

import (
	"context"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A resumed run must not start a second turn on a session another run already
// owns — two turns would interleave their blocks in one session. When the claim
// is lost the interruption stays on the row so the next startup retries it.
func TestResumeExecution_LeavesMarkerWhenSessionIsBusy(t *testing.T) {
	backend := newResumeBackend()
	s, _, sessionID, execID := seedInterruptedExecution(t, backend, "active")

	// Something else is already driving this session.
	claimCtx, claimed := tryClaimSessionRun(sessionID)
	require.True(t, claimed, "the test needs the session claim to succeed first")
	t.Cleanup(func() { FinishClaimedSessionRun(claimCtx, sessionID) })

	require.NoError(t, s.resumeExecution(execID))

	status, flag := executionRow(t, execID)
	assert.Equal(t, statusCancelled, status,
		"the row must go back to describing the interrupted run, not claim to be running")
	assert.Equal(t, 1, flag,
		"the interruption is still unaccounted for; the next startup must retry it")
	assert.Empty(t, backend.lastPrompt(), "no turn may start while another run owns the session")
}

// Shutdown may have cancelled the claim before the body starts (claim release
// runs after the body returns), so the body re-checks and aborts quietly.
func TestRunResumedExecution_AbortsWhenContextIsAlreadyCancelled(t *testing.T) {
	backend := newResumeBackend()
	s, task, sessionID, execID := seedInterruptedExecution(t, backend, "active")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s.runResumedExecution(ctx, execID, task, model.Agents["test-agent"], sessionID, "event")

	assert.Empty(t, backend.lastPrompt(), "a cancelled claim must not reach the backend")
}
