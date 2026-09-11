package handler

import (
	"net/http"
	"strconv"
	"strings"

	"clawbench/internal/forge"
	"clawbench/internal/service"
)

// forgeItemView is the frontend-facing shape of an issue or PR. It flattens the
// provider's Item and adds the binding identity so the UI never needs to know
// which platform served it.
type forgeItemView struct {
	Platform     string   `json:"platform"`
	Host         string   `json:"host"`
	Owner        string   `json:"owner"`
	Repo         string   `json:"repo"`
	Type         string   `json:"type"`
	Number       int      `json:"number"`
	Title        string   `json:"title"`
	Body         string   `json:"body,omitempty"`
	State        string   `json:"state"`
	Draft        bool     `json:"draft,omitempty"`
	Author       string   `json:"author"`
	Assignees    []string `json:"assignees,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	CommentCount int      `json:"commentCount"`
	URL          string   `json:"url"`
	CreatedAt    string   `json:"createdAt"`
	UpdatedAt    string   `json:"updatedAt"`
	Slug         string   `json:"slug"`
}

func toForgeItemView(pf *service.ProjectForge, item forge.Item) forgeItemView {
	assignees := make([]string, 0, len(item.Assignees))
	for _, a := range item.Assignees {
		assignees = append(assignees, a.Login)
	}
	labels := make([]string, 0, len(item.Labels))
	for _, l := range item.Labels {
		labels = append(labels, l.Name)
	}
	slug := ""
	if pf != nil {
		slug = pf.Slug()
	}
	return forgeItemView{
		Platform:     string(item.Platform),
		Host:         pf.Host,
		Owner:        pf.Owner,
		Repo:         pf.Repo,
		Type:         string(item.Type),
		Number:       item.Number,
		Title:        item.Title,
		Body:         item.Body,
		State:        string(item.State),
		Draft:        item.Draft,
		Author:       item.Author.Login,
		Assignees:    assignees,
		Labels:       labels,
		CommentCount: item.CommentCount,
		URL:          item.URL,
		CreatedAt:    item.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt:    item.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		Slug:         slug,
	}
}

// ServeForgeItems lists issues or PRs for the project's bound repository.
//
//	GET /api/forge/items?type=issue|pr&state=open|closed|all&page=N&perPage=N&q=...
//
// It also supports a "mine" filter which restricts the result to items
// involving the authenticated account.
func ServeForgeItems(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	pf, err := service.GetProjectForge(projectPath)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	if pf == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "no repository bound to this project",
			"code":  "NoForgeBinding",
		})
		return
	}
	provider, err := newForgeProvider(pf)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	q := r.URL.Query()
	typ := forge.ItemTypeIssue
	if q.Get("type") == string(forge.ItemTypeChangeRequest) {
		typ = forge.ItemTypeChangeRequest
	}
	opts := forge.ListOptions{
		Type:      typ,
		State:     normalizeForgeState(q.Get("state")),
		Page:      atoiDefault(q.Get("page"), 1),
		PerPage:   atoiDefault(q.Get("perPage"), 30),
		Query:     strings.TrimSpace(q.Get("q")),
		Sort:      "updated",
		Direction: "desc",
	}

	res, err := provider.ListItems(forgeContext(r), opts)
	if err != nil {
		writeForgeError(w, r, err)
		return
	}

	items := make([]forgeItemView, 0, len(res.Items))
	for _, it := range res.Items {
		items = append(items, toForgeItemView(pf, it))
	}

	// Optional "mine" filter, applied server-side using the token's identity.
	if q.Get("mine") == "1" {
		me, meErr := provider.CurrentUser(forgeContext(r))
		if meErr != nil {
			writeForgeError(w, r, meErr)
			return
		}
		items = filterForgeItemsMine(items, me.Login, q.Get("mineScope"))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":    items,
		"hasMore":  res.HasMore,
		"nextPage": res.NextPage,
		"binding": map[string]any{
			"platform": pf.Platform,
			"host":     pf.Host,
			"owner":    pf.Owner,
			"repo":     pf.Repo,
			"slug":     pf.Slug(),
			"source":   pf.Source,
		},
	})
}

// filterForgeItemsMine keeps items related to login according to scope:
//
//	"assigned" → assignee == login
//	"created"  → author == login
//	"review"   → change requests with login as author or assignee (best effort
//	             without a reviewer list; see the design's "跟我相关" definition)
//	""/"all"   → any involvement (author or assignee)
func filterForgeItemsMine(items []forgeItemView, login, scope string) []forgeItemView {
	if login == "" {
		return items
	}
	matches := func(it forgeItemView) bool {
		isAssignee := containsString(it.Assignees, login)
		isAuthor := it.Author == login
		switch scope {
		case "assigned":
			return isAssignee
		case "created":
			return isAuthor
		case "review":
			return it.Type == string(forge.ItemTypeChangeRequest) && (isAuthor || isAssignee)
		default:
			return isAuthor || isAssignee
		}
	}
	out := make([]forgeItemView, 0, len(items))
	for _, it := range items {
		if matches(it) {
			out = append(out, it)
		}
	}
	return out
}

func containsString(list []string, target string) bool {
	for _, s := range list {
		if s == target {
			return true
		}
	}
	return false
}

// ServeForgeItem returns a single issue or PR.
//
//	GET /api/forge/item?type=issue|pr&number=N
func ServeForgeItem(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	pf, err := service.GetProjectForge(projectPath)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	if pf == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "no repository bound", "code": "NoForgeBinding"})
		return
	}
	number := atoiDefault(r.URL.Query().Get("number"), 0)
	if number <= 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", nil)
		return
	}
	typ := forge.ItemTypeIssue
	if r.URL.Query().Get("type") == string(forge.ItemTypeChangeRequest) {
		typ = forge.ItemTypeChangeRequest
	}

	provider, err := newForgeProvider(pf)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	item, err := provider.GetItem(forgeContext(r), typ, number)
	if err != nil {
		writeForgeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": toForgeItemView(pf, item)})
}

// ServeForgeComments returns a page of comments for an item, oldest first.
//
//	GET /api/forge/comments?type=issue|pr&number=N&page=N&perPage=N
func ServeForgeComments(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	pf, err := service.GetProjectForge(projectPath)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	if pf == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "no repository bound", "code": "NoForgeBinding"})
		return
	}
	number := atoiDefault(r.URL.Query().Get("number"), 0)
	if number <= 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", nil)
		return
	}
	typ := forge.ItemTypeIssue
	if r.URL.Query().Get("type") == string(forge.ItemTypeChangeRequest) {
		typ = forge.ItemTypeChangeRequest
	}

	provider, err := newForgeProvider(pf)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	comments, err := provider.ListComments(
		forgeContext(r), typ, number,
		atoiDefault(r.URL.Query().Get("page"), 1),
		atoiDefault(r.URL.Query().Get("perPage"), 30),
	)
	if err != nil {
		writeForgeError(w, r, err)
		return
	}

	out := make([]map[string]any, 0, len(comments))
	for _, c := range comments {
		out = append(out, map[string]any{
			"id":        c.ID,
			"author":    c.Author.Login,
			"body":      c.Body,
			"createdAt": c.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			"updatedAt": c.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"comments": out})
}

// ServeForgeBinding reads, sets, or clears the project's repository binding.
//
//	GET    /api/forge/binding          → current binding (or null)
//	POST   /api/forge/binding          → {platform, host, owner, repo} or {url}
//	DELETE /api/forge/binding          → remove the binding
func ServeForgeBinding(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		pf, err := service.GetProjectForge(projectPath)
		if err != nil {
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
			return
		}
		if pf == nil {
			// Unbound: offer a suggestion derived from the git remote so the UI
			// can pre-fill the binding form. It is NOT persisted — the user must
			// confirm, which is also the SSRF guard's explicit-confirmation step
			// for non-official hosts.
			writeJSON(w, http.StatusOK, map[string]any{
				"binding":   nil,
				"suggested": suggestForgeBinding(projectPath),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"binding": bindingView(pf)})
	case http.MethodPost:
		serveForgeBindingSet(w, r, projectPath)
	case http.MethodDelete:
		if err := service.DeleteProjectForge(projectPath); err != nil {
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

// forgeBindingRequest accepts either an explicit platform/host/owner/repo or a
// raw remote URL to parse.
type forgeBindingRequest struct {
	Platform string `json:"platform"`
	Host     string `json:"host"`
	Owner    string `json:"owner"`
	Repo     string `json:"repo"`
	// URL is a remote URL (https or ssh form) parsed server-side.
	URL string `json:"url"`
	// Source marks how the binding was established; defaults to "manual".
	Source string `json:"source"`
}

func serveForgeBindingSet(w http.ResponseWriter, r *http.Request, projectPath string) {
	var req forgeBindingRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	var remote forge.Remote
	if req.URL != "" {
		parsed, err := forge.ParseRemoteURL(req.URL)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid repository url", "code": "InvalidRemoteURL"})
			return
		}
		remote = parsed
	} else {
		if req.Platform == "" || req.Host == "" || req.Owner == "" || req.Repo == "" {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", nil)
			return
		}
		remote = forge.Remote{
			Platform: forge.Platform(req.Platform),
			Host:     strings.ToLower(req.Host),
			Owner:    req.Owner,
			Repo:     req.Repo,
		}
	}

	// Refuse to bind a host that must never receive credentials.
	if err := checkForgeHostAllowed(remote.Host); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "code": "UnsafeHost"})
		return
	}

	source := req.Source
	if source == "" {
		source = "manual"
	}
	pf := service.ProjectForgeFromRemote(projectPath, remote, source)
	if err := service.UpsertProjectForge(pf); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	stored, err := service.GetProjectForge(projectPath)
	if err != nil || stored == nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"binding": bindingView(stored)})
}

// ServeForgeRemotes lists the project's git remotes, parsed into forge
// repositories. Remotes that are not forge URLs are returned with a null parse
// so the UI can show them but not offer them as a binding.
//
//	GET /api/forge/remotes
func ServeForgeRemotes(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	remotes, err := listGitRemotes(projectPath)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"remotes": []any{}})
		return
	}
	out := make([]map[string]any, 0, len(remotes))
	for _, rem := range remotes {
		entry := map[string]any{
			"name": rem.Name,
			"url":  rem.URL,
		}
		if parsed, perr := forge.ParseRemoteURL(rem.URL); perr == nil {
			entry["platform"] = string(parsed.Platform)
			entry["host"] = parsed.Host
			entry["owner"] = parsed.Owner
			entry["repo"] = parsed.Repo
			entry["slug"] = parsed.Slug()
		}
		out = append(out, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"remotes": out})
}

// ServeForgeTest verifies that the bound repository is reachable with the
// configured credential.
//
//	POST /api/forge/test
func ServeForgeTest(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	pf, err := service.GetProjectForge(projectPath)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	if pf == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "no repository bound", "code": "NoForgeBinding"})
		return
	}
	provider, err := newForgeProvider(pf)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	user, err := provider.CurrentUser(forgeContext(r))
	if err != nil {
		var fe *forge.Error
		code := "ForgeError"
		if errorsAsForge(err, &fe) {
			code = string(fe.Kind)
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "code": code})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"identity": user.Login,
		"binding":  bindingView(pf),
	})
}

func bindingView(pf *service.ProjectForge) map[string]any {
	return map[string]any{
		"platform": pf.Platform,
		"host":     pf.Host,
		"owner":    pf.Owner,
		"repo":     pf.Repo,
		"slug":     pf.Slug(),
		"source":   pf.Source,
	}
}

// normalizeForgeState clamps the state query to the values the providers accept.
func normalizeForgeState(state string) string {
	switch state {
	case "open", "closed", "all":
		return state
	default:
		return "open"
	}
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// suggestForgeBinding derives a candidate binding from the project's git
// remotes. It prefers "origin", falls back to the first remote that parses as a
// forge URL, and returns nil when none qualifies. The result is a suggestion
// only — callers must not persist it without user confirmation.
func suggestForgeBinding(projectPath string) map[string]any {
	remotes, err := listGitRemotes(projectPath)
	if err != nil {
		return nil
	}
	// Prefer origin, then any parseable remote.
	ordered := make([]gitRemote, 0, len(remotes))
	for _, rem := range remotes {
		if rem.Name == "origin" {
			ordered = append([]gitRemote{rem}, ordered...)
		} else {
			ordered = append(ordered, rem)
		}
	}
	for _, rem := range ordered {
		parsed, perr := forge.ParseRemoteURL(rem.URL)
		if perr != nil {
			continue
		}
		// Never suggest a host that must not receive credentials.
		if checkForgeHostAllowed(parsed.Host) != nil {
			continue
		}
		return map[string]any{
			"platform": string(parsed.Platform),
			"host":     parsed.Host,
			"owner":    parsed.Owner,
			"repo":     parsed.Repo,
			"slug":     parsed.Slug(),
			"remote":   rem.Name,
		}
	}
	return nil
}

// ServeForgeUnread returns the unread forge-event count. The count is
// independent of the notification toggles: it tracks "are there new changes",
// so turning every notification off does not silently kill the badge.
//
//	GET /api/forge/unread
func ServeForgeUnread(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	n, err := service.CountUnreadForgeEvents()
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": n})
}

// ServeForgeMarkRead clears the unread count (called when the user opens the
// Issues & PRs tab).
//
//	POST /api/forge/read
func ServeForgeMarkRead(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if err := service.MarkForgeEventsRead(); err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": 0})
}
