package steering

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ai"
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

func (f *fakeTransport) CallExtension(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return f.CallRaw(ctx, method, params)
}

func (f *fakeTransport) AcpSessionID() string { return f.sessionID }

func (f *fakeTransport) PromptInFlight() bool { return f.promptInFlight }

func resp(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// variant is one adapter's option set. The shared behavior must hold for both;
// only the _meta block and the startedNewTurn reason differ.
type variant struct {
	name string
	opts Options
}

var variants = []variant{
	{
		name: "claude",
		opts: Options{IdleBehavior: "promptRequired", StartedNewTurnReason: "rejected"},
	},
	{
		name: "codex",
		opts: Options{StartedNewTurnReason: "stale"},
	},
}

func eachVariant(t *testing.T, fn func(t *testing.T, v variant)) {
	t.Helper()
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) { fn(t, v) })
	}
}

// TestInjector_InjectedOnOutcomeInjected verifies the happy path: the adapter
// joined the running turn and we report Injected=true.
func TestInjector_InjectedOnOutcomeInjected(t *testing.T) {
	eachVariant(t, func(t *testing.T, v variant) {
		tr := &fakeTransport{
			sessionID:      "acp-sess-1",
			promptInFlight: true,
			response:       resp(t, map[string]any{"outcome": "injected"}),
		}

		res, err := New(v.opts).Inject(context.Background(), tr, ai.MidTurnInjectRequest{
			SessionID: "cb-sess",
			Content:   "hello",
		})

		require.NoError(t, err)
		assert.True(t, res.Injected)
		require.Len(t, tr.calls, 1)
		assert.Equal(t, "_session/steering", tr.calls[0])
	})
}

// TestInjector_RequestShape pins the wire payload: sessionId, a prompt array
// (not contentBlocks), and _meta only for the adapter that honors idleBehavior.
func TestInjector_RequestShape(t *testing.T) {
	eachVariant(t, func(t *testing.T, v variant) {
		tr := &fakeTransport{
			sessionID:      "acp-sess-9",
			promptInFlight: true,
			response:       resp(t, map[string]any{"outcome": "injected"}),
		}

		_, err := New(v.opts).Inject(context.Background(), tr, ai.MidTurnInjectRequest{Content: "the text"})
		require.NoError(t, err)

		params, ok := tr.lastParams.(steerParams)
		require.True(t, ok, "expected steerParams, got %T", tr.lastParams)
		assert.Equal(t, "acp-sess-9", params.SessionID, "must use the ACP session id")
		require.Len(t, params.Prompt, 1)
		assert.Equal(t, "text", params.Prompt[0].Type)
		assert.Equal(t, "the text", params.Prompt[0].Text)

		if v.opts.IdleBehavior == "" {
			assert.Nil(t, params.Meta, "this adapter ignores _meta, so none must be sent")
			return
		}
		require.NotNil(t, params.Meta, "this adapter honors _meta.steering.idleBehavior")
		assert.Equal(t, v.opts.IdleBehavior, params.Meta.Steering.IdleBehavior)
	})
}

// TestInjector_DeclinesWhenNoPromptInFlight is the ghost-turn guard: without a
// live turn we must NOT call steering (the adapter would otherwise start a turn
// ClawBench does not own).
func TestInjector_DeclinesWhenNoPromptInFlight(t *testing.T) {
	eachVariant(t, func(t *testing.T, v variant) {
		tr := &fakeTransport{
			sessionID:      "s",
			promptInFlight: false,
			response:       resp(t, map[string]any{"outcome": "startedNewTurn"}),
		}
		res, err := New(v.opts).Inject(context.Background(), tr, ai.MidTurnInjectRequest{Content: "x"})
		require.NoError(t, err)
		assert.False(t, res.Injected)
		assert.Equal(t, "idle", res.Reason)
		assert.False(t, tr.callRawUsed, "must not call steering when no turn is running")
	})
}

// TestInjector_NoSessionMeansIdle verifies we never issue a steer without an
// ACP session.
func TestInjector_NoSessionMeansIdle(t *testing.T) {
	eachVariant(t, func(t *testing.T, v variant) {
		tr := &fakeTransport{sessionID: "", promptInFlight: true}
		res, err := New(v.opts).Inject(context.Background(), tr, ai.MidTurnInjectRequest{Content: "x"})
		require.NoError(t, err)
		assert.False(t, res.Injected)
		assert.Equal(t, "idle", res.Reason)
		assert.False(t, tr.callRawUsed)
	})
}

// TestInjector_DeclinesOnNonInjectedOutcomes covers every non-"injected"
// outcome: none may be reported as a successful injection.
func TestInjector_DeclinesOnNonInjectedOutcomes(t *testing.T) {
	eachVariant(t, func(t *testing.T, v variant) {
		wantStale := v.opts.StartedNewTurnReason
		if wantStale == "" {
			wantStale = "rejected"
		}
		cases := map[string]string{
			"promptRequired": "idle",
			"startedNewTurn": wantStale,
			"failed":         "rejected",
			"something-new":  "rejected",
		}
		for outcome, wantReason := range cases {
			t.Run(outcome, func(t *testing.T) {
				tr := &fakeTransport{
					sessionID:      "s",
					promptInFlight: true,
					response:       resp(t, map[string]any{"outcome": outcome}),
				}
				res, err := New(v.opts).Inject(context.Background(), tr, ai.MidTurnInjectRequest{Content: "x"})
				require.NoError(t, err)
				assert.False(t, res.Injected)
				assert.Equal(t, wantReason, res.Reason)
			})
		}
	})
}

// TestInjector_TransportErrorIsReported verifies a dead connection surfaces as
// an error (logged by the seam) and not as a bogus success.
func TestInjector_TransportErrorIsReported(t *testing.T) {
	eachVariant(t, func(t *testing.T, v variant) {
		tr := &fakeTransport{sessionID: "s", promptInFlight: true, err: errors.New("connection closed")}
		res, err := New(v.opts).Inject(context.Background(), tr, ai.MidTurnInjectRequest{Content: "x"})
		require.Error(t, err)
		assert.False(t, res.Injected)
		assert.Equal(t, "error", res.Reason)
	})
}

// TestInjector_MalformedResponseIsError verifies a non-JSON reply cannot be
// mistaken for acceptance.
func TestInjector_MalformedResponseIsError(t *testing.T) {
	eachVariant(t, func(t *testing.T, v variant) {
		tr := &fakeTransport{sessionID: "s", promptInFlight: true, response: json.RawMessage(`not json`)}
		res, err := New(v.opts).Inject(context.Background(), tr, ai.MidTurnInjectRequest{Content: "x"})
		require.Error(t, err)
		assert.False(t, res.Injected)
		assert.Equal(t, "error", res.Reason)
	})
}

// TestInjector_AttachmentHeaderGoesThroughVerbatim verifies a message that
// references a file is steered like any other: the attachment is a prompt
// header, not bytes on the wire, so the policy must not special-case it.
func TestInjector_AttachmentHeaderGoesThroughVerbatim(t *testing.T) {
	eachVariant(t, func(t *testing.T, v variant) {
		tr := &fakeTransport{
			sessionID:      "s",
			promptInFlight: true,
			response:       resp(t, map[string]any{"outcome": "injected"}),
		}
		const content = "[User uploaded 1 file(s): /tmp/a.go]\nsee file"
		res, err := New(v.opts).Inject(context.Background(), tr, ai.MidTurnInjectRequest{
			Content: content,
		})
		require.NoError(t, err)
		assert.True(t, res.Injected)
		assert.True(t, tr.callRawUsed)

		params, ok := tr.lastParams.(steerParams)
		require.True(t, ok, "expected steerParams, got %T", tr.lastParams)
		require.Len(t, params.Prompt, 1)
		assert.Equal(t, content, params.Prompt[0].Text)
	})
}

// An adapter that leaves StartedNewTurnReason empty gets "rejected": the
// zero-option case must still report a concrete reason, never "".
func TestNew_DefaultsStartedNewTurnReason(t *testing.T) {
	tr := &fakeTransport{
		sessionID:      "s",
		promptInFlight: true,
		response:       resp(t, map[string]any{"outcome": "startedNewTurn"}),
	}
	res, err := New(Options{}).Inject(context.Background(), tr, ai.MidTurnInjectRequest{Content: "x"})
	require.NoError(t, err)
	assert.False(t, res.Injected)
	assert.Equal(t, "rejected", res.Reason)
}
