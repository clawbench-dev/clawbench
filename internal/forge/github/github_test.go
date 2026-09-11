package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/forge"
)

func newTestProvider(t *testing.T, handler http.Handler) *Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	p, err := New(Config{Token: "test-token", BaseURL: srv.URL}, "acme", "widgets")
	require.NoError(t, err)
	return p
}

func TestNew_RequiresOwnerAndRepo(t *testing.T) {
	_, err := New(Config{}, "", "widgets")
	require.Error(t, err)
	_, err = New(Config{}, "acme", "")
	require.Error(t, err)
}

func TestCurrentUser(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// WithEnterpriseURLs prefixes the API path with /api/v3/.
		assert.Equal(t, "/api/v3/user", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"login":"octocat","name":"The Octocat"}`))
	}))
	got, err := p.CurrentUser(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "octocat", got.Login)
	assert.Equal(t, "The Octocat", got.Name)
}

func TestListItems_IssuesExcludesPullRequests(t *testing.T) {
	// GitHub's issues endpoint returns PRs too; the issues tab must exclude them.
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/repos/acme/widgets/issues")
		assert.Equal(t, "all", r.URL.Query().Get("state"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"number":1,"title":"a real issue","state":"open","user":{"login":"alice"},
			 "html_url":"https://github.com/acme/widgets/issues/1","updated_at":"2026-09-10T10:00:00Z"},
			{"number":2,"title":"a pull request","state":"open","user":{"login":"bob"},
			 "pull_request":{"url":"https://api.github.com/repos/acme/widgets/pulls/2"},
			 "html_url":"https://github.com/acme/widgets/pull/2","updated_at":"2026-09-10T11:00:00Z"}
		]`))
	}))
	res, err := p.ListItems(context.Background(), forge.ListOptions{Type: forge.ItemTypeIssue, State: "all"})
	require.NoError(t, err)
	require.Len(t, res.Items, 1, "pull requests must be filtered out of the issues list")
	assert.Equal(t, 1, res.Items[0].Number)
	assert.Equal(t, forge.ItemTypeIssue, res.Items[0].Type)
	assert.Equal(t, "alice", res.Items[0].Author.Login)
}

func TestListItems_PullsMergedState(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/repos/acme/widgets/pulls")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"number":5,"title":"merged pr","state":"closed","merged":true,
			 "merged_at":"2026-09-09T12:00:00Z","user":{"login":"carol"},
			 "html_url":"https://github.com/acme/widgets/pull/5","updated_at":"2026-09-09T12:00:00Z"},
			{"number":6,"title":"open pr","state":"open","draft":true,
			 "user":{"login":"dave"},"html_url":"https://github.com/acme/widgets/pull/6",
			 "updated_at":"2026-09-10T09:00:00Z"}
		]`))
	}))
	res, err := p.ListItems(context.Background(), forge.ListOptions{Type: forge.ItemTypeChangeRequest})
	require.NoError(t, err)
	require.Len(t, res.Items, 2)
	assert.Equal(t, forge.StateMerged, res.Items[0].State, "merged PR must normalize to merged")
	require.NotNil(t, res.Items[0].MergedAt)
	assert.Equal(t, forge.StateOpen, res.Items[1].State)
	assert.True(t, res.Items[1].Draft)
}

func TestListItems_Pagination(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Advertise a next page via the Link header.
		w.Header().Set("Link", `<https://api.github.com/repos/acme/widgets/issues?page=2>; rel="next"`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"number":1,"title":"x","state":"open","user":{"login":"a"},"updated_at":"2026-09-10T10:00:00Z"}]`))
	}))
	res, err := p.ListItems(context.Background(), forge.ListOptions{Type: forge.ItemTypeIssue})
	require.NoError(t, err)
	assert.True(t, res.HasMore)
	assert.Equal(t, 2, res.NextPage)
}

func TestGetItem_NotFound(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	_, err := p.GetItem(context.Background(), forge.ItemTypeIssue, 999)
	require.Error(t, err)
	assert.True(t, forge.IsNotFoundError(err), "404 must classify as not_found")
}

func TestListItems_AuthErrorClassified(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	_, err := p.ListItems(context.Background(), forge.ListOptions{Type: forge.ItemTypeIssue})
	require.Error(t, err)
	assert.True(t, forge.IsAuthError(err), "401 must classify as auth")
}

func TestListItems_RateLimitClassified(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	}))
	_, err := p.ListItems(context.Background(), forge.ListOptions{Type: forge.ItemTypeIssue})
	require.Error(t, err)
	// 403 is ambiguous; with rate-limit headers present go-github surfaces a
	// RateLimitError and we must classify it as rate_limit, not auth.
	assert.True(t, forge.IsRateLimitError(err), "rate limit must classify as rate_limit")
}

func TestListComments_OrderedOldestFirst(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/repos/acme/widgets/issues/7/comments")
		assert.Equal(t, "created", r.URL.Query().Get("sort"))
		assert.Equal(t, "asc", r.URL.Query().Get("direction"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":101,"body":"first","user":{"login":"a"},"created_at":"2026-09-01T00:00:00Z","updated_at":"2026-09-01T00:00:00Z"},
			{"id":102,"body":"second","user":{"login":"b"},"created_at":"2026-09-02T00:00:00Z","updated_at":"2026-09-03T00:00:00Z"}
		]`))
	}))
	comments, err := p.ListComments(context.Background(), forge.ItemTypeIssue, 7, 1, 30)
	require.NoError(t, err)
	require.Len(t, comments, 2)
	assert.Equal(t, int64(101), comments[0].ID)
	assert.Equal(t, "first", comments[0].Body)
	// An edited comment keeps its ID but changes updated_at — both are exposed.
	assert.NotEqual(t, comments[1].CreatedAt, comments[1].UpdatedAt)
}

func TestListComments_RejectsInvalidNumber(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	_, err := p.ListComments(context.Background(), forge.ItemTypeIssue, 0, 1, 30)
	require.Error(t, err)
}

func TestListComments_RejectsUnsupportedType(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	_, err := p.ListComments(context.Background(), forge.ItemType("bogus"), 1, 1, 30)
	require.Error(t, err)
}

func TestNormalizeState(t *testing.T) {
	assert.Equal(t, forge.StateMerged, normalizeState("closed", true))
	assert.Equal(t, forge.StateClosed, normalizeState("closed", false))
	assert.Equal(t, forge.StateOpen, normalizeState("open", false))
}

func TestStateParamDefaultsToOpen(t *testing.T) {
	assert.Equal(t, "open", stateParam(""))
	assert.Equal(t, "open", stateParam("bogus"))
	assert.Equal(t, "all", stateParam("all"))
}

func TestPerPageClamped(t *testing.T) {
	assert.Equal(t, 30, perPageOrDefault(0))
	assert.Equal(t, 100, perPageOrDefault(500))
	assert.Equal(t, 50, perPageOrDefault(50))
}
