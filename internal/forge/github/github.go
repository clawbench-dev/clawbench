// Package github adapts the GitHub REST API to the forge.Provider interface.
//
// It is strictly read-only: only GET-backed client methods are used.
package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	gogithub "github.com/google/go-github/v85/github"

	"clawbench/internal/forge"
)

// Config configures a GitHub provider instance.
type Config struct {
	// Token is a GitHub PAT (preferably fine-grained, read-only). Required for
	// private repositories.
	Token string
	// BaseURL overrides the API base (empty = api.github.com). Used by tests.
	BaseURL string
	// HTTPClient optionally overrides the transport (timeouts, TLS policy).
	HTTPClient *http.Client
}

// Provider implements forge.Provider against GitHub.
type Provider struct {
	client *gogithub.Client
	owner  string
	repo   string
}

// New builds a GitHub provider scoped to a single owner/repo.
func New(cfg Config, owner, repo string) (*Provider, error) {
	if owner == "" || repo == "" {
		return nil, fmt.Errorf("github: owner and repo are required")
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	client := gogithub.NewClient(httpClient)
	if cfg.Token != "" {
		client = client.WithAuthToken(cfg.Token)
	}
	if cfg.BaseURL != "" {
		var err error
		client, err = client.WithEnterpriseURLs(strings.TrimSuffix(cfg.BaseURL, "/"), strings.TrimSuffix(cfg.BaseURL, "/"))
		if err != nil {
			return nil, fmt.Errorf("github: configure base url: %w", err)
		}
	}
	return &Provider{client: client, owner: owner, repo: repo}, nil
}

// CurrentUser returns the authenticated account.
func (p *Provider) CurrentUser(ctx context.Context) (forge.Author, error) {
	user, _, err := p.client.Users.Get(ctx, "")
	if err != nil {
		return forge.Author{}, wrapErr(err)
	}
	return forge.Author{Login: user.GetLogin(), Name: user.GetName()}, nil
}

// VerifyToken checks that a credential authenticates against GitHub, returning
// the account it belongs to.
//
// It is host-scoped, not repo-scoped: verifying a token must not require a
// bound repository. GET /user is the standard probe — it needs no repo
// permission and fails with 401 for a bad token.
func VerifyToken(ctx context.Context, cfg Config) (forge.Author, error) {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	client := gogithub.NewClient(httpClient)
	if cfg.Token != "" {
		client = client.WithAuthToken(cfg.Token)
	}
	if cfg.BaseURL != "" {
		var err error
		client, err = client.WithEnterpriseURLs(strings.TrimSuffix(cfg.BaseURL, "/"), strings.TrimSuffix(cfg.BaseURL, "/"))
		if err != nil {
			return forge.Author{}, &forge.Error{
				Kind: forge.ErrKindUnsupported, Message: "configure base url: " + err.Error(), Err: err,
			}
		}
	}
	user, _, err := client.Users.Get(ctx, "")
	if err != nil {
		return forge.Author{}, wrapErr(err)
	}
	return forge.Author{Login: user.GetLogin(), Name: user.GetName()}, nil
}

// ListItems returns a page of issues or pull requests.
func (p *Provider) ListItems(ctx context.Context, opts forge.ListOptions) (forge.ListResult, error) {
	if opts.Type == forge.ItemTypeChangeRequest {
		return p.listPulls(ctx, opts)
	}
	return p.listIssues(ctx, opts)
}

func (p *Provider) listIssues(ctx context.Context, opts forge.ListOptions) (forge.ListResult, error) {
	// Issues have no merged lifecycle on either platform, and the GitHub list
	// endpoint does not reject the value — stateParam falls through to "open",
	// so forwarding it would return OPEN issues under a "merged" filter. Answer
	// the impossible query locally, mirroring the GitLab adapter.
	if opts.State == string(forge.StateMerged) {
		return forge.ListResult{Items: []forge.Item{}}, nil
	}
	if opts.Query != "" {
		return p.searchItems(ctx, opts, false)
	}
	lo := &gogithub.IssueListByRepoOptions{
		State:     stateParam(opts.State),
		Sort:      opts.Sort,
		Direction: opts.Direction,
		ListOptions: gogithub.ListOptions{
			Page:    pageOrDefault(opts.Page),
			PerPage: perPageOrDefault(opts.PerPage),
		},
	}
	if !opts.Since.IsZero() {
		lo.Since = opts.Since
	}
	issues, resp, err := p.client.Issues.ListByRepo(ctx, p.owner, p.repo, lo)
	if err != nil {
		return forge.ListResult{}, wrapErr(err)
	}
	items := make([]forge.Item, 0, len(issues))
	for _, iss := range issues {
		// The issues endpoint also returns pull requests; keep this list to
		// genuine issues so the two tabs stay disjoint.
		if iss.IsPullRequest() {
			continue
		}
		items = append(items, convertIssue(iss))
	}
	return listResult(items, resp), nil
}

// searchItems runs a text search scoped to this repo via GitHub's search API.
// The repo-scoped list endpoints have no text parameter, so search must go
// through /search/issues. `isPR` selects which half of the result to keep.
func (p *Provider) searchItems(ctx context.Context, opts forge.ListOptions, isPR bool) (forge.ListResult, error) {
	// Scope the query to this repository and to the requested half (issues vs
	// PRs) so the two tabs stay disjoint, mirroring the list endpoints.
	kind := "is:issue"
	if isPR {
		kind = "is:pr"
	}
	q := fmt.Sprintf("%s repo:%s/%s %s", kind, p.owner, p.repo, opts.Query)
	// The search API needs the merged/closed split spelled out as qualifiers,
	// because state:closed there also matches merged pull requests.
	switch opts.State {
	case string(forge.StateMerged):
		if isPR {
			q += " is:merged"
		}
	case string(forge.StateClosed):
		q += " state:closed"
		if isPR {
			q += " is:unmerged"
		}
	case "open":
		q += " state:open"
	}

	so := &gogithub.SearchOptions{
		Sort:  searchSortParam(opts.Sort),
		Order: opts.Direction,
		ListOptions: gogithub.ListOptions{
			Page:    pageOrDefault(opts.Page),
			PerPage: perPageOrDefault(opts.PerPage),
		},
	}
	res, resp, err := p.client.Search.Issues(ctx, q, so)
	if err != nil {
		return forge.ListResult{}, wrapErr(err)
	}
	items := make([]forge.Item, 0, len(res.Issues))
	for _, iss := range res.Issues {
		if isPR {
			items = append(items, convertIssueAsPull(iss))
		} else {
			items = append(items, convertIssue(iss))
		}
	}
	return listResult(items, resp), nil
}

// searchSortParam maps the generic sort field to the search API's accepted
// values (best-match is the default and is expressed as an empty string).
func searchSortParam(sort string) string {
	switch sort {
	case "updated", "created", "comments":
		return sort
	default:
		return ""
	}
}

func (p *Provider) listPulls(ctx context.Context, opts forge.ListOptions) (forge.ListResult, error) {
	// Two state filters cannot be served by the list endpoint:
	//
	//   - "merged" is not a value it understands. GitHub does not reject it —
	//     it silently falls back to OPEN pull requests.
	//   - "closed" must EXCLUDE merged PRs to match GitLab, but the list
	//     endpoint folds the two together and offers no way to separate them.
	//
	// Both go to the search API, which can express them (is:merged /
	// is:unmerged). Filtering merged rows out locally instead would be wrong in
	// a way the UI cannot recover from: a page whose PRs are all merged comes
	// back empty while hasMore stays true, and an empty list has no scrollable
	// area, so the infinite scroll never fires and later pages are unreachable.
	//
	// Two costs are accepted here, both preferable to the above:
	//
	//   - The search API is rate-limited far below the REST list endpoint
	//     (~30/min authenticated). This only applies when the user explicitly
	//     selects Closed or Merged; Open and All keep using the list endpoint.
	//   - Search serves at most 1000 results, so a very old closed PR can be
	//     unreachable. The list endpoint has no such cap, which is why it is
	//     still preferred wherever it can answer the query at all.
	if opts.State == string(forge.StateMerged) ||
		opts.State == string(forge.StateClosed) ||
		opts.Query != "" {
		return p.searchItems(ctx, opts, true)
	}
	lo := &gogithub.PullRequestListOptions{
		State:     stateParam(opts.State),
		Sort:      pullSortParam(opts.Sort),
		Direction: opts.Direction,
		ListOptions: gogithub.ListOptions{
			Page:    pageOrDefault(opts.Page),
			PerPage: perPageOrDefault(opts.PerPage),
		},
	}
	pulls, resp, err := p.client.PullRequests.List(ctx, p.owner, p.repo, lo)
	if err != nil {
		return forge.ListResult{}, wrapErr(err)
	}
	items := make([]forge.Item, 0, len(pulls))
	for _, pr := range pulls {
		items = append(items, convertPull(pr))
	}
	return listResult(items, resp), nil
}

// GetItem returns a single issue or pull request.
func (p *Provider) GetItem(ctx context.Context, typ forge.ItemType, number int) (forge.Item, error) {
	if typ == forge.ItemTypeChangeRequest {
		pr, _, err := p.client.PullRequests.Get(ctx, p.owner, p.repo, number)
		if err != nil {
			return forge.Item{}, wrapErr(err)
		}
		return convertPull(pr), nil
	}
	iss, _, err := p.client.Issues.Get(ctx, p.owner, p.repo, number)
	if err != nil {
		return forge.Item{}, wrapErr(err)
	}
	return convertIssue(iss), nil
}

// ListComments returns a page of comments for an item, oldest first.
func (p *Provider) ListComments(ctx context.Context, typ forge.ItemType, number, page, perPage int) ([]forge.Comment, error) {
	number, err := commentTarget(typ, number)
	if err != nil {
		return nil, err
	}
	lo := &gogithub.IssueListCommentsOptions{
		Sort:      gogithub.Ptr("created"),
		Direction: gogithub.Ptr("asc"),
		ListOptions: gogithub.ListOptions{
			Page:    pageOrDefault(page),
			PerPage: perPageOrDefault(perPage),
		},
	}
	comments, _, err := p.client.Issues.ListComments(ctx, p.owner, p.repo, number, lo)
	if err != nil {
		return nil, wrapErr(err)
	}
	out := make([]forge.Comment, 0, len(comments))
	for _, c := range comments {
		out = append(out, forge.Comment{
			ID:        c.GetID(),
			Author:    authorFromUser(c.User),
			Body:      c.GetBody(),
			CreatedAt: c.GetCreatedAt().Time,
			UpdatedAt: c.GetUpdatedAt().Time,
		})
	}
	return out, nil
}

// commentTarget validates that the comment endpoint supports this item type.
// GitHub's issue-comment endpoint serves both issues and PRs (a PR is an
// issue), so the number is passed through unchanged.
func commentTarget(typ forge.ItemType, number int) (int, error) {
	if number <= 0 {
		return 0, fmt.Errorf("github: invalid item number %d", number)
	}
	switch typ {
	case forge.ItemTypeIssue, forge.ItemTypeChangeRequest:
		return number, nil
	default:
		return 0, fmt.Errorf("github: unsupported item type %q", typ)
	}
}

func convertIssue(iss *gogithub.Issue) forge.Item {
	return forge.Item{
		Platform:     forge.PlatformGitHub,
		Type:         forge.ItemTypeIssue,
		Number:       iss.GetNumber(),
		Title:        iss.GetTitle(),
		Body:         iss.GetBody(),
		State:        normalizeState(iss.GetState(), false),
		Author:       authorFromUser(iss.User),
		Assignees:    authorsFromUsers(iss.Assignees),
		Labels:       labelsFromGitHub(iss.Labels),
		CommentCount: iss.GetComments(),
		URL:          iss.GetHTMLURL(),
		CreatedAt:    iss.GetCreatedAt().Time,
		UpdatedAt:    iss.GetUpdatedAt().Time,
	}
}

func convertPull(pr *gogithub.PullRequest) forge.Item {
	merged := pr.GetMerged()
	item := forge.Item{
		Platform:     forge.PlatformGitHub,
		Type:         forge.ItemTypeChangeRequest,
		Number:       pr.GetNumber(),
		Title:        pr.GetTitle(),
		Body:         pr.GetBody(),
		State:        normalizeState(pr.GetState(), merged),
		Draft:        pr.GetDraft(),
		Author:       authorFromUser(pr.User),
		Assignees:    authorsFromUsers(pr.Assignees),
		CommentCount: pr.GetComments(),
		URL:          pr.GetHTMLURL(),
		CreatedAt:    pr.GetCreatedAt().Time,
		UpdatedAt:    pr.GetUpdatedAt().Time,
		// The head branch is what the PR's CI runs are filtered by.
		SourceBranch: pr.GetHead().GetRef(),
	}
	if merged {
		if t := pr.GetMergedAt(); !t.IsZero() {
			tt := t.Time
			item.MergedAt = &tt
		}
	}
	return item
}

// convertIssueAsPull adapts a search-result Issue that is actually a PR. The
// search API returns both under the issue shape, which omits the merged flag,
// so a merged PR surfaces as "closed" here — acceptable for list rendering,
// and the detail view re-fetches via GetItem for exact state.
// convertIssueAsPull re-types an issue-shaped payload as a change request.
//
// The search API returns pull requests through the Issue shape, where the merge
// state lives in the nested pull_request object (the list endpoint's PullRequest
// type has no equivalent here). Without reading it, a merged PR found via search
// would normalize to "closed" and be indistinguishable from an unmerged one.
func convertIssueAsPull(iss *gogithub.Issue) forge.Item {
	item := convertIssue(iss)
	item.Type = forge.ItemTypeChangeRequest
	if links := iss.PullRequestLinks; links != nil && links.MergedAt != nil {
		merged := links.MergedAt.Time
		item.State = forge.StateMerged
		item.MergedAt = &merged
	}
	return item
}

// normalizeState collapses the platform's (state, merged) pair into a forge
// State. GitHub reports merged PRs as state="closed" with merged=true.
func normalizeState(state string, merged bool) forge.State {
	if merged {
		return forge.StateMerged
	}
	if state == "closed" {
		return forge.StateClosed
	}
	return forge.StateOpen
}

func stateParam(state string) string {
	switch state {
	case "open", "closed", "all":
		return state
	default:
		return "open"
	}
}

// pullSortParam maps the generic sort field to GitHub's pull request sort
// values (which differ from the issues endpoint).
func pullSortParam(sort string) string {
	switch sort {
	case "updated", "created", "popularity", "long-running":
		return sort
	case "":
		return ""
	default:
		return "updated"
	}
}

func pageOrDefault(page int) int {
	if page <= 0 {
		return 1
	}
	return page
}

func perPageOrDefault(n int) int {
	if n <= 0 {
		return 30
	}
	if n > 100 {
		return 100
	}
	return n
}

func authorFromUser(u *gogithub.User) forge.Author {
	if u == nil {
		return forge.Author{}
	}
	return forge.Author{Login: u.GetLogin(), Name: u.GetName()}
}

func authorsFromUsers(users []*gogithub.User) []forge.Author {
	if len(users) == 0 {
		return nil
	}
	out := make([]forge.Author, 0, len(users))
	for _, u := range users {
		out = append(out, authorFromUser(u))
	}
	return out
}

func labelsFromGitHub(labels []*gogithub.Label) []forge.Label {
	if len(labels) == 0 {
		return nil
	}
	out := make([]forge.Label, 0, len(labels))
	for _, l := range labels {
		out = append(out, forge.Label{Name: l.GetName(), Color: l.GetColor()})
	}
	return out
}

func listResult(items []forge.Item, resp *gogithub.Response) forge.ListResult {
	res := forge.ListResult{Items: items}
	if resp != nil && resp.NextPage > 0 {
		res.HasMore = true
		res.NextPage = resp.NextPage
	}
	return res
}

// wrapErr classifies a go-github error into a forge.Error.
func wrapErr(err error) error {
	if err == nil {
		return nil
	}
	var rateLimit *gogithub.RateLimitError
	if errors.As(err, &rateLimit) {
		status := 0
		retryAfter := 0
		if rateLimit.Response != nil {
			status = rateLimit.Response.StatusCode
			if v := rateLimit.Response.Header.Get("Retry-After"); v != "" {
				_, _ = fmt.Sscanf(v, "%d", &retryAfter)
			}
		}
		return forge.NewRateLimitError(status, retryAfter, rateLimit.Message)
	}
	var abuse *gogithub.AbuseRateLimitError
	if errors.As(err, &abuse) {
		retryAfter := 0
		if abuse.RetryAfter != nil {
			retryAfter = int(abuse.RetryAfter.Seconds())
		}
		return forge.NewRateLimitError(http.StatusTooManyRequests, retryAfter, abuse.Message)
	}
	var respErr *gogithub.ErrorResponse
	if errors.As(err, &respErr) {
		status := 0
		if respErr.Response != nil {
			status = respErr.Response.StatusCode
		}
		msg := respErr.Message
		if msg == "" {
			msg = http.StatusText(status)
		}
		return forge.NewHTTPError(status, msg, err)
	}
	// Transport-level failure (DNS, dial, TLS, timeout).
	return &forge.Error{Kind: forge.ErrKindNetwork, Message: err.Error(), Err: err}
}
