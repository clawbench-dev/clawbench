package forge_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/forge"
	gh "clawbench/internal/forge/github"
	gl "clawbench/internal/forge/gitlab"
)

// This is the cross-platform contract test: both adapters must satisfy
// forge.Provider and normalize their platform vocabulary to the same
// forge.Item / forge.Comment shape. It guards the "don't write two
// implementations" goal at the type and behavior level.

func TestContract_BothAdaptersImplementProvider(t *testing.T) {
	// Compile-time: a GitHub provider and a GitLab provider are both usable
	// wherever a forge.Provider is expected.
	ghSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ghSrv.Close()
	glSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer glSrv.Close()

	ghProvider, err := gh.New(gh.Config{BaseURL: ghSrv.URL}, "o", "r")
	require.NoError(t, err)
	glProvider, err := gl.New(gl.Config{Host: glSrv.URL[len("http://"):], Scheme: "http"}, "g", "r")
	require.NoError(t, err)

	var providers []forge.Provider = []forge.Provider{ghProvider, glProvider}
	assert.Len(t, providers, 2)
}

func TestContract_NormalizedStatesMatch(t *testing.T) {
	// A merged change request and a closed issue must normalize identically
	// regardless of platform, so the UI needs no platform-specific branches.
	ghSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v3/repos/o/r/pulls" {
			_, _ = w.Write([]byte(`[{"number":1,"title":"m","state":"closed","merged":true,
				"user":{"login":"a"},"updated_at":"2026-09-10T00:00:00Z"}]`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer ghSrv.Close()
	glSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"iid":1,"title":"m","state":"merged","author":{"username":"a"},
			"updated_at":"2026-09-10T00:00:00Z"}]`))
	}))
	defer glSrv.Close()

	ghProvider, err := gh.New(gh.Config{BaseURL: ghSrv.URL}, "o", "r")
	require.NoError(t, err)
	glProvider, err := gl.New(gl.Config{Host: glSrv.URL[len("http://"):], Scheme: "http"}, "g", "r")
	require.NoError(t, err)

	ctx := context.Background()
	ghRes, err := ghProvider.ListItems(ctx, forge.ListOptions{Type: forge.ItemTypeChangeRequest})
	require.NoError(t, err)
	glRes, err := glProvider.ListItems(ctx, forge.ListOptions{Type: forge.ItemTypeChangeRequest})
	require.NoError(t, err)

	require.Len(t, ghRes.Items, 1)
	require.Len(t, glRes.Items, 1)
	assert.Equal(t, forge.StateMerged, ghRes.Items[0].State)
	assert.Equal(t, forge.StateMerged, glRes.Items[0].State)
	assert.Equal(t, ghRes.Items[0].State, glRes.Items[0].State,
		"both platforms must normalize a merged change request to the same state")
	assert.Equal(t, forge.ItemTypeChangeRequest, ghRes.Items[0].Type)
	assert.Equal(t, forge.ItemTypeChangeRequest, glRes.Items[0].Type)
}

func TestContract_ItemTypeIsPlatformIndependent(t *testing.T) {
	// A GitHub PR and a GitLab MR must both report ItemTypeChangeRequest so the
	// "PRs" tab has a single filter.
	assert.Equal(t, forge.ItemTypeChangeRequest, forge.ItemType("pr"))
	assert.NotEqual(t, forge.ItemTypeIssue, forge.ItemTypeChangeRequest)
}
