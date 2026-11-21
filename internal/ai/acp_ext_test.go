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

	"clawbench/internal/ai/acptest"
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

// ---------------------------------------------------------------------------
// SDK hook wiring
// ---------------------------------------------------------------------------

// newHookClient wires a real ClientSideConnection to the client exactly as
// spawnLocked does: the SDK's unhandled-notification hook is the only path these
// frames can take.
func newHookClient(t *testing.T) (*ClawBenchACPClient, *recordingACPClient, func(string)) {
	t.Helper()
	agentToClientR, agentToClientW := io.Pipe()
	client := NewClawBenchACPClient()
	ch := make(chan StreamEvent, 8)
	client.RegisterSession("s1", ch)

	rec := &recordingACPClient{ClawBenchACPClient: client}
	conn := acp.NewClientSideConnection(rec, io.Discard, agentToClientR)
	conn.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
	conn.SetUnhandledHandler(client.handleUnhandledNotification)

	write := func(line string) {
		_, err := agentToClientW.Write([]byte(line + "\n"))
		require.NoError(t, err)
	}
	settle := func() { time.Sleep(150 * time.Millisecond) }
	return client, rec, func(line string) {
		write(line)
		settle()
	}
}

// An unknown session/update variant reaches the extension handler with its
// payload intact — the SDK cannot decode it, so the hook is its only path.
func TestUnhandled_UnknownVariantDelivered(t *testing.T) {
	c, _, send := newHookClient(t)
	var got []ExtensionUpdate
	c.SetExtensionUpdateHandler(func(u ExtensionUpdate) { got = append(got, u) })

	send(`{"jsonrpc":"2.0","method":"session/update","params":{` +
		`"sessionId":"s1","update":{"sessionUpdate":"subagent_spawned",` +
		`"subagentSessionId":"child-1","name":"probe-alpha","task":"do work"}}}`)

	require.Len(t, got, 1)
	assert.Equal(t, "s1", got[0].SessionID)
	assert.Equal(t, "subagent_spawned", got[0].Variant)
	assert.Contains(t, string(got[0].Params), "probe-alpha")
	assert.Contains(t, string(got[0].Params), "child-1")
}

// A known variant stays on the typed path: it decodes, so the hook must not
// fire and the frame must not be processed twice.
func TestUnhandled_KnownVariantStaysTyped(t *testing.T) {
	c, rec, send := newHookClient(t)
	called := false
	c.SetExtensionUpdateHandler(func(ExtensionUpdate) { called = true })

	send(`{"jsonrpc":"2.0","method":"session/update","params":{` +
		`"sessionId":"s1","update":{"sessionUpdate":"agent_message_chunk",` +
		`"content":{"type":"text","text":"hi"}}}}`)

	assert.False(t, called, "known variants are the typed callback's job")
	require.Len(t, rec.got, 1)
	require.NotNil(t, rec.got[0].Update.AgentMessageChunk)
}

// A custom notification method reaches the notification handler; the same frame
// gets the -32601 the SDK would have answered.
func TestUnhandled_CustomMethodDelivered(t *testing.T) {
	c, _, send := newHookClient(t)
	var got []ExtensionNotification
	c.SetExtensionNotificationHandler(func(n ExtensionNotification) { got = append(got, n) })

	send(`{"jsonrpc":"2.0","method":"_codebuddy.ai/artifact","params":{` +
		`"sessionId":"s1","event":"created","artifact":{"uri":"file:///x"}}}`)

	require.Len(t, got, 1)
	assert.Equal(t, "_codebuddy.ai/artifact", got[0].Method)
	assert.Contains(t, string(got[0].Params), "file:///x")
}

// A spec method the client does implement never reaches the hook. (The
// recording client forwards to this one, so session/update is implemented.)
func TestUnhandled_ImplementedMethodNotReported(t *testing.T) {
	c, _, send := newHookClient(t)
	called := false
	c.SetExtensionNotificationHandler(func(ExtensionNotification) { called = true })
	c.SetExtensionUpdateHandler(func(ExtensionUpdate) { called = true })

	send(`{"jsonrpc":"2.0","method":"session/update","params":{` +
		`"sessionId":"s1","update":{"sessionUpdate":"agent_thought_chunk",` +
		`"content":{"type":"text","text":"hm"}}}}`)
	assert.False(t, called, "a decodable update is handled by the SDK dispatch")
}

// An unknown variant for a session with no active stream is dropped, matching
// the typed path's rule.
func TestUnhandled_NoRouteDropped(t *testing.T) {
	c, _, send := newHookClient(t)
	called := false
	c.SetExtensionUpdateHandler(func(ExtensionUpdate) { called = true })

	send(`{"jsonrpc":"2.0","method":"session/update","params":{` +
		`"sessionId":"unknown","update":{"sessionUpdate":"subagent_spawned"}}}`)
	assert.False(t, called, "no route → drop")
}

// With no handler registered the frames are a logged no-op, not a panic.
func TestUnhandled_NoHandler(t *testing.T) {
	_, _, send := newHookClient(t)
	assert.NotPanics(t, func() {
		send(`{"jsonrpc":"2.0","method":"session/update","params":{` +
			`"sessionId":"s1","update":{"sessionUpdate":"subagent_spawned"}}}`)
		send(`{"jsonrpc":"2.0","method":"_codebuddy.ai/command","params":{}}`)
	})
}

// ---------------------------------------------------------------------------
// Discriminator validation (typed path guard)
// ---------------------------------------------------------------------------

// sessionUpdateVariantMismatch must flag a variant the SDK parsed into the
// WRONG known struct (its preserved discriminator differs from the struct's).
func TestSessionUpdateVariantMismatch(t *testing.T) {
	// The SDK now rejects a foreign discriminator at decode time; the guard is
	// the backstop for any frame that still lands in a look-alike struct.
	var u acp.SessionUpdate
	assert.Error(t, json.Unmarshal([]byte(`{"sessionUpdate":"tool_call_pending","toolCallId":"x","title":"Bash"}`), &u),
		"a foreign discriminator must not decode into a known variant")

	var misclassified acp.SessionUpdate
	require.NoError(t, json.Unmarshal([]byte(`{"sessionUpdate":"tool_call","toolCallId":"x","title":"Bash"}`), &misclassified))
	require.NotNil(t, misclassified.ToolCall)
	misclassified.ToolCall.SessionUpdate = "tool_call_pending"
	assert.True(t, sessionUpdateVariantMismatch(misclassified), "misclassified variant must be flagged")

	// A genuine tool_call is not flagged.
	var ok acp.SessionUpdate
	require.NoError(t, json.Unmarshal([]byte(`{"sessionUpdate":"tool_call","toolCallId":"x","title":"Bash"}`), &ok))
	assert.False(t, sessionUpdateVariantMismatch(ok))

	// An unknown variant no longer decodes at all.
	var si acp.SessionUpdate
	assert.Error(t, json.Unmarshal([]byte(`{"sessionUpdate":"model_update","model":"m"}`), &si))
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
		{"plan_update", "plan_update", "plan_update_pending", `"plan":{"type":"items","planId":"p1","entries":[]}`},
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

			// A foreign discriminator no longer lands in a look-alike struct:
			// the SDK rejects it. If a future release starts decoding one
			// again, the guard must flag it — that is what this branch pins.
			var foreign acp.SessionUpdate
			if err := json.Unmarshal([]byte(`{"sessionUpdate":"`+tc.foreign+`",`+tc.fields+`}`), &foreign); err == nil {
				assert.True(t, sessionUpdateVariantMismatch(foreign),
					"a %s frame that decoded must be flagged as misclassified", tc.foreign)
			}
		})
	}

	// An update with no union member set is not a mismatch.
	assert.False(t, sessionUpdateVariantMismatch(acp.SessionUpdate{}))
}

// The typed callback must IGNORE a misclassified variant so it never emits a
// bogus tool_use for a frame that is not a tool call.
func TestSessionUpdate_MisclassifiedVariantIgnored(t *testing.T) {
	c := NewClawBenchACPClient()
	ch := make(chan StreamEvent, 8)
	c.RegisterSession("s1", ch)

	// Simulate what an older SDK delivered for tool_call_pending: a ToolCall
	// that kept the foreign discriminator. The v0.20 SDK rejects the frame at
	// decode time, so the shape is built by hand here.
	var u acp.SessionUpdate
	require.NoError(t, json.Unmarshal([]byte(`{"sessionUpdate":"tool_call","toolCallId":"call_x","title":"Bash"}`), &u))
	require.NotNil(t, u.ToolCall)
	u.ToolCall.SessionUpdate = "tool_call_pending"
	n := acp.SessionNotification{SessionId: "s1", Update: u}
	require.NoError(t, c.SessionUpdate(context.Background(), n))

	assert.Empty(t, ch, "a misclassified variant must not produce a tool_use event")
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
			Meta: acptest.PatchMeta(map[string]any{metaKeyCodeBuddyHistoryReplay: "start"}),
		}},
	}
	require.NoError(t, c.SessionUpdate(context.Background(), start))
	assert.False(t, c.ReplayEndSeen(), "start must not mark the replay complete")

	// end marker → set
	end := acp.SessionNotification{
		SessionId: "s1",
		Update: acp.SessionUpdate{SessionInfoUpdate: &acp.SessionSessionInfoUpdate{
			Meta: acptest.PatchMeta(map[string]any{metaKeyCodeBuddyHistoryReplay: "end"}),
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
			Meta: acptest.PatchMeta(map[string]any{metaKeyCodeBuddyHistoryReplay: "end"}),
		}},
	}))
	assert.False(t, c.ReplayEndSeen(), "a live update must not set the replay marker")
}
