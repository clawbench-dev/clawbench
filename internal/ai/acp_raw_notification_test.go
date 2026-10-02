package ai

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// recordingACPClient embeds the real client so it satisfies acp.Client while
// recording the typed notifications the SDK delivers.
type recordingACPClient struct {
	*ClawBenchACPClient
	got []acp.SessionNotification
}

func (c *recordingACPClient) SessionUpdate(_ context.Context, n acp.SessionNotification) error {
	c.got = append(c.got, n)
	return nil
}

// TestSDKMisclassifiesUnknownSessionUpdateVariant documents the exact gap the
// raw notification bypass exists to close.
//
// The ACP Go SDK's SessionUpdate union has no case for extension variants such
// as `subagent_spawned`. Its UnmarshalJSON falls through to a set of
// discriminator-only blocks, and SessionInfoUpdate's block matches on the mere
// presence of `sessionUpdate` — so an unknown variant is NOT dropped, it is
// silently re-typed as SessionInfoUpdate with:
//   - SessionUpdate = the raw discriminator ("subagent_spawned"), and
//   - every payload field (subagentSessionId / name / task) LOST.
//
// The typed callback therefore fires, but with nothing usable. A known variant
// in the same shape is classified correctly, so the difference is the
// discriminator, not the transport.
func TestSDKMisclassifiesUnknownSessionUpdateVariant(t *testing.T) {
	clientReads, agentWrites := io.Pipe()

	base := NewClawBenchACPClient()
	client := &recordingACPClient{ClawBenchACPClient: base}

	conn := acp.NewClientSideConnection(client, io.Discard, clientReads)
	conn.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))

	write := func(line string) {
		_, err := agentWrites.Write([]byte(line + "\n"))
		require.NoError(t, err)
	}
	settle := func() { time.Sleep(150 * time.Millisecond) }

	// 1. Unknown extension variant.
	write(`{"jsonrpc":"2.0","method":"session/update","params":{` +
		`"sessionId":"s1","update":{"sessionUpdate":"subagent_spawned",` +
		`"subagentSessionId":"child-1","name":"probe-alpha","task":"do work"}}}`)
	settle()
	require.Len(t, client.got, 1, "the SDK still delivers the frame (it does not drop it)")
	got := client.got[0].Update
	require.NotNil(t, got.SessionInfoUpdate, "unknown variant is misclassified as SessionInfoUpdate")
	assert.Equal(t, "subagent_spawned", got.SessionInfoUpdate.SessionUpdate,
		"the discriminator string survives")
	// The payload is gone: SessionInfoUpdate has no field for it, and the
	// fallback block does not populate Meta.
	assert.Empty(t, got.SessionInfoUpdate.Meta, "extension payload fields are lost")

	// 2. Known variant: same transport, classified correctly.
	write(`{"jsonrpc":"2.0","method":"session/update","params":{` +
		`"sessionId":"s1","update":{"sessionUpdate":"agent_message_chunk",` +
		`"content":{"type":"text","text":"hello"}}}}`)
	settle()
	require.Len(t, client.got, 2)
	require.NotNil(t, client.got[1].Update.AgentMessageChunk)
	assert.Nil(t, client.got[1].Update.SessionInfoUpdate)
	assert.Equal(t, "hello", client.got[1].Update.AgentMessageChunk.Content.Text.Text)
}

// ---------------------------------------------------------------------------
// DispatchRawNotification — client-side routing
// ---------------------------------------------------------------------------

func newRawNotifClient(t *testing.T) *ClawBenchACPClient {
	t.Helper()
	c := NewClawBenchACPClient()
	ch := make(chan StreamEvent, 4)
	c.RegisterSession("s1", ch)
	return c
}

// An unknown variant on an active session reaches the handler with its payload
// intact — the whole point of the bypass.
func TestDispatchRawNotification_UnknownVariantDelivered(t *testing.T) {
	c := newRawNotifClient(t)
	var got []ExtensionUpdate
	c.SetExtensionUpdateHandler(func(u ExtensionUpdate) { got = append(got, u) })

	c.DispatchRawNotification([]byte(`{"jsonrpc":"2.0","method":"session/update","params":{` +
		`"sessionId":"s1","update":{"sessionUpdate":"subagent_spawned",` +
		`"subagentSessionId":"child-1","name":"probe-alpha","task":"do work"}}}`))

	require.Len(t, got, 1)
	assert.Equal(t, "s1", got[0].SessionID)
	assert.Equal(t, "subagent_spawned", got[0].Variant)
	// The payload the SDK would have dropped is intact.
	assert.Contains(t, string(got[0].Params), "probe-alpha")
	assert.Contains(t, string(got[0].Params), "child-1")
}

// A known variant must NOT be handed to the extension handler: the SDK's typed
// callback already handles it, and doing both would double-process every frame.
func TestDispatchRawNotification_KnownVariantIgnored(t *testing.T) {
	c := newRawNotifClient(t)
	called := false
	c.SetExtensionUpdateHandler(func(ExtensionUpdate) { called = true })

	c.DispatchRawNotification([]byte(`{"jsonrpc":"2.0","method":"session/update","params":{` +
		`"sessionId":"s1","update":{"sessionUpdate":"agent_message_chunk",` +
		`"content":{"type":"text","text":"hi"}}}}`))
	assert.False(t, called, "known variants are the SDK's job")
}

// Non-session/update lines are ignored.
func TestDispatchRawNotification_IgnoresOtherLines(t *testing.T) {
	c := newRawNotifClient(t)
	called := false
	c.SetExtensionUpdateHandler(func(ExtensionUpdate) { called = true })

	c.DispatchRawNotification([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	c.DispatchRawNotification([]byte(`{"jsonrpc":"2.0","method":"session/request_permission","params":{}}`))
	assert.False(t, called)
}

// An unknown variant for a session with no active stream is dropped, matching
// the typed path's rule.
func TestDispatchRawNotification_NoRouteDropped(t *testing.T) {
	c := NewClawBenchACPClient()
	called := false
	c.SetExtensionUpdateHandler(func(ExtensionUpdate) { called = true })

	c.DispatchRawNotification([]byte(`{"jsonrpc":"2.0","method":"session/update","params":{` +
		`"sessionId":"unknown","update":{"sessionUpdate":"subagent_spawned"}}}`))
	assert.False(t, called, "no route → drop")
}

// With no handler registered the variant is ignored (default off).
func TestDispatchRawNotification_NoHandler(t *testing.T) {
	c := newRawNotifClient(t)
	// Should not panic.
	c.DispatchRawNotification([]byte(`{"jsonrpc":"2.0","method":"session/update","params":{` +
		`"sessionId":"s1","update":{"sessionUpdate":"subagent_spawned"}}}`))
}

// ---------------------------------------------------------------------------
// Discriminator validation (typed path guard)
// ---------------------------------------------------------------------------

// sessionUpdateVariantMismatch must flag a variant the SDK parsed into the
// WRONG known struct (its preserved discriminator differs from the struct's).
func TestSessionUpdateVariantMismatch(t *testing.T) {
	// tool_call_pending carries toolCallId+title, so the SDK parses it as
	// ToolCall while keeping the original discriminator.
	var u acp.SessionUpdate
	require.NoError(t, json.Unmarshal([]byte(`{"sessionUpdate":"tool_call_pending","toolCallId":"x","title":"Bash"}`), &u))
	require.NotNil(t, u.ToolCall)
	assert.Equal(t, "tool_call_pending", u.ToolCall.SessionUpdate)
	assert.True(t, sessionUpdateVariantMismatch(u), "misclassified variant must be flagged")

	// A genuine tool_call is not flagged.
	var ok acp.SessionUpdate
	require.NoError(t, json.Unmarshal([]byte(`{"sessionUpdate":"tool_call","toolCallId":"x","title":"Bash"}`), &ok))
	assert.False(t, sessionUpdateVariantMismatch(ok))

	// Unknown variant swallowed as SessionInfoUpdate is flagged too.
	var si acp.SessionUpdate
	require.NoError(t, json.Unmarshal([]byte(`{"sessionUpdate":"model_update","model":"m"}`), &si))
	require.NotNil(t, si.SessionInfoUpdate)
	assert.True(t, sessionUpdateVariantMismatch(si))
}

// Every known union member must be checked against its own discriminator: a
// missing case would let a misclassified frame of that shape through as if it
// were real (e.g. a `plan_removed` parsed into the Plan struct). Table-driven so
// adding a union member without wiring its guard is caught here.
//
// The SDK's fallback matches on field presence, not the discriminator: it picks
// the first struct whose required keys are all present, so a foreign name over
// the same fields lands in that struct while SessionUpdate keeps the foreign
// name. `session_info_update` is the catch-all (it only needs `sessionUpdate`),
// so it is what swallows most unknown variants.
func TestSessionUpdateVariantMismatch_EveryUnionMember(t *testing.T) {
	cases := []struct {
		name    string
		genuine string
		foreign string
		fields  string // JSON members after the discriminator
	}{
		{"user_message_chunk", "user_message_chunk", "user_message_pending", `"content":{"type":"text","text":"hi"}`},
		{"agent_message_chunk", "agent_message_chunk", "agent_message_pending", `"content":{"type":"text","text":"hi"}`},
		{"agent_thought_chunk", "agent_thought_chunk", "agent_thought_pending", `"content":{"type":"text","text":"hi"}`},
		{"tool_call", "tool_call", "tool_call_pending", `"toolCallId":"x","title":"Bash"`},
		{"tool_call_update", "tool_call_update", "tool_call_update_pending", `"toolCallId":"x","status":"completed"`},
		{"plan", "plan", "plan_pending", `"entries":[]`},
		{"plan_update", "plan_update", "plan_update_pending", `"plan":{}`},
		{"plan_removed", "plan_removed", "plan_removed_pending", `"id":"p1"`},
		{"available_commands_update", "available_commands_update", "available_commands_pending", `"availableCommands":[]`},
		{"current_mode_update", "current_mode_update", "current_mode_pending", `"currentModeId":"code"`},
		{"config_option_update", "config_option_update", "config_option_pending", `"configOptions":[]`},
		{"session_info_update", "session_info_update", "model_update", `"title":"t"`},
		{"usage_update", "usage_update", "usage_pending", `"used":1,"size":2`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The genuine variant is not flagged.
			var ok acp.SessionUpdate
			require.NoError(t, json.Unmarshal([]byte(`{"sessionUpdate":"`+tc.genuine+`",`+tc.fields+`}`), &ok))
			assert.False(t, sessionUpdateVariantMismatch(ok),
				"a genuine %s must not be flagged", tc.name)

			// A foreign discriminator over the same fields is flagged. Which
			// struct the SDK's field-presence match picks is its own concern;
			// what must hold is that the mismatch is detected.
			var foreign acp.SessionUpdate
			require.NoError(t, json.Unmarshal([]byte(`{"sessionUpdate":"`+tc.foreign+`",`+tc.fields+`}`), &foreign))
			assert.True(t, sessionUpdateVariantMismatch(foreign),
				"a %s frame must be flagged as misclassified", tc.foreign)
		})
	}

	// An update with no union member set is not a mismatch.
	assert.False(t, sessionUpdateVariantMismatch(acp.SessionUpdate{}))
}

// A malformed line (not JSON) is dropped without panicking: it runs on the
// stdout pump goroutine, so a panic would kill the connection.
func TestDispatchRawNotification_MalformedLineIgnored(t *testing.T) {
	c := newRawNotifClient(t)
	called := false
	c.SetExtensionNotificationHandler(func(ExtensionNotification) { called = true })

	c.DispatchRawNotification([]byte(`{not json`))
	assert.False(t, called)
}

// session/update with malformed params must not reach the extension handler —
// the probe unmarshal fails and the frame is dropped.
func TestDispatchRawNotification_MalformedSessionUpdateParams(t *testing.T) {
	c := newRawNotifClient(t)
	called := false
	c.SetExtensionUpdateHandler(func(ExtensionUpdate) { called = true })

	c.DispatchRawNotification([]byte(`{"jsonrpc":"2.0","method":"session/update","params":"not-an-object"}`))
	assert.False(t, called)
}

// An extension notification with no registered handler is a logged no-op, not a
// panic — the default state before a backend wires its handler.
func TestDispatchRawNotification_CustomMethodNoHandler(t *testing.T) {
	c := newRawNotifClient(t)
	assert.NotPanics(t, func() {
		c.DispatchRawNotification([]byte(`{"jsonrpc":"2.0","method":"_codebuddy.ai/command","params":{}}`))
	})
}

// An empty method is not a notification (a response carries id + no method).
func TestDispatchRawNotification_EmptyMethodIgnored(t *testing.T) {
	c := newRawNotifClient(t)
	called := false
	c.SetExtensionNotificationHandler(func(ExtensionNotification) { called = true })

	c.DispatchRawNotification([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	assert.False(t, called)
}

// The typed callback must IGNORE a misclassified variant so it never emits a
// bogus tool_use for a frame that is not a tool call.
func TestSessionUpdate_MisclassifiedVariantIgnored(t *testing.T) {
	c := NewClawBenchACPClient()
	ch := make(chan StreamEvent, 8)
	c.RegisterSession("s1", ch)

	// Simulate what the SDK delivers for tool_call_pending.
	var u acp.SessionUpdate
	require.NoError(t, json.Unmarshal([]byte(`{"sessionUpdate":"tool_call_pending","toolCallId":"call_x","title":"Bash"}`), &u))
	n := acp.SessionNotification{SessionId: "s1", Update: u}
	require.NoError(t, c.SessionUpdate(context.Background(), n))

	assert.Empty(t, ch, "a misclassified variant must not produce a tool_use event")
}

// ---------------------------------------------------------------------------
// Custom notification methods
// ---------------------------------------------------------------------------

// A custom agent→client notification method reaches the handler with its params
// intact.
func TestDispatchRawNotification_CustomMethodDelivered(t *testing.T) {
	c := newRawNotifClient(t)
	var got []ExtensionNotification
	c.SetExtensionNotificationHandler(func(n ExtensionNotification) { got = append(got, n) })

	c.DispatchRawNotification([]byte(`{"jsonrpc":"2.0","method":"_codebuddy.ai/artifact","params":{` +
		`"sessionId":"s1","event":"created","artifact":{"uri":"file:///x"}}}`))

	require.Len(t, got, 1)
	assert.Equal(t, "_codebuddy.ai/artifact", got[0].Method)
	assert.Contains(t, string(got[0].Params), "file:///x")
}

// Spec client methods are the SDK's job, not the extension handler's.
func TestDispatchRawNotification_SpecMethodIgnored(t *testing.T) {
	c := newRawNotifClient(t)
	called := false
	c.SetExtensionNotificationHandler(func(ExtensionNotification) { called = true })

	for _, m := range []string{"session/request_permission", "fs/read_text_file", "terminal/output", "elicitation/create"} {
		c.DispatchRawNotification([]byte(`{"jsonrpc":"2.0","method":"` + m + `","params":{}}`))
	}
	assert.False(t, called, "spec methods are the SDK's job")
}

// Requests (with an id) are not notifications and must be ignored here.
func TestDispatchRawNotification_RequestIgnored(t *testing.T) {
	c := newRawNotifClient(t)
	called := false
	c.SetExtensionNotificationHandler(func(ExtensionNotification) { called = true })

	// A request: has both method and id.
	c.DispatchRawNotification([]byte(`{"jsonrpc":"2.0","id":7,"method":"_codebuddy.ai/question","params":{}}`))
	assert.False(t, called, "requests are not notifications")
}

// Responses (id + result, no method) are ignored.
func TestDispatchRawNotification_ResponseIgnored(t *testing.T) {
	c := newRawNotifClient(t)
	called := false
	c.SetExtensionNotificationHandler(func(ExtensionNotification) { called = true })

	c.DispatchRawNotification([]byte(`{"jsonrpc":"2.0","id":"cb-1","result":{}}`))
	c.DispatchRawNotification([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32601}}`))
	assert.False(t, called)
}

// session/update lines go to the VARIANT handler, not the notification handler.
func TestDispatchRawNotification_SessionUpdateNotTreatedAsMethod(t *testing.T) {
	c := newRawNotifClient(t)
	notifCalled, variantCalled := false, false
	c.SetExtensionNotificationHandler(func(ExtensionNotification) { notifCalled = true })
	c.SetExtensionUpdateHandler(func(ExtensionUpdate) { variantCalled = true })

	c.DispatchRawNotification([]byte(`{"jsonrpc":"2.0","method":"session/update","params":{` +
		`"sessionId":"s1","update":{"sessionUpdate":"subagent_spawned"}}}`))
	assert.True(t, variantCalled)
	assert.False(t, notifCalled, "session/update must not go to the method handler")
}

// ---------------------------------------------------------------------------
// historyReplay end marker
// ---------------------------------------------------------------------------

// A session_info_update carrying historyReplay:"end" during a LoadSession
// replay must set the end marker; anything else must not.
func TestReplayEndMarker_SetOnlyOnEndDuringReplay(t *testing.T) {
	c := NewClawBenchACPClient()
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	conn.loadSessionActive.Store(true)
	c.connRef = conn
	c.RegisterSession("s1", make(chan StreamEvent, 16))
	defer c.ResetReplayEnd()

	// start marker → not end
	start := acp.SessionNotification{
		SessionId: "s1",
		Update: acp.SessionUpdate{SessionInfoUpdate: &acp.SessionSessionInfoUpdate{
			Meta: map[string]any{metaKeyCodeBuddyHistoryReplay: "start"},
		}},
	}
	require.NoError(t, c.SessionUpdate(context.Background(), start))
	assert.False(t, c.ReplayEndSeen(), "start must not mark the replay complete")

	// end marker → set
	end := acp.SessionNotification{
		SessionId: "s1",
		Update: acp.SessionUpdate{SessionInfoUpdate: &acp.SessionSessionInfoUpdate{
			Meta: map[string]any{metaKeyCodeBuddyHistoryReplay: "end"},
		}},
	}
	require.NoError(t, c.SessionUpdate(context.Background(), end))
	assert.True(t, c.ReplayEndSeen())

	// Reset clears it (a new replay must judge on its own marker).
	c.ResetReplayEnd()
	assert.False(t, c.ReplayEndSeen())
}

// The marker is only honored during a replay; a live session_info_update with
// the key must not set it.
func TestReplayEndMarker_IgnoredWhenNotReplaying(t *testing.T) {
	c := NewClawBenchACPClient()
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	// loadSessionActive stays false.
	c.connRef = conn
	c.RegisterSession("s1", make(chan StreamEvent, 16))
	defer c.ResetReplayEnd()

	require.NoError(t, c.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: "s1",
		Update: acp.SessionUpdate{SessionInfoUpdate: &acp.SessionSessionInfoUpdate{
			Meta: map[string]any{metaKeyCodeBuddyHistoryReplay: "end"},
		}},
	}))
	assert.False(t, c.ReplayEndSeen(), "a live update must not set the replay marker")
}
