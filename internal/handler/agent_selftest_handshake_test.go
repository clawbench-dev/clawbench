package handler

import (
	"context"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The handshake exists to name the model the agent reports for itself. When the
// connection cannot be brought up there is nothing trustworthy to report, and
// inventing a model would make the self-test claim it exercised one it never
// asked for.
func TestHandshakeModelForSelfTest_EmptyWhenConnectionFails(t *testing.T) {
	defer setupAgentTestEnv(t)()

	agent := model.GetAgent("claude")
	require.NotNil(t, agent)
	agent.AcpCommand = "/nonexistent/clawbench-selftest-agent"
	t.Cleanup(func() { agent.AcpCommand = "claude acp" })

	got := handshakeModelForSelfTest(context.Background(), agent, "self-test-session", t.TempDir())
	assert.Empty(t, got)
}
