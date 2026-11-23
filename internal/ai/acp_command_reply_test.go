package ai

import (
	"context"
	"testing"
	"testing/synctest"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
)

func commandUpdate(id acp.ToolCallId, daemon bool) acp.SessionUpdate {
	return acp.StartToolCall(id, "command", acp.WithStartKind(acp.ToolKindExecute),
		acp.WithStartStatus(acp.ToolCallStatusInProgress),
		acp.WithStartRawInput(map[string]any{"CommandLine": "echo ok", "IsDaemon": daemon}))
}

func usageUpdate() acp.SessionUpdate {
	return acp.SessionUpdate{UsageUpdate: &acp.SessionUsageUpdate{}}
}

// waitResult carries both return values off the waiting goroutine.
type waitResult struct {
	waited bool
	err    error
}

func TestAsyncCommandReply(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := &asyncCommandReply{commands: make(map[acp.ToolCallId]bool)}
		r.observe(commandUpdate("daemon", true))
		waited, err := r.wait(t.Context(), nil)
		require.False(t, waited, "a daemon command must not hold the turn open")
		require.NoError(t, err)

		r.observe(commandUpdate("one", false))
		r.observe(commandUpdate("two", false))
		r.observe(agentTextUpdate("waiting"))
		r.observe(usageUpdate())
		r.observe(acp.UpdateToolCall("one", acp.WithUpdateStatus(acp.ToolCallStatusCompleted)))
		done := make(chan waitResult, 1)
		go func() {
			waited, err := r.wait(t.Context(), nil)
			done <- waitResult{waited: waited, err: err}
		}()
		synctest.Wait()
		require.Empty(t, done)
		r.observe(agentTextUpdate("one finished"))
		r.observe(usageUpdate())
		synctest.Wait()
		require.Empty(t, done)
		r.observe(acp.UpdateToolCall("two", acp.WithUpdateStatus(acp.ToolCallStatusFailed)))
		r.observe(usageUpdate())
		synctest.Wait()
		require.Empty(t, done)
		r.observe(agentTextUpdate("checking the result"))
		r.observe(usageUpdate())
		r.observe(commandUpdate("three", false))
		synctest.Wait()
		require.Empty(t, done)
		r.observe(acp.UpdateToolCall("three", acp.WithUpdateStatus(acp.ToolCallStatusCompleted)))
		r.observe(agentTextUpdate("finished"))
		r.observe(usageUpdate())
		result := <-done
		require.NoError(t, result.err)
		require.True(t, result.waited)
	})
}

// TestAsyncCommandReplyCompletionBeforeWait covers the ordering the CLI can
// produce: the command finishes (and its result streams in) before Prompt gets
// to inspect the response. The waiting generation must survive that so the
// turn is not declared finished on an intermediate frame.
func TestAsyncCommandReplyCompletionBeforeWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := NewClawBenchACPClient()
		require.Nil(t, client.RegisterSession("ordinary", nil).commands)
		client.UnregisterSession("ordinary")
		client.trackFiniteCommands = true
		r := client.RegisterSession("native", nil).commands
		r.observe(commandUpdate("one", false))
		r.observe(agentTextUpdate("waiting"))
		r.observe(usageUpdate())
		r.observe(acp.UpdateToolCall("one", acp.WithUpdateStatus(acp.ToolCallStatusCompleted)))
		done := make(chan waitResult, 1)
		go func() {
			waited, err := r.wait(t.Context(), nil)
			done <- waitResult{waited: waited, err: err}
		}()
		synctest.Wait()
		require.Empty(t, done)
		r.observe(agentTextUpdate("finished"))
		r.observe(usageUpdate())
		result := <-done
		require.NoError(t, result.err)
		require.True(t, result.waited)
		client.UnregisterSession("native")
		require.Empty(t, client.backgroundWork)
	})
}

// TestAsyncCommandReplyNothingToWaitFor is the ordinary turn: no command was
// tracked, so wait must not block and must report that it did not wait.
func TestAsyncCommandReplyNothingToWaitFor(t *testing.T) {
	r := &asyncCommandReply{commands: make(map[acp.ToolCallId]bool)}
	r.observe(agentTextUpdate("hello"))
	r.observe(usageUpdate())
	waited, err := r.wait(t.Context(), nil)
	require.False(t, waited)
	require.NoError(t, err)
}

// TestAsyncCommandReplyIgnoresNonCommandTools guards the shape check: a
// non-execute tool or an execute call without Antigravity's CommandLine is not
// a background command and must not keep the turn open.
func TestAsyncCommandReplyIgnoresNonCommandTools(t *testing.T) {
	r := &asyncCommandReply{commands: make(map[acp.ToolCallId]bool)}
	r.observe(acp.StartToolCall("read", "Read file", acp.WithStartKind(acp.ToolKindRead),
		acp.WithStartRawInput(map[string]any{"path": "a.txt"})))
	r.observe(acp.StartToolCall("other-shape", "Shell", acp.WithStartKind(acp.ToolKindExecute),
		acp.WithStartRawInput(map[string]any{"command": "sleep 5"})))
	r.observe(agentTextUpdate("working"))
	r.observe(usageUpdate())
	waited, err := r.wait(t.Context(), nil)
	require.False(t, waited)
	require.NoError(t, err)
}

func TestAsyncCommandReplyStops(t *testing.T) {
	r := &asyncCommandReply{commands: make(map[acp.ToolCallId]bool)}
	r.observe(commandUpdate("one", false))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	waited, err := r.wait(ctx, nil)
	require.True(t, waited)
	require.ErrorIs(t, err, context.Canceled)
	disconnected := make(chan struct{})
	close(disconnected)
	waited, err = r.wait(t.Context(), disconnected)
	require.True(t, waited)
	require.Error(t, err)
	require.False(t, isACPPeerDisconnected(err), "an accepted prompt must not be retried")
}
