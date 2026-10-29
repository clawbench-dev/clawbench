package ai

import (
	"context"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// CodeBuddy pushes its plugin/skill commands only after its PluginManager has
// loaded (~4s). A short window would return the built-in list only — i.e. the
// refresh would silently drop exactly the commands that were missing.
func TestProbeCommandWindow_CodeBuddyWaitsForPluginLoad(t *testing.T) {
	maxWait, quiet := probeCommandWindow(&model.Agent{ID: "cb", Backend: "codebuddy"})

	assert.GreaterOrEqual(t, quiet, codebuddyPluginLoadDelay,
		"the list must be considered settled only after the plugin push window")
	assert.Greater(t, maxWait, quiet,
		"maxWait must leave room beyond the quiet window")
}

func TestProbeCommandWindow_OtherBackendsUseShortWindow(t *testing.T) {
	maxWait, quiet := probeCommandWindow(&model.Agent{ID: "claude", Backend: "claude"})

	assert.Less(t, maxWait, codebuddyPluginLoadDelay,
		"a non-CodeBuddy agent must not pay the 4s plugin window")
	assert.Less(t, quiet, maxWait)
}

// The probe must return as soon as the pushed list stops changing, not sit on
// the full window: a refresh that always costs the maximum is a slow refresh.
func TestWaitForProbeCommands_ReturnsWhenListSettles(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "probe-settle"}, "probe-settle")
	client := NewClawBenchACPClient()
	conn.SetClientForTest(client)

	// The agent's first push: a command already cached before we start waiting.
	client.SetCommands(acpAvailableCommand("/review"))

	start := time.Now()
	cmds := waitForProbeCommands(context.Background(), conn, 2*time.Second, 100*time.Millisecond)
	elapsed := time.Since(start)

	require.Len(t, cmds, 1)
	assert.Equal(t, "/review", cmds[0].Name)
	assert.Less(t, elapsed, 2*time.Second,
		"a stable list must end the wait, not consume the whole window")
}

// A later push (the plugin commands arriving after the built-in ones) must be
// picked up: the result is the list as it stands at the end of the window, not
// the first snapshot.
func TestWaitForProbeCommands_SeesLaterPush(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "probe-late"}, "probe-late")
	client := NewClawBenchACPClient()
	conn.SetClientForTest(client)
	client.SetCommands(acpAvailableCommand("/help"))

	go func() {
		time.Sleep(120 * time.Millisecond)
		client.SetCommands(acpAvailableCommand("/help", "/skill"))
	}()

	cmds := waitForProbeCommands(context.Background(), conn, 3*time.Second, 200*time.Millisecond)

	require.Len(t, cmds, 2, "the later push must be in the result")
	assert.Equal(t, "/skill", cmds[1].Name)
}

// An agent that never pushes anything must not hang the refresh: the window is
// the bound, and an empty result means "keep what we had".
func TestWaitForProbeCommands_TimesOutWithEmptyResult(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "probe-silent"}, "probe-silent")
	conn.SetClientForTest(NewClawBenchACPClient())

	start := time.Now()
	cmds := waitForProbeCommands(context.Background(), conn, 300*time.Millisecond, 100*time.Millisecond)

	assert.Empty(t, cmds)
	assert.Less(t, time.Since(start), 2*time.Second)
}

// A cancelled request (the user closed the drawer) must return immediately
// rather than run out the window.
func TestWaitForProbeCommands_RespectsContext(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "probe-cancel"}, "probe-cancel")
	client := NewClawBenchACPClient()
	conn.SetClientForTest(client)
	client.SetCommands(acpAvailableCommand("/help"))

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	cmds := waitForProbeCommands(ctx, conn, 5*time.Second, 4*time.Second)

	require.Len(t, cmds, 1)
	assert.Less(t, time.Since(start), time.Second)
}

// The probe's session/delete is unconditional: CodeBuddy implements it without
// advertising sessionCapabilities, so gating on the capability would leak one
// orphan session per refresh. A connection that cannot serve the call reports
// an error instead of panicking.
func TestSendDeleteSession_ReportsUnusableConnection(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "del-unusable"}, "del-unusable")
	err := conn.sendDeleteSession(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not usable")
}

// The capability gate must stay in deleteACPSession (the permanent-delete path),
// not leak into the probe's unconditional call.
func TestSendDeleteSession_IgnoresAdvertisedCapability(t *testing.T) {
	resetGlobalRegistryForTest(t)
	reg := GetAgentCapabilityRegistry()
	agent := &model.Agent{ID: "del-nocap-probe", Backend: "codebuddy", AcpCommand: "codebuddy --acp"}
	reg.UpdateDeleteSession(agent.ID, false)

	conn := newACPConn(agent, "del-nocap-probe")
	conn.SetAliveForTest()
	conn.SetSessionMappingForTest("del-nocap-probe", "acp-probe-1")

	// The pipe-based connection cannot complete the RPC, so the call fails — the
	// point is that it was ATTEMPTED despite the capability being false. A
	// capability-gated implementation would return the "not usable" error
	// instead, which is what distinguishes the two paths.
	err := conn.sendDeleteSession(context.Background())
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "not usable",
		"the call must reach the RPC even when the capability is not advertised")
}

// acpAvailableCommand is a tiny builder keeping the tests readable.
func acpAvailableCommand(names ...string) []acp.AvailableCommand {
	out := make([]acp.AvailableCommand, 0, len(names))
	for _, n := range names {
		out = append(out, acp.AvailableCommand{Name: n})
	}
	return out
}
