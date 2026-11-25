//nolint:goconst // JSON response field names are domain strings, not config constants
package handler

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"clawbench/internal/filex"
	"clawbench/internal/gitignore"
	"clawbench/internal/model"
	"clawbench/internal/platform"
)

// ListDir returns the contents of a directory within the current project.
func ListDir(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	relPath := strings.TrimPrefix(r.URL.Query().Get("path"), "/")
	basePath, err := filepath.Abs(projectPath)
	if err != nil {
		slog.Error("failed to resolve project path", slog.String("path", projectPath), slog.String("err", err.Error()))
		model.WriteError(w, model.Internal(err))
		return
	}

	absPath, ok := validateAndResolveReadPath(w, r, basePath, relPath)
	if !ok {
		return
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		if isNotDirError(err) {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "NotADirectory")
		} else if os.IsNotExist(err) {
			writeLocalizedError(w, r, model.NotFound(nil, "DirectoryNotFound"))
		} else {
			model.WriteError(w, model.Internal(fmt.Errorf("cannot read directory")))
		}
		return
	}

	within := func(absPath string) bool { return isPathUnderBase(absPath, basePath) }
	items := buildDirEntries(absPath, entries, within, gitignore.ForDir(absPath))

	relFromBase, _ := filepath.Rel(basePath, absPath)
	relFromBase = filepath.ToSlash(relFromBase)
	var parent *string
	if relFromBase != "." {
		parentDir := path.Dir(relFromBase)
		if parentDir != "." {
			parent = &parentDir
		} else {
			empty := ""
			parent = &empty
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"path":   relFromBase,
		"parent": parent,
		"items":  items,
	})
}

// FileTreeEntry is a single file in a directory-tree listing, with a path
// relative to the queried directory (for tree reconstruction on download).
type FileTreeEntry struct {
	Rel  string `json:"rel"`
	Size int64  `json:"size"`
}

// ServeListTree handles GET /api/file/list-tree?path=<rel>
// Recursively lists every file under the queried directory (relative to the
// current project). Each file's `rel` is its path relative to the queried
// directory so the client can reconstruct the exact tree on download.
//
// An ABSOLUTE path is project-external (the file manager browses those, and an
// escaping symlink is entered through its resolved target) and is validated
// against the configured roots instead of being joined to the project —
// ValidatePath joins, which silently turned "/tmp/x" into "<project>/tmp/x".
func ServeListTree(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	rawPath := r.URL.Query().Get("path")
	basePath, err := filepath.Abs(projectPath)
	if err != nil {
		slog.Error("failed to resolve project path", slog.String("path", projectPath), slog.String("err", err.Error()))
		model.WriteError(w, model.Internal(err))
		return
	}

	var absPath string
	if filepath.IsAbs(rawPath) {
		absPath, err = filepath.Abs(rawPath)
		if err != nil || !isPathUnderAnyRoot(absPath) {
			writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
			return
		}
	} else {
		relPath := strings.TrimPrefix(rawPath, "/")
		absPath, ok = validateAndResolvePath(w, r, basePath, relPath)
		if !ok {
			return
		}
	}

	var files []FileTreeEntry

	// If the queried path is itself a file, return just that file.
	if info, statErr := os.Stat(absPath); statErr == nil && !info.IsDir() {
		files = append(files, FileTreeEntry{Rel: filepath.Base(absPath), Size: info.Size()})
		writeJSON(w, http.StatusOK, map[string]interface{}{"files": files})
		return
	}

	err = filepath.Walk(absPath, func(fullPath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(absPath, fullPath)
		if relErr != nil {
			return relErr
		}
		files = append(files, FileTreeEntry{
			Rel:  filepath.ToSlash(rel),
			Size: info.Size(),
		})
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Cannot access directory"})
		return
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Rel < files[j].Rel
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"files": files})
}

// resolveFilePath determines the absolute path and whether it's external to the
// project. Supports absolute paths via ?target= query param and project-relative
// paths via URL path.
func resolveFilePath(w http.ResponseWriter, r *http.Request, projectPath string) (absPath string, isExternal bool, ok bool) {
	if queryPath := r.URL.Query().Get("target"); queryPath != "" {
		// Accept paths starting with / (POSIX-style absolute from frontend)
		// or platform-native absolute paths (e.g. C:\ on Windows).
		if !strings.HasPrefix(queryPath, "/") && !filepath.IsAbs(queryPath) {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidFilePath")
			return "", false, false
		}
		var err error
		absPath, err = filepath.Abs(queryPath)
		if err != nil {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidFilePath")
			return "", false, false
		}
		if !isPathUnderAnyRoot(absPath) {
			writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
			return "", false, false
		}
		return absPath, true, true
	}

	// Project-relative path from URL path
	filepathStr := r.URL.Path
	if !strings.HasPrefix(filepathStr, "/api/fs/file/") {
		http.NotFound(w, r)
		return "", false, false
	}
	filepathStr = filepathStr[len("/api/fs/file/"):]
	// Strip leading slashes to handle double-slash URLs (/api/fs/file//path)
	// caused by encodeURIComponent("/path") which encodes as %2Fpath.
	// Go's ServeMux decodes %2F back to /, producing double slashes.
	filepathStr = strings.TrimLeft(filepathStr, "/")
	filepathStr = path.Clean(filepathStr)

	if filepathStr == ".." || path.IsAbs(filepathStr) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidFilePath")
		return "", false, false
	}

	basePath, _ := filepath.Abs(projectPath)
	absPath, ok = validateAndResolveReadPath(w, r, basePath, filepathStr)
	return absPath, false, ok
}

// GetFile returns the content of a single file.
//
// Supports an optional line window (?lineStart=&lineEnd=, 1-based inclusive)
// for the quick-preview pane: only those lines are returned, along with the
// file's total line count. Without the params the whole file is returned, so
// existing callers are unaffected.
//
// The whole-file response is capped at maxGetFileBytes; a line-window request is
// not, because it streams lines and never holds the file in memory. That is what
// lets the preview pane open a file far larger than the whole-file ceiling.
func GetFile(w http.ResponseWriter, r *http.Request) {
	// The optional line window (?lineStart/?lineEnd, quick-preview pane).
	//
	// Parsed (and rejected) BEFORE the size/binary early-returns below so an
	// invalid range is a 400 for every file, as the OpenAPI spec promises.
	// Parsing it after meant a binary file answered 200 isBinary:true for
	// `?lineStart=0`, which silently contradicts the documented contract.
	winStart, winEnd, hasWindow, winErr := parseLineWindow(r)
	if hasWindow && winErr != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidLineRange")
		return
	}

	// A line window is streamed line by line and never holds the file in
	// memory, so the whole-file size ceiling must not apply to it — that was
	// what made the preview pane refuse large files it could perfectly well
	// read a window from. The window has its own byte/line ceilings
	// (maxWindowBytes / maxWindowLines). The target is still resolved and
	// validated (existence, not-a-directory, path scope) either way.
	m, ok := resolveGetFileTarget(w, r, !hasWindow)
	if !ok {
		return
	}

	isText := model.IsTextFile(m.info.Name())
	forceText := r.URL.Query().Get("forceText") == "1"

	// For non-text files, check if the content is actually binary (via null-byte
	// sniffing). If binary, return isBinary=true without the content — the
	// frontend shows a placeholder with "Open as text" button.
	// Use ?forceText=1 to override: returns sanitized content (truncated +
	// non-printable chars replaced) safe for DOM rendering.
	if !isText && !forceText {
		isBinary, sniffErr := sniffBinaryContent(m.absPath)
		if sniffErr != nil {
			model.WriteError(w, model.Internal(fmt.Errorf("cannot open file")))
			return
		}
		if isBinary {
			writeBinaryFileResponse(w, m)
			return
		}
	}

	// Subtype detection (OpenAPI → ReDoc) needs the whole document, and so does
	// sanitization. Both are skipped on the window path, which only ever serves
	// the plain-text preview pane.
	//
	// A non-text extension that sniffed as text (LICENSE, an extensionless
	// script) is line-windowable too: the window path splits on line boundaries
	// and does not need the whole-file sanitize to answer it, while reading the
	// whole file just to ignore the window would defeat the pane's reason for
	// existing on a large file. forceText still ignores the window (its whole
	// point is the sanitized whole-document response).
	if hasWindow && !forceText {
		writeLineWindowResponse(w, m, winStart, winEnd)
		return
	}

	writeWholeFileResponse(w, m, isText)
}

// resolveGetFileTarget resolves and validates the file a GetFile request refers
// to. On failure it has already written the error response, so the caller only
// checks ok.
//
// enforceSizeLimit is false for line-window requests: those are streamed line by
// line, so the whole-file ceiling (maxGetFileBytes) does not bound what they
// cost. Existence, not-a-directory and path-scope checks always run.
func resolveGetFileTarget(w http.ResponseWriter, r *http.Request, enforceSizeLimit bool) (fileResponseMeta, bool) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return fileResponseMeta{}, false
	}

	absPath, isExternal, ok := resolveFilePath(w, r, projectPath)
	if !ok {
		return fileResponseMeta{}, false
	}

	info, err := os.Stat(absPath)
	if err != nil {
		handleStatError(w, r, absPath, err)
		return fileResponseMeta{}, false
	}
	if info.IsDir() {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "NotAFile")
		return fileResponseMeta{}, false
	}
	if enforceSizeLimit && info.Size() > maxGetFileBytes {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "FileTooLarge")
		return fileResponseMeta{}, false
	}

	return fileResponseMeta{absPath: absPath, projectPath: projectPath, isExternal: isExternal, info: info}, true
}

// writeWholeFileResponse reads the file in full and answers with its content,
// sanitizing non-text files (forceText, or a non-text extension that sniffed as
// text) and converting an OpenAPI YAML spec to JSON for ReDoc.
func writeWholeFileResponse(w http.ResponseWriter, m fileResponseMeta, isText bool) {
	content, err := os.ReadFile(m.absPath)
	if err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("cannot read file")))
		return
	}

	// Sanitize content for non-text files when forceText is used,
	// or when the file passed binary sniffing (non-text ext but actually text).
	var truncated bool
	if !isText {
		content, truncated = sanitizeTextContent(content)
	}

	subtype := model.DetectSubtype(m.info.Name(), string(content))
	var specJSON string
	if subtype == model.SubtypeOpenAPI && isYAMLSpec(m.info.Name()) {
		specJSON = model.ConvertSpecToJSON(string(content))
	}

	respPath := responsePath(m.absPath, m.projectPath, m.isExternal)
	linkTarget, isSymlink := resolveLinkTarget(m.absPath, m.projectPath, m.isExternal)

	writeJSON(w, http.StatusOK, FileContent{
		Content:    string(content),
		Name:       m.info.Name(),
		Path:       respPath,
		Supported:  model.IsSupportedFile(m.info.Name()),
		Size:       m.info.Size(),
		Truncated:  truncated,
		Subtype:    subtype,
		SpecJSON:   specJSON,
		LinkTarget: linkTarget,
		IsSymlink:  isSymlink,
	})
}

// isYAMLSpec reports whether name is a YAML document, which is the only OpenAPI
// flavor that needs converting to JSON for ReDoc.
func isYAMLSpec(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml")
}

// fileResponseMeta carries the resolved path and stat info shared by GetFile's
// response branches, so each branch does not re-derive the response path.
type fileResponseMeta struct {
	absPath     string
	projectPath string
	isExternal  bool
	info        os.FileInfo
}

// writeBinaryFileResponse answers a binary file: metadata plus isBinary=true and
// no content, which the frontend renders as an "Open as text" placeholder.
func writeBinaryFileResponse(w http.ResponseWriter, m fileResponseMeta) {
	respPath := responsePath(m.absPath, m.projectPath, m.isExternal)
	linkTarget, isSymlink := resolveLinkTarget(m.absPath, m.projectPath, m.isExternal)
	writeJSON(w, http.StatusOK, FileContent{
		Content:    "",
		Name:       m.info.Name(),
		Path:       respPath,
		Supported:  false,
		IsBinary:   true,
		Size:       m.info.Size(),
		LinkTarget: linkTarget,
		IsSymlink:  isSymlink,
	})
}

// writeLineWindowResponse answers a line-window request for the quick-preview
// pane, streaming only the requested lines instead of the whole file.
func writeLineWindowResponse(w http.ResponseWriter, m fileResponseMeta, winStart, winEnd int) {
	win, readErr := readFileLineWindow(m.absPath, winStart, winEnd)
	if readErr != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("cannot read file")))
		return
	}
	respPath := responsePath(m.absPath, m.projectPath, m.isExternal)
	linkTarget, isSymlink := resolveLinkTarget(m.absPath, m.projectPath, m.isExternal)
	// Only the opening window can identify a subtype: the spec version key sits
	// at the top of the document, and extension-based subtypes are already known
	// to the frontend. This also keeps scroll-driven growth windows (which start
	// mid-file) from paying for a YAML/JSON sniff on every request.
	//
	// The preview card classifies a `.yaml`/`.json` target from it: an OpenAPI
	// spec must render as docs, not as a code slice.
	subtype := ""
	if win.Start <= 1 {
		subtype = model.DetectSubtypeWindow(m.info.Name(), win.Text)
	}
	writeJSON(w, http.StatusOK, FileContent{
		Content:         win.Text,
		Name:            m.info.Name(),
		Path:            respPath,
		Supported:       model.IsSupportedFile(m.info.Name()),
		Size:            m.info.Size(),
		Truncated:       win.Truncated,
		Subtype:         subtype,
		LinkTarget:      linkTarget,
		IsSymlink:       isSymlink,
		TotalLines:      win.TotalLines,
		WindowStart:     win.Start,
		WindowEnd:       win.End,
		WindowTruncated: win.Truncated,
	})
}

// handleStatError writes an appropriate error response for os.Stat failures,
// including broken symlink detection.
func handleStatError(w http.ResponseWriter, r *http.Request, absPath string, err error) {
	if !os.IsNotExist(err) {
		slog.Warn("file access error", "path", absPath, "err", err)
		model.WriteError(w, model.Internal(fmt.Errorf("cannot access file")))
		return
	}
	// os.Stat fails on dangling symlinks. Check with Lstat —
	// if it's a symlink, include the target in the error message
	// so the user knows the link is broken (not just "file not found").
	linfo, lerr := os.Lstat(absPath)
	if lerr == nil && linfo.Mode()&os.ModeSymlink != 0 {
		target, _ := os.Readlink(absPath)
		slog.Warn("broken symlink", "path", absPath, "target", target)
		writeLocalizedErrorf(w, r, http.StatusNotFound, "BrokenSymlink", map[string]any{"Target": target})
		return
	}
	slog.Warn("file not found", "path", absPath)
	writeLocalizedError(w, r, model.NotFound(nil, "FileNotFoundShort"))
}

// rawFilePrefix is the URL-path prefix of the current raw-file endpoint.
const rawFilePrefix = "/api/fs/raw/"

// legacyLocalFilePrefix is the pre-rename raw-file endpoint, restored as a
// backward-compatibility alias for installed clients that cannot be updated.
//
// Why the alias exists: the rename to /api/fs/raw/ (see file_routes_rename_test.go)
// was a hard cutover — the old route was dropped and 404s. But the URL is built in
// NATIVE client code, not in the web frontend:
//
//	android/.../MainActivity.java  downloadFile()      -> "/api/local-file/…"
//	desktop/src/main/download.ts   resolveLocalFileUrl -> "/api/local-file/…"
//
// A frontend update cannot reach that code: the Android WebView loads the latest
// JS from the server, so the UI looks current while the native download bridge
// still emits the deleted URL. Those builds also cannot self-recover through the
// in-app updater (their embedded APK predates the fix), so a file-manager
// download simply 404s forever. The alias keeps them working until they are gone.
//
// Scope is deliberately minimal — one endpoint, matching what those clients
// actually call. The alias accepts the legacy `?path=` parameter name ONLY under
// this prefix (see resolveLocalFilePath), so the current /api/fs/raw/ endpoint
// keeps the renamed `?target=` shape and does not regain the Crawlab LFI
// fingerprint (`GET /api/file?path=…`) that motivated the rename.
//
// Deprecated: remove once no pre-rename Android/desktop client remains in use.
const legacyLocalFilePrefix = "/api/local-file/"

// resolveLocalFilePath determines the absolute path for ServeLocalFile.
// Supports absolute paths via ?target= query param and project-relative paths via URL path.
//
// The deprecated /api/local-file/ alias (legacyLocalFilePrefix) is also served
// here, with two compatibility accommodations — both gated on the request path so
// the current endpoint is unaffected:
//   - the absolute-path query param may be spelled `?path=` (the pre-rename name);
//   - the URL-path form is matched against the legacy prefix instead.
func resolveLocalFilePath(w http.ResponseWriter, r *http.Request, projectPath string) (string, bool) {
	// A request is "legacy" purely by its URL prefix. Everything below — the
	// param-name fallback and the prefix used for the URL-path form — keys off
	// this one flag, so the two accommodations cannot drift apart.
	legacy := strings.HasPrefix(r.URL.Path, legacyLocalFilePrefix)

	// Absolute path via query param. `?target=` is the current name; `?path=` is
	// the pre-rename name, honored only on the deprecated prefix. Accepting it on
	// /api/fs/raw/ too would undo the rename's whole point.
	queryPath := r.URL.Query().Get("target")
	if queryPath == "" && legacy {
		queryPath = r.URL.Query().Get("path")
	}
	if queryPath != "" {
		// Absolute path — serves files outside the project directory
		if !strings.HasPrefix(queryPath, "/") && !filepath.IsAbs(queryPath) {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidPath")
			return "", false
		}
		absPath, err := filepath.Abs(queryPath)
		if err != nil {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidPath")
			return "", false
		}
		if !isPathUnderAnyRoot(absPath) {
			writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
			return "", false
		}
		return absPath, true
	}

	// Project-relative path from URL path
	prefix := rawFilePrefix
	if legacy {
		prefix = legacyLocalFilePrefix
	}
	filepathStr := r.URL.Path
	if !strings.HasPrefix(filepathStr, prefix) {
		http.NotFound(w, r)
		return "", false
	}
	filepathStr = filepathStr[len(prefix):]
	// Strip leading slashes to handle double-slash URLs (/api/fs/raw//path)
	// caused by encodeURIComponent("/path") which encodes as %2Fpath.
	filepathStr = strings.TrimLeft(filepathStr, "/")
	filepathStr = path.Clean(filepathStr)

	if filepathStr == ".." || path.IsAbs(filepathStr) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidPath")
		return "", false
	}

	basePath, _ := filepath.Abs(projectPath)
	return validateAndResolveReadPath(w, r, basePath, filepathStr)
}

// ServeLocalFile serves a file directly (for images, PDFs, etc.).
// Supports project-relative paths via URL path and absolute paths via ?target=
// query param (for files outside the project directory).
func ServeLocalFile(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	absPath, ok := resolveLocalFilePath(w, r, projectPath)
	if !ok {
		return
	}

	info, err := os.Stat(absPath)
	if err != nil {
		writeLocalizedError(w, r, model.NotFound(nil, "FileNotFoundShort"))
		return
	}
	if info.IsDir() {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "NotADirectory")
		return
	}

	ext := strings.ToLower(filepath.Ext(absPath))
	// This is the AUTHENTICATED endpoint, so the web-asset additions (text/html
	// and friends) apply. The public share endpoint deliberately does not get
	// them — see webAssetMimeTypes.
	mime := mimeForLocalFile(ext)

	// If ?download=1 is present, force download with Content-Disposition header.
	// Use http.ServeContent instead of http.ServeFile to avoid a 301 redirect
	// for files named "index.html" — http.ServeFile treats "index.html" as a
	// directory index and redirects to "./", which changes the URL path to
	// point at the parent directory, triggering a NotADirectory error.
	if r.URL.Query().Get("download") == "1" {
		fileName := filepath.Base(absPath)
		w.Header().Set("Content-Disposition", filex.ContentDispositionAttachment(fileName))
		w.Header().Set("Content-Type", mime)
		f, openErr := os.Open(absPath)
		if openErr != nil {
			model.WriteError(w, model.Internal(fmt.Errorf("cannot open file")))
			return
		}
		defer func() { _ = f.Close() }()
		http.ServeContent(w, r, filex.SanitizeArchiveName(fileName), info.ModTime(), f)
		return
	}

	// Use http.ServeContent rather than http.ServeFile: ServeFile treats a file
	// named "index.html" as a directory index and answers 301 to "./". That
	// breaks the HTML file preview, whose iframe points straight at
	// .../index.html — the redirect rewrites the URL to the parent directory and
	// the preview ends up loading a directory listing error instead of the
	// document. ServeContent has no such special case. (The ?download=1 branch
	// below already worked around this for the same reason.)
	w.Header().Set("Content-Type", mime)
	f, err := os.Open(absPath)
	if err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("cannot open file")))
		return
	}
	defer func() { _ = f.Close() }()
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

// ServeProjects handles GET (list directory) and POST (create directory) for projects.
func ServeProjects(w http.ResponseWriter, r *http.Request) { //nolint:gocyclo // multi-method file/directory handler
	switch r.Method {
	case http.MethodPost:
		serveProjectsCreate(w, r)
		return
	case http.MethodGet:
		// continue below
	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}

	rawPath := r.URL.Query().Get("path")

	// Root-level browsing: when path is empty, show root entries
	if rawPath == "" || rawPath == "/" {
		if platform.IsWindows() && len(model.RootPaths) > 1 {
			// Windows: return synthetic drive entries
			items := make([]DirEntry, 0, len(model.RootPaths))
			for _, drive := range model.RootPaths {
				items = append(items, DirEntry{Name: drive, Type: "dir"})
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"path":   "",
				"parent": nil,
				"items":  items,
			})
			return
		}
		// Unix or single-drive Windows: list the first root directory
		if len(model.RootPaths) > 0 {
			rawPath = model.RootPaths[0]
		}
	}

	var absPath string
	if filepath.IsAbs(rawPath) {
		absPath = rawPath
	} else if len(model.RootPaths) > 0 {
		absPath, _ = filepath.Abs(filepath.Join(model.RootPaths[0], rawPath))
	}

	if absPath == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidPath")
		return
	}

	if !isPathUnderAnyRoot(absPath) {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		if isNotDirError(err) {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "NotADirectory")
		} else if os.IsNotExist(err) {
			writeLocalizedError(w, r, model.NotFound(nil, "DirectoryNotFound"))
		} else {
			model.WriteError(w, model.Internal(fmt.Errorf("cannot read directory")))
		}
		return
	}

	items := buildDirEntries(absPath, entries, isPathUnderAnyRoot, gitignore.ForDir(absPath))

	// Compute parent — stop at root level (no parent above root/drives).
	// A UNC share root counts as root level even when it is not in RootPaths
	// (the share list is a cached snapshot, and a share can be typed in). The
	// share is the volume boundary: filepath.Dir would otherwise return the
	// share itself, and the client would offer a parent it is already at.
	var parent *string
	isAtRoot := platform.IsWindows() && platform.IsUNCShareRoot(absPath)
	for _, root := range model.RootPaths {
		cleanRoot := filepath.Clean(root)
		if filepath.Clean(absPath) == cleanRoot {
			isAtRoot = true
			break
		}
	}
	if !isAtRoot {
		p := filepath.ToSlash(filepath.Dir(absPath))
		parent = &p
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"path":   filepath.ToSlash(absPath),
		"parent": parent,
		"items":  items,
	})
}

// containsGlobChars returns true if the path contains characters that are
// invalid in filesystem paths (glob wildcards, angle brackets, double-star).
// These characters indicate the string is a glob pattern or template variable,
// not a real file path.
func containsGlobChars(path string) bool {
	return strings.ContainsAny(path, "*?[]<>") || strings.Contains(path, "**")
}

// ServeFileBatchExists handles POST /api/file/batch-exists
// Body:   { "paths": ["src/main.go", "lib/", "**/*.class"] }
// Response: { "results": { "src/main.go": "file", "lib": "dir", "**/*.class": "none" } }
// Each path is checked against the project directory. Paths containing glob
// characters are short-circuited to "none" without touching the filesystem.
func ServeFileBatchExists(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var req struct {
		Paths []string `json:"paths"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.Paths) == 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MissingPath")
		return
	}
	if len(req.Paths) > 100 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "TooManyPaths")
		return
	}

	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	baseAbs, err := filepath.Abs(projectPath)
	if err != nil {
		model.WriteError(w, model.Internal(err))
		return
	}

	results := make(map[string]string, len(req.Paths))
	for _, p := range req.Paths {
		// Short-circuit glob patterns and template variables
		if containsGlobChars(p) {
			results[p] = "none"
			continue
		}
		// Expand ~ to home directory so paths like ~/.bashrc resolve correctly
		p = platform.ExpandTilde(p)
		var absPath string
		if strings.HasPrefix(p, "/") || filepath.IsAbs(p) {
			// Absolute path — stat directly without project scoping
			var err error
			absPath, err = filepath.Abs(p)
			if err != nil {
				results[p] = "none"
				continue
			}
		} else {
			// Relative path — resolve against project root
			var ok bool
			absPath, ok = resolveReadPath(baseAbs, p)
			if !ok {
				results[p] = "none"
				continue
			}
		}
		info, err := os.Stat(absPath)
		if err != nil {
			results[p] = "none"
		} else if info.IsDir() {
			results[p] = "dir"
		} else {
			results[p] = "file"
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"results": results})
}

// imageBase64Exts is the set of image extensions allowed in the batch-base64 endpoint.
// Only includes formats with inline browser support and known MIME types.
var imageBase64Exts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".webp": true, ".svg": true, ".bmp": true, ".ico": true,
}

// ServeFileBatchBase64 reads multiple image files and returns their base64-encoded content.
//
// POST /api/file/batch-base64
// Body:   { "paths": ["img/logo.png", "img/diagram.svg"] }
//
//	Response: {
//	  "results": { "img/logo.png": { "mime": "image/png", "data": "base64..." } },
//	  "skipped": [{ "path": "img/huge.png", "reason": "exceeds 2MB limit" }]
//	}
//
// Only image files are allowed. Per-file size cap is 2MB.
// Total response size budget is 20MB of base64 data (~15MB raw).
// Max 50 paths per request.
func ServeFileBatchBase64(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var req struct {
		Paths []string `json:"paths"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.Paths) == 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MissingPath")
		return
	}
	if len(req.Paths) > 50 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "TooManyPaths")
		return
	}

	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	baseAbs, err := filepath.Abs(projectPath)
	if err != nil {
		model.WriteError(w, model.Internal(err))
		return
	}

	results, skipped := batchBase64ProcessPaths(req.Paths, baseAbs)

	resp := map[string]interface{}{
		"results": results,
	}
	if len(skipped) > 0 {
		resp["skipped"] = skipped
	}
	writeJSON(w, http.StatusOK, resp)
}

type batchBase64Result struct {
	Mime string `json:"mime"`
	Data string `json:"data"`
}

type batchBase64Skip struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// batchBase64ProcessPaths processes each path and returns results and skipped items.
func batchBase64ProcessPaths(paths []string, baseAbs string) (map[string]batchBase64Result, []batchBase64Skip) {
	const maxFileSize = 2 * 1024 * 1024     // 2MB per file
	const maxTotalBase64 = 20 * 1024 * 1024 // 20MB total base64 output

	results := make(map[string]batchBase64Result, len(paths))
	var skipped []batchBase64Skip
	var totalBase64 int

	for _, p := range paths {
		res, skip, encodedLen := batchBase64ProcessOne(p, baseAbs, maxFileSize, totalBase64, maxTotalBase64)
		if skip != nil {
			skipped = append(skipped, *skip)
			continue
		}
		results[p] = *res
		totalBase64 += encodedLen
	}
	return results, skipped
}

// batchBase64ProcessOne handles a single path for batch-base64.
// Returns (result, skip, encodedLen). Exactly one of result/skip is non-nil.
func batchBase64ProcessOne(p, baseAbs string, maxFileSize, totalBase64, maxTotalBase64 int) (*batchBase64Result, *batchBase64Skip, int) {
	lower := strings.ToLower(p)
	ext := filepath.Ext(lower)
	if !imageBase64Exts[ext] {
		return nil, &batchBase64Skip{Path: p, Reason: "not an image file"}, 0
	}

	absPath, ok := batchBase64ResolvePath(p, baseAbs)
	if !ok {
		return nil, &batchBase64Skip{Path: p, Reason: "access denied"}, 0
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		reason := "read error"
		if errors.Is(err, os.ErrNotExist) {
			reason = "file not found"
		}
		return nil, &batchBase64Skip{Path: p, Reason: reason}, 0
	}

	if len(data) > maxFileSize {
		return nil, &batchBase64Skip{Path: p, Reason: "exceeds 2MB limit"}, 0
	}

	encodedSize := base64.StdEncoding.EncodedLen(len(data))
	if totalBase64+encodedSize > maxTotalBase64 {
		return nil, &batchBase64Skip{Path: p, Reason: "total size exceeded"}, 0
	}

	mime := mimeTypes[ext]
	if mime == "" {
		mime = mimeOctetStream
	}

	b64 := base64.StdEncoding.EncodeToString(data)
	return &batchBase64Result{Mime: mime, Data: b64}, nil, len(b64)
}

// batchBase64ResolvePath resolves a path to an absolute path with access checks.
func batchBase64ResolvePath(p, baseAbs string) (string, bool) {
	p = platform.ExpandTilde(p)
	if strings.HasPrefix(p, "/") || filepath.IsAbs(p) {
		abs, err := filepath.Abs(p)
		if err != nil || !isPathUnderAnyRoot(abs) {
			return "", false
		}
		return abs, true
	}
	absPath, valid := model.ValidatePath(baseAbs, p)
	return absPath, valid
}

// ── File-related DTOs ──────────────────────────────────────────────────────────

// buildDirEntries builds a sorted list of directory entries
// isNotDirError returns true if the error indicates the path is not a directory
// (e.g. it is a file). This handles syscall.ENOTDIR on Unix and
// ERROR_DIRECTORY (0x267) on Windows, which ReadDir returns when called on a file.
func isNotDirError(err error) bool {
	if errors.Is(err, syscall.ENOTDIR) {
		return true
	}
	// Windows: ReadDir on a file returns ERROR_DIRECTORY (0x267) wrapped in PathError.
	// We check the errno value directly because syscall.ERROR_DIRECTORY is not
	// available on non-Windows builds.
	pe := &os.PathError{}
	if errors.As(err, &pe) {
		var errno syscall.Errno
		if errors.As(pe.Err, &errno) {
			return true
		}
	}
	return false
}

// buildDirEntries builds a sorted list of directory entries. parentDir is the
// absolute directory being listed, and within resolves a symlink target's
// absolute path to whether it is allowed (stays within the configured base),
// preventing symlink traversal. A symlink-to-directory is typed as "dir" so the
// frontend can navigate into it; symlinks whose target escapes the base or is
// dangling stay listed but non-navigable.
//
// ign may be nil (not a git repository), in which case no entry is flagged as
// ignored.
func buildDirEntries(parentDir string, entries []os.DirEntry, within func(absPath string) bool, ign *gitignore.Matcher) []DirEntry {
	var items []DirEntry
	for _, entry := range entries {
		name := entry.Name()
		isSymlink := entry.Type()&os.ModeSymlink != 0

		// Resolve symlink-to-directory before classifying so linked dirs can be
		// entered (entry.IsDir()/Info() use lstat and would misclassify them).
		if isSymlink {
			item := classifySymlinkEntry(parentDir, entry, within)
			// gitignore matching uses lstat, so a symlink counts as a FILE even
			// when its target is a directory: a directory-only pattern like
			// "build/" does not match a symlink named "build". The entry's Type
			// is still "dir" so the UI can navigate into it.
			item.Ignored = isIgnored(ign, parentDir, name, false)
			items = append(items, item)
			continue
		}

		// Try to get file info with a timeout to avoid blocking on
		// unresponsive network mounts (e.g. NFS hard mounts).
		info, infoErr := fileInfoWithTimeout(entry)
		if infoErr != nil {
			// Timeout or error — use DirEntry.IsDir() as a fallback
			// so the entry still appears in the listing (without size/modTime).
			slog.Warn("failed to get file info, using fallback", slog.String("name", name), slog.String("err", infoErr.Error()))
			if entry.IsDir() {
				items = append(items, DirEntry{
					Name:    name,
					Type:    "dir",
					Ignored: isIgnored(ign, parentDir, name, true),
				})
			} else {
				entryType := "file"
				if model.IsImageFile(name) {
					entryType = "image"
				}
				items = append(items, DirEntry{
					Name:      name,
					Type:      entryType,
					Supported: model.IsSupportedFile(name),
					Ignored:   isIgnored(ign, parentDir, name, false),
				})
			}
			continue
		}
		if entry.IsDir() {
			modified := info.ModTime().Format(time.RFC3339)
			items = append(items, DirEntry{
				Name:     name,
				Type:     "dir",
				Modified: modified,
				Ignored:  isIgnored(ign, parentDir, name, true),
			})
		} else {
			entryType := "file"
			if model.IsImageFile(name) {
				entryType = "image"
			}
			items = append(items, DirEntry{
				Name:      name,
				Type:      entryType,
				Modified:  info.ModTime().Format(time.RFC3339),
				Size:      info.Size(),
				Supported: model.IsSupportedFile(name),
				Ignored:   isIgnored(ign, parentDir, name, false),
			})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Type != items[j].Type {
			return items[i].Type == "dir"
		}
		return items[i].Name < items[j].Name
	})
	return items
}

// isIgnored reports whether git would ignore the child of parentDir. It is a
// thin nil-safe wrapper so the listing code stays free of repository details.
func isIgnored(ign *gitignore.Matcher, parentDir, name string, isDir bool) bool {
	if ign == nil {
		return false
	}
	return ign.Ignored(filepath.Join(parentDir, name), isDir)
}

// classifySymlinkEntry classifies a symlink entry by following its target.
// Returns a "dir" entry when the target is a directory; otherwise a
// non-navigable file entry (dangling targets stay listed but cannot be
// entered). Always sets Symlink, and Broken for dangling.
//
// A symlink-to-directory whose target escapes the browsed base is still typed
// "dir" and carries its resolved absolute path in Target: the base-scoped
// listing endpoint refuses to follow such a link, but the target is an ordinary
// directory the client may browse directly through the external-browsing
// endpoint (/api/projects). Without Target the client would re-request the
// symlink's own base-relative path and get "access denied". Targets outside
// every configured root stay non-navigable, matching that endpoint's refusal.
func classifySymlinkEntry(parentDir string, entry os.DirEntry, within func(absPath string) bool) DirEntry {
	name := entry.Name()
	fullPath := filepath.Join(parentDir, name)

	// Following stat resolves the symlink target. Dangling links fail here.
	target, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Dangling/broken symlink — list it, but non-navigable.
			slog.Warn("broken symlink", slog.String("path", fullPath))
			return DirEntry{Name: name, Type: "file", Symlink: true, Broken: true}
		}
		slog.Warn("failed to stat symlink target", slog.String("path", fullPath), slog.String("err", err.Error()))
		return DirEntry{Name: name, Type: "file", Symlink: true}
	}

	if target.IsDir() {
		modified := target.ModTime().Format(time.RFC3339)
		if within(fullPath) {
			return DirEntry{Name: name, Type: "dir", Symlink: true, Modified: modified}
		}
		if resolved, resolveErr := filepath.EvalSymlinks(fullPath); resolveErr == nil && isPathUnderAnyRoot(resolved) {
			// ToSlash matches how every other absolute path in this API is
			// reported (e.g. ServeProjects' "path"), so the client sees one
			// separator convention regardless of platform.
			return DirEntry{Name: name, Type: "dir", Symlink: true, Target: filepath.ToSlash(resolved), Modified: modified}
		}
	}

	// File symlink, or directory symlink whose target is outside every root
	// (shown as a non-navigable file, consistent with copy/archive handling).
	entryType := "file"
	if model.IsImageFile(name) {
		entryType = "image"
	}
	return DirEntry{
		Name:      name,
		Type:      entryType,
		Symlink:   true,
		Modified:  target.ModTime().Format(time.RFC3339),
		Size:      target.Size(),
		Supported: model.IsSupportedFile(name),
	}
}

const fileInfoTimeout = 3 * time.Second

// fileInfoWithTimeout calls entry.Info() with a timeout to avoid blocking
// indefinitely on unresponsive network filesystems (e.g. NFS hard mounts).
func fileInfoWithTimeout(entry os.DirEntry) (os.FileInfo, error) {
	type result struct {
		info os.FileInfo
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		info, err := entry.Info()
		ch <- result{info, err}
	}()
	select {
	case r := <-ch:
		return r.info, r.err
	case <-time.After(fileInfoTimeout):
		return nil, fmt.Errorf("timeout getting file info for %q after %v", entry.Name(), fileInfoTimeout)
	}
}

// serveProjectsCreate handles POST /api/projects (create directory under any root path).
func serveProjectsCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "DirectoryNameRequired")
		return
	}
	var absPath string
	if req.Path == "" || req.Path == "/" {
		if len(model.RootPaths) > 0 {
			absPath = model.RootPaths[0]
		}
	} else if filepath.IsAbs(req.Path) {
		absPath = req.Path
	} else {
		rel := strings.TrimPrefix(req.Path, "/")
		if len(model.RootPaths) > 0 {
			var err error
			absPath, err = filepath.Abs(filepath.Join(model.RootPaths[0], rel))
			if err != nil {
				slog.Warn("failed to resolve path", slog.String("path", req.Path), slog.String("err", err.Error()))
			}
		}
	}
	if !isPathUnderAnyRoot(absPath) {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}
	newDir := filepath.Join(absPath, req.Name)
	// Validate that the resolved new directory stays under a root path
	// (req.Name could contain ".." path traversal components)
	newDirAbs, err := filepath.Abs(newDir)
	if err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("resolve path failed: %w", err)))
		return
	}
	if !isPathUnderAnyRoot(newDirAbs) {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}
	if err := os.Mkdir(newDirAbs, 0o755); err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("create directory failed: %w", err)))
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "path": filepath.ToSlash(newDirAbs)})
}

const (
	// maxWindowLines bounds a single ?lineStart/?lineEnd window. The preview
	// pane renders at most 200 lines plus context, so this is generous
	// headroom while still rejecting a pathological "give me everything".
	maxWindowLines = 2000
	// maxWindowBytes bounds the window's serialized size for the same reason.
	maxWindowBytes = 2 * 1024 * 1024
)

// parseLineWindow extracts the optional ?lineStart/?lineEnd window from the
// request. present is false when neither param is supplied (whole-file request);
// when present is true the range is validated and start/end are usable, so a
// malformed or inverted range is reported via err for the caller to answer 400
// rather than silently returning the whole file.
func parseLineWindow(r *http.Request) (start, end int, present bool, err error) {
	rawStart := r.URL.Query().Get("lineStart")
	rawEnd := r.URL.Query().Get("lineEnd")
	if rawStart == "" && rawEnd == "" {
		return 0, 0, false, nil
	}
	if rawStart == "" {
		return 0, 0, true, errors.New("lineStart is required when lineEnd is set")
	}

	start, err = strconv.Atoi(rawStart)
	if err != nil || start < 1 {
		return 0, 0, true, errors.New("lineStart must be a positive integer")
	}

	// A lone lineStart means "just that line".
	end = start
	if rawEnd != "" {
		end, err = strconv.Atoi(rawEnd)
		if err != nil || end < start {
			return 0, 0, true, errors.New("lineEnd must be an integer >= lineStart")
		}
	}
	if end-start+1 > maxWindowLines {
		end = start + maxWindowLines - 1
	}
	return start, end, true, nil
}

// lineWindow is the result of reading a line window out of a file.
type lineWindow struct {
	// Text is the requested lines joined by "\n".
	Text string
	// TotalLines is the file's line count.
	TotalLines int
	// Start and End delimit the lines actually returned. End < Start signals
	// "no lines captured" (the range starts past EOF, or the byte cap tripped on
	// the very first line); the caller renders that as the out-of-range notice
	// rather than an empty body.
	Start, End int
	// Truncated reports that the byte cap cut the window short.
	Truncated bool
}

// readFileLineWindow streams absPath and returns the lines [startLine, endLine]
// (1-based, inclusive) joined by "\n", plus the file's total line count and the
// last line actually included. Only the requested window is materialized, so a
// large file costs a sequential scan but no large allocation — the whole point
// of the line-window path for the quick-preview pane.
//
// Line splitting mirrors the frontend's /\r\n|\r|\n/: "\r\n" counts as a single
// separator, a bare "\r" or "\n" each count as one, and a file ending in a
// separator has a trailing empty line (so "a\n" is 2 lines). An empty file has 0
// lines, matching sliceCodeForPreview's empty-content special case.
func readFileLineWindow(absPath string, startLine, endLine int) (lineWindow, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return lineWindow{}, err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return lineWindow{}, err
	}
	if info.Size() == 0 {
		return lineWindow{}, nil
	}

	w := &lineWindowCollector{startLine: startLine, endLine: endLine}
	if err := w.scan(f); err != nil {
		return lineWindow{}, err
	}

	// Finalize the last line. When the file ended on a separator this is the
	// trailing empty line JS split() also yields; lineNo already counts it, and
	// starts at 1, so lineNo is exactly the file's line count either way.
	w.appendLine()

	win := lineWindow{
		Text:       strings.Join(w.lines, "\n"),
		TotalLines: w.lineNo,
		Start:      startLine,
		End:        startLine - 1,
		Truncated:  w.truncated,
	}
	if len(w.lines) > 0 {
		win.End = startLine + len(w.lines) - 1
	}
	return win, nil
}

// lineWindowCollector accumulates the lines of [startLine, endLine] while the
// caller streams the file, so a huge file never has to be held in memory.
type lineWindowCollector struct {
	startLine, endLine int
	lines              []string
	cur                []byte
	lineNo             int
	bytesUsed          int
	truncated          bool
	crPending          bool
}

// scan consumes r in fixed-size chunks, splitting on /\r\n|\r|\n/ exactly like
// the frontend's split() (see readFileLineWindow's doc comment for the rules).
func (w *lineWindowCollector) scan(r io.Reader) error {
	w.lineNo = 1
	buf := make([]byte, 64*1024)
	for {
		n, readErr := r.Read(buf)
		for _, b := range buf[:n] {
			w.consumeByte(b)
		}
		if readErr != nil {
			if readErr != io.EOF {
				return readErr
			}
			return nil
		}
	}
}

// consumeByte feeds one byte to the splitter, emitting a line at each boundary.
func (w *lineWindowCollector) consumeByte(b byte) {
	if w.crPending {
		w.crPending = false
		if b == '\n' {
			return // the LF half of a CRLF: same single separator
		}
	}
	switch b {
	case '\n':
		w.appendLine()
		w.lineNo++
	case '\r':
		w.appendLine()
		w.lineNo++
		w.crPending = true
	default:
		w.cur = append(w.cur, b)
	}
}

// appendLine captures the current line when it falls inside the window. It is
// called for every line boundary, including empty ones.
func (w *lineWindowCollector) appendLine() {
	if w.truncated || w.lineNo < w.startLine || w.lineNo > w.endLine {
		w.cur = w.cur[:0]
		return
	}
	if w.bytesUsed+len(w.cur) > maxWindowBytes {
		w.truncated = true
		w.cur = w.cur[:0]
		return
	}
	w.lines = append(w.lines, string(w.cur))
	w.bytesUsed += len(w.cur)
	if len(w.lines) > 1 {
		w.bytesUsed++ // the joining "\n"
	}
	w.cur = w.cur[:0]
}
