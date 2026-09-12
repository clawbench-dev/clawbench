package codebuddy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ai"
	"clawbench/internal/model"
)

// fakeTransport records the calls a policy makes and replays a canned response.
// It exists because the policy only depends on the narrow ai.RawRPCTransport
// interface — no live ACP connection needed to test it.
type fakeTransport struct {
	sessionID string

	calls       []string
	lastParams  any
	response    json.RawMessage
	err         error
	callRawUsed bool
}

func (f *fakeTransport) CallRaw(_ context.Context, method string, params any) (json.RawMessage, error) {
	f.callRawUsed = true
	f.calls = append(f.calls, method)
	f.lastParams = params
	if f.err != nil {
		return nil, f.err
	}
	return f.response, nil
}

func (f *fakeTransport) AcpSessionID() string { return f.sessionID }

func mustSteerResponse(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// TestMidTurnInjector_InjectedOnSteeredTrue verifies the happy path: the agent
// accepted the message into the running turn, and we surface the owner turn id.
func TestMidTurnInjector_InjectedOnSteeredTrue(t *testing.T) {
	tr := &fakeTransport{
		sessionID: "acp-sess-1",
		response:  mustSteerResponse(t, map[string]any{"steered": true, "ownerRequestId": "req-abc"}),
	}

	res, err := (&midTurnInjector{}).Inject(context.Background(), tr, ai.MidTurnInjectRequest{
		SessionID:           "cb-sess",
		Content:             "hello",
		ClientUserMessageID: "pending-123",
	})

	require.NoError(t, err)
	assert.True(t, res.Injected)
	assert.Equal(t, "req-abc", res.OwnerRequestID)
	require.Len(t, tr.calls, 1)
	assert.Equal(t, "session/steer", tr.calls[0])
}

// TestMidTurnInjector_RequestShape pins the wire payload: a wrong sessionId,
// block type or missing client id would silently break injection.
func TestMidTurnInjector_RequestShape(t *testing.T) {
	tr := &fakeTransport{
		sessionID: "acp-sess-9",
		response:  mustSteerResponse(t, map[string]any{"steered": true, "ownerRequestId": "r"}),
	}

	_, err := (&midTurnInjector{}).Inject(context.Background(), tr, ai.MidTurnInjectRequest{
		Content:             "the text",
		ClientUserMessageID: "pending-9",
	})
	require.NoError(t, err)

	params, ok := tr.lastParams.(steerParams)
	require.True(t, ok, "expected steerParams, got %T", tr.lastParams)
	assert.Equal(t, "acp-sess-9", params.SessionID, "must use the ACP session id, not the ClawBench one")
	assert.Equal(t, "pending-9", params.ClientUserMessageID)
	require.Len(t, params.ContentBlocks, 1)
	assert.Equal(t, "text", params.ContentBlocks[0].Type)
	assert.Equal(t, "the text", params.ContentBlocks[0].Text)
}

// TestMidTurnInjector_DeclinesWhenTurnEnded covers the {steered:false} replies:
// "idle" and "stale" both mean "the turn is not there any more, queue it".
func TestMidTurnInjector_DeclinesWhenTurnEnded(t *testing.T) {
	for _, reason := range []string{"idle", "stale"} {
		t.Run(reason, func(t *testing.T) {
			tr := &fakeTransport{
				sessionID: "s",
				response:  mustSteerResponse(t, map[string]any{"steered": false, "reason": reason}),
			}
			res, err := (&midTurnInjector{}).Inject(context.Background(), tr, ai.MidTurnInjectRequest{Content: "x"})
			require.NoError(t, err)
			assert.False(t, res.Injected)
			assert.Equal(t, reason, res.Reason)
		})
	}
}

// TestMidTurnInjector_UnknownDeclineReasonIsRejected verifies an unexpected
// {steered:false} reason is normalized rather than passed through raw.
func TestMidTurnInjector_UnknownDeclineReasonIsRejected(t *testing.T) {
	tr := &fakeTransport{
		sessionID: "s",
		response:  mustSteerResponse(t, map[string]any{"steered": false, "reason": "something-new"}),
	}
	res, err := (&midTurnInjector{}).Inject(context.Background(), tr, ai.MidTurnInjectRequest{Content: "x"})
	require.NoError(t, err)
	assert.False(t, res.Injected)
	assert.Equal(t, "rejected", res.Reason)
}

// TestMidTurnInjector_TransportErrorIsReported verifies a dead connection
// surfaces as an error (logged by the seam) and not as a bogus success.
func TestMidTurnInjector_TransportErrorIsReported(t *testing.T) {
	tr := &fakeTransport{sessionID: "s", err: errors.New("connection closed")}
	res, err := (&midTurnInjector{}).Inject(context.Background(), tr, ai.MidTurnInjectRequest{Content: "x"})
	require.Error(t, err)
	assert.False(t, res.Injected)
	assert.Equal(t, "error", res.Reason)
}

// TestMidTurnInjector_MalformedResponseIsError verifies a non-JSON reply cannot
// be mistaken for acceptance.
func TestMidTurnInjector_MalformedResponseIsError(t *testing.T) {
	tr := &fakeTransport{sessionID: "s", response: json.RawMessage(`not json`)}
	res, err := (&midTurnInjector{}).Inject(context.Background(), tr, ai.MidTurnInjectRequest{Content: "x"})
	require.Error(t, err)
	assert.False(t, res.Injected)
	assert.Equal(t, "error", res.Reason)
}

// TestMidTurnInjector_NoSessionMeansIdle verifies we never issue a steer without
// an ACP session — there is nothing running to inject into.
func TestMidTurnInjector_NoSessionMeansIdle(t *testing.T) {
	tr := &fakeTransport{sessionID: ""}
	res, err := (&midTurnInjector{}).Inject(context.Background(), tr, ai.MidTurnInjectRequest{Content: "x"})
	require.NoError(t, err)
	assert.False(t, res.Injected)
	assert.Equal(t, "idle", res.Reason)
	assert.False(t, tr.callRawUsed, "must not call session/steer without an ACP session id")
}

// TestMidTurnInjector_AttachmentsDecline verifies messages carrying files fall
// back to the queue path, which owns the file/directory prompt prefixes.
func TestMidTurnInjector_AttachmentsDecline(t *testing.T) {
	tr := &fakeTransport{
		sessionID: "s",
		response:  mustSteerResponse(t, map[string]any{"steered": true}),
	}
	res, err := (&midTurnInjector{}).Inject(context.Background(), tr, ai.MidTurnInjectRequest{
		Content: "see file",
		Files:   []model.FileEntry{{Path: "/tmp/a.go"}},
	})
	require.NoError(t, err)
	assert.False(t, res.Injected)
	assert.Equal(t, "unsupported", res.Reason)
	assert.False(t, tr.callRawUsed, "must not steer a message with attachments")
}
