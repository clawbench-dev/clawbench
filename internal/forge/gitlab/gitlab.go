// Package gitlab adapts the GitLab REST API (v4) to the forge.Provider
// interface using a deliberately lightweight client.
//
// Why not the official gitlab-org/api/client-go: its dependency tree pulls in
// protovalidate, protobuf, cel-go, graphql-go and go-keyring, which is a poor
// trade for a read-only integration that touches a handful of endpoints. This
// client implements only the requests this package needs.
//
// It is strictly read-only: only GET requests are issued.
package gitlab

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

// Config configures a GitLab provider instance.
type Config struct {
	// Token is a GitLab PAT (scope: read_api).
	Token string
	// Host is the GitLab host (e.g. "gitlab.com" or "git.acme.internal:8443").
	Host string
	// Scheme overrides the URL scheme; defaults to https.
	Scheme string
	// HTTPClient optionally overrides the transport (timeouts, TLS policy).
	HTTPClient *http.Client
}

// Provider implements forge.Provider against GitLab.
type Provider struct {
	baseURL string
	token   string
	project string // URL-encoded namespace/project path
	client  *http.Client
}

// New builds a GitLab provider scoped to a single namespace/project path.
func New(cfg Config, namespace, repo string) (*Provider, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("gitlab: host is required")
	}
	if namespace == "" || repo == "" {
		return nil, fmt.Errorf("gitlab: namespace and repo are required")
	}
	scheme := cfg.Scheme
	if scheme == "" {
		scheme = "https"
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	fullPath := namespace + "/" + repo
	return &Provider{
		baseURL: fmt.Sprintf("%s://%s/api/v4", scheme, cfg.Host),
		token:   cfg.Token,
		project: url.PathEscape(fullPath),
		client:  client,
	}, nil
}

// CurrentUser returns the authenticated account.
func (p *Provider) CurrentUser(ctx context.Context) (forge.Author, error) {
	return verifyUser(ctx, p.baseURL, p.token, p.client)
}

// VerifyToken checks that a credential authenticates against a GitLab host,
// returning the account it belongs to.
//
// It is host-scoped, not project-scoped: verifying a token must not require a
// bound repository. GET /user is the standard probe and fails with 401 for a
// bad token.
func VerifyToken(ctx context.Context, cfg Config) (forge.Author, error) {
	if cfg.Host == "" {
		return forge.Author{}, fmt.Errorf("gitlab: host is required")
	}
	scheme := cfg.Scheme
	if scheme == "" {
		scheme = "https"
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return verifyUser(ctx, fmt.Sprintf("%s://%s/api/v4", scheme, cfg.Host), cfg.Token, client)
}

// verifyUser probes GET /user, the shared implementation behind both the
// provider's CurrentUser and the host-scoped VerifyToken.
func verifyUser(ctx context.Context, baseURL, token string, client *http.Client) (forge.Author, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/user", http.NoBody)
	if err != nil {
		return forge.Author{}, &forge.Error{Kind: forge.ErrKindNetwork, Message: err.Error(), Err: err}
	}
	if token != "" {
		req.Header.Set("PRIVATE-TOKEN", token)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return forge.Author{}, &forge.Error{Kind: forge.ErrKindNetwork, Message: err.Error(), Err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return forge.Author{}, classifyError(resp, body)
	}
	var u struct {
		Username string `json:"username"`
		Name     string `json:"name"`
	}
	if err := json.Unmarshal(body, &u); err != nil {
		return forge.Author{}, &forge.Error{
			Kind: forge.ErrKindUnknown, Status: resp.StatusCode, Message: "decode response: " + err.Error(), Err: err,
		}
	}
	return forge.Author{Login: u.Username, Name: u.Name}, nil
}

// ListItems returns a page of issues or merge requests.
func (p *Provider) ListItems(ctx context.Context, opts forge.ListOptions) (forge.ListResult, error) {
	path := "/projects/" + p.project + "/issues"
	if opts.Type == forge.ItemTypeChangeRequest {
		path = "/projects/" + p.project + "/merge_requests"
	}
	q := url.Values{}
	q.Set("state", stateParam(opts.State))
	q.Set("order_by", orderByParam(opts.Sort))
	q.Set("sort", directionParam(opts.Direction))
	q.Set("page", strconv.Itoa(pageOrDefault(opts.Page)))
	q.Set("per_page", strconv.Itoa(perPageOrDefault(opts.PerPage)))
	if !opts.Since.IsZero() {
		// GitLab accepts an ISO-8601 timestamp for updated_after.
		q.Set("updated_after", opts.Since.UTC().Format(time.RFC3339))
	}
	if opts.Query != "" {
		q.Set("search", opts.Query)
	}

	var raw []gitlabItem
	hdr, err := p.getRaw(ctx, path, q, &raw)
	if err != nil {
		return forge.ListResult{}, err
	}
	items := make([]forge.Item, 0, len(raw))
	for i := range raw {
		items = append(items, raw[i].toItem(opts.Type))
	}
	return listResult(items, hdr), nil
}

// GetItem returns a single issue or merge request.
func (p *Provider) GetItem(ctx context.Context, typ forge.ItemType, number int) (forge.Item, error) {
	if number <= 0 {
		return forge.Item{}, fmt.Errorf("gitlab: invalid item number %d", number)
	}
	resource := "issues"
	if typ == forge.ItemTypeChangeRequest {
		resource = "merge_requests"
	} else if typ != forge.ItemTypeIssue {
		return forge.Item{}, fmt.Errorf("gitlab: unsupported item type %q", typ)
	}
	path := fmt.Sprintf("/projects/%s/%s/%d", p.project, resource, number)
	var raw gitlabItem
	if err := p.get(ctx, path, nil, &raw); err != nil {
		return forge.Item{}, err
	}
	return raw.toItem(typ), nil
}

// ListComments returns a page of notes for an item, oldest first.
//
// GitLab has no repository-level comment endpoint; notes are fetched per
// item. Ordering is ascending by creation so callers can page upward.
func (p *Provider) ListComments(ctx context.Context, typ forge.ItemType, number, page, perPage int) ([]forge.Comment, error) {
	if number <= 0 {
		return nil, fmt.Errorf("gitlab: invalid item number %d", number)
	}
	resource := "issues"
	if typ == forge.ItemTypeChangeRequest {
		resource = "merge_requests"
	} else if typ != forge.ItemTypeIssue {
		return nil, fmt.Errorf("gitlab: unsupported item type %q", typ)
	}
	q := url.Values{}
	q.Set("sort", "asc")
	q.Set("order_by", "created_at")
	q.Set("page", strconv.Itoa(pageOrDefault(page)))
	q.Set("per_page", strconv.Itoa(perPageOrDefault(perPage)))

	path := fmt.Sprintf("/projects/%s/%s/%d/notes", p.project, resource, number)
	var notes []gitlabNote
	if err := p.get(ctx, path, q, &notes); err != nil {
		return nil, err
	}
	out := make([]forge.Comment, 0, len(notes))
	for _, n := range notes {
		// System notes are platform-generated ("changed the description",
		// "added label X"). They are noise for a comment timeline.
		if n.System {
			continue
		}
		out = append(out, forge.Comment{
			ID:        n.ID,
			Author:    forge.Author{Login: n.Author.Username, Name: n.Author.Name},
			Body:      n.Body,
			CreatedAt: parseTime(n.CreatedAt),
			UpdatedAt: parseTime(n.UpdatedAt),
		})
	}
	return out, nil
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
	if p.token != "" {
		req.Header.Set("PRIVATE-TOKEN", p.token)
	}
	req.Header.Set("Accept", "application/json")

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

// classifyError maps a non-2xx GitLab response to a forge.Error. It is a
// package function (not a method) so the host-scoped VerifyToken can share it.
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

	// GitLab signals throttling with 429, or with a RateLimit-Remaining: 0 on
	// a 403. Prefer the explicit rate-limit signal over the generic status.
	if resp.StatusCode == http.StatusTooManyRequests {
		return forge.NewRateLimitError(resp.StatusCode, retryAfter(resp), msg)
	}
	if resp.StatusCode == http.StatusForbidden && resp.Header.Get("RateLimit-Remaining") == "0" {
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
	// GitLab also exposes RateLimit-Reset as a Unix timestamp.
	if v := resp.Header.Get("RateLimit-Reset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			secs := int(time.Until(time.Unix(int64(n), 0)).Seconds())
			if secs > 0 {
				return secs
			}
		}
	}
	return 0
}

// gitlabItem is the shared shape of a GitLab issue or merge request.
type gitlabItem struct {
	IID            int          `json:"iid"`
	Title          string       `json:"title"`
	Description    string       `json:"description"`
	State          string       `json:"state"` // opened / closed / merged (MR) / locked
	Author         gitlabUser   `json:"author"`
	Assignees      []gitlabUser `json:"assignees"`
	Labels         []string     `json:"labels"`
	UserNotesCount int          `json:"user_notes_count"`
	WebURL         string       `json:"web_url"`
	CreatedAt      string       `json:"created_at"`
	UpdatedAt      string       `json:"updated_at"`
	MergedAt       string       `json:"merged_at"`
	// MR-only fields.
	Draft bool `json:"draft"`
}

type gitlabUser struct {
	Username string `json:"username"`
	Name     string `json:"name"`
}

type gitlabNote struct {
	ID        int64      `json:"id"`
	Body      string     `json:"body"`
	Author    gitlabUser `json:"author"`
	System    bool       `json:"system"`
	CreatedAt string     `json:"created_at"`
	UpdatedAt string     `json:"updated_at"`
}

func (g gitlabItem) toItem(typ forge.ItemType) forge.Item {
	// GitLab MRs report state="merged" directly; issues use opened/closed.
	state := normalizeState(g.State)
	item := forge.Item{
		Platform:     forge.PlatformGitLab,
		Type:         typ,
		Number:       g.IID,
		Title:        g.Title,
		Body:         g.Description,
		State:        state,
		Draft:        g.Draft,
		Author:       forge.Author{Login: g.Author.Username, Name: g.Author.Name},
		Assignees:    convertAssignees(g.Assignees),
		Labels:       convertLabels(g.Labels),
		CommentCount: g.UserNotesCount,
		URL:          g.WebURL,
		CreatedAt:    parseTime(g.CreatedAt),
		UpdatedAt:    parseTime(g.UpdatedAt),
	}
	if state == forge.StateMerged && g.MergedAt != "" {
		t := parseTime(g.MergedAt)
		item.MergedAt = &t
	}
	return item
}

// normalizeState maps GitLab's state vocabulary (opened/closed/merged/locked)
// to the forge vocabulary.
func normalizeState(state string) forge.State {
	switch state {
	case "merged":
		return forge.StateMerged
	case "closed", "locked":
		return forge.StateClosed
	default:
		return forge.StateOpen
	}
}

func convertAssignees(users []gitlabUser) []forge.Author {
	if len(users) == 0 {
		return nil
	}
	out := make([]forge.Author, 0, len(users))
	for _, u := range users {
		out = append(out, forge.Author{Login: u.Username, Name: u.Name})
	}
	return out
}

func convertLabels(labels []string) []forge.Label {
	if len(labels) == 0 {
		return nil
	}
	out := make([]forge.Label, 0, len(labels))
	for _, l := range labels {
		out = append(out, forge.Label{Name: l})
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

func stateParam(state string) string {
	switch state {
	case "open":
		return "opened"
	case "closed", "all":
		return state
	default:
		return "opened"
	}
}

func orderByParam(sort string) string {
	switch sort {
	case "updated", "created":
		return sort
	default:
		return "updated"
	}
}

func directionParam(dir string) string {
	if dir == "asc" {
		return "asc"
	}
	return "desc"
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

// listResult derives pagination state from GitLab's X-Next-Page header.
func listResult(items []forge.Item, hdr http.Header) forge.ListResult {
	res := forge.ListResult{Items: items}
	if hdr == nil {
		return res
	}
	if next := hdr.Get("X-Next-Page"); next != "" {
		if n, err := strconv.Atoi(next); err == nil && n > 0 {
			res.HasMore = true
			res.NextPage = n
		}
	}
	return res
}
