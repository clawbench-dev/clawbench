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
	"log/slog"
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

	token, _, err := service.UpsertFileShare(absPath, name)
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
	shares, err := service.ListFileShares()
	if err != nil {
		slog.Error("share: list failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}

	// Project path for display-path relativization; may be empty when no
	// project cookie is set (still list everything, paths stay absolute).
	projectPath := middleware.GetProjectFromCookie(r)
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
			CreatedAt: s.CreatedAt,
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

	switch {
	case req.All:
		// One-click clear: revoke every share link at once.
		if err := service.DeleteAllFileShares(); err != nil {
			slog.Error("share: delete-all failed", "err", err)
			model.WriteError(w, model.Internal(err))
			return
		}
	case req.Token != "":
		if err := service.DeleteFileShareByToken(req.Token); err != nil {
			slog.Error("share: revoke-by-token failed", "token", req.Token, "err", err)
			model.WriteError(w, model.Internal(err))
			return
		}
	default:
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MissingToken")
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

// ServeSharePublic serves the unauthenticated token-scoped data endpoints:
//   - GET /api/share/{token}/file        → FileContent JSON
//   - GET /api/share/{token}/download    → raw bytes with Content-Disposition
//   - GET /api/share/{token}/local/{rel} → raw bytes resolved against the shared
//     file's directory (or ?path= absolute)
func ServeSharePublic(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	token, rest, ok := parseSharePublicPath(r.URL.Path)
	if !ok || token == "" {
		http.NotFound(w, r)
		return
	}

	absPath, name, exists, err := service.GetFileShareByToken(token)
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

	switch {
	case rest == "" || rest == entryTypeFile:
		serveShareFileContent(w, r, absPath)
	case rest == "download":
		serveShareRaw(w, r, absPath, name, true)
	case rest == "local" || strings.HasPrefix(rest, "local/"):
		serveShareLocal(w, r, absPath, rest)
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

	if info.Size() > 10*1024*1024 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "FileTooLarge")
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

// serveShareLocal serves a file referenced by the shared document (markdown
// images etc.). Relative paths resolve against the shared file's directory;
// absolute paths are accepted via ?path= and validated against root paths.
func serveShareLocal(w http.ResponseWriter, r *http.Request, sharedAbsPath, rest string) {
	// Absolute path via ?path= — referenced outside the shared file's dir.
	if queryPath := r.URL.Query().Get("path"); queryPath != "" {
		if !strings.HasPrefix(queryPath, "/") && !filepath.IsAbs(queryPath) {
			http.NotFound(w, r)
			return
		}
		absTarget, aerr := filepath.Abs(queryPath)
		if aerr != nil || !isPathUnderAnyRoot(absTarget) {
			http.NotFound(w, r)
			return
		}
		serveShareRaw(w, r, absTarget, filepath.Base(absTarget), false)
		return
	}

	// Relative path from /local/{rel...}: resolve against the shared file's dir.
	rel := strings.TrimPrefix(rest, "local")
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
	serveShareHTML(w, r)
}

// serveShareHTML serves the share SPA entry (share.html), preferring the disk
// public/ dir then falling back to embedded dist (mirrors ServeIndex).
func serveShareHTML(w http.ResponseWriter, r *http.Request) {
	fsys := frontend.GetFS()
	if fi, err := fsys.Open("share.html"); err == nil {
		_ = fi.Close()
		frontend.ServeFileFromFS(w, r, fsys, "share.html")
		return
	}
	// Dev fallback: serve from web/ source directory.
	http.ServeFile(w, r, filepath.Join("web", "share.html"))
}
