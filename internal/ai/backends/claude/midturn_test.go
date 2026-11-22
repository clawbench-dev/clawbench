package claude

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ai"
)

// These tests drive the REGISTERED policy (`midTurn`, the same value register.go
// hands to the plugin), not a fresh steering.New. The shared package's tests
// cover the option matrix; what matters here is that claude's declaration is the
// right one — an adapter that honors idleBehavior must actually ask for it, or
// an idle injection silently starts a turn the host does not track.

type fakeTransport struct {
	sessionID string
	inFlight  bool
	params    any
	response  json.RawMessage
	err       error
}

func (f *fakeTransport) CallRaw(_ context.Context, _ string, params any) (json.RawMessage, error) {
	f.params = params
	return f.response, f.err
}

func (f *fakeTransport) CallExtension(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return f.CallRaw(ctx, method, params)
}

func (f *fakeTransport) AcpSessionID() string { return f.sessionID }

func (f *fakeTransport) PromptInFlight() bool { return f.inFlight }

func paramsMap(t *testing.T, params any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(params)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	return m
}

// claude-agent-acp honors `_meta.steering.idleBehavior`; the policy must send
// "promptRequired" so an idle session declines cleanly instead of starting a
// turn ClawBench does not own.
func TestMidTurnPolicy_SendsIdleBehaviorPromptRequired(t *testing.T) {
	tr := &fakeTransport{
		sessionID: "acp-sess",
		inFlight:  true,
		response:  json.RawMessage(`{"outcome":"injected"}`),
	}

	res, err := midTurn.Inject(context.Background(), tr, ai.MidTurnInjectRequest{Content: "hi"})
	require.NoError(t, err)
	require.True(t, res.Injected)

	m := paramsMap(t, tr.params)
	assert.Equal(t, "acp-sess", m["sessionId"])
	prompt, ok := m["prompt"].([]any)
	require.True(t, ok)
	require.Len(t, prompt, 1)
	assert.Equal(t, "text", prompt[0].(map[string]any)["type"])
	assert.Equal(t, "hi", prompt[0].(map[string]any)["text"])

	meta, ok := m["_meta"].(map[string]any)
	require.True(t, ok, "claude must send _meta.steering.idleBehavior")
	steering, ok := meta["steering"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "promptRequired", steering["idleBehavior"])
}

// The two non-injected outcomes claude can produce must map to their documented
// reasons: promptRequired is a clean idle decline, startedNewTurn is a refusal.
func TestMidTurnPolicy_OutcomeMapping(t *testing.T) {
	cases := map[string]string{
		"promptRequired": "idle",
		"startedNewTurn": "rejected",
		"failed":         "rejected",
	}
	for outcome, want := range cases {
		t.Run(outcome, func(t *testing.T) {
			tr := &fakeTransport{
				sessionID: "acp-sess",
				inFlight:  true,
				response:  json.RawMessage(`{"outcome":"` + outcome + `"}`),
			}
			res, err := midTurn.Inject(context.Background(), tr, ai.MidTurnInjectRequest{Content: "hi"})
			require.NoError(t, err)
			assert.False(t, res.Injected)
			assert.Equal(t, want, res.Reason)
		})
	}
}

// Without a live turn nothing may be sent, whatever the adapter would answer.
func TestMidTurnPolicy_DeclinesWhenIdle(t *testing.T) {
	tr := &fakeTransport{sessionID: "acp-sess", inFlight: false, response: json.RawMessage(`{"outcome":"injected"}`)}
	res, err := midTurn.Inject(context.Background(), tr, ai.MidTurnInjectRequest{Content: "hi"})
	require.NoError(t, err)
	assert.False(t, res.Injected)
	assert.Equal(t, "idle", res.Reason)
	assert.Nil(t, tr.params, "must not call the adapter when no turn is running")
}
