package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gogithub "github.com/google/go-github/v85/github"
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

// TestListItems_PullsCarryHeadBranch: the head branch is the key the CI lookup
// uses, so a PR that arrives without it would silently show no pipelines.
func TestListItems_PullsCarryHeadBranch(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"number":5,"title":"pr","state":"open","user":{"login":"carol"},
			 "head":{"ref":"feat/login-fix"},
			 "html_url":"https://github.com/acme/widgets/pull/5",
			 "updated_at":"2026-09-09T12:00:00Z"}
		]`))
	}))

	res, err := p.ListItems(context.Background(), forge.ListOptions{Type: forge.ItemTypeChangeRequest})
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	assert.Equal(t, "feat/login-fix", res.Items[0].SourceBranch)
}

// TestListItems_PullsCarryLabels guards a real asymmetry: convertIssue set
// Labels but convertPull did not, so a PR's labels were silently dropped while
// an issue's were kept. The event context renders labels, so a PR event task
// would have received an empty label list for no reason.
func TestListItems_PullsCarryLabels(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/repos/acme/widgets/pulls")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"number":5,"title":"pr","state":"open","user":{"login":"carol"},
			 "labels":[{"name":"bug","color":"d73a4a"},{"name":"urgent"}],
			 "html_url":"https://github.com/acme/widgets/pull/5",
			 "updated_at":"2026-09-09T12:00:00Z"}
		]`))
	}))

	res, err := p.ListItems(context.Background(), forge.ListOptions{Type: forge.ItemTypeChangeRequest})
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	require.Len(t, res.Items[0].Labels, 2)
	assert.Equal(t, "bug", res.Items[0].Labels[0].Name)
	assert.Equal(t, "d73a4a", res.Items[0].Labels[0].Color)
	assert.Equal(t, "urgent", res.Items[0].Labels[1].Name)
}

// TestGetItem_PullCarriesLabels covers the same gap on the detail path, which
// is a separate conversion call site.
func TestGetItem_PullCarriesLabels(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"number":5,"title":"pr","state":"open",
			 "user":{"login":"carol"},"labels":[{"name":"bug"}],
			 "html_url":"https://github.com/acme/widgets/pull/5",
			 "updated_at":"2026-09-09T12:00:00Z"}`))
	}))

	item, err := p.GetItem(context.Background(), forge.ItemTypeChangeRequest, 5)
	require.NoError(t, err)
	require.Len(t, item.Labels, 1)
	assert.Equal(t, "bug", item.Labels[0].Name)
}

// TestListItems_IssuesHaveNoHeadBranch: an issue has no branch, and inventing one
// would make the CI lookup query a branch that does not exist.
func TestListItems_IssuesHaveNoHeadBranch(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"number":5,"title":"issue","state":"open","user":{"login":"carol"},
			 "html_url":"https://github.com/acme/widgets/issues/5",
			 "updated_at":"2026-09-09T12:00:00Z"}
		]`))
	}))

	res, err := p.ListItems(context.Background(), forge.ListOptions{Type: forge.ItemTypeIssue})
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	assert.Empty(t, res.Items[0].SourceBranch)
}

// TestListItems_PullsMergedState covers the merged normalization.
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

// TestListItems_PullsClosedExcludesMerged pins the cross-platform agreement:
// GitLab's state=closed never includes merged MRs, so GitHub's closed filter
// must drop merged PRs too.
//
// It must be served by the search API rather than filtered locally, because a
// page whose PRs are ALL merged would come back empty while hasMore stayed true
// — an empty list has no scrollable area, so the caller's infinite scroll never
// fires and the remaining pages are unreachable.
func TestListItems_PullsClosedExcludesMerged(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/search/issues",
			"closed must be served by search, so an all-merged page cannot stall the scroll")
		q := r.URL.Query().Get("q")
		assert.Contains(t, q, "state:closed")
		assert.Contains(t, q, "is:unmerged", "merged PRs must be excluded by the query, not dropped locally")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_count":1,"items":[
			{"number":6,"title":"closed unmerged","state":"closed",
			 "pull_request":{"url":"https://api.github.com/repos/acme/widgets/pulls/6"},
			 "user":{"login":"dave"},
			 "html_url":"https://github.com/acme/widgets/pull/6","updated_at":"2026-09-10T09:00:00Z"}
		]}`))
	}))
	res, err := p.ListItems(context.Background(), forge.ListOptions{
		Type: forge.ItemTypeChangeRequest, State: string(forge.StateClosed),
	})
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	assert.Equal(t, 6, res.Items[0].Number)
	assert.Equal(t, forge.StateClosed, res.Items[0].State)
}

// TestListItems_PullsMergedUsesSearchApi covers the merged filter, which the
// list endpoint cannot express: GitHub accepts state=merged but silently
// returns OPEN pull requests, so the request must go to the search API instead.
func TestListItems_PullsMergedUsesSearchApi(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/search/issues", "merged must be served by search, not the list endpoint")
		assert.Contains(t, r.URL.Query().Get("q"), "is:merged")
		assert.Contains(t, r.URL.Query().Get("q"), "is:pr")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"total_count":1,
			"items":[
				{"number":5,"title":"merged pr","state":"closed",
				 "pull_request":{"url":"https://api.github.com/repos/acme/widgets/pulls/5",
				                 "merged_at":"2026-09-09T12:00:00Z"},
				 "user":{"login":"carol"},
				 "html_url":"https://github.com/acme/widgets/pull/5","updated_at":"2026-09-09T12:00:00Z"}
			]}`))
	}))
	res, err := p.ListItems(context.Background(), forge.ListOptions{
		Type: forge.ItemTypeChangeRequest, State: string(forge.StateMerged),
	})
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	// The search payload carries the merge time in the nested pull_request
	// object; without reading it this would normalize to "closed".
	assert.Equal(t, forge.StateMerged, res.Items[0].State)
	require.NotNil(t, res.Items[0].MergedAt)
	assert.Equal(t, forge.ItemTypeChangeRequest, res.Items[0].Type)
}

// TestListItems_PullsSearchClosedExcludesMerged covers the search path's state
// handling: state:closed there also matches merged PRs, so is:unmerged is
// required to keep the two filters disjoint.
func TestListItems_PullsSearchClosedExcludesMerged(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/search/issues")
		q := r.URL.Query().Get("q")
		assert.Contains(t, q, "state:closed")
		assert.Contains(t, q, "is:unmerged")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_count":0,"items":[]}`))
	}))
	_, err := p.ListItems(context.Background(), forge.ListOptions{
		Type: forge.ItemTypeChangeRequest, State: string(forge.StateClosed), Query: "fix",
	})
	require.NoError(t, err)
}

// TestListItems_IssuesMergedFilterIsEmptyNotOpen pins the same class of bug the
// GitLab adapter has: GitHub does not reject state=merged on the issues
// endpoint, it silently falls back to "open". Forwarding it would show open
// issues under a "merged" filter, which is worse than an error because nothing
// signals the filter was ignored.
func TestListItems_IssuesMergedFilterIsEmptyNotOpen(t *testing.T) {
	called := false
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"number":1,"title":"open issue","state":"open","user":{"login":"a"},"updated_at":"2026-09-10T10:00:00Z"}]`))
	}))
	res, err := p.ListItems(context.Background(), forge.ListOptions{
		Type: forge.ItemTypeIssue, State: string(forge.StateMerged),
	})
	require.NoError(t, err)
	assert.Empty(t, res.Items, "issues have no merged state, so the result must be empty")
	assert.False(t, called, "an impossible filter must not reach the API and be silently downgraded to open")
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

// TestListItems_SearchUsesSearchAPI guards that a text query is actually sent:
// the repo-scoped list endpoints have no text parameter, so search must go
// through /search/issues. Silently ignoring the query made the search box look
// broken (it returned the unfiltered list).
func TestListItems_SearchUsesSearchAPI(t *testing.T) {
	var gotQuery string
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/search/issues")
		gotQuery = r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_count":1,"items":[{"number":7,"title":"Fix crash","state":"open","html_url":"https://github.com/acme/widgets/issues/7","user":{"login":"alice"}}]}`))
	}))

	res, err := p.ListItems(context.Background(), forge.ListOptions{
		Type: forge.ItemTypeIssue, State: "open", Query: "crash",
	})
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	assert.Equal(t, 7, res.Items[0].Number)
	assert.Equal(t, "Fix crash", res.Items[0].Title)

	// The query must be scoped to this repo and to issues only.
	assert.Contains(t, gotQuery, "crash")
	assert.Contains(t, gotQuery, "repo:acme/widgets")
	assert.Contains(t, gotQuery, "is:issue")
	assert.NotContains(t, gotQuery, "is:pr")
}

// TestListItems_SearchPullsScopesToPRs verifies the PR tab's search is scoped to
// PRs so the two tabs stay disjoint.
func TestListItems_SearchPullsScopesToPRs(t *testing.T) {
	var gotQuery string
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_count":0,"items":[]}`))
	}))

	_, err := p.ListItems(context.Background(), forge.ListOptions{
		Type: forge.ItemTypeChangeRequest, State: "open", Query: "fix",
	})
	require.NoError(t, err)
	assert.Contains(t, gotQuery, "is:pr")
	assert.NotContains(t, gotQuery, "is:issue")
}

// TestListItems_NoQueryUsesListEndpoint ensures the normal (unsearched) path is
// unchanged.
func TestListItems_NoQueryUsesListEndpoint(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.NotContains(t, r.URL.Path, "/search/")
		assert.Contains(t, r.URL.Path, "/repos/acme/widgets/issues")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))

	_, err := p.ListItems(context.Background(), forge.ListOptions{
		Type: forge.ItemTypeIssue, State: "open",
	})
	require.NoError(t, err)
}

// TestVerifyToken_HostScoped covers the token probe used by the credential and
// identity flows: it needs no repository, so it must authenticate against
// /user and return the account.
func TestVerifyToken_HostScoped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v3/user", r.URL.Path)
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"login":"octocat","name":"The Octocat"}`))
	}))
	defer srv.Close()

	got, err := VerifyToken(context.Background(), Config{
		Token:      "tok",
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	})
	require.NoError(t, err)
	assert.Equal(t, "octocat", got.Login)
}

// TestVerifyToken_AuthError covers the rejected-credential path.
func TestVerifyToken_AuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	defer srv.Close()

	_, err := VerifyToken(context.Background(), Config{
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	})
	require.Error(t, err)
	assert.True(t, forge.IsAuthError(err))
}

// TestGetItem_ChangeRequest covers the PR branch of GetItem, which fetches from
// the pulls endpoint rather than issues.
func TestGetItem_ChangeRequest(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/repos/acme/widgets/pulls/12")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"number":12,"title":"a pr","state":"closed","merged":true,
			"user":{"login":"bob"},"html_url":"u",
			"created_at":"2026-09-01T10:00:00Z","updated_at":"2026-09-02T10:00:00Z"}`))
	}))

	item, err := p.GetItem(context.Background(), forge.ItemTypeChangeRequest, 12)
	require.NoError(t, err)
	assert.Equal(t, 12, item.Number)
	assert.Equal(t, forge.ItemTypeChangeRequest, item.Type)
	assert.Equal(t, forge.StateMerged, item.State)
}

// TestSearchSortParam covers the mapping to the search API's accepted values:
// anything unrecognized falls back to best-match (the empty string).
func TestSearchSortParam(t *testing.T) {
	assert.Equal(t, "updated", searchSortParam("updated"))
	assert.Equal(t, "created", searchSortParam("created"))
	assert.Equal(t, "comments", searchSortParam("comments"))
	assert.Equal(t, "", searchSortParam("best-match"))
	assert.Equal(t, "", searchSortParam("bogus"))
}

// TestConvertIssueAsPull covers the search-result adaptation: the search API
// returns PRs under the issue shape, so only the type needs correcting.
func TestConvertIssueAsPull(t *testing.T) {
	iss := &gogithub.Issue{}
	iss.Number = gogithub.Ptr(5)
	iss.Title = gogithub.Ptr("search hit")
	item := convertIssueAsPull(iss)
	assert.Equal(t, forge.ItemTypeChangeRequest, item.Type)
	assert.Equal(t, 5, item.Number)
	assert.Equal(t, "search hit", item.Title)
}

// TestAuthorsFromUsers covers the nil and populated cases of the assignee
// conversion, including a nil element inside the slice.
func TestAuthorsFromUsers(t *testing.T) {
	assert.Nil(t, authorsFromUsers(nil), "no users yields nil, not an empty slice")

	login, name := "alice", "Alice"
	out := authorsFromUsers([]*gogithub.User{
		{Login: &login, Name: &name},
		nil,
	})
	require.Len(t, out, 2)
	assert.Equal(t, "alice", out[0].Login)
	assert.Equal(t, "Alice", out[0].Name)
	assert.Equal(t, forge.Author{}, out[1], "a nil user must not panic")
}

// TestLabelsFromGitHub covers the label conversion, including the color the
// frontend uses to tint the chip.
func TestLabelsFromGitHub(t *testing.T) {
	assert.Nil(t, labelsFromGitHub(nil))

	n, c := "bug", "d73a4a"
	out := labelsFromGitHub([]*gogithub.Label{{Name: &n, Color: &c}})
	require.Len(t, out, 1)
	assert.Equal(t, "bug", out[0].Name)
	assert.Equal(t, "d73a4a", out[0].Color)
}

// TestAuthorFromUser_Nil covers the defensive nil check.
func TestAuthorFromUser_Nil(t *testing.T) {
	assert.Equal(t, forge.Author{}, authorFromUser(nil))
}

// TestWrapErr_Classifies covers each error family the adapter maps: a rate
// limit with a Retry-After, an abuse (secondary) limit, a plain HTTP error, and
// a transport failure.
func TestWrapErr_Classifies(t *testing.T) {
	assert.Nil(t, wrapErr(nil), "a nil error stays nil")

	// Transport-level failure.
	assert.Equal(t, forge.ErrKindNetwork, kindOf(t, wrapErr(errors.New("dial tcp: i/o timeout"))))

	// A plain HTTP error response is classified by status.
	resp := &http.Response{StatusCode: http.StatusNotFound}
	assert.Equal(t, forge.ErrKindNotFound,
		kindOf(t, wrapErr(&gogithub.ErrorResponse{Response: resp, Message: "Not Found"})))

	// A rate limit carries the retry delay through.
	rl := wrapErr(&gogithub.RateLimitError{
		Response: &http.Response{
			StatusCode: http.StatusForbidden,
			Header:     http.Header{"Retry-After": []string{"30"}},
		},
		Message: "rate limited",
	})
	var fe *forge.Error
	require.ErrorAs(t, rl, &fe)
	assert.Equal(t, forge.ErrKindRateLimit, fe.Kind)
	assert.Equal(t, 30, fe.RetryAfterSeconds)

	// A secondary (abuse) limit is also a rate limit.
	secs := 45 * time.Second
	ab := wrapErr(&gogithub.AbuseRateLimitError{Message: "slow down", RetryAfter: &secs})
	require.ErrorAs(t, ab, &fe)
	assert.Equal(t, forge.ErrKindRateLimit, fe.Kind)
	assert.Equal(t, 45, fe.RetryAfterSeconds)
}

// kindOf extracts the classified error kind, failing the test on a non-forge error.
func kindOf(t *testing.T, err error) forge.ErrorKind {
	t.Helper()
	var fe *forge.Error
	require.ErrorAs(t, err, &fe)
	return fe.Kind
}

// TestListItems_SinceIsForwarded covers the incremental-sync parameter: a
// non-zero Since must reach the API so a poll only re-reads recent changes.
func TestListItems_SinceIsForwarded(t *testing.T) {
	since := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	var gotSince string
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSince = r.URL.Query().Get("since")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))

	_, err := p.ListItems(context.Background(), forge.ListOptions{
		Type: forge.ItemTypeIssue, State: "all", Since: since,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, gotSince, "Since must be forwarded as the since query parameter")
	assert.Contains(t, gotSince, "2026-09-01")
}
