// Public conversation-share links (capability URLs).
//
// A session share records WHICH messages a link exposes (a resolved id list
// under a capability token) and nothing else; the JSON is assembled from the
// live tables on every public request. The management endpoint
// (/api/share/session) is auth-protected; the public read endpoints
// (/api/share/{token}/meta, /api/share/{token}/session) are deliberately NOT
// behind middleware.Auth — the token itself is the sole credential. When no
// share record exists the public endpoints return 404, so the feature has zero
// exposure when unused.
//
// See internal/service/session_share_payload.go for why the assembly inlines
// tool input/output and thinking text rather than referencing the side tables.
package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"strings"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// sessionShareMaxConcurrent bounds simultaneous payload assemblies.
//
// An assembly is several queries plus up to 16 MiB of JSON, and the read pool is
// only two connections (store/db.go). The public endpoint is unauthenticated and
// unthrottled, so one hot link pasted into a group chat would otherwise starve
// every other reader in the process.
var sessionShareMaxConcurrent = min(4, runtime.NumCPU())

var sessionShareSem = make(chan struct{}, sessionShareMaxConcurrent)

// sessionShareQueueTimeout bounds how long a request may WAIT for a slot before
// giving up with 503. Queueing without a bound is not a limiter: a hot link would
// pile up goroutines (each holding a request and its buffers) until the process
// runs out of memory, which is worse than refusing service.
//
// A var so tests can shrink it instead of sleeping for the production value.
var sessionShareQueueTimeout = 10 * time.Second

// acquireSessionShareSlot waits for a slot, the client to go away, or the queue
// timeout. Returns false with a reason the caller maps to a response.
func acquireSessionShareSlot(ctx context.Context) (ok bool, timedOut bool) {
	if sessionShareQueueTimeout <= 0 {
		// Synchronous fast path: never wait, so a saturated endpoint answers
		// immediately instead of holding the connection.
		select {
		case sessionShareSem <- struct{}{}:
			return true, false
		case <-ctx.Done():
			return false, false
		default:
			return false, true
		}
	}

	timer := time.NewTimer(sessionShareQueueTimeout)
	defer timer.Stop()
	select {
	case sessionShareSem <- struct{}{}:
		return true, false
	case <-ctx.Done():
		return false, false
	case <-timer.C:
		return false, true
	}
}

func releaseSessionShareSlot() { <-sessionShareSem }

// sessionShareResponse is the payload for session-share management endpoints.
type sessionShareResponse struct {
	Token string `json:"token,omitempty"`
	Path  string `json:"path,omitempty"` // the share URL path (/share/{token})
	// MessageCount is the number of messages in the stored SELECTION. Messages
	// deleted after the share was created still count here; the public page
	// renders only the survivors.
	MessageCount int `json:"messageCount,omitempty"`
}

// sessionShareStatusResponse is the GET payload: share state plus the message
// list the dialog renders. The list is returned even when no share exists yet,
// so opening the dialog needs a single request.
type sessionShareStatusResponse struct {
	Token        string `json:"token,omitempty"`
	Path         string `json:"path,omitempty"`
	MessageCount int    `json:"messageCount,omitempty"`
	// SelectedIDs is the frozen selection of the existing share. The dialog is
	// read-only once a link exists (the selection lives in the link), so it
	// needs these ids to show which messages the link actually carries instead
	// of offering a selection that would do nothing.
	SelectedIDs []int64                         `json:"selectedIds,omitempty"`
	Messages    []service.SessionMessagePreview `json:"messages"`
	// SessionAgent + Speakers let the dialog render the real speaker icon per
	// row. The dialog is self-contained (it fetches its own data for an
	// arbitrary session), so the roster must travel with this response rather
	// than being read from a current-session composable.
	SessionAgent *service.SpeakerIdentity           `json:"sessionAgent,omitempty"`
	Speakers     map[string]service.SpeakerIdentity `json:"speakers,omitempty"`
}

// sessionShareRequest is the shared request shape for POST and DELETE. GET
// passes the session id as a query param instead.
type sessionShareRequest struct {
	SessionID  string  `json:"sessionId"`
	MessageIDs []int64 `json:"messageIds"`
}

// decodeSessionShareRequest reads the JSON body (POST/DELETE) and falls back to
// the query string for a body-less request. The body is decoded exactly once:
// re-reading it after a first decode is not possible.
func decodeSessionShareRequest(w http.ResponseWriter, r *http.Request) (sessionShareRequest, bool) {
	var req sessionShareRequest
	if r.ContentLength != 0 {
		if !decodeJSON(w, r, &req) {
			return req, false
		}
	}
	if req.SessionID == "" {
		req.SessionID = r.URL.Query().Get("session_id")
	}
	if req.SessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return req, false
	}
	// A body-less request may still carry an explicit selection (useful for
	// scripted calls); the dialog always uses the body.
	if len(req.MessageIDs) == 0 {
		req.MessageIDs = parseMessageIDsQuery(r)
	}
	return req, true
}

// parseMessageIDsQuery reads a comma-separated `message_ids` query param.
// Unparseable entries are skipped; an empty result means "all messages".
func parseMessageIDsQuery(r *http.Request) []int64 {
	raw := r.URL.Query().Get("message_ids")
	if raw == "" {
		return nil
	}
	ids := make([]int64, 0)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var id int64
		if _, err := fmt.Sscanf(part, "%d", &id); err != nil || id <= 0 {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

// ServeSessionShareManage handles creating/regenerating (POST), querying (GET)
// and revoking (DELETE) a conversation share.
//
// Registered as an exact route (/api/share/session) so it is matched ahead of
// the public /api/share/ subtree. There is no ambiguity with a capability
// token: tokens are 32 hex characters, never the literal "session".
func ServeSessionShareManage(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		serveSessionShareCreate(w, r)
	case http.MethodGet:
		serveSessionShareStatus(w, r)
	case http.MethodDelete:
		serveSessionShareRevoke(w, r)
	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

// requireOwnedSession confirms a session exists and belongs to the project in
// the request cookie.
//
// A share must not be creatable for another project's conversation. Archived
// sessions are rejected because GetSessionFullInfo excludes them, matching the
// rest of the app (archived conversations are not addressable by id) — the
// PUBLIC read path is the exception, and it uses GetSessionFullInfoAny instead.
func requireOwnedSession(w http.ResponseWriter, r *http.Request, sessionID string) bool {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return false
	}
	info := service.GetSessionFullInfo(sessionID)
	if info == nil {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
		return false
	}
	if info.ProjectPath != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return false
	}
	return true
}

func serveSessionShareStatus(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return
	}
	if !requireOwnedSession(w, r, sessionID) {
		return
	}

	messages, err := service.GetSessionMessagesForSelection(sessionID)
	if err != nil {
		slog.Error("session share: list messages failed", "session", sessionID, "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}

	// The dialog is authenticated (owner-only), so the real avatars are fine
	// here — the share payload needs them too for the anonymous viewer.
	sessionAgent, speakers := service.ResolveSessionSpeakers(sessionID)
	resp := sessionShareStatusResponse{Messages: messages, SessionAgent: sessionAgent, Speakers: speakers}
	token, exists, err := service.GetSessionShareBySession(sessionID)
	if err != nil {
		slog.Error("session share: status lookup failed", "session", sessionID, "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	if exists {
		resp.Token = token
		resp.Path = sharePathFromToken(token)
		if share, found, err := service.GetSessionShareByToken(token); err == nil && found {
			resp.MessageCount = share.MessageCount
			resp.SelectedIDs = share.MessageIDs
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func serveSessionShareCreate(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeSessionShareRequest(w, r)
	if !ok {
		return
	}
	ok = requireOwnedSession(w, r, req.SessionID)
	if !ok {
		return
	}

	// Only the SELECTION is resolved and stored — the payload is assembled on
	// every public read, so creating a share never builds (or trims) 16 MiB of
	// JSON just to record which messages the user picked.
	ids, err := service.ResolveSessionShareSelection(req.SessionID, req.MessageIDs)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrSessionShareEmptySelection):
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "ShareSessionNoContent")
		case errors.Is(err, service.ErrSessionShareUnknownMessage):
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "ShareSessionInvalidSelection")
		default:
			slog.Error("session share: resolve selection failed", "session", req.SessionID, "err", err)
			model.WriteError(w, model.Internal(err))
		}
		return
	}

	token, _, err := service.UpsertSessionShare(req.SessionID, ids)
	if err != nil {
		slog.Error("session share: upsert failed", "session", req.SessionID, "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}

	writeJSON(w, http.StatusOK, sessionShareResponse{
		Token:        token,
		Path:         sharePathFromToken(token),
		MessageCount: len(ids),
	})
}

func serveSessionShareRevoke(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeSessionShareRequest(w, r)
	if !ok {
		return
	}
	if !requireOwnedSession(w, r, req.SessionID) {
		return
	}
	if err := service.DeleteSessionShareBySession(req.SessionID); err != nil {
		slog.Error("session share: revoke failed", "session", req.SessionID, "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// sessionShareListItem is one entry in the shared-conversations drawer.
//
// Field names mirror service.SessionShare so the frontend reads one shape.
type sessionShareListItem struct {
	Token        string `json:"token"`
	SessionID    string `json:"sessionId"`
	Title        string `json:"title"`
	Backend      string `json:"backend"`
	MessageCount int    `json:"messageCount"`
	CreatedAt    string `json:"createdAt"`
	Archived     bool   `json:"archived"`
}

// ServeSessionShareList lists this project's conversation shares (GET) or
// revokes one by token / all of them (DELETE).
//
// Mirrors ServeShareList for files, with two deliberate differences:
//
//   - The list is scoped to the project in the request cookie. A conversation
//     title is private content, so listing another project's shares would
//     disclose it. The file list is global because a path is already scoped by
//     the file manager's own navigation.
//   - An archived session's share IS listed. Archiving keeps the share alive
//     (the snapshot is a frozen copy) and makes the session unreachable by id,
//     so without this the link would be revocable only from the database. The
//     archived flag lets the UI drop the "open conversation" action for it.
func ServeSessionShareList(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		serveSessionShareList(w, r)
	case http.MethodDelete:
		serveSessionShareRevokeByToken(w, r)
	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

func serveSessionShareList(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	shares, err := service.ListSessionShares(projectPath)
	if err != nil {
		slog.Error("session share: list failed", "project", projectPath, "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}

	items := make([]sessionShareListItem, 0, len(shares))
	for _, s := range shares {
		items = append(items, sessionShareListItem{
			Token:        s.Token,
			SessionID:    s.SessionID,
			Title:        s.Title,
			Backend:      s.Backend,
			MessageCount: s.MessageCount,
			CreatedAt:    formatShareTime(s.CreatedAt),
			Archived:     s.Archived,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"shares": items})
}

// serveSessionShareRevokeByToken revokes one share by token, or every share of
// the project when all=1.
//
// Deleting by token (rather than by session id) is what keeps an archived
// session's share revocable: the session id is not addressable, but the token
// is right there in the list.
func serveSessionShareRevokeByToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
		All   bool   `json:"all"`
	}
	if r.ContentLength != 0 {
		if !decodeJSON(w, r, &req) {
			return
		}
	}
	if req.Token == "" {
		req.Token = r.URL.Query().Get("token")
	}
	req.All = req.All || r.URL.Query().Get("all") == "1"

	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	switch {
	case req.All:
		serveSessionShareRevokeAll(w, projectPath)
	case req.Token != "":
		serveSessionShareRevokeOne(w, r, projectPath, req.Token)
	default:
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MissingToken")
	}
}

func serveSessionShareRevokeOne(w http.ResponseWriter, r *http.Request, projectPath, token string) {
	// Confirm the token belongs to this project before touching it. An unknown
	// token and another project's token get the SAME 404, so neither case
	// discloses whether the other exists.
	owner, found, err := service.GetSessionShareProjectByToken(token)
	if err != nil {
		slog.Error("session share: ownership lookup failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	if !found || owner != projectPath {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
		return
	}

	if err := service.DeleteSessionShareByToken(token); err != nil {
		slog.Error("session share: revoke-by-token failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// serveSessionShareRevokeAll revokes every share of THIS project.
//
// Deliberately a per-token loop rather than one DELETE: the statement must be
// scoped by project, and a blanket delete would revoke other projects' links
// too.
func serveSessionShareRevokeAll(w http.ResponseWriter, projectPath string) {
	shares, err := service.ListSessionShares(projectPath)
	if err != nil {
		slog.Error("session share: list for clear-all failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	for _, s := range shares {
		if err := service.DeleteSessionShareByToken(s.Token); err != nil {
			slog.Error("session share: clear-all revoke failed", "err", err)
			model.WriteError(w, model.Internal(err))
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ─── Public token-scoped endpoints ───────────────────────────────────────────

// shareMetaResponse tells the share SPA which kind of share a token addresses.
type shareMetaResponse struct {
	Kind         string `json:"kind"` // shareKindFile | shareKindSession
	Title        string `json:"title,omitempty"`
	MessageCount int    `json:"messageCount,omitempty"`
}

// serveShareMeta reports whether a token is a file share or a session share.
//
// The SPA cannot tell from the URL, and probing /session then /file would mean
// two requests plus a misleading 404 in the console for every file share. An
// explicit meta endpoint keeps the dispatch to one request.
//
// Unknown tokens get the same uniform 404 as every other public endpoint (no
// existence disclosure).
func serveShareMeta(w http.ResponseWriter, r *http.Request, token string) {
	share, ok, err := service.GetSessionShareByToken(token)
	if err != nil {
		slog.Error("share: meta session lookup failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	if ok {
		writeJSON(w, http.StatusOK, shareMetaResponse{
			Kind:         shareKindSession,
			Title:        share.Title,
			MessageCount: share.MessageCount,
		})
		return
	}

	_, name, _, fileOK, err := service.GetFileShareByToken(token)
	if err != nil {
		slog.Error("share: meta file lookup failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	if fileOK {
		writeJSON(w, http.StatusOK, shareMetaResponse{Kind: shareKindFile, Title: name})
		return
	}
	http.NotFound(w, r)
}

// serveShareSessionPayload assembles and writes the conversation JSON.
//
// Assembly happens on every request (nothing is stored but the selection), so
// the response carries a strong ETag derived from the body: a reader that comes
// back with If-None-Match gets a 304 and no re-transfer. The assembly itself is
// still paid — a cheap fingerprint that skipped it could not be made correct
// (summaries land asynchronously, tool outputs are updated in place, and avatars
// come from an in-process agent registry, none of which a SQL-only fingerprint
// observes), and a stale 304 is a silent, permanent content bug.
//
// Revocation is checked BEFORE the conditional request: a revoked token must
// 404 even when the caller still holds a valid ETag.
func serveShareSessionPayload(w http.ResponseWriter, r *http.Request, token string) {
	share, ok, err := service.GetSessionShareByToken(token)
	if err != nil {
		slog.Error("share: public session lookup failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}

	acquired, timedOut := acquireSessionShareSlot(r.Context())
	if !acquired {
		if timedOut {
			// Saturated: refuse rather than queue without bound. Retry-After tells
			// a well-behaved client how long to wait; an assembly is bounded by
			// the same window.
			w.Header().Set("Retry-After", "5")
			writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "ServiceBusy")
			return
		}
		// The client went away while queued; nothing to write.
		return
	}
	defer releaseSessionShareSlot()

	// createdAt goes into the payload verbatim, so an unparseable value must not
	// be papered over silently. Falling back to time.Now() would make the payload
	// differ on every request and defeat the ETag (no 304 ever), so a corrupt row
	// keeps the deterministic zero time and says so in the log — the reader sees
	// an odd date, but the caching contract still holds.
	createdAt, ok := parseShareTime(share.CreatedAt)
	if !ok {
		slog.Warn("share: unparseable created_at, payload will carry the zero time",
			"session", share.SessionID, "raw", share.CreatedAt)
	}
	body, _, err := service.BuildSessionSharePayload(share.SessionID, share.MessageIDs, createdAt)
	switch {
	case errors.Is(err, service.ErrSessionShareNothingToShow),
		errors.Is(err, service.ErrSessionShareSessionGone):
		// No content left (every selected message was deleted, or the session row
		// is gone). From the reader's side this is the same as revocation.
		http.NotFound(w, r)
		return
	case err != nil:
		slog.Error("share: assemble session payload failed", "session", share.SessionID, "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}

	etag := bodyETag([]byte(body))
	w.Header().Set("ETag", etag)
	// Overrides the no-store middleware.NoCache already set on this route: no-store
	// and 304 are contradictory (the browser would never send the conditional
	// request). private keeps a revoked link out of shared caches; no-cache still
	// makes the browser revalidate on every use.
	w.Header().Set("Cache-Control", "private, no-cache, max-age=0")

	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	// gosec flags this as tainted HTML output, but the body is the output of
	// json.Marshal (see BuildSessionSharePayload), which escapes <, > and & to
	// \u003c/\u003e/\u0026 — it cannot contain a raw tag.
	_, _ = w.Write([]byte(body))
}
