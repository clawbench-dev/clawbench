// Public conversation-share links (capability URLs).
//
// A session share freezes a conversation into an opaque JSON snapshot at
// creation time and stores it under a capability token. The management endpoint
// (/api/share/session) is auth-protected; the public read endpoints
// (/api/share/{token}/meta, /api/share/{token}/session) are deliberately NOT
// behind middleware.Auth — the token itself is the sole credential. When no
// share record exists the public endpoints return 404, so the feature has zero
// exposure when unused.
//
// See internal/service/session_share_payload.go for why the snapshot inlines
// tool input/output and thinking text rather than referencing the side tables.
package handler

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// sessionShareResponse is the payload for session-share management endpoints.
type sessionShareResponse struct {
	Token string `json:"token,omitempty"`
	Path  string `json:"path,omitempty"` // the share URL path (/share/{token})
	// MessageCount is the number of messages frozen into the current snapshot.
	MessageCount int `json:"messageCount,omitempty"`
}

// sessionShareStatusResponse is the GET payload: share state plus the message
// list the dialog renders. The list is returned even when no share exists yet,
// so opening the dialog needs a single request.
type sessionShareStatusResponse struct {
	Token        string                          `json:"token,omitempty"`
	Path         string                          `json:"path,omitempty"`
	MessageCount int                             `json:"messageCount,omitempty"`
	Messages     []service.SessionMessagePreview `json:"messages"`
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
// the request cookie, returning its metadata and the project path.
//
// A share must not be creatable for another project's conversation, and the
// snapshot's path relativization needs the creator's project root. Archived
// sessions are rejected because GetSessionFullInfo excludes them, matching the
// rest of the app (archived conversations are not addressable by id).
func requireOwnedSession(w http.ResponseWriter, r *http.Request, sessionID string) (*service.SessionInfo, string, bool) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return nil, "", false
	}
	info := service.GetSessionFullInfo(sessionID)
	if info == nil {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
		return nil, "", false
	}
	if info.ProjectPath != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return nil, "", false
	}
	return info, projectPath, true
}

func serveSessionShareStatus(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return
	}
	if _, _, ok := requireOwnedSession(w, r, sessionID); !ok {
		return
	}

	messages, err := service.GetSessionMessagesForSelection(sessionID)
	if err != nil {
		slog.Error("session share: list messages failed", "session", sessionID, "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}

	resp := sessionShareStatusResponse{Messages: messages}
	token, exists, err := service.GetSessionShareBySession(sessionID)
	if err != nil {
		slog.Error("session share: status lookup failed", "session", sessionID, "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	if exists {
		resp.Token = token
		resp.Path = sharePathFromToken(token)
		if _, _, count, found, err := service.GetSessionShareByToken(token); err == nil && found {
			resp.MessageCount = count
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func serveSessionShareCreate(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeSessionShareRequest(w, r)
	if !ok {
		return
	}
	info, projectPath, ok := requireOwnedSession(w, r, req.SessionID)
	if !ok {
		return
	}

	homeDir, _ := os.UserHomeDir()

	payload, count, err := service.BuildSessionSharePayload(req.SessionID, req.MessageIDs, projectPath, homeDir)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrSessionShareEmptySelection):
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "ShareSessionNoContent")
		case errors.Is(err, service.ErrSessionShareUnknownMessage):
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "ShareSessionInvalidSelection")
		default:
			slog.Error("session share: build payload failed", "session", req.SessionID, "err", err)
			model.WriteError(w, model.Internal(err))
		}
		return
	}

	token, _, err := service.UpsertSessionShare(req.SessionID, info.Title, info.Backend, count, payload)
	if err != nil {
		slog.Error("session share: upsert failed", "session", req.SessionID, "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}

	writeJSON(w, http.StatusOK, sessionShareResponse{
		Token:        token,
		Path:         sharePathFromToken(token),
		MessageCount: count,
	})
}

func serveSessionShareRevoke(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeSessionShareRequest(w, r)
	if !ok {
		return
	}
	if _, _, ok := requireOwnedSession(w, r, req.SessionID); !ok {
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
			CreatedAt:    s.CreatedAt,
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
// Deliberately not service.DeleteAllSessionShares(): that statement is global
// and would revoke other projects' links too.
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
	_, title, count, ok, err := service.GetSessionShareByToken(token)
	if err != nil {
		slog.Error("share: meta session lookup failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	if ok {
		writeJSON(w, http.StatusOK, shareMetaResponse{Kind: shareKindSession, Title: title, MessageCount: count})
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

// serveShareSessionPayload writes the frozen snapshot verbatim.
//
// The stored payload is already JSON, so it is streamed as-is rather than
// unmarshaled and re-marshaled: re-encoding would double the memory spike on
// an anonymous request and could reorder keys for no benefit.
func serveShareSessionPayload(w http.ResponseWriter, r *http.Request, token string) {
	payload, _, _, ok, err := service.GetSessionShareByToken(token)
	if err != nil {
		slog.Error("share: public session lookup failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	// gosec flags this as tainted HTML output, but the payload is the output of
	// json.Marshal (see BuildSessionSharePayload), which escapes <, > and & to
	// \u003c/\u003e/\u0026 — the stored snapshot cannot contain a raw tag.
	_, _ = w.Write([]byte(payload)) //nolint:gosec // stored payload is json.Marshal output, HTML-escaped by encoding/json
}
