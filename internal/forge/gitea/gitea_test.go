package gitea

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/forge"
)

// newTestProvider wires a provider against a handler, using plain http so the
// test server needs no certificate.
func newTestProvider(t *testing.T, h http.HandlerFunc) (*Provider, func()) {
	t.Helper()
	srv := httptest.NewServer(h)
	host := strings.TrimPrefix(srv.URL, "http://")
	p, err := New(Config{Host: host, Scheme: "http", Token: "tok"}, "acme", "widgets")
	require.NoError(t, err)
	return p, srv.Close
}

func TestNew_ValidatesInputs(t *testing.T) {
	_, err := New(Config{}, "acme", "widgets")
	assert.Error(t, err, "host is required")

	_, err = New(Config{Host: "git.example.com"}, "", "widgets")
	assert.Error(t, err, "owner is required")

	_, err = New(Config{Host: "git.example.com"}, "acme", "")
	assert.Error(t, err, "repo is required")
}

func TestCurrentUser(t *testing.T) {
	p, done := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/user", r.URL.Path)
		assert.Equal(t, "token tok", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"login":"octo","full_name":"Octo Cat"}`))
	})
	defer done()

	author, err := p.CurrentUser(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "octo", author.Login)
	assert.Equal(t, "Octo Cat", author.Name)
}

func TestVerifyToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/user", r.URL.Path)
		_, _ = w.Write([]byte(`{"login":"octo","full_name":"Octo Cat"}`))
	}))
	defer srv.Close()

	author, err := VerifyToken(context.Background(), Config{
		Host:   strings.TrimPrefix(srv.URL, "http://"),
		Scheme: "http",
		Token:  "tok",
	})
	require.NoError(t, err)
	assert.Equal(t, "octo", author.Login)
}

func TestVerifyToken_RequiresHost(t *testing.T) {
	_, err := VerifyToken(context.Background(), Config{})
	assert.Error(t, err)
}

// TestListItems_IssuesParams pins the request shape: the type filter is what
// keeps the two tabs disjoint, and `since` is what makes the incremental poll
// cheap on the server side.
func TestListItems_IssuesParams(t *testing.T) {
	var got map[string]string
	p, done := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/repos/acme/widgets/issues", r.URL.Path)
		q := r.URL.Query()
		got = map[string]string{
			"state": q.Get("state"),
			"type":  q.Get("type"),
			"sort":  q.Get("sort"),
			"since": q.Get("since"),
			"q":     q.Get("q"),
			"page":  q.Get("page"),
			"limit": q.Get("limit"),
		}
		w.Header().Set("X-HasMore", "false")
		_, _ = w.Write([]byte(`[{
			"id":1,"number":7,"title":"Broken build","body":"details","state":"open",
			"user":{"login":"alice","full_name":"Alice"},
			"assignees":[{"login":"bob","full_name":"Bob"}],
			"labels":[{"name":"bug","color":"ff0000"}],
			"comments":3,
			"html_url":"https://git.example.com/acme/widgets/issues/7",
			"created_at":"2026-09-01T10:00:00Z",
			"updated_at":"2026-09-02T11:30:00Z"
		}]`))
	})
	defer done()

	since := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	res, err := p.ListItems(context.Background(), forge.ListOptions{
		Type:      forge.ItemTypeIssue,
		State:     "open",
		Page:      2,
		PerPage:   30,
		Query:     "broken",
		Since:     since,
		Sort:      "updated",
		Direction: "desc",
	})
	require.NoError(t, err)

	assert.Equal(t, "open", got["state"])
	assert.Equal(t, "issues", got["type"])
	assert.Equal(t, "recentupdate", got["sort"])
	assert.Equal(t, "2026-09-01T00:00:00Z", got["since"])
	assert.Equal(t, "broken", got["q"])
	assert.Equal(t, "2", got["page"])
	assert.Equal(t, "30", got["limit"])

	require.Len(t, res.Items, 1)
	item := res.Items[0]
	assert.Equal(t, forge.PlatformGitea, item.Platform)
	assert.Equal(t, forge.ItemTypeIssue, item.Type)
	assert.Equal(t, 7, item.Number)
	assert.Equal(t, "Broken build", item.Title)
	assert.Equal(t, "details", item.Body)
	assert.Equal(t, forge.StateOpen, item.State)
	assert.Equal(t, "alice", item.Author.Login)
	require.Len(t, item.Assignees, 1)
	assert.Equal(t, "bob", item.Assignees[0].Login)
	require.Len(t, item.Labels, 1)
	assert.Equal(t, "bug", item.Labels[0].Name)
	assert.Equal(t, "ff0000", item.Labels[0].Color)
	assert.Equal(t, 3, item.CommentCount)
	assert.Equal(t, "https://git.example.com/acme/widgets/issues/7", item.URL)
	assert.Equal(t, 2026, item.CreatedAt.Year())
	assert.False(t, res.HasMore)
}

// TestListItems_PullRequestsFromIssuesEndpoint covers the merged/closed
// distinction: Gitea reports a merged pull as state=closed and only the
// merged_at timestamp tells them apart.
func TestListItems_PullRequestsFromIssuesEndpoint(t *testing.T) {
	p, done := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "pulls", r.URL.Query().Get("type"))
		_, _ = w.Write([]byte(`[
			{"number":1,"title":"merged one","state":"closed",
			 "pull_request":{"merged_at":"2026-09-03T00:00:00Z","html_url":"u"}},
			{"number":2,"title":"closed unmerged","state":"closed",
			 "pull_request":{"html_url":"u"}},
			{"number":3,"title":"open one","state":"open","pull_request":{"html_url":"u"}}
		]`))
	})
	defer done()

	res, err := p.ListItems(context.Background(), forge.ListOptions{Type: forge.ItemTypeChangeRequest})
	require.NoError(t, err)
	require.Len(t, res.Items, 3)

	assert.Equal(t, forge.StateMerged, res.Items[0].State)
	require.NotNil(t, res.Items[0].MergedAt)
	assert.Equal(t, forge.StateClosed, res.Items[1].State, "closed without a merge is not merged")
	assert.Nil(t, res.Items[1].MergedAt)
	assert.Equal(t, forge.StateOpen, res.Items[2].State)
	for _, it := range res.Items {
		assert.Equal(t, forge.ItemTypeChangeRequest, it.Type, "a pull_request object makes it a change request")
		// The list payload carries no head branch; only the detail view has it.
		assert.Empty(t, it.SourceBranch)
	}
}

func TestListItems_MergedStateOnIssuesIsEmpty(t *testing.T) {
	// Issues have no merged lifecycle. Forwarding the filter would return
	// CLOSED issues under a "merged" label, which is the opposite of the promise.
	p, done := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no request may be issued for an impossible filter")
	})
	defer done()

	res, err := p.ListItems(context.Background(), forge.ListOptions{
		Type:  forge.ItemTypeIssue,
		State: string(forge.StateMerged),
	})
	require.NoError(t, err)
	assert.Empty(t, res.Items)
}

func TestListItems_PaginationHeaders(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		body     string
		wantMore bool
	}{
		{
			name:     "X-HasMore true wins",
			headers:  map[string]string{"X-HasMore": "true"},
			body:     `[{"number":1}]`,
			wantMore: true,
		},
		{
			name:     "X-HasMore false wins even on a full page",
			headers:  map[string]string{"X-HasMore": "false"},
			body:     `[{"number":1},{"number":2}]`,
			wantMore: false,
		},
		{
			name:     "X-Total-Count larger than the page",
			headers:  map[string]string{"X-Total-Count": "9"},
			body:     `[{"number":1},{"number":2}]`,
			wantMore: true,
		},
		{
			name:     "X-Total-Count equal to the page",
			headers:  map[string]string{"X-Total-Count": "2"},
			body:     `[{"number":1},{"number":2}]`,
			wantMore: false,
		},
		{
			name:     "no headers, full page",
			body:     `[{"number":1},{"number":2}]`,
			wantMore: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, done := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.headers {
					w.Header().Set(k, v)
				}
				_, _ = w.Write([]byte(tc.body))
			})
			defer done()

			res, err := p.ListItems(context.Background(), forge.ListOptions{PerPage: 2})
			require.NoError(t, err)
			assert.Equal(t, tc.wantMore, res.HasMore)
			if tc.wantMore {
				assert.Equal(t, 2, res.NextPage)
			} else {
				assert.Zero(t, res.NextPage)
			}
		})
	}
}

func TestGetItem_IssueAndPull(t *testing.T) {
	p, done := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/acme/widgets/issues/5":
			_, _ = w.Write([]byte(`{"number":5,"title":"issue","state":"open"}`))
		case "/api/v1/repos/acme/widgets/pulls/6":
			_, _ = w.Write([]byte(`{"number":6,"title":"pull","state":"closed","merged":true,
				"merged_at":"2026-09-04T00:00:00Z","draft":true,
				"head":{"ref":"feat/x"},"comments":2}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	defer done()

	issue, err := p.GetItem(context.Background(), forge.ItemTypeIssue, 5)
	require.NoError(t, err)
	assert.Equal(t, forge.ItemTypeIssue, issue.Type)
	assert.Equal(t, forge.StateOpen, issue.State)

	pull, err := p.GetItem(context.Background(), forge.ItemTypeChangeRequest, 6)
	require.NoError(t, err)
	assert.Equal(t, forge.ItemTypeChangeRequest, pull.Type)
	assert.Equal(t, forge.StateMerged, pull.State)
	assert.True(t, pull.Draft)
	assert.Equal(t, "feat/x", pull.SourceBranch, "the detail payload is where the head branch comes from")
	require.NotNil(t, pull.MergedAt)
	assert.Equal(t, 2, pull.CommentCount)
}

func TestGetItem_RejectsBadInput(t *testing.T) {
	p, done := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no request may be issued")
	})
	defer done()

	_, err := p.GetItem(context.Background(), forge.ItemTypeIssue, 0)
	assert.Error(t, err)

	_, err = p.GetItem(context.Background(), forge.ItemTypePipeline, 1)
	assert.Error(t, err)
}

// TestListItems_NumberQuery pins the two-step lookup: the issue endpoint says
// which kind the number is, and a pull is then re-read from the pull endpoint so
// SourceBranch is filled in.
func TestListItems_NumberQuery(t *testing.T) {
	var paths []string
	p, done := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/api/v1/repos/acme/widgets/issues/9":
			_, _ = w.Write([]byte(`{"number":9,"title":"pull as issue","state":"open",
				"pull_request":{"html_url":"u"}}`))
		case "/api/v1/repos/acme/widgets/pulls/9":
			_, _ = w.Write([]byte(`{"number":9,"title":"pull","state":"open","head":{"ref":"b"}}`))
		case "/api/v1/repos/acme/widgets/issues/404":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	defer done()

	res, err := p.ListItems(context.Background(), forge.ListOptions{
		Type:  forge.ItemTypeChangeRequest,
		Query: "#9",
	})
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	assert.Equal(t, "pull", res.Items[0].Title)
	assert.Equal(t, "b", res.Items[0].SourceBranch)
	assert.Equal(t, []string{
		"/api/v1/repos/acme/widgets/issues/9",
		"/api/v1/repos/acme/widgets/pulls/9",
	}, paths)

	// Asking for issues with a pull number is an empty page, not a wrong row.
	paths = nil
	res, err = p.ListItems(context.Background(), forge.ListOptions{
		Type:  forge.ItemTypeIssue,
		Query: "9",
	})
	require.NoError(t, err)
	assert.Empty(t, res.Items)
	assert.Len(t, paths, 1, "a type mismatch must not cost a second request")

	// A number that does not exist is an empty result, not an error card.
	res, err = p.ListItems(context.Background(), forge.ListOptions{Query: "404"})
	require.NoError(t, err)
	assert.Empty(t, res.Items)
}

func TestListComments(t *testing.T) {
	p, done := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/repos/acme/widgets/issues/3/comments", r.URL.Path)
		assert.Equal(t, "2", r.URL.Query().Get("page"))
		assert.Equal(t, "10", r.URL.Query().Get("limit"))
		_, _ = w.Write([]byte(`[{"id":11,"body":"hi","user":{"login":"a","full_name":"A"},
			"created_at":"2026-09-01T00:00:00Z","updated_at":"2026-09-01T00:00:00Z"}]`))
	})
	defer done()

	out, err := p.ListComments(context.Background(), forge.ItemTypeChangeRequest, 3, 2, 10)
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, int64(11), out[0].ID)
	assert.Equal(t, "a", out[0].Author.Login)
	assert.Equal(t, "hi", out[0].Body)
	assert.Equal(t, 2026, out[0].CreatedAt.Year())
}

func TestListComments_RejectsBadInput(t *testing.T) {
	p, done := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no request may be issued")
	})
	defer done()

	_, err := p.ListComments(context.Background(), forge.ItemTypeIssue, 0, 1, 10)
	assert.Error(t, err)

	_, err = p.ListComments(context.Background(), forge.ItemTypePipeline, 1, 1, 10)
	assert.Error(t, err)
}

// TestErrorClassification guards the mapping the UI reacts to: auth means "fix
// your token", network means "check connectivity", rate limit means "wait".
func TestErrorClassification(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		header    map[string]string
		wantKind  forge.ErrorKind
		wantMsg   string
		wantRetry int
	}{
		{
			name:     "unauthorized is auth",
			status:   http.StatusUnauthorized,
			body:     `{"message":"token is required"}`,
			wantKind: forge.ErrKindAuth,
			wantMsg:  "token is required",
		},
		{
			name:     "forbidden is auth",
			status:   http.StatusForbidden,
			wantKind: forge.ErrKindAuth,
			wantMsg:  http.StatusText(http.StatusForbidden),
		},
		{
			name:     "not found",
			status:   http.StatusNotFound,
			wantKind: forge.ErrKindNotFound,
		},
		{
			name:      "too many requests carries retry-after",
			status:    http.StatusTooManyRequests,
			header:    map[string]string{"Retry-After": "42"},
			wantKind:  forge.ErrKindRateLimit,
			wantRetry: 42,
		},
		{
			name:     "server error",
			status:   http.StatusBadGateway,
			wantKind: forge.ErrKindServer,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, done := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.header {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
				if tc.body != "" {
					_, _ = w.Write([]byte(tc.body))
				}
			})
			defer done()

			_, err := p.ListItems(context.Background(), forge.ListOptions{})
			require.Error(t, err)
			var fe *forge.Error
			require.ErrorAs(t, err, &fe)
			assert.Equal(t, tc.wantKind, fe.Kind)
			if tc.wantMsg != "" {
				assert.Equal(t, tc.wantMsg, fe.Message)
			}
			assert.Equal(t, tc.wantRetry, fe.RetryAfterSeconds)
		})
	}
}

// TestNetworkErrorIsClassified covers the transport branch: a closed server must
// read as "network", not as a platform verdict.
func TestNetworkErrorIsClassified(t *testing.T) {
	p, done := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {})
	done() // close before use

	_, err := p.CurrentUser(context.Background())
	require.Error(t, err)
	var fe *forge.Error
	require.ErrorAs(t, err, &fe)
	assert.Equal(t, forge.ErrKindNetwork, fe.Kind)
}

func TestPerPageClampedToServerMaximum(t *testing.T) {
	// Asking for more than Gitea returns would make a full page look short, and
	// the "is there more" fallback would stop early — silently skipping items.
	assert.Equal(t, giteaMaxPerPage, perPageOrDefault(100))
	assert.Equal(t, 30, perPageOrDefault(0))
	assert.Equal(t, 10, perPageOrDefault(10))
}

func TestSortParam(t *testing.T) {
	assert.Equal(t, "recentupdate", sortParam("updated", "desc"))
	assert.Equal(t, "leastupdate", sortParam("updated", "asc"))
	assert.Equal(t, "newest", sortParam("created", "desc"))
	assert.Equal(t, "oldest", sortParam("created", "asc"))
	assert.Equal(t, "recentupdate", sortParam("", ""))
}

func TestStateParam(t *testing.T) {
	assert.Equal(t, "open", stateParam("open"))
	assert.Equal(t, "closed", stateParam("closed"))
	assert.Equal(t, "all", stateParam("all"))
	assert.Equal(t, "closed", stateParam("merged"), "Gitea has no merged state; it is a closed pull")
	assert.Equal(t, "open", stateParam("nonsense"))
}
