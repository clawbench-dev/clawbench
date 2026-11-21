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

func TestAsyncCommandReply(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := &asyncCommandReply{commands: make(map[acp.ToolCallId]bool)}
		usage := acp.SessionUpdate{UsageUpdate: &acp.SessionUsageUpdate{}}
		r.observe(commandUpdate("daemon", true))
		require.NoError(t, r.wait(t.Context(), nil))
		r.observe(commandUpdate("one", false))
		r.observe(commandUpdate("two", false))
		r.observe(agentTextUpdate("waiting"))
		r.observe(usage)
		r.observe(acp.UpdateToolCall("one", acp.WithUpdateStatus(acp.ToolCallStatusCompleted)))
		done := make(chan error, 1)
		go func() { done <- r.wait(t.Context(), nil) }()
		synctest.Wait()
		require.Empty(t, done)
		r.observe(agentTextUpdate("one finished"))
		r.observe(usage)
		synctest.Wait()
		require.Empty(t, done)
		r.observe(acp.UpdateToolCall("two", acp.WithUpdateStatus(acp.ToolCallStatusFailed)))
		r.observe(usage)
		synctest.Wait()
		require.Empty(t, done)
		r.observe(agentTextUpdate("checking the result"))
		r.observe(usage)
		r.observe(commandUpdate("three", false))
		synctest.Wait()
		require.Empty(t, done)
		r.observe(acp.UpdateToolCall("three", acp.WithUpdateStatus(acp.ToolCallStatusCompleted)))
		r.observe(agentTextUpdate("finished"))
		r.observe(usage)
		require.NoError(t, <-done)
	})
}

func TestAsyncCommandReplyCompletionBeforeWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := NewClawBenchACPClient()
		require.Nil(t, client.trackCommandReply("ordinary"))
		client.commandReplies = make(map[string]*asyncCommandReply)
		r := client.trackCommandReply("native")
		usage := acp.SessionUpdate{UsageUpdate: &acp.SessionUsageUpdate{}}
		r.observe(commandUpdate("one", false))
		r.observe(agentTextUpdate("waiting"))
		r.observe(usage)
		r.observe(acp.UpdateToolCall("one", acp.WithUpdateStatus(acp.ToolCallStatusCompleted)))
		done := make(chan error, 1)
		go func() { done <- r.wait(t.Context(), nil) }()
		synctest.Wait()
		require.Empty(t, done)
		r.observe(agentTextUpdate("finished"))
		r.observe(usage)
		require.NoError(t, <-done)
		client.UnregisterSession("native")
		require.Empty(t, client.commandReplies)
	})
}

func TestAsyncCommandReplyStops(t *testing.T) {
	r := &asyncCommandReply{commands: make(map[acp.ToolCallId]bool)}
	r.observe(commandUpdate("one", false))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, r.wait(ctx, nil), context.Canceled)
	disconnected := make(chan struct{})
	close(disconnected)
	err := r.wait(t.Context(), disconnected)
	require.Error(t, err)
	require.False(t, isACPPeerDisconnected(err), "an accepted prompt must not be retried")
}
