// Package gitea adapts the Gitea REST API (v1) to the forge.Provider interface.
//
// Forgejo is a community fork that keeps the same API and the same paths, so one
// adapter serves both — the platform value is "gitea" for the pair (see
// forge.ParsePlatform).
//
// Why a hand-written client rather than an SDK: the requests this package needs
// are a handful of GETs, and the available Go clients pull in a dependency tree
// that is a poor trade for a read-only integration (the same reasoning as the
// GitLab adapter).
//
// It is strictly read-only: only GET requests are issued.
package gitea

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"clawbench/internal/forge"
)

// Config configures a Gitea provider instance.
type Config struct {
	// Token is a Gitea/Forgejo access token (scope: read:repository is enough).
	Token string
	// Host is the instance host (e.g. "git.acme.internal" or "git.acme.internal:3000").
	Host string
	// Scheme overrides the URL scheme; defaults to https.
	Scheme string
	// HTTPClient optionally overrides the transport (timeouts, TLS policy).
	HTTPClient *http.Client
}

// Provider implements forge.Provider against one Gitea/Forgejo repository.
type Provider struct {
	// baseURL is the API root ("https://host/api/v1").
	baseURL string
	// webBase is the instance origin ("https://host"), used to build web links.
	// Kept separate because baseURL carries the /api/v1 suffix.
	webBase string
	token   string
	owner   string
	repo    string
	client  *http.Client
}

// giteaMaxPerPage is the page size this adapter will ask for.
//
// Gitea clamps responses to API.MaxResponseItems, which defaults to 50. Asking
// for more would make a full page look short, and the caller's "is there another
// page" fallback (see paginationFromHeaders) would then stop early — silently
// skipping items. Clamping here keeps the request and the check consistent.
const giteaMaxPerPage = 50

// New builds a provider scoped to a single owner/repo.
func New(cfg Config, owner, repo string) (*Provider, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("gitea: host is required")
	}
	if owner == "" || repo == "" {
		return nil, fmt.Errorf("gitea: owner and repo are required")
	}
	scheme := cfg.Scheme
	if scheme == "" {
		scheme = "https"
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	origin := fmt.Sprintf("%s://%s", scheme, cfg.Host)
	return &Provider{
		baseURL: origin + "/api/v1",
		webBase: origin,
		token:   cfg.Token,
		owner:   owner,
		repo:    repo,
		client:  client,
	}, nil
}

// CurrentUser returns the authenticated account.
func (p *Provider) CurrentUser(ctx context.Context) (forge.Author, error) {
	return verifyUser(ctx, p.baseURL, p.token, p.client)
}

// VerifyToken checks that a credential authenticates against a Gitea/Forgejo
// host, returning the account it belongs to.
//
// It is host-scoped, not repo-scoped: verifying a token must not require a bound
// repository. GET /user is the standard probe and fails with 401 for a bad token.
func VerifyToken(ctx context.Context, cfg Config) (forge.Author, error) {
	if cfg.Host == "" {
		return forge.Author{}, fmt.Errorf("gitea: host is required")
	}
	scheme := cfg.Scheme
	if scheme == "" {
		scheme = "https"
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return verifyUser(ctx, fmt.Sprintf("%s://%s/api/v1", scheme, cfg.Host), cfg.Token, client)
}

// verifyUser probes GET /user, the shared implementation behind both the
// provider's CurrentUser and the host-scoped VerifyToken.
func verifyUser(ctx context.Context, baseURL, token string, client *http.Client) (forge.Author, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/user", http.NoBody)
	if err != nil {
		return forge.Author{}, &forge.Error{Kind: forge.ErrKindNetwork, Message: err.Error(), Err: err}
	}
	applyHeaders(req, token)

	resp, err := client.Do(req)
	if err != nil {
		return forge.Author{}, &forge.Error{Kind: forge.ErrKindNetwork, Message: err.Error(), Err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return forge.Author{}, classifyError(resp, body)
	}
	var u giteaUser
	if err := json.Unmarshal(body, &u); err != nil {
		return forge.Author{}, &forge.Error{
			Kind: forge.ErrKindUnknown, Status: resp.StatusCode, Message: "decode response: " + err.Error(), Err: err,
		}
	}
	return forge.Author{Login: u.Login, Name: u.FullName}, nil
}

// ListItems returns a page of issues or pull requests.
//
// Gitea serves both from one endpoint, told apart by `type`. The list payload
// carries no head branch for a pull request, so SourceBranch stays empty here;
// GetItem fetches the pull itself, which does have it. That is the same split
// the GitHub adapter makes and the reason CI-per-item is a detail-view call.
func (p *Provider) ListItems(ctx context.Context, opts forge.ListOptions) (forge.ListResult, error) {
	// A bare number is an exact lookup rather than a text search — see
	// forge.ParseNumberQuery.
	if number, ok := forge.ParseNumberQuery(opts.Query); ok {
		return p.listByNumber(ctx, opts.Type, number)
	}
	// Issues have no merged lifecycle, and Gitea would answer `state=closed` for
	// what the caller asked as `merged`. Answer the impossible query locally.
	if opts.Type != forge.ItemTypeChangeRequest && opts.State == string(forge.StateMerged) {
		return forge.ListResult{Items: []forge.Item{}}, nil
	}
	// Gitea has no merged state AND no `is:merged` search qualifier (verified
	// against a live instance: `q=is:merged` matches nothing), so the closed /
	// merged split has to be applied locally. Without this, the "merged" filter
	// would list closed-but-unmerged pull requests under a merged label.
	if opts.Type == forge.ItemTypeChangeRequest &&
		(opts.State == string(forge.StateMerged) || opts.State == string(forge.StateClosed)) {
		return p.listPullsByMergeState(ctx, opts)
	}
	return p.listItemsPage(ctx, opts, opts.State)
}

// listItemsPage issues ONE list request with an explicit state filter.
func (p *Provider) listItemsPage(ctx context.Context, opts forge.ListOptions, state string) (forge.ListResult, error) {
	kind := "issues"
	if opts.Type == forge.ItemTypeChangeRequest {
		kind = "pulls"
	}

	q := url.Values{}
	q.Set("state", stateParam(state))
	q.Set("type", kind)
	q.Set("sort", sortParam(opts.Sort, opts.Direction))
	q.Set("page", strconv.Itoa(pageOrDefault(opts.Page)))
	q.Set("limit", strconv.Itoa(perPageOrDefault(opts.PerPage)))
	if !opts.Since.IsZero() {
		q.Set("since", opts.Since.UTC().Format(time.RFC3339))
	}
	if opts.Query != "" {
		q.Set("q", opts.Query)
	}

	var raw []giteaIssue
	hdr, err := p.getRaw(ctx, p.repoPath("/issues"), q, &raw)
	if err != nil {
		return forge.ListResult{}, err
	}
	items := make([]forge.Item, 0, len(raw))
	for i := range raw {
		items = append(items, raw[i].toItem(opts.Type))
	}
	return forge.ListResult{
		Items:    items,
		HasMore:  hasMore(hdr, len(raw), perPageOrDefault(opts.PerPage)),
		NextPage: nextPage(hdr, len(raw), perPageOrDefault(opts.PerPage), opts.Page),
	}, nil
}

// maxMergeFilterPages bounds how many closed pages one "merged"/"closed" request
// may consume.
//
// The filter is local, so a page can come back with fewer matches than the caller
// asked for — or none at all. Walking a few more pages keeps a page full in the
// normal case; the cap keeps a repository whose closed pulls are all unmerged
// (or all merged) from turning one click into a long scan. When the cap is hit
// the result still reports HasMore, so scrolling continues from there.
const maxMergeFilterPages = 5

// listPullsByMergeState answers the closed / merged filters by walking closed
// results and splitting them on the merge timestamp.
//
// `opts.Page` is a starting point: the walk may consume several server pages to
// fill the caller's page, and NextPage reports where to continue.
func (p *Provider) listPullsByMergeState(ctx context.Context, opts forge.ListOptions) (forge.ListResult, error) {
	wantMerged := opts.State == string(forge.StateMerged)
	want := perPageOrDefault(opts.PerPage)

	page := pageOrDefault(opts.Page)
	out := make([]forge.Item, 0, want)
	for range maxMergeFilterPages {
		pageOpts := opts
		pageOpts.State = string(forge.StateClosed)
		pageOpts.Page = page
		res, err := p.listItemsPage(ctx, pageOpts, string(forge.StateClosed))
		if err != nil {
			return forge.ListResult{}, err
		}
		for _, it := range res.Items {
			if (it.State == forge.StateMerged) == wantMerged {
				out = append(out, it)
			}
		}
		if len(out) >= want || !res.HasMore {
			return forge.ListResult{Items: out, HasMore: res.HasMore, NextPage: res.NextPage}, nil
		}
		page = res.NextPage
	}
	// The cap stopped the walk before the caller's page was full. Reporting
	// HasMore keeps the next scroll going from where this one ended.
	return forge.ListResult{Items: out, HasMore: true, NextPage: page}, nil
}

// GetItem returns a single issue or pull request.
func (p *Provider) GetItem(ctx context.Context, typ forge.ItemType, number int) (forge.Item, error) {
	if number <= 0 {
		return forge.Item{}, fmt.Errorf("gitea: invalid item number %d", number)
	}
	switch typ {
	case forge.ItemTypeChangeRequest:
		var raw giteaPull
		if err := p.get(ctx, p.repoPath(fmt.Sprintf("/pulls/%d", number)), nil, &raw); err != nil {
			return forge.Item{}, err
		}
		return raw.toItem(), nil
	case forge.ItemTypeIssue:
		var raw giteaIssue
		if err := p.get(ctx, p.repoPath(fmt.Sprintf("/issues/%d", number)), nil, &raw); err != nil {
			return forge.Item{}, err
		}
		return raw.toItem(forge.ItemTypeIssue), nil
	default:
		return forge.Item{}, fmt.Errorf("gitea: unsupported item type %q", typ)
	}
}

// listByNumber resolves a numeric search to the single item it names.
//
// A miss is an empty page, not an error: the user is searching, and "there is
// no such issue" is a legitimate answer.
//
// Gitea numbers issues and pull requests from ONE sequence, and the issues
// endpoint answers for both (a pull request is an issue carrying a
// pull_request object). That makes the issue endpoint the probe: it says which
// kind the number is, and the requested type decides whether to show it.
func (p *Provider) listByNumber(ctx context.Context, typ forge.ItemType, number int) (forge.ListResult, error) {
	probe, err := p.GetItem(ctx, forge.ItemTypeIssue, number)
	if err != nil {
		if forge.IsNotFoundError(err) {
			return forge.ListResult{Items: []forge.Item{}}, nil
		}
		return forge.ListResult{}, err
	}
	isPull := probe.Type == forge.ItemTypeChangeRequest
	if (typ == forge.ItemTypeChangeRequest) != isPull {
		return forge.ListResult{Items: []forge.Item{}}, nil
	}
	if !isPull {
		return forge.ListResult{Items: []forge.Item{probe}}, nil
	}
	// The issue payload has no head branch, so the pull is re-read from its own
	// endpoint — the same one the detail view uses.
	item, err := p.GetItem(ctx, forge.ItemTypeChangeRequest, number)
	if err != nil {
		if forge.IsNotFoundError(err) {
			return forge.ListResult{Items: []forge.Item{}}, nil
		}
		return forge.ListResult{}, err
	}
	return forge.ListResult{Items: []forge.Item{item}}, nil
}

// ListComments returns a page of comments for an item, oldest first.
//
// Gitea comments are issue comments, and a pull request IS an issue, so the same
// endpoint serves both. There is no system-note flag: every row is a real
// comment, unlike GitLab's timeline.
func (p *Provider) ListComments(ctx context.Context, typ forge.ItemType, number, page, perPage int) ([]forge.Comment, error) {
	if number <= 0 {
		return nil, fmt.Errorf("gitea: invalid item number %d", number)
	}
	if typ != forge.ItemTypeIssue && typ != forge.ItemTypeChangeRequest {
		return nil, fmt.Errorf("gitea: unsupported item type %q", typ)
	}
	q := url.Values{}
	q.Set("page", strconv.Itoa(pageOrDefault(page)))
	q.Set("limit", strconv.Itoa(perPageOrDefault(perPage)))

	var raw []giteaComment
	if err := p.get(ctx, p.repoPath(fmt.Sprintf("/issues/%d/comments", number)), q, &raw); err != nil {
		return nil, err
	}
	out := make([]forge.Comment, 0, len(raw))
	for _, c := range raw {
		out = append(out, forge.Comment{
			ID:        c.ID,
			Author:    forge.Author{Login: c.User.Login, Name: c.User.FullName},
			Body:      c.Body,
			CreatedAt: parseTime(c.CreatedAt),
			UpdatedAt: parseTime(c.UpdatedAt),
		})
	}
	return out, nil
}

// repoPath builds "/repos/{owner}/{repo}<suffix>". Both segments are escaped:
// they come from a bound remote, and a stray slash must not become a new path
// segment on the instance.
func (p *Provider) repoPath(suffix string) string {
	return "/repos/" + url.PathEscape(p.owner) + "/" + url.PathEscape(p.repo) + suffix
}

// get performs a GET and decodes the JSON body into out.
func (p *Provider) get(ctx context.Context, path string, q url.Values, out any) error {
	_, err := p.getRaw(ctx, path, q, out)
	return err
}

// getRaw performs a GET and returns the response headers (for pagination) after
// decoding the body. The body is fully consumed and closed before returning, so
// no caller holds an open response.
func (p *Provider) getRaw(ctx context.Context, path string, q url.Values, out any) (http.Header, error) {
	u := p.baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, &forge.Error{Kind: forge.ErrKindNetwork, Message: err.Error(), Err: err}
	}
	applyHeaders(req, p.token)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, &forge.Error{Kind: forge.ErrKindNetwork, Message: err.Error(), Err: err}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return resp.Header, classifyError(resp, body)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.Header, &forge.Error{Kind: forge.ErrKindUnknown, Message: "decode response: " + err.Error(), Err: err}
		}
	}
	return resp.Header, nil
}

// applyHeaders sets the auth and accept headers shared by every request.
//
// "Authorization: token <t>" is Gitea's documented form; it also accepts
// "Bearer", but the token form is what its own UI and docs use.
func applyHeaders(req *http.Request, token string) {
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
	req.Header.Set("Accept", "application/json")
}

// classifyError maps a non-2xx response to a forge.Error. It is a package
// function (not a method) so the host-scoped VerifyToken can share it.
func classifyError(resp *http.Response, body []byte) error {
	msg := strings.TrimSpace(string(body))
	var apiErr struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Message != "" {
		msg = apiErr.Message
	}
	if msg == "" {
		msg = http.StatusText(resp.StatusCode)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return forge.NewRateLimitError(resp.StatusCode, retryAfter(resp), msg)
	}
	return forge.NewHTTPError(resp.StatusCode, msg, nil)
}

func retryAfter(resp *http.Response) int {
	if v := resp.Header.Get("Retry-After"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 0
}

// giteaUser is the minimal identity shape shared by every payload.
type giteaUser struct {
	Login    string `json:"login"`
	FullName string `json:"full_name"`
}

type giteaLabel struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// giteaPullRef is the subset of the pull_request object an ISSUE payload carries.
//
// It deliberately has no `merged` or `draft` field: those live on the pull
// endpoint. The merge is therefore read from merged_at, and a list row cannot
// show the draft flag — the detail view can.
type giteaPullRef struct {
	MergedAt *string `json:"merged_at"`
	HTMLURL  string  `json:"html_url"`
}

// giteaIssue is the shape of both an issue and (when PullRequest is set) a pull
// request as returned by the list and issue endpoints.
type giteaIssue struct {
	ID          int64         `json:"id"`
	Number      int           `json:"number"`
	Title       string        `json:"title"`
	Body        string        `json:"body"`
	State       string        `json:"state"`
	User        giteaUser     `json:"user"`
	Assignees   []giteaUser   `json:"assignees"`
	Labels      []giteaLabel  `json:"labels"`
	Comments    int           `json:"comments"`
	HTMLURL     string        `json:"html_url"`
	CreatedAt   string        `json:"created_at"`
	UpdatedAt   string        `json:"updated_at"`
	PullRequest *giteaPullRef `json:"pull_request"`
}

// giteaComment is one issue/PR comment. There is no system-note flag: every row
// is a real comment.
type giteaComment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	User      giteaUser `json:"user"`
	CreatedAt string    `json:"created_at"`
	UpdatedAt string    `json:"updated_at"`
}

// giteaPull is the pull endpoint's payload: the same item plus what only the
// pull view reports — head branch, merged flag, draft.
type giteaPull struct {
	ID        int64        `json:"id"`
	Number    int          `json:"number"`
	Title     string       `json:"title"`
	Body      string       `json:"body"`
	State     string       `json:"state"`
	User      giteaUser    `json:"user"`
	Assignees []giteaUser  `json:"assignees"`
	Labels    []giteaLabel `json:"labels"`
	Comments  int          `json:"comments"`
	HTMLURL   string       `json:"html_url"`
	CreatedAt string       `json:"created_at"`
	UpdatedAt string       `json:"updated_at"`
	Merged    bool         `json:"merged"`
	MergedAt  *string      `json:"merged_at"`
	Draft     bool         `json:"draft"`
	Head      struct {
		Ref string `json:"ref"`
	} `json:"head"`
}

func (g giteaIssue) toItem(typ forge.ItemType) forge.Item {
	item := forge.Item{
		Platform:     forge.PlatformGitea,
		Type:         typ,
		Number:       g.Number,
		Title:        g.Title,
		Body:         g.Body,
		State:        normalizeIssueState(g.State),
		Author:       forge.Author{Login: g.User.Login, Name: g.User.FullName},
		Assignees:    convertAssignees(g.Assignees),
		Labels:       convertLabels(g.Labels),
		CommentCount: g.Comments,
		URL:          g.HTMLURL,
		CreatedAt:    parseTime(g.CreatedAt),
		UpdatedAt:    parseTime(g.UpdatedAt),
	}
	// A pull request surfaced through the issues endpoint carries only its
	// merged_at, so that is what tells "closed after merge" from "closed".
	if g.PullRequest != nil {
		item.Type = forge.ItemTypeChangeRequest
		if g.PullRequest.MergedAt != nil && *g.PullRequest.MergedAt != "" {
			t := parseTime(*g.PullRequest.MergedAt)
			item.State = forge.StateMerged
			item.MergedAt = &t
		}
	}
	return item
}

func (g giteaPull) toItem() forge.Item {
	item := forge.Item{
		Platform:     forge.PlatformGitea,
		Type:         forge.ItemTypeChangeRequest,
		Number:       g.Number,
		Title:        g.Title,
		Body:         g.Body,
		State:        normalizeIssueState(g.State),
		Draft:        g.Draft,
		Author:       forge.Author{Login: g.User.Login, Name: g.User.FullName},
		Assignees:    convertAssignees(g.Assignees),
		Labels:       convertLabels(g.Labels),
		CommentCount: g.Comments,
		URL:          g.HTMLURL,
		CreatedAt:    parseTime(g.CreatedAt),
		UpdatedAt:    parseTime(g.UpdatedAt),
		SourceBranch: g.Head.Ref,
	}
	// `merged` is authoritative here (a merged pull reports state "closed");
	// merged_at fills in when.
	if g.Merged || (g.MergedAt != nil && *g.MergedAt != "") {
		item.State = forge.StateMerged
		if g.MergedAt != nil && *g.MergedAt != "" {
			t := parseTime(*g.MergedAt)
			item.MergedAt = &t
		}
	}
	return item
}

// normalizeIssueState maps Gitea's state vocabulary onto the forge one. Gitea
// reports only open/closed for both kinds; merged is derived by the callers.
func normalizeIssueState(state string) forge.State {
	switch state {
	case "closed":
		return forge.StateClosed
	default:
		return forge.StateOpen
	}
}

func convertAssignees(users []giteaUser) []forge.Author {
	if len(users) == 0 {
		return nil
	}
	out := make([]forge.Author, 0, len(users))
	for _, u := range users {
		out = append(out, forge.Author{Login: u.Login, Name: u.FullName})
	}
	return out
}

func convertLabels(labels []giteaLabel) []forge.Label {
	if len(labels) == 0 {
		return nil
	}
	out := make([]forge.Label, 0, len(labels))
	for _, l := range labels {
		out = append(out, forge.Label{Name: l.Name, Color: l.Color})
	}
	return out
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// stateParam maps the generic state vocabulary onto Gitea's.
//
// Gitea accepts open/closed/all. "merged" is not a state it knows — a merged
// pull request is closed — so it is answered as closed here, and the adapter's
// normalization is what reports the merge.
func stateParam(state string) string {
	switch state {
	case "open", "closed", "all":
		return state
	case "merged":
		return "closed"
	default:
		return "open"
	}
}

// sortParam maps the generic sort vocabulary onto Gitea's.
//
// Gitea encodes direction INSIDE the sort value ("recentupdate" is newest first,
// "leastupdate" oldest first); there is no separate direction parameter. An
// unknown sort falls back to recentupdate, which is what every caller in this
// app asks for.
func sortParam(sort, direction string) string {
	asc := direction == "asc"
	switch sort {
	case "created":
		if asc {
			return "oldest"
		}
		return "newest"
	default:
		if asc {
			return "leastupdate"
		}
		return "recentupdate"
	}
}

func pageOrDefault(page int) int {
	if page <= 0 {
		return 1
	}
	return page
}

// perPageOrDefault clamps the page size to what Gitea will actually return, so a
// "full page" always means "there may be another page" (see paginationFromHeaders).
func perPageOrDefault(n int) int {
	if n <= 0 {
		return 30
	}
	if n > giteaMaxPerPage {
		return giteaMaxPerPage
	}
	return n
}

// hasMore reports whether another page exists.
//
// Gitea sends X-HasMore (1.20+), which is authoritative. Older instances send
// only X-Total-Count; when neither is usable the caller's own request size is
// the fallback: a page that came back full may have more behind it, and one that
// came back short is the end.
func hasMore(hdr http.Header, got, requested int) bool {
	if v := hdr.Get("X-HasMore"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	if v := hdr.Get("X-Total-Count"); v != "" {
		if total, err := strconv.Atoi(v); err == nil {
			return total > got
		}
	}
	return got >= requested
}

// nextPage derives the next page number. It only has meaning when hasMore is
// true; callers must check that first.
func nextPage(hdr http.Header, got, requested, current int) int {
	if !hasMore(hdr, got, requested) {
		return 0
	}
	return pageOrDefault(current) + 1
}
