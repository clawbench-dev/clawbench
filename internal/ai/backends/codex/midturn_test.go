package codex

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ai"
)

// These tests drive the REGISTERED policy (`midTurn`, the same value custom.go
// hands to the plugin). The shared package's tests cover the option matrix; what
// matters here is that codex's declaration is the right one — codex-acp ignores
// `_meta`, so sending it would be a lie, and an idle injection would start a
// turn the host does not track.

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

// codex-acp's params parser reads only sessionId/prompt, so a `_meta` block
// would be ignored — and claiming an option we do not have is how a future
// reader gets misled.
func TestMidTurnPolicy_SendsNoMeta(t *testing.T) {
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
	assert.Equal(t, "hi", prompt[0].(map[string]any)["text"])
	assert.NotContains(t, m, "_meta", "codex ignores _meta; sending it would claim an option it does not support")
}

// A turn that settles between the PromptInFlight check and the call makes the
// adapter start one we do not own. codex reports that as "stale", not "rejected".
func TestMidTurnPolicy_StartedNewTurnIsStale(t *testing.T) {
	tr := &fakeTransport{
		sessionID: "acp-sess",
		inFlight:  true,
		response:  json.RawMessage(`{"outcome":"startedNewTurn"}`),
	}
	res, err := midTurn.Inject(context.Background(), tr, ai.MidTurnInjectRequest{Content: "hi"})
	require.NoError(t, err)
	assert.False(t, res.Injected)
	assert.Equal(t, "stale", res.Reason)
}

// Without a live turn codex would START one, so nothing may be sent.
func TestMidTurnPolicy_DeclinesWhenIdle(t *testing.T) {
	tr := &fakeTransport{sessionID: "acp-sess", inFlight: false, response: json.RawMessage(`{"outcome":"startedNewTurn"}`)}
	res, err := midTurn.Inject(context.Background(), tr, ai.MidTurnInjectRequest{Content: "hi"})
	require.NoError(t, err)
	assert.False(t, res.Injected)
	assert.Equal(t, "idle", res.Reason)
	assert.Nil(t, tr.params, "must not call the adapter when no turn is running")
}
