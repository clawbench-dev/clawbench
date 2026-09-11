package gitlab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/forge"
)

// newTestProvider points the provider at a local test server. The provider
// builds URLs as scheme://host/api/v4, so we split the test server URL.
func newTestProvider(t *testing.T, handler http.Handler, namespace, repo string) *Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	// srv.URL is http://127.0.0.1:PORT
	host := srv.URL[len("http://"):]
	p, err := New(Config{Token: "glpat-test", Host: host, Scheme: "http"}, namespace, repo)
	require.NoError(t, err)
	return p
}

func TestNew_Validation(t *testing.T) {
	_, err := New(Config{}, "group", "repo")
	require.Error(t, err, "host required")
	_, err = New(Config{Host: "gitlab.com"}, "", "repo")
	require.Error(t, err, "namespace required")
	_, err = New(Config{Host: "gitlab.com"}, "group", "")
	require.Error(t, err, "repo required")
}

func TestNew_MultiLevelNamespaceEncoded(t *testing.T) {
	// GitLab project paths are URL-encoded, so a multi-level group becomes
	// group%2Fsub%2Fteam%2Fwidgets.
	p, err := New(Config{Host: "gitlab.com", Token: "t"}, "group/sub/team", "widgets")
	require.NoError(t, err)
	assert.Equal(t, "group%2Fsub%2Fteam%2Fwidgets", p.project)
}

func TestCurrentUser(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v4/user", r.URL.Path)
		assert.Equal(t, "glpat-test", r.Header.Get("PRIVATE-TOKEN"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"username":"jdoe","name":"Jane Doe"}`))
	}), "group", "widgets")
	got, err := p.CurrentUser(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "jdoe", got.Login)
	assert.Equal(t, "Jane Doe", got.Name)
}

func TestListItems_IssuesUsesOpenedState(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v4/projects/group%2Fwidgets/issues", r.URL.EscapedPath())
		// GitLab uses "opened", not "open".
		assert.Equal(t, "opened", r.URL.Query().Get("state"))
		assert.Equal(t, "updated", r.URL.Query().Get("order_by"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"iid":1,"title":"bug","description":"body","state":"opened",
			 "author":{"username":"alice","name":"Alice"},
			 "labels":["bug"],"user_notes_count":3,
			 "web_url":"https://gitlab.com/group/widgets/-/issues/1",
			 "created_at":"2026-09-01T00:00:00Z","updated_at":"2026-09-10T00:00:00Z"}
		]`))
	}), "group", "widgets")
	res, err := p.ListItems(context.Background(), forge.ListOptions{Type: forge.ItemTypeIssue})
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	assert.Equal(t, forge.StateOpen, res.Items[0].State)
	assert.Equal(t, "alice", res.Items[0].Author.Login)
	assert.Equal(t, 3, res.Items[0].CommentCount)
	require.Len(t, res.Items[0].Labels, 1)
	assert.Equal(t, "bug", res.Items[0].Labels[0].Name)
}

func TestListItems_MergeRequestsMergedState(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v4/projects/group%2Fwidgets/merge_requests", r.URL.EscapedPath())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"iid":10,"title":"feature","state":"merged","merged_at":"2026-09-09T12:00:00Z",
			 "author":{"username":"bob"},"draft":false,
			 "web_url":"https://gitlab.com/group/widgets/-/merge_requests/10",
			 "updated_at":"2026-09-09T12:00:00Z"},
			{"iid":11,"title":"wip","state":"opened","draft":true,
			 "author":{"username":"carol"},
			 "web_url":"https://gitlab.com/group/widgets/-/merge_requests/11",
			 "updated_at":"2026-09-10T09:00:00Z"}
		]`))
	}), "group", "widgets")
	res, err := p.ListItems(context.Background(), forge.ListOptions{Type: forge.ItemTypeChangeRequest})
	require.NoError(t, err)
	require.Len(t, res.Items, 2)
	assert.Equal(t, forge.StateMerged, res.Items[0].State)
	require.NotNil(t, res.Items[0].MergedAt)
	assert.Equal(t, forge.StateOpen, res.Items[1].State)
	assert.True(t, res.Items[1].Draft)
}

func TestListItems_SinceBecomesUpdatedAfter(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The incremental fetch relies on updated_after carrying a timestamp,
		// not a date — GitLab accepts RFC3339.
		assert.Equal(t, "2026-09-10T08:30:00Z", r.URL.Query().Get("updated_after"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}), "group", "widgets")
	since := mustParse(t, "2026-09-10T08:30:00Z")
	_, err := p.ListItems(context.Background(), forge.ListOptions{Type: forge.ItemTypeIssue, Since: since})
	require.NoError(t, err)
}

func TestListItems_PaginationViaNextPageHeader(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Next-Page", "3")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"iid":1,"title":"x","state":"opened","author":{"username":"a"},"updated_at":"2026-09-10T00:00:00Z"}]`))
	}), "group", "widgets")
	res, err := p.ListItems(context.Background(), forge.ListOptions{Type: forge.ItemTypeIssue})
	require.NoError(t, err)
	assert.True(t, res.HasMore)
	assert.Equal(t, 3, res.NextPage)
}

func TestGetItem(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v4/projects/group%2Fwidgets/issues/42", r.URL.EscapedPath())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"iid":42,"title":"detail","description":"d","state":"closed",
			"author":{"username":"a"},"updated_at":"2026-09-10T00:00:00Z"}`))
	}), "group", "widgets")
	item, err := p.GetItem(context.Background(), forge.ItemTypeIssue, 42)
	require.NoError(t, err)
	assert.Equal(t, 42, item.Number)
	assert.Equal(t, forge.StateClosed, item.State)
}

func TestGetItem_RejectsBadInput(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), "g", "r")
	_, err := p.GetItem(context.Background(), forge.ItemTypeIssue, 0)
	require.Error(t, err)
	_, err = p.GetItem(context.Background(), forge.ItemType("bogus"), 1)
	require.Error(t, err)
}

func TestListComments_SkipsSystemNotes(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v4/projects/group%2Fwidgets/issues/7/notes", r.URL.EscapedPath())
		assert.Equal(t, "asc", r.URL.Query().Get("sort"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":1,"body":"changed the description","system":true,"author":{"username":"a"},
			 "created_at":"2026-09-01T00:00:00Z","updated_at":"2026-09-01T00:00:00Z"},
			{"id":2,"body":"a real comment","system":false,"author":{"username":"b","name":"Bee"},
			 "created_at":"2026-09-02T00:00:00Z","updated_at":"2026-09-03T00:00:00Z"}
		]`))
	}), "group", "widgets")
	comments, err := p.ListComments(context.Background(), forge.ItemTypeIssue, 7, 1, 30)
	require.NoError(t, err)
	require.Len(t, comments, 1, "system notes must be filtered out")
	assert.Equal(t, int64(2), comments[0].ID)
	assert.Equal(t, "Bee", comments[0].Author.Name)
	// Edited comment: id stable, updated_at differs from created_at.
	assert.NotEqual(t, comments[0].CreatedAt, comments[0].UpdatedAt)
}

func TestListComments_RejectsBadInput(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), "g", "r")
	_, err := p.ListComments(context.Background(), forge.ItemTypeIssue, 0, 1, 30)
	require.Error(t, err)
	_, err = p.ListComments(context.Background(), forge.ItemType("bogus"), 1, 1, 30)
	require.Error(t, err)
}

func TestErrors_AuthClassified(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"401 Unauthorized"}`))
	}), "g", "r")
	_, err := p.ListItems(context.Background(), forge.ListOptions{Type: forge.ItemTypeIssue})
	require.Error(t, err)
	assert.True(t, forge.IsAuthError(err))
}

func TestErrors_NotFoundClassified(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"404 Project Not Found"}`))
	}), "g", "r")
	_, err := p.GetItem(context.Background(), forge.ItemTypeIssue, 1)
	require.Error(t, err)
	assert.True(t, forge.IsNotFoundError(err))
}

func TestErrors_RateLimitVia429(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"Too many requests"}`))
	}), "g", "r")
	_, err := p.ListItems(context.Background(), forge.ListOptions{Type: forge.ItemTypeIssue})
	require.Error(t, err)
	assert.True(t, forge.IsRateLimitError(err))
	var fe *forge.Error
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, 30, fe.RetryAfterSeconds)
}

func TestErrors_RateLimitVia403WithHeader(t *testing.T) {
	// GitLab can return 403 with RateLimit-Remaining: 0 instead of 429.
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Forbidden"}`))
	}), "g", "r")
	_, err := p.ListItems(context.Background(), forge.ListOptions{Type: forge.ItemTypeIssue})
	require.Error(t, err)
	assert.True(t, forge.IsRateLimitError(err), "403 with RateLimit-Remaining:0 is throttling, not auth")
}

func TestNormalizeState(t *testing.T) {
	assert.Equal(t, forge.StateMerged, normalizeState("merged"))
	assert.Equal(t, forge.StateClosed, normalizeState("closed"))
	assert.Equal(t, forge.StateClosed, normalizeState("locked"))
	assert.Equal(t, forge.StateOpen, normalizeState("opened"))
}

func TestParseTime_InvalidReturnsZero(t *testing.T) {
	assert.True(t, parseTime("").IsZero())
	assert.True(t, parseTime("not-a-time").IsZero())
}

func TestStateParam(t *testing.T) {
	assert.Equal(t, "opened", stateParam("open"))
	assert.Equal(t, "opened", stateParam(""))
	assert.Equal(t, "all", stateParam("all"))
	assert.Equal(t, "closed", stateParam("closed"))
}

func mustParse(t *testing.T, s string) (tt time.Time) {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, s)
	require.NoError(t, err)
	return parsed
}
