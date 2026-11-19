package forge_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/forge"
	gt "clawbench/internal/forge/gitea"
	gh "clawbench/internal/forge/github"
	gl "clawbench/internal/forge/gitlab"
)

// This is the cross-platform contract test: every adapter must satisfy
// forge.Provider and normalize its platform vocabulary to the same
// forge.Item / forge.Comment shape. It guards the "don't write one
// implementation per platform" goal at the type and behavior level.

func TestContract_EveryAdapterImplementsProvider(t *testing.T) {
	// Compile-time: a provider of each platform is usable wherever a
	// forge.Provider is expected.
	ghSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ghSrv.Close()
	glSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer glSrv.Close()
	gtSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer gtSrv.Close()

	ghProvider, err := gh.New(gh.Config{BaseURL: ghSrv.URL}, "o", "r")
	require.NoError(t, err)
	glProvider, err := gl.New(gl.Config{Host: glSrv.URL[len("http://"):], Scheme: "http"}, "g", "r")
	require.NoError(t, err)
	gtProvider, err := gt.New(gt.Config{Host: gtSrv.URL[len("http://"):], Scheme: "http"}, "o", "r")
	require.NoError(t, err)

	providers := []forge.Provider{ghProvider, glProvider, gtProvider}
	assert.Len(t, providers, 3)
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
	// Gitea reports a merged pull as state=closed plus a merged_at timestamp.
	gtSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"number":1,"title":"m","state":"closed",
			"user":{"login":"a"},"updated_at":"2026-09-10T00:00:00Z",
			"pull_request":{"merged_at":"2026-09-10T00:00:00Z","html_url":"u"}}]`))
	}))
	defer gtSrv.Close()

	ghProvider, err := gh.New(gh.Config{BaseURL: ghSrv.URL}, "o", "r")
	require.NoError(t, err)
	glProvider, err := gl.New(gl.Config{Host: glSrv.URL[len("http://"):], Scheme: "http"}, "g", "r")
	require.NoError(t, err)
	gtProvider, err := gt.New(gt.Config{Host: gtSrv.URL[len("http://"):], Scheme: "http"}, "o", "r")
	require.NoError(t, err)

	ctx := context.Background()
	ghRes, err := ghProvider.ListItems(ctx, forge.ListOptions{Type: forge.ItemTypeChangeRequest})
	require.NoError(t, err)
	glRes, err := glProvider.ListItems(ctx, forge.ListOptions{Type: forge.ItemTypeChangeRequest})
	require.NoError(t, err)
	gtRes, err := gtProvider.ListItems(ctx, forge.ListOptions{Type: forge.ItemTypeChangeRequest})
	require.NoError(t, err)

	require.Len(t, ghRes.Items, 1)
	require.Len(t, glRes.Items, 1)
	require.Len(t, gtRes.Items, 1)
	for _, res := range []forge.ListResult{ghRes, glRes, gtRes} {
		assert.Equal(t, forge.StateMerged, res.Items[0].State,
			"every platform must normalize a merged change request to the same state")
		assert.Equal(t, forge.ItemTypeChangeRequest, res.Items[0].Type)
	}
}

func TestContract_ItemTypeIsPlatformIndependent(t *testing.T) {
	// A GitHub PR and a GitLab MR must both report ItemTypeChangeRequest so the
	// "PRs" tab has a single filter.
	assert.Equal(t, forge.ItemTypeChangeRequest, forge.ItemType("pr"))
	assert.NotEqual(t, forge.ItemTypeIssue, forge.ItemTypeChangeRequest)
}
