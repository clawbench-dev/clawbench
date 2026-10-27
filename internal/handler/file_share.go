// Public file-share links (capability URLs).
//
// A share row maps an opaque, unguessable token to an absolute file path.
// Management endpoints (/api/share) are auth-protected; the public endpoints
// (/share/{token} page + /api/share/{token}/... data) are deliberately NOT
// behind middleware.Auth — the token itself is the sole credential. When no
// share record exists, the public endpoints return 404, so the feature has
// zero exposure when unused.
package handler

import (
	"encoding/base64"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"clawbench/internal/frontend"
	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/service"
)

// shareDocSegment is the URL path segment that serves a shared HTML document
// (and everything it references) by PATH rather than by query string.
//
// Why a separate segment: an iframe loading a document needs the document's own
// URL to be the base for its relative references (`css/x.css`, `img/y.png`).
// The query-string forms (`/local?path=…`, `/local/<rel>`) either lose the
// directory (a query is not part of the base path) or cannot express a path
// outside the shared file's own directory. `/doc/<rel>` keeps the full
// directory in the PATH, so the browser derives the right base for free —
// which is what makes a shared landing page render with its real stylesheet.
const shareDocSegment = "doc"

// shareLocalSegment is the URL path segment that addresses a file relative to
// the shared file's directory (see serveShareLocal). Named so the parser and
// the resolver cannot drift apart.
const shareLocalSegment = "local"

// shareSessionSegment is the URL path segment that addresses a conversation
// share payload (see serveShareSessionPayload).
const shareSessionSegment = "session"

// shareMetaSegment is the URL path segment that reports whether a token is a
// file share or a conversation share (see serveShareMeta). Shared by both kinds.
const shareMetaSegment = "meta"

// shareKindFile and shareKindSession are the `kind` values the share meta
// endpoint reports, telling the share SPA which renderer to use. Kept distinct
// from entryTypeFile, which classifies a directory-search entry rather than a
// share.
const (
	shareKindFile    = "file"
	shareKindSession = "session"
)

// shareResponse is the payload for share management endpoints.
type shareResponse struct {
	Token string `json:"token,omitempty"`
	Path  string `json:"path,omitempty"` // the share URL path (/share/{token})
}

// sharePathFromToken returns the share URL path for a token.
func sharePathFromToken(token string) string {
	return "/share/" + token
}

// ServeShareManage handles creating/regenerating (POST), querying (GET) and
// revoking (DELETE) a file share. The target path comes from the `path` query
// param or (POST) JSON body. Project-relative paths resolve against the
// project cookie; absolute paths are validated against the root paths.
func ServeShareManage(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		serveShareCreate(w, r)
	case http.MethodGet:
		serveShareStatus(w, r)
	case http.MethodDelete:
		serveShareRevoke(w, r)
	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

// shareRequestPath extracts the target path from the JSON body or query string.
func shareRequestPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req struct {
		Path string `json:"path"`
	}
	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		if !decodeJSON(w, r, &req) {
			return "", false
		}
	}
	if req.Path == "" {
		req.Path = r.URL.Query().Get("path")
	}
	if req.Path == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MissingPath")
		return "", false
	}
	return req.Path, true
}

// resolveShareTarget validates the requested path and confirms it is a file
// (directories cannot be shared). Returns the absolute path.
func resolveShareTarget(w http.ResponseWriter, r *http.Request, pathStr string) (string, bool) {
	absPath, ok := resolveAbsPath(w, r, pathStr)
	if !ok {
		return "", false
	}
	info, err := os.Stat(absPath)
	if err != nil {
		writeLocalizedError(w, r, model.NotFound(nil, "FileNotFoundShort"))
		return "", false
	}
	if info.IsDir() {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "NotAFile")
		return "", false
	}
	return absPath, true
}

// resolveShareRoot determines the directory a new share is confined to.
//
// The public read endpoints run WITHOUT auth, so they cannot consult the
// project cookie — the boundary must be captured here, while the request is
// still authenticated.
//
// A shared file inside the active project gets the project root as its
// boundary, which is what the share SPA needs: the markdown renderer rewrites
// media references to ?path=<abs> using the document's own directory, and a
// reference like ../images/pic.jpg legitimately resolves above that directory
// but still inside the project.
//
// Only the project cookie is consulted. service.GetDefaultProject() must NOT
// be used as a fallback here: its own chain ends at the home directory and then
// at RootPaths[0] (== "/" on Unix), and production projects live *under* the
// home directory, so isPathUnderBase would accept it and confine the share to
// $HOME — handing a token holder ~/.ssh, config files, and everything else.
// With no cookie we therefore fall back to the shared file's own directory,
// the only choice that cannot widen what the link exposes.
func resolveShareRoot(r *http.Request, absPath string) string {
	fileDir := filepath.Dir(absPath)

	projectPath := middleware.GetProjectFromCookie(r)
	if projectPath == "" {
		return fileDir
	}
	if !isPathUnderBase(absPath, projectPath) {
		return fileDir
	}
	// Store the path as given: readers compare with isPathUnderBase, which
	// resolves symlinks on BOTH sides, so a symlinked component in the project
	// path is handled at read time and needs no pre-resolution here.
	return projectPath
}

func serveShareCreate(w http.ResponseWriter, r *http.Request) {
	pathStr, ok := shareRequestPath(w, r)
	if !ok {
		return
	}
	absPath, ok := resolveShareTarget(w, r, pathStr)
	if !ok {
		return
	}
	name := filepath.Base(absPath)

	// The project cookie scopes the management list, so capture it at creation
	// alongside the confinement root. Empty (no cookie) parks the share on the 0
	// sentinel, which no project's list matches — the fail-closed direction: it
	// stays reachable by its own link but is not disclosed in any drawer.
	token, _, err := service.UpsertFileShare(absPath, name, resolveShareRoot(r, absPath), middleware.GetProjectFromCookie(r))
	if err != nil {
		slog.Error("share: upsert failed", "path", absPath, "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}

	writeJSON(w, http.StatusOK, shareResponse{
		Token: token,
		Path:  sharePathFromToken(token),
	})
}

func serveShareStatus(w http.ResponseWriter, r *http.Request) {
	pathStr, ok := shareRequestPath(w, r)
	if !ok {
		return
	}
	absPath, ok := resolveShareTarget(w, r, pathStr)
	if !ok {
		return
	}

	token, _, exists, err := service.GetFileShareByPath(absPath)
	if err != nil {
		slog.Error("share: status lookup failed", "path", absPath, "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	if !exists {
		writeJSON(w, http.StatusOK, shareResponse{})
		return
	}
	writeJSON(w, http.StatusOK, shareResponse{
		Token: token,
		Path:  sharePathFromToken(token),
	})
}

func serveShareRevoke(w http.ResponseWriter, r *http.Request) {
	pathStr, ok := shareRequestPath(w, r)
	if !ok {
		return
	}
	absPath, ok := resolveShareTarget(w, r, pathStr)
	if !ok {
		return
	}
	if err := service.DeleteFileShareByPath(absPath); err != nil {
		slog.Error("share: revoke failed", "path", absPath, "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// shareListItem is one entry in the shared-files drawer.
type shareListItem struct {
	Token     string `json:"token"`
	Name      string `json:"name"`
	Path      string `json:"path"` // display path (project-relative when under project, else absolute)
	CreatedAt string `json:"createdAt"`
	Exists    bool   `json:"exists"`
}

// ServeShareList lists all active shares (GET) or revokes one by token (DELETE).
// DELETE does not require the shared file to still exist, so stale shares for
// files deleted outside the app can still be revoked here.
func ServeShareList(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		serveShareList(w, r)
	case http.MethodDelete:
		serveShareRevokeByToken(w, r)
	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

func serveShareList(w http.ResponseWriter, r *http.Request) {
	// Project-scoped, like the conversation-share list: without the cookie there
	// is no project to scope to, and an unscoped list would disclose every
	// project's shared paths. An empty cookie therefore resolves to the 0
	// sentinel and matches only unattributable shares.
	projectPath := middleware.GetProjectFromCookie(r)

	shares, err := service.ListFileShares(projectPath)
	if err != nil {
		slog.Error("share: list failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}

	projectAbs, _ := filepath.Abs(projectPath)

	items := make([]shareListItem, 0, len(shares))
	for _, s := range shares {
		_, statErr := os.Stat(s.Path)
		exists := statErr == nil

		display := s.Path
		if projectAbs != "" {
			if rel, err := filepath.Rel(projectAbs, s.Path); err == nil &&
				rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				display = filepath.ToSlash(rel)
			}
		}

		items = append(items, shareListItem{
			Token:     s.Token,
			Name:      s.Name,
			Path:      display,
			CreatedAt: formatShareTime(s.CreatedAt),
			Exists:    exists,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"shares": items})
}

func serveShareRevokeByToken(w http.ResponseWriter, r *http.Request) {
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

	projectPath := middleware.GetProjectFromCookie(r)

	switch {
	case req.All:
		// One-click clear: revoke every share link of THIS project only.
		if err := service.DeleteAllFileShares(projectPath); err != nil {
			slog.Error("share: delete-all failed", "err", err)
			model.WriteError(w, model.Internal(err))
			return
		}
	case req.Token != "":
		serveShareRevokeOneByToken(w, r, projectPath, req.Token)
		return
	default:
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MissingToken")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// serveShareRevokeOneByToken revokes a single share after confirming it belongs
// to the caller's project.
//
// The ownership check matters even though the token is unguessable: without it a
// caller could revoke another project's link, and the two failure modes must be
// indistinguishable. An unknown token and a foreign token therefore get the SAME
// 404, so neither discloses whether the other exists.
func serveShareRevokeOneByToken(w http.ResponseWriter, r *http.Request, projectPath, token string) {
	owner, found, err := service.GetFileShareProjectByToken(token)
	if err != nil {
		slog.Error("share: ownership lookup failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	if !found || owner != projectPath {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "ShareNotFound")
		return
	}
	if err := service.DeleteFileShareByToken(token); err != nil {
		slog.Error("share: revoke-by-token failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ─── Public token-scoped endpoints ───────────────────────────────────────────

// parseSharePublicPath parses /api/share/{token}/... request paths.
// Returns the token and the remaining sub-path after the token.
func parseSharePublicPath(urlPath string) (token, rest string, ok bool) {
	const prefix = "/api/share/"
	if !strings.HasPrefix(urlPath, prefix) {
		return "", "", false
	}
	rest = strings.TrimPrefix(urlPath, prefix)
	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		return rest, "", true
	}
	return rest[:slash], rest[slash+1:], true
}

// confineSharePath resolves a non-empty ?path= value to an absolute path and
// checks it lies inside the share's scope. Returns ok=false when the path is
// relative (never a valid target here) or escapes the root.
//
// shareRoot is the boundary captured when the share was created (see
// resolveShareRoot); an empty value (a row predating the column) falls back to
// the shared file's own directory, the fail-closed direction. Callers write the
// uniform 404 themselves so the rejection cannot be told apart from "missing".
func confineSharePath(queryPath, sharedAbsPath, shareRoot string) (string, bool) {
	if !strings.HasPrefix(queryPath, "/") && !filepath.IsAbs(queryPath) {
		return "", false
	}
	root := shareRoot
	if root == "" {
		root = filepath.Dir(sharedAbsPath)
	}
	// filepath.Abs only errors for a drive-relative path whose working
	// directory is gone (Windows); treat that exactly like an out-of-scope
	// target so there is a single rejection path.
	absTarget, err := filepath.Abs(queryPath)
	if err != nil || !isPathUnderBase(absTarget, root) {
		return "", false
	}
	return absTarget, true
}

// resolveShareScopePath returns the file a token-scoped endpoint should read:
// the shared file itself when no ?path= is given, otherwise the absolute target
// confined to the share's scope.
//
// On rejection it writes the uniform 404 and returns ok=false. Callers must not
// distinguish "outside scope" from "missing": these endpoints are public, so the
// response must not reveal anything about paths the token does not cover.
func resolveShareScopePath(w http.ResponseWriter, r *http.Request, sharedAbsPath, shareRoot string) (string, bool) {
	queryPath := r.URL.Query().Get("path")
	if queryPath == "" {
		return sharedAbsPath, true
	}
	absTarget, ok := confineSharePath(queryPath, sharedAbsPath, shareRoot)
	if !ok {
		http.NotFound(w, r)
		return "", false
	}
	return absTarget, true
}

// serveShareSessionSubpath dispatches the conversation-share subpaths (meta,
// session), returning true when it handled the request.
//
// Split out of ServeSharePublic so the file-share routing stays readable: the
// conversation branches must run before the file_shares lookup (a session token
// has no file_shares row), but they are otherwise independent of it.
func serveShareSessionSubpath(w http.ResponseWriter, r *http.Request, token, rest string) bool {
	switch rest {
	case shareMetaSegment:
		serveShareMeta(w, r, token)
		return true
	case shareSessionSegment:
		serveShareSessionPayload(w, r, token)
		return true
	default:
		return false
	}
}

// ServeSharePublic serves the unauthenticated token-scoped data endpoints:
//   - GET /api/share/{token}/file        → FileContent JSON (shared file)
//   - GET /api/share/{token}/content?path= → FileContent JSON for a file the
//     shared document references (same JSON shape as /file, so the share SPA
//     can render it in place); directories are refused (no listing endpoint)
//   - GET /api/share/{token}/download    → raw bytes with Content-Disposition
//     (?path= downloads a referenced file instead of the shared one)
//   - GET /api/share/{token}/local/{rel} → raw bytes resolved against the shared
//     file's directory (or ?path= absolute)
func ServeSharePublic(w http.ResponseWriter, r *http.Request) {
	// HEAD is allowed alongside GET: it is how clients (and agents) probe a URL
	// before transferring it, and every branch below writes its body through
	// helpers that net/http already suppresses for HEAD. Rejecting it turned a
	// harmless size probe into a 405 and made share URLs look broken.
	if !requireMethod(w, r, http.MethodGet, http.MethodHead) {
		return
	}
	token, rest, ok := parseSharePublicPath(r.URL.Path)
	if !ok || token == "" {
		http.NotFound(w, r)
		return
	}

	// Conversation-share subpaths are resolved BEFORE the file-share lookup:
	// a session token has no file_shares row, so looking that up first would
	// 404 every session request before it could reach its own branch.
	if serveShareSessionSubpath(w, r, token, rest) {
		return
	}

	// The stored name is unused: /download derives its filename from the
	// resolved target so that ?path= downloads a referenced file under its own
	// name rather than the shared file's.
	absPath, _, shareRoot, exists, err := service.GetFileShareByToken(token)
	if err != nil {
		slog.Error("share: public token lookup failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	if !exists {
		// Unknown/revoked token → uniform 404 (do not reveal record existence).
		http.NotFound(w, r)
		return
	}

	serveShareFileRoute(w, r, rest, absPath, shareRoot)
}

// serveShareFileRoute dispatches a resolved file-share token to the endpoint
// named by the trailing path segment. Split out of ServeSharePublic to keep
// that function's branch count readable.
func serveShareFileRoute(w http.ResponseWriter, r *http.Request, rest, absPath, shareRoot string) {
	switch {
	case rest == "" || rest == entryTypeFile:
		serveShareFileContent(w, r, absPath)
	case rest == "content":
		// Same JSON payload as /file, but for a path the shared document
		// references (?path=). The share SPA renders it in place.
		target, ok := resolveShareScopePath(w, r, absPath, shareRoot)
		if !ok {
			return
		}
		serveShareFileContent(w, r, target)
	case rest == "download":
		target, ok := resolveShareScopePath(w, r, absPath, shareRoot)
		if !ok {
			return
		}
		serveShareRaw(w, r, target, filepath.Base(target), true)
	case rest == shareLocalSegment || strings.HasPrefix(rest, shareLocalSegment+"/"):
		serveShareLocal(w, r, absPath, shareRoot, rest)
	case rest == shareDocSegment || strings.HasPrefix(rest, shareDocSegment+"/"):
		serveShareDoc(w, r, absPath, shareRoot, rest)
	default:
		http.NotFound(w, r)
	}
}

// serveShareFileContent responds with the FileContent JSON for the shared file.
// Mirrors GetFile's reading logic but always uses the stored absolute path and
// reports it as the response path (the share view resolves media via its own
// token-scoped endpoints).
func serveShareFileContent(w http.ResponseWriter, r *http.Request, absPath string) {
	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		// Shared file deleted/moved since the link was created → 404.
		http.NotFound(w, r)
		return
	}

	// Over the inline cap the file is NOT rejected — every file is shareable and
	// downloadable regardless of size (the download/local endpoints stream and
	// have no cap). Only the inlined-content response degrades: the whole file
	// would otherwise be read into memory and JSON-escaped for an anonymous
	// caller. Respond 200 with no content so the share SPA falls back to its
	// download card instead of reporting a broken link. maxGetFileBytes is the
	// same "max bytes worth inlining into a FileContent response" bound the
	// authenticated GetFile endpoint uses.
	if info.Size() > maxGetFileBytes {
		writeJSON(w, http.StatusOK, FileContent{
			Name:      info.Name(),
			Path:      absPath,
			Supported: model.IsSupportedFile(info.Name()),
			Size:      info.Size(),
			TooLarge:  true,
		})
		return
	}

	isText := model.IsTextFile(info.Name())
	forceText := r.URL.Query().Get("forceText") == "1"

	isBinary := false
	if !isText && !forceText {
		binary, sniffErr := sniffBinaryContent(absPath)
		if sniffErr != nil {
			model.WriteError(w, model.Internal(sniffErr))
			return
		}
		isBinary = binary
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		model.WriteError(w, model.Internal(err))
		return
	}

	var truncated bool
	if !isText && !isBinary {
		content, truncated = sanitizeTextContent(content)
	}

	subtype := model.DetectSubtype(info.Name(), string(content))
	var specJSON string
	if subtype == model.SubtypeOpenAPI {
		lower := strings.ToLower(info.Name())
		if strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") {
			specJSON = model.ConvertSpecToJSON(string(content))
		}
	}

	linkTarget, isSymlink := resolveLinkTarget(absPath, "", true)

	writeJSON(w, http.StatusOK, FileContent{
		Content:    string(content),
		Name:       info.Name(),
		Path:       absPath,
		Supported:  model.IsSupportedFile(info.Name()),
		Size:       info.Size(),
		Truncated:  truncated,
		IsBinary:   isBinary,
		Subtype:    subtype,
		SpecJSON:   specJSON,
		LinkTarget: linkTarget,
		IsSymlink:  isSymlink,
	})
}

// serveShareRaw streams raw file bytes, optionally with a download disposition.
func serveShareRaw(w http.ResponseWriter, r *http.Request, absPath, name string, download bool) {
	info, err := os.Stat(absPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if info.IsDir() {
		http.NotFound(w, r)
		return
	}

	ext := strings.ToLower(filepath.Ext(absPath))
	mime := mimeTypes[ext]
	if mime == "" {
		mime = mimeOctetStream
	}

	if download {
		w.Header().Set("Content-Disposition", contentDispositionAttachment(name))
		w.Header().Set("Content-Type", mime)
		f, err := os.Open(absPath)
		if err != nil {
			model.WriteError(w, model.Internal(err))
			return
		}
		defer func() { _ = f.Close() }()
		http.ServeContent(w, r, sanitizeArchiveName(name), info.ModTime(), f)
		return
	}

	w.Header().Set("Content-Type", mime)
	http.ServeFile(w, r, absPath)
}

// serveShareDoc serves a shared HTML document and everything it references,
// addressed by PATH under /api/share/{token}/doc/{dirKey}/{rel}.
//
// This exists because the share SPA renders a shared .html in an iframe, and an
// iframe needs the document's own URL as the base for its relative references.
// The query-string forms cannot supply one: `/local?path=…` derives a base of
// `/local` (a query is not part of the base path), and `/local/<rel>` resolves
// only against the shared file's own directory. With either, a shared landing
// page whose stylesheet is `css/landing.css` resolved to `/share/css/landing.css`
// and rendered as unstyled black-on-white HTML.
//
// {dirKey} is the base64url encoding of the DOCUMENT'S DIRECTORY (absolute), and
// {rel} is the referenced file relative to it:
//
//	shared: <proj>/web/landing/index.html
//	GET /api/share/{token}/doc/<b64 of /…/web/landing>/index.html
//	  → <proj>/web/landing/index.html
//	GET /api/share/{token}/doc/<b64 of /…/web/landing>/css/landing.css
//	  → <proj>/web/landing/css/landing.css
//
// so the browser's base is `/api/share/{token}/doc/<dirKey>/` and the document's
// own `css/landing.css` resolves there directly.
//
// Why encode the directory instead of using its literal path: the base must be
// a SINGLE stable directory prefix for every document in the share, including
// one reached through a followed link in a sibling directory. A literal path
// cannot express "up and over" (`doc/../other/`) because the browser normalises
// the URL before sending it. An opaque key can point anywhere in scope.
//
// SECURITY — this is the one public endpoint that answers `text/html`, and it
// does so only under a `sandbox` CSP (see serveShareDocFile). The sandbox makes
// the document an OPAQUE ORIGIN: it cannot read cookies, cannot call the
// authenticated API with the reader's session, and cannot touch the parent
// document — the same guarantee the iframe's own `sandbox="allow-scripts"` gave
// when the document was inlined via srcdoc. Serving text/html WITHOUT that CSP
// would be a session-level XSS (verified in a browser before this design: the
// shared page reached authenticated APIs), which is why the generic
// `mimeTypes` table still must not contain `.html`.
func serveShareDoc(w http.ResponseWriter, r *http.Request, sharedAbsPath, shareRoot, rest string) {
	rel := strings.TrimPrefix(rest, shareDocSegment)
	rel = strings.TrimLeft(rel, "/")
	if rel == "" {
		// No directory key: nothing to serve. (The share SPA always supplies
		// one; a bare /doc is a hand-typed URL.)
		http.NotFound(w, r)
		return
	}

	slash := strings.IndexByte(rel, '/')
	if slash < 0 {
		http.NotFound(w, r)
		return
	}
	dirKey, filePath := rel[:slash], rel[slash+1:]
	docDir, ok := decodeShareDocDir(dirKey)
	if !ok {
		http.NotFound(w, r)
		return
	}
	// The key is opaque and caller-supplied, so it is only a HINT: the real
	// authority is the confinement check below. Without it, anyone holding a
	// token could read the whole filesystem by encoding "/etc".
	if !isPathUnderBase(docDir, shareScopeRoot(sharedAbsPath, shareRoot)) {
		http.NotFound(w, r)
		return
	}

	filePath = path.Clean(strings.TrimLeft(filePath, "/"))
	if filePath == "." || filePath == ".." || strings.HasPrefix(filePath, "../") || path.IsAbs(filePath) {
		http.NotFound(w, r)
		return
	}
	// ValidatePath resolves symlinks and refuses anything escaping docDir, so a
	// symlinked asset cannot be used to read outside the share's scope.
	absTarget, valid := model.ValidatePath(docDir, filePath)
	if !valid {
		http.NotFound(w, r)
		return
	}
	serveShareDocFile(w, r, absTarget)
}

// encodeShareDocDir is the inverse of decodeShareDocDir (tests build URLs with
// it). base64url with no padding: the key is a URL path segment, so `+`, `/`
// and `=` are all illegal or ambiguous there.
func encodeShareDocDir(dir string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(dir))
}

func decodeShareDocDir(key string) (string, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(key)
	if err != nil {
		return "", false
	}
	dir := string(raw)
	if dir == "" || !filepath.IsAbs(dir) {
		return "", false
	}
	return filepath.Clean(dir), true
}

// shareScopeRoot mirrors confineSharePath's root selection: the stored root, or
// the shared file's directory for rows predating that column (fail closed).
func shareScopeRoot(sharedAbsPath, shareRoot string) string {
	if shareRoot != "" {
		return shareRoot
	}
	return filepath.Dir(sharedAbsPath)
}

// serveShareDocFile streams one file for the /doc/ endpoint.
//
// Every response carries the sandbox CSP — not just HTML. A shared document can
// reference `.svg` and `.js` too, and an SVG is scriptable when navigated to
// directly; applying the sandbox uniformly means the grant cannot be forgotten
// for one content type. `allow-scripts` is required (documents legitimately run
// their own JS, e.g. the landing page's language switch); `allow-same-origin` is
// deliberately ABSENT, which is what keeps the document in an opaque origin.
func serveShareDocFile(w http.ResponseWriter, r *http.Request, absPath string) {
	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}

	ext := strings.ToLower(filepath.Ext(absPath))
	// The web-asset table (html/css/js/fonts) applies HERE and only here on the
	// public side; `mimeTypes` stays free of active types. Anything not in
	// either table falls back to octet-stream, which the browser will not
	// execute or style.
	contentType := mimeForLocalFile(ext)
	if contentType == mimeOctetStream && ext != "" {
		// A shared document referencing an unusual asset extension should still
		// get a usable type where the platform knows one (`.webmanifest`, `.map`,
		// `.wasm`…). Safe despite the sniffing surface: the CSP sandbox makes
		// active content inert regardless of the declared type.
		if sniffed := mime.TypeByExtension(ext); sniffed != "" {
			contentType = sniffed
		}
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Security-Policy", shareDocSandboxCSP)
	// The token is in the URL, so a Referer would hand it to every third-party
	// asset the document embeds — the same reason the share PAGE sets it.
	w.Header().Set("Referrer-Policy", "no-referrer")
	// Never let a browser re-sniff the body into something executable.
	w.Header().Set("X-Content-Type-Options", "nosniff")

	// ServeContent, not ServeFile: ServeFile treats a file named index.html as a
	// directory index and answers 301 to "./", which would rewrite the URL and
	// destroy the base this endpoint exists to provide.
	f, err := os.Open(absPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

// shareDocSandboxCSP keeps a shared document in an opaque origin while letting
// it run its own scripts.
//
// `sandbox allow-scripts` (no `allow-same-origin`) is the load-bearing part: the
// document gets a unique opaque origin, so it cannot read the reader's cookies,
// cannot call the authenticated API as them, and cannot reach the parent page.
// The remaining directives only restrict where it may load assets from — they
// are deliberately permissive about `https:`/`data:` because a shared document
// legitimately embeds third-party images, and restrictive about framing and
// plugins.
const shareDocSandboxCSP = "sandbox allow-scripts; " +
	"default-src 'self' 'unsafe-inline' 'unsafe-eval' data: blob: https: http:; " +
	"frame-ancestors 'self'; object-src 'none'"

// serveShareLocal serves a file referenced by the shared document (markdown
// images etc.). Relative paths resolve against the shared file's directory;
// absolute paths are accepted via ?path= and confined to the share's scope
// (see confineSharePath).
func serveShareLocal(w http.ResponseWriter, r *http.Request, sharedAbsPath, shareRoot, rest string) {
	// Absolute path via ?path= — referenced outside the shared file's dir.
	if queryPath := r.URL.Query().Get("path"); queryPath != "" {
		absTarget, ok := confineSharePath(queryPath, sharedAbsPath, shareRoot)
		if !ok {
			http.NotFound(w, r)
			return
		}
		serveShareRaw(w, r, absTarget, filepath.Base(absTarget), false)
		return
	}

	// Relative path from /local/{rel...}: resolve against the shared file's dir.
	rel := strings.TrimPrefix(rest, shareLocalSegment)
	rel = strings.TrimLeft(rel, "/")
	if rel == "" {
		// Bare /local without a resource — serve the shared file itself.
		info, err := os.Stat(sharedAbsPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		serveShareRaw(w, r, sharedAbsPath, info.Name(), false)
		return
	}
	rel = path.Clean(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") || path.IsAbs(rel) {
		http.NotFound(w, r)
		return
	}

	baseDir := filepath.Dir(sharedAbsPath)
	absTarget, valid := model.ValidatePath(baseDir, rel)
	if !valid {
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(absTarget)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	serveShareRaw(w, r, absTarget, info.Name(), false)
}

// ServeSharePage serves the public read-only share SPA for /share/{token}.
// The token lives in the URL path; the SPA reads it and fetches data via
// /api/share/{token}/... .
func ServeSharePage(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet, http.MethodHead) {
		return
	}
	// The token in this URL IS the credential, and the page renders
	// author-supplied content that routinely references third-party assets (an
	// agent writing `![](https://cdn.example/a.png)` is enough). Under the
	// browser default the Referer of those subresource requests carries the full
	// share URL, so merely OPENING a shared conversation would hand the token to
	// every third-party host it embeds — no reader action required.
	//
	// no-referrer (not strict-origin-when-cross-origin) because same-site
	// navigations still send the full URL under the default policy.
	w.Header().Set("Referrer-Policy", "no-referrer")
	serveShareHTML(w, r, shareTokenFromPagePath(r.URL.Path))
}

// shareTokenFromPagePath extracts the token from /share/{token}.
//
// Returns "" for the bare /share/ path and for anything with extra segments —
// those are not share pages, and the shell is served unmodified rather than
// pointing at a URL that cannot exist.
func shareTokenFromPagePath(urlPath string) string {
	const prefix = "/share/"
	if !strings.HasPrefix(urlPath, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(urlPath, prefix)
	if rest == "" || strings.ContainsRune(rest, '/') {
		return ""
	}
	return rest
}

// shareDiscoveryFor looks up what to advertise for a token.
//
// A token that resolves to neither kind returns ok=false: the SPA will show its
// own "not found" state, and injecting pointers to a 404 endpoint would be worse
// than injecting nothing. A lookup ERROR also yields ok=false — the page still
// renders for a browser, and discovery is a progressive enhancement, not part of
// the share contract.
func shareDiscoveryFor(token string) (shareDiscovery, bool) {
	if !validShareToken(token) {
		return shareDiscovery{}, false
	}
	// Conversation first, mirroring serveShareMeta: the two token spaces are
	// distinct, so the order only matters for clarity.
	if s, ok, err := service.GetSessionShareByToken(token); err == nil && ok {
		return shareDiscovery{
			Token:        token,
			Title:        s.Title,
			Kind:         shareKindSession,
			MessageCount: s.MessageCount,
		}, true
	}
	if _, name, _, ok, err := service.GetFileShareByToken(token); err == nil && ok {
		return shareDiscovery{Token: token, Title: name, Kind: shareKindFile}, true
	}
	return shareDiscovery{}, false
}

// serveShareHTML serves the share SPA entry (share.html) with the discovery
// block injected, preferring the disk build dir then falling back to embedded
// dist (mirrors ServeIndex).
//
// The shell is READ and rewritten rather than streamed: the injection is what
// makes the page legible to a non-browser client, and it needs the token, which
// a static file server cannot know. The document is ~2.3 KB, so buffering it per
// request costs nothing. If anything about the rewrite fails, the unmodified
// shell is still served — a browser is unaffected either way.
func serveShareHTML(w http.ResponseWriter, r *http.Request, token string) {
	doc, ok := readShareShell()
	if !ok {
		http.NotFound(w, r)
		return
	}
	if d, found := shareDiscoveryFor(token); found {
		doc = replaceShareTitle(doc, d.Title)
		doc = injectShareDiscovery(doc, d)
	}
	// Same policy ServeFileFromFS applies to entry HTML: the document references
	// content-hashed assets, so a cached copy would keep loading stale JS after a
	// rebuild.
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	// HEAD carries the headers and no body; Write is a no-op for it, but guarding
	// keeps the intent explicit and avoids depending on net/http's behavior.
	if r.Method != http.MethodHead {
		// doc is a build artifact plus a block assembled from html.EscapeString /
		// json.Marshal'd values and a token validated to [A-Za-z0-9_-]: no
		// request-controlled bytes reach the response unescaped.
		_, _ = io.WriteString(w, doc) //nolint:gosec // G705: escaped above; see comment
	}
}

// readShareShell loads share.html from the disk build dir, then the embedded
// dist, then the web/ source tree (dev).
func readShareShell() (string, bool) {
	fsys := frontend.GetFS()
	if f, err := fsys.Open("share.html"); err == nil {
		defer func() { _ = f.Close() }()
		if raw, err := io.ReadAll(f); err == nil {
			return string(raw), true
		}
	}
	if raw, err := os.ReadFile(filepath.Join("web", "share.html")); err == nil {
		return string(raw), true
	}
	return "", false
}
