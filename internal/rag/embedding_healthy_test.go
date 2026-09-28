package rag

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// IsHealthy is the single implementation of the /v1/models check — the
// connectivity test no longer has its own copy. These tests pin the contract
// its production callers (indexer health loop, search) depend on.

func TestIsHealthy_ModelListed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, testV1Models, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"data":[{"id":"%s"},{"id":"nomic-embed"}]}`, testModelBgeM3)
	}))
	defer srv.Close()

	c := NewEmbeddingClient(srv.URL, testModelBgeM3, "")
	reachable, modelAvailable, err := c.IsHealthy(context.Background())
	require.NoError(t, err)
	assert.True(t, reachable)
	assert.True(t, modelAvailable)
}

// A tagged model name (Ollama style "bge-m3:latest") must match the configured
// untagged name — the prefix rule the indexer relies on to avoid a spurious
// "model not available" warning.
func TestIsHealthy_ModelPrefixMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"data":[{"id":"%s"}]}`, testModelBgeM3Latest)
	}))
	defer srv.Close()

	c := NewEmbeddingClient(srv.URL, testModelBgeM3, "")
	reachable, modelAvailable, err := c.IsHealthy(context.Background())
	require.NoError(t, err)
	assert.True(t, reachable)
	assert.True(t, modelAvailable)
}

// Servers without /v1/models (older Ollama) answer 404. That must be read as
// "reachable, cannot verify" — not as broken, and not as a hard error.
func TestIsHealthy_ModelsEndpointMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewEmbeddingClient(srv.URL, testModelBgeM3, "")
	reachable, modelAvailable, err := c.IsHealthy(context.Background())
	require.NoError(t, err)
	assert.True(t, reachable, "404 on /v1/models means the endpoint is absent, not unreachable")
	assert.True(t, modelAvailable, "cannot verify the model, so it must not be reported missing")
}

// A reachable server that does not list the configured model reports the model
// as unavailable while still reachable.
func TestIsHealthy_ModelNotListed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"data":[{"id":"nomic-embed"}]}`)
	}))
	defer srv.Close()

	c := NewEmbeddingClient(srv.URL, testModelBgeM3, "")
	reachable, modelAvailable, err := c.IsHealthy(context.Background())
	require.NoError(t, err)
	assert.True(t, reachable)
	assert.False(t, modelAvailable)
}

// An unparseable model list means "reachable, unverifiable" — the health loop
// must not treat a decode failure as a hard error.
func TestIsHealthy_UnparseableModelList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `not json`)
	}))
	defer srv.Close()

	c := NewEmbeddingClient(srv.URL, testModelBgeM3, "")
	reachable, modelAvailable, err := c.IsHealthy(context.Background())
	require.Error(t, err)
	assert.True(t, reachable)
	assert.False(t, modelAvailable)
}

// A connection failure is "not reachable" with no error, so the caller can
// distinguish "down" from "misconfigured".
func TestIsHealthy_Unreachable(t *testing.T) {
	c := NewEmbeddingClient("http://127.0.0.1:19999", testModelBgeM3, "")
	reachable, modelAvailable, err := c.IsHealthy(context.Background())
	require.NoError(t, err)
	assert.False(t, reachable)
	assert.False(t, modelAvailable)
}

// A non-404 HTTP error (e.g. auth-scoped model listing) is a hard error.
func TestIsHealthy_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewEmbeddingClient(srv.URL, testModelBgeM3, "")
	reachable, modelAvailable, err := c.IsHealthy(context.Background())
	require.Error(t, err)
	assert.False(t, reachable)
	assert.False(t, modelAvailable)
}

// The API key is forwarded as a bearer token on the models request.
func TestIsHealthy_SendsAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"data":[{"id":"%s"}]}`, testModelBgeM3)
	}))
	defer srv.Close()

	c := NewEmbeddingClient(srv.URL, testModelBgeM3, "secret")
	reachable, modelAvailable, err := c.IsHealthy(context.Background())
	require.NoError(t, err)
	assert.True(t, reachable)
	assert.True(t, modelAvailable)
}
