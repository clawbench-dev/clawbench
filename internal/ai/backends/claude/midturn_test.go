package claude

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

// fakeTransport records the calls the policy makes and replays a canned
// response. It exists because the policy only depends on the narrow
// ai.RawRPCTransport interface — no live ACP connection needed to test it.
type fakeTransport struct {
	sessionID      string
	promptInFlight bool

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

func (f *fakeTransport) PromptInFlight() bool { return f.promptInFlight }

func steeringResp(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// TestMidTurnInjector_InjectedOnOutcomeInjected verifies the happy path: the
// adapter joined the running turn and we report Injected=true.
func TestMidTurnInjector_InjectedOnOutcomeInjected(t *testing.T) {
	tr := &fakeTransport{
		sessionID:      "acp-sess-1",
		promptInFlight: true,
		response:       steeringResp(t, map[string]any{"outcome": "injected"}),
	}

	res, err := (&midTurnInjector{}).Inject(context.Background(), tr, ai.MidTurnInjectRequest{
		SessionID: "cb-sess",
		Content:   "hello",
	})

	require.NoError(t, err)
	assert.True(t, res.Injected)
	require.Len(t, tr.calls, 1)
	assert.Equal(t, "_session/steering", tr.calls[0])
}

// TestMidTurnInjector_RequestShape pins the wire payload: sessionId, a prompt
// array (not contentBlocks), and the claude-only idleBehavior option.
func TestMidTurnInjector_RequestShape(t *testing.T) {
	tr := &fakeTransport{
		sessionID:      "acp-sess-9",
		promptInFlight: true,
		response:       steeringResp(t, map[string]any{"outcome": "injected"}),
	}

	_, err := (&midTurnInjector{}).Inject(context.Background(), tr, ai.MidTurnInjectRequest{Content: "the text"})
	require.NoError(t, err)

	params, ok := tr.lastParams.(steerParams)
	require.True(t, ok, "expected steerParams, got %T", tr.lastParams)
	assert.Equal(t, "acp-sess-9", params.SessionID, "must use the ACP session id")
	require.Len(t, params.Prompt, 1)
	assert.Equal(t, "text", params.Prompt[0].Type)
	assert.Equal(t, "the text", params.Prompt[0].Text)
	require.NotNil(t, params.Meta, "claude must send _meta.steering.idleBehavior")
	assert.Equal(t, "promptRequired", params.Meta.Steering.IdleBehavior)
}

// TestMidTurnInjector_DeclinesWhenNoPromptInFlight is the ghost-turn guard:
// without a live turn we must NOT call steering (the adapter would otherwise
// start a turn ClawBench does not own).
func TestMidTurnInjector_DeclinesWhenNoPromptInFlight(t *testing.T) {
	tr := &fakeTransport{
		sessionID:      "s",
		promptInFlight: false,
		response:       steeringResp(t, map[string]any{"outcome": "startedNewTurn"}),
	}
	res, err := (&midTurnInjector{}).Inject(context.Background(), tr, ai.MidTurnInjectRequest{Content: "x"})
	require.NoError(t, err)
	assert.False(t, res.Injected)
	assert.Equal(t, "idle", res.Reason)
	assert.False(t, tr.callRawUsed, "must not call steering when no turn is running")
}

// TestMidTurnInjector_DeclinesWhenNoSession verifies we never issue a steer
// without an ACP session.
func TestMidTurnInjector_NoSessionMeansIdle(t *testing.T) {
	tr := &fakeTransport{sessionID: "", promptInFlight: true}
	res, err := (&midTurnInjector{}).Inject(context.Background(), tr, ai.MidTurnInjectRequest{Content: "x"})
	require.NoError(t, err)
	assert.False(t, res.Injected)
	assert.Equal(t, "idle", res.Reason)
	assert.False(t, tr.callRawUsed)
}

// TestMidTurnInjector_DeclinesOnNonInjectedOutcomes covers every non-"injected"
// outcome: none may be reported as a successful injection.
func TestMidTurnInjector_DeclinesOnNonInjectedOutcomes(t *testing.T) {
	cases := map[string]string{
		"promptRequired": "idle",
		"startedNewTurn": "rejected",
		"failed":         "rejected",
		"something-new":  "rejected",
	}
	for outcome, wantReason := range cases {
		t.Run(outcome, func(t *testing.T) {
			tr := &fakeTransport{
				sessionID:      "s",
				promptInFlight: true,
				response:       steeringResp(t, map[string]any{"outcome": outcome}),
			}
			res, err := (&midTurnInjector{}).Inject(context.Background(), tr, ai.MidTurnInjectRequest{Content: "x"})
			require.NoError(t, err)
			assert.False(t, res.Injected)
			assert.Equal(t, wantReason, res.Reason)
		})
	}
}

// TestMidTurnInjector_TransportErrorIsReported verifies a dead connection
// surfaces as an error (logged by the seam) and not as a bogus success.
func TestMidTurnInjector_TransportErrorIsReported(t *testing.T) {
	tr := &fakeTransport{sessionID: "s", promptInFlight: true, err: errors.New("connection closed")}
	res, err := (&midTurnInjector{}).Inject(context.Background(), tr, ai.MidTurnInjectRequest{Content: "x"})
	require.Error(t, err)
	assert.False(t, res.Injected)
	assert.Equal(t, "error", res.Reason)
}

// TestMidTurnInjector_MalformedResponseIsError verifies a non-JSON reply cannot
// be mistaken for acceptance.
func TestMidTurnInjector_MalformedResponseIsError(t *testing.T) {
	tr := &fakeTransport{sessionID: "s", promptInFlight: true, response: json.RawMessage(`not json`)}
	res, err := (&midTurnInjector{}).Inject(context.Background(), tr, ai.MidTurnInjectRequest{Content: "x"})
	require.Error(t, err)
	assert.False(t, res.Injected)
	assert.Equal(t, "error", res.Reason)
}

// TestMidTurnInjector_AttachmentsDecline verifies messages carrying files fall
// back to the queue path, which owns the file/directory prompt prefixes.
func TestMidTurnInjector_AttachmentsDecline(t *testing.T) {
	tr := &fakeTransport{
		sessionID:      "s",
		promptInFlight: true,
		response:       steeringResp(t, map[string]any{"outcome": "injected"}),
	}
	res, err := (&midTurnInjector{}).Inject(context.Background(), tr, ai.MidTurnInjectRequest{
		Content: "see file",
		Files:   []model.FileEntry{{Path: "/tmp/a.go"}},
	})
	require.NoError(t, err)
	assert.False(t, res.Injected)
	assert.Equal(t, "unsupported", res.Reason)
	assert.False(t, tr.callRawUsed)
}
