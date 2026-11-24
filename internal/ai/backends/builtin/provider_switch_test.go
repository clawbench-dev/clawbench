package builtin

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"clawbench/internal/agentcore/llm"
	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sseServer counts the requests it receives and answers with one text chunk.
func sseServer(t *testing.T, hits *int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv
}

func drain(t *testing.T, c llm.Client) {
	t.Helper()
	ch, err := c.Stream(context.Background(), llm.Request{
		Messages: []llm.Message{llm.TextMessage(llm.RoleUser, "hi")},
	})
	require.NoError(t, err)
	for range ch {
	}
}

// A provider-scoped model id is the user's explicit pick. The turn must call
// THAT provider's endpoint, not the agent's bound one — otherwise picking
// another provider's model would silently keep running the bound provider's.
func TestBuildClient_ScopedModelUsesItsOwnProvider(t *testing.T) {
	var hitsBound, hitsPicked int32
	boundSrv := sseServer(t, &hitsBound)
	pickedSrv := sseServer(t, &hitsPicked)

	model.ReplaceProviders([]*model.Provider{
		{
			ID: 1, Name: "Bound", Protocol: model.ProviderProtocolOpenAI,
			BaseURL: boundSrv.URL, APIKey: "k1", Models: []string{"model-bound"},
		},
		{
			ID: 2, Name: "Picked", Protocol: model.ProviderProtocolOpenAI,
			BaseURL: pickedSrv.URL, APIKey: "k2", Models: []string{"model-picked"},
		},
	})
	t.Cleanup(func() { model.ReplaceProviders(nil) })

	bound := int64(1)
	agent := &model.Agent{ID: "builtin-pick", Backend: "builtin", BuiltinBackend: true, ProviderID: &bound}

	client, modelID, err := buildClient(agent, model.ProviderScopedModelID(2, "model-picked"))
	require.NoError(t, err)
	assert.Equal(t, "model-picked", modelID, "the wire sees the bare model name")

	drain(t, client)
	assert.Equal(t, int32(1), atomic.LoadInt32(&hitsPicked), "the picked provider must serve the turn")
	assert.Equal(t, int32(0), atomic.LoadInt32(&hitsBound), "the bound provider must not be called")
}

// A plain id (a session created before the picker could name a provider) and a
// scoped id whose provider has since been deleted both keep running the bound
// provider. Refusing the turn would be worse than falling back.
func TestBuildClient_PlainAndDanglingIDsFallBackToTheBinding(t *testing.T) {
	var hits int32
	srv := sseServer(t, &hits)

	model.ReplaceProviders([]*model.Provider{{
		ID: 1, Name: "Bound", Protocol: model.ProviderProtocolOpenAI,
		BaseURL: srv.URL, APIKey: "k1", Models: []string{"model-bound"},
	}})
	t.Cleanup(func() { model.ReplaceProviders(nil) })

	bound := int64(1)
	agent := &model.Agent{ID: "builtin-fallback", Backend: "builtin", BuiltinBackend: true, ProviderID: &bound}

	for _, override := range []string{
		"model-bound",
		model.ProviderScopedModelID(999, "model-gone"),
	} {
		client, modelID, err := buildClient(agent, override)
		require.NoError(t, err, "override %q", override)
		assert.NotEmpty(t, modelID)
		drain(t, client)
	}
	assert.Equal(t, int32(2), atomic.LoadInt32(&hits))
}
