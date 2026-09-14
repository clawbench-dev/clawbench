package handler

import (
	"net/http"
	"strconv"
	"strings"

	"clawbench/internal/forge"
	"clawbench/internal/service"
)

// JSON field names shared by the forge endpoints. Extracted so the same key is
// never spelled two different ways across responses.
const (
	jsonItems          = "items"
	jsonHasMore        = "hasMore"
	jsonBinding        = "binding"
	jsonHost           = "host"
	jsonNoForgeBinding = "NoForgeBinding"
	jsonCode           = "code"
	// jsonCount is the response key for a numeric total (the unread count).
	jsonCount = "count"
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
	// Unread is true when this item has forge activity the user has not seen.
	// It is filled from the same rows the dock badge counts, so the badge and
	// the per-row dot cannot disagree.
	Unread bool `json:"unread,omitempty"`
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
	var slug, host, owner, repo string
	if pf != nil {
		slug, host, owner, repo = pf.Slug(), pf.Host, pf.Owner, pf.Repo
	}
	return forgeItemView{
		Platform:     string(item.Platform),
		Host:         host,
		Owner:        owner,
		Repo:         repo,
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
			strReqError: "no repository bound to this project",
			jsonCode:    jsonNoForgeBinding,
		})
		return
	}
	provider, err := newForgeProvider(pf)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{strReqError: err.Error()})
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
		writeForgeError(w, err)
		return
	}

	// Tag each row with its unread state. One query for the whole page, and no
	// extra provider call — the list and the dock badge read the same rows.
	unreadKeys, err := service.UnreadForgeItemKeys(pf.RepoKey())
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	items := make([]forgeItemView, 0, len(res.Items))
	for _, it := range res.Items {
		view := toForgeItemView(pf, it)
		view.Unread = unreadKeys[forge.ItemKeyForNumber(it.Type, it.Number)]
		items = append(items, view)
	}

	// Optional "mine" filter, applied server-side using the token's identity.
	if q.Get("mine") == "1" {
		me, meErr := provider.CurrentUser(forgeContext(r))
		if meErr != nil {
			writeForgeError(w, meErr)
			return
		}
		items = filterForgeItemsMine(items, me.Login, q.Get("mineScope"))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		jsonItems:   items,
		jsonHasMore: res.HasMore,
		"nextPage":  res.NextPage,
		jsonBinding: map[string]any{
			"platform": pf.Platform,
			jsonHost:   pf.Host,
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
		writeJSON(w, http.StatusNotFound, map[string]any{strReqError: "no repository bound", jsonCode: jsonNoForgeBinding})
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
		writeJSON(w, http.StatusInternalServerError, map[string]any{strReqError: err.Error()})
		return
	}
	item, err := provider.GetItem(forgeContext(r), typ, number)
	if err != nil {
		writeForgeError(w, err)
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
		writeJSON(w, http.StatusNotFound, map[string]any{strReqError: "no repository bound", jsonCode: jsonNoForgeBinding})
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
		writeJSON(w, http.StatusInternalServerError, map[string]any{strReqError: err.Error()})
		return
	}
	comments, err := provider.ListComments(
		forgeContext(r), typ, number,
		atoiDefault(r.URL.Query().Get("page"), 1),
		atoiDefault(r.URL.Query().Get("perPage"), 30),
	)
	if err != nil {
		writeForgeError(w, err)
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
			// Unbound: bind the highest-priority git remote automatically when it
			// points at a platform-operated host, so the common case needs no
			// dialog at all. Users who bind the wrong repository can change or
			// clear it from the panel header.
			//
			// Non-official hosts (a self-hosted GitLab, say) are NOT auto-bound:
			// the user's explicit confirmation is the SSRF guard's confirmation
			// step for hosts whose endpoints we cannot vouch for, so those still
			// come back as a suggestion for the UI to confirm.
			if remote, _, ok := pickForgeRemote(projectPath); ok && isOfficialForgeHost(remote.Host) {
				created, aerr := service.AutoBindProjectForge(projectPath, remote)
				if aerr != nil {
					writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
					return
				}
				if created {
					if bound, gerr := service.GetProjectForge(projectPath); gerr == nil && bound != nil {
						writeJSON(w, http.StatusOK, map[string]any{jsonBinding: bindingView(bound)})
						return
					}
				}
			}
			// Either nothing to bind, a non-official host needing confirmation,
			// or the user previously opted out (AutoBindProjectForge is a no-op
			// then) — report unbound, with a suggestion when one exists.
			writeJSON(w, http.StatusOK, map[string]any{
				jsonBinding: nil,
				"suggested": suggestForgeBinding(projectPath),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{jsonBinding: bindingView(pf)})
	case http.MethodPost:
		serveForgeBindingSet(w, r, projectPath)
	case http.MethodDelete:
		// Record the opt-out BEFORE deleting, so the auto-bind on the next GET
		// cannot bind it straight back.
		if err := service.SetForgeBindOptOut(projectPath, true); err != nil {
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
			return
		}
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
			writeJSON(w, http.StatusBadRequest, map[string]any{strReqError: "invalid repository url", jsonCode: "InvalidRemoteURL"})
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
		writeJSON(w, http.StatusBadRequest, map[string]any{strReqError: err.Error(), jsonCode: "UnsafeHost"})
		return
	}

	source := req.Source
	if source == "" {
		source = "manual"
	}
	pf := service.ProjectForgeFromRemote(projectPath, remote, source)
	if err := service.UpsertProjectForge(pf); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{strReqError: err.Error()})
		return
	}
	// An explicit bind supersedes any earlier unbind, so the project follows
	// normal auto-bind behavior again if this binding is later cleared.
	if err := service.SetForgeBindOptOut(projectPath, false); err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	stored, err := service.GetProjectForge(projectPath)
	if err != nil || stored == nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{jsonBinding: bindingView(stored)})
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
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, strReqError: "no repository bound", jsonCode: jsonNoForgeBinding})
		return
	}
	provider, err := newForgeProvider(pf)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, strReqError: err.Error()})
		return
	}
	user, err := provider.CurrentUser(forgeContext(r))
	if err != nil {
		var fe *forge.Error
		code := "ForgeError"
		if errorsAsForge(err, &fe) {
			code = string(fe.Kind)
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, strReqError: err.Error(), jsonCode: code})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"identity":  user.Login,
		jsonBinding: bindingView(pf),
	})
}

func bindingView(pf *service.ProjectForge) map[string]any {
	return map[string]any{
		"platform": pf.Platform,
		jsonHost:   pf.Host,
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

// pickForgeRemote derives the highest-priority candidate binding from the
// project's git remotes. It prefers "origin", falls back to the first remote
// that parses as a forge URL, and reports false when none qualifies. Hosts that
// must never receive credentials are skipped.
func pickForgeRemote(projectPath string) (forge.Remote, string, bool) {
	remotes, err := listGitRemotes(projectPath)
	if err != nil {
		return forge.Remote{}, "", false
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
		return parsed, rem.Name, true
	}
	return forge.Remote{}, "", false
}

// isOfficialForgeHost reports whether host is exactly a platform-operated host.
//
// It deliberately does NOT use Remote.IsOfficialHost(), which strips the port:
// that would treat "github.com:8443" as official and let auto-binding persist it,
// after which newForgeProvider would send the stored token to
// https://github.com:8443/api/v3. An exact match keeps auto-binding on the hosts
// whose endpoints are known.
func isOfficialForgeHost(host string) bool {
	return host == forge.GitHubHost || host == forge.GitLabHost
}

// suggestForgeBinding derives a candidate binding from the project's git
// remotes. The result is a suggestion only — non-official hosts must not be
// persisted without user confirmation (see ServeForgeBinding).
func suggestForgeBinding(projectPath string) map[string]any {
	parsed, name, ok := pickForgeRemote(projectPath)
	if !ok {
		return nil
	}
	return map[string]any{
		"platform": string(parsed.Platform),
		jsonHost:   parsed.Host,
		"owner":    parsed.Owner,
		"repo":     parsed.Repo,
		"slug":     parsed.Slug(),
		"remote":   name,
	}
}

// ServeForgeUnread returns the number of unread ITEMS for the project's bound
// repository. The count is independent of the notification toggles: it tracks
// "are there new changes", so turning every notification off does not silently
// kill the badge.
//
// It is project-scoped because the panel it points at is one project's
// repository; a global count would be a number the user cannot act on.
//
//	GET /api/forge/unread
func ServeForgeUnread(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	// An unbound project has nothing to be unread about. This is a normal state,
	// not an error: the panel shows its "bind a repository" prompt.
	pf, err := service.GetProjectForge(projectPath)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	if pf == nil {
		writeJSON(w, http.StatusOK, map[string]any{jsonCount: 0})
		return
	}
	n, err := service.CountUnreadForgeEvents(pf.RepoKey())
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{jsonCount: n})
}

// ServeForgeMarkRead marks unread forge activity as read.
//
// With no body (or an empty itemKey) it marks the whole repository, which is the
// "mark all read" action. With `{"itemKey":"issue/42"}` it marks just that item,
// which is what opening a row does.
//
//	POST /api/forge/read
func ServeForgeMarkRead(w http.ResponseWriter, r *http.Request) {
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
		writeJSON(w, http.StatusOK, map[string]any{jsonCount: 0})
		return
	}

	// The body is optional: an absent or empty body means "all".
	var req struct {
		ItemKey string `json:"itemKey"`
	}
	if err = decodeOptionalJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{strReqError: err.Error()})
		return
	}
	if err = service.MarkForgeEventsRead(pf.RepoKey(), req.ItemKey); err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	// Report the remaining count so the client can settle its badge without a
	// second round trip.
	n, err := service.CountUnreadForgeEvents(pf.RepoKey())
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{jsonCount: n})
}
