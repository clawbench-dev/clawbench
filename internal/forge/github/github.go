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

// ListItems returns a page of issues or pull requests.
func (p *Provider) ListItems(ctx context.Context, opts forge.ListOptions) (forge.ListResult, error) {
	if opts.Type == forge.ItemTypeChangeRequest {
		return p.listPulls(ctx, opts)
	}
	return p.listIssues(ctx, opts)
}

func (p *Provider) listIssues(ctx context.Context, opts forge.ListOptions) (forge.ListResult, error) {
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

func (p *Provider) listPulls(ctx context.Context, opts forge.ListOptions) (forge.ListResult, error) {
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
	}
	if merged {
		if t := pr.GetMergedAt(); !t.IsZero() {
			tt := t.Time
			item.MergedAt = &tt
		}
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
