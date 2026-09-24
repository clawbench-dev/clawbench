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
	"unicode/utf8"

	"clawbench/internal/gitignore"
	"clawbench/internal/model"
	"clawbench/internal/platform"
)

// mimeOctetStream is the generic binary MIME type used when a file extension
// is not recognized.
const mimeOctetStream = "application/octet-stream"

// mimeTypes maps file extensions to MIME types for ServeLocalFile.
var mimeTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
	".ico":  "image/x-icon",
	".bmp":  "image/bmp",
	".pdf":  "application/pdf",
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
	".ogg":  "audio/ogg",
	".m4a":  "audio/mp4",
	".aac":  "audio/aac",
	".flac": "audio/flac",
	".wma":  "audio/x-ms-wma",
	".opus": "audio/opus",
	".mp4":  "video/mp4",
	".mkv":  "video/x-matroska",
	".avi":  "video/x-msvideo",
	".mov":  "video/quicktime",
	".webm": "video/webm",
	".flv":  "video/x-flv",
	".wmv":  "video/x-ms-wmv",
	".m4v":  "video/mp4",
	".3gp":  "video/3gpp",
	".m3u8": "application/vnd.apple.mpegurl",
	// Office documents
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".doc":  "application/msword",
	".xls":  "application/vnd.ms-excel",
	".ppt":  "application/vnd.ms-powerpoint",
}

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

	absPath, ok := validateAndResolvePath(w, r, basePath, relPath)
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
func ServeListTree(w http.ResponseWriter, r *http.Request) {
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

	absPath, ok := validateAndResolvePath(w, r, basePath, relPath)
	if !ok {
		return
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
	absPath, ok = validateAndResolvePath(w, r, basePath, filepathStr)
	return absPath, false, ok
}

// GetFile returns the content of a single file.
//
// Supports an optional line window (?lineStart=&lineEnd=, 1-based inclusive)
// for the quick-preview pane: only those lines are returned, along with the
// file's total line count. Without the params the whole file is returned, so
// existing callers are unaffected.
func GetFile(w http.ResponseWriter, r *http.Request) {
	m, ok := resolveGetFileTarget(w, r)
	if !ok {
		return
	}

	// Line-window requests (quick-preview pane) are only meaningful for real
	// text files: forceText/binary sanitization rewrites the byte stream, so the
	// window is ignored there and the full sanitized content is returned.
	//
	// Parsed (and rejected) BEFORE the binary early-return below so an invalid
	// range is a 400 for every file, as the OpenAPI spec promises. Parsing it
	// after meant a binary file answered 200 isBinary:true for `?lineStart=0`,
	// which silently contradicts the documented contract.
	isText := model.IsTextFile(m.info.Name())
	forceText := r.URL.Query().Get("forceText") == "1"
	winStart, winEnd, hasWindow, winErr := parseLineWindow(r)
	if hasWindow && winErr != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidLineRange")
		return
	}

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
	if hasWindow && isText {
		writeLineWindowResponse(w, m, winStart, winEnd)
		return
	}

	writeWholeFileResponse(w, m, isText)
}

// resolveGetFileTarget resolves and validates the file a GetFile request refers
// to. On failure it has already written the error response, so the caller only
// checks ok.
func resolveGetFileTarget(w http.ResponseWriter, r *http.Request) (fileResponseMeta, bool) {
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
	if info.Size() > maxGetFileBytes {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "FileTooLarge")
		return fileResponseMeta{}, false
	}

	return fileResponseMeta{absPath: absPath, projectPath: projectPath, isExternal: isExternal, info: info}, true
}

// maxGetFileBytes caps the whole-file response; the line-window path exists
// precisely so a larger file can still be previewed.
const maxGetFileBytes = 10 * 1024 * 1024

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
	writeJSON(w, http.StatusOK, FileContent{
		Content:         win.Text,
		Name:            m.info.Name(),
		Path:            respPath,
		Supported:       model.IsSupportedFile(m.info.Name()),
		Size:            m.info.Size(),
		Truncated:       win.Truncated,
		LinkTarget:      linkTarget,
		IsSymlink:       isSymlink,
		TotalLines:      win.TotalLines,
		WindowStart:     win.Start,
		WindowEnd:       win.End,
		WindowTruncated: win.Truncated,
	})
}

// resolveLinkTarget returns the symlink target path (relative to projectPath when
// the link is project-internal, absolute otherwise) for a symlink at absPath,
// plus whether the entry is a symlink. Returns ("", false) for non-symlinks and
// when the target cannot be read.
func resolveLinkTarget(absPath, projectPath string, isExternal bool) (string, bool) {
	linfo, err := os.Lstat(absPath)
	if err != nil || linfo.Mode()&os.ModeSymlink == 0 {
		return "", false
	}
	target, err := os.Readlink(absPath)
	if err != nil {
		return "", true
	}
	// Resolve a relative link target against the link's directory, then present
	// it project-relative (or absolute for external files), matching respPath.
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(absPath), target)
	}
	resp := responsePath(target, projectPath, isExternal)
	if resp == "" {
		resp = target
	}
	return resp, true
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

// sniffBinaryContent reads the beginning of a file and returns true if it
// contains null bytes (indicating binary content).
func sniffBinaryContent(absPath string) (bool, error) {
	sniffBuf := make([]byte, binarySniffSize)
	f, err := os.Open(absPath)
	if err != nil {
		return false, err
	}
	n, _ := f.Read(sniffBuf)
	_ = f.Close()

	for i := range n {
		if sniffBuf[i] == 0 {
			return true, nil
		}
	}
	return false, nil
}

// responsePath returns the path to include in API responses: relative for
// project files, absolute for external files.
func responsePath(absPath, projectPath string, isExternal bool) string {
	if isExternal {
		return absPath
	}
	relPath, _ := filepath.Rel(projectPath, absPath)
	return filepath.ToSlash(relPath)
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
	return validateAndResolvePath(w, r, basePath, filepathStr)
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
	mime := mimeTypes[ext]
	if mime == "" {
		mime = mimeOctetStream
	}

	// If ?download=1 is present, force download with Content-Disposition header.
	// Use http.ServeContent instead of http.ServeFile to avoid a 301 redirect
	// for files named "index.html" — http.ServeFile treats "index.html" as a
	// directory index and redirects to "./", which changes the URL path to
	// point at the parent directory, triggering a NotADirectory error.
	if r.URL.Query().Get("download") == "1" {
		fileName := filepath.Base(absPath)
		w.Header().Set("Content-Disposition", contentDispositionAttachment(fileName))
		w.Header().Set("Content-Type", mime)
		f, err := os.Open(absPath)
		if err != nil {
			model.WriteError(w, model.Internal(fmt.Errorf("cannot open file")))
			return
		}
		defer func() { _ = f.Close() }()
		http.ServeContent(w, r, sanitizeArchiveName(fileName), info.ModTime(), f)
		return
	}

	w.Header().Set("Content-Type", mime)
	http.ServeFile(w, r, absPath)
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

	// Compute parent — stop at root level (no parent above root/drives)
	var parent *string
	isAtRoot := false
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
			absPath, ok = model.ValidatePath(baseAbs, p)
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

// DirEntry represents a directory entry in API responses
type DirEntry struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Modified  string `json:"modified,omitempty"`
	Size      int64  `json:"size"`
	Supported bool   `json:"supported"`
	Symlink   bool   `json:"symlink,omitempty"`
	Broken    bool   `json:"broken,omitempty"`
	// Ignored marks an entry git would not track (matched by .gitignore, or
	// sitting under an excluded directory). It is advisory only: ignored entries
	// stay listed and fully operable, the UI just dims them.
	Ignored bool `json:"ignored,omitempty"`
}

// FileContent represents file content in API responses
type FileContent struct {
	Content    string `json:"content"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	Supported  bool   `json:"supported"`
	IsBinary   bool   `json:"isBinary,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
	Size       int64  `json:"size"`
	Subtype    string `json:"subtype,omitempty"`
	SpecJSON   string `json:"specJson,omitempty"`
	LinkTarget string `json:"linkTarget,omitempty"`
	IsSymlink  bool   `json:"isSymlink,omitempty"`

	// TooLarge marks a metadata-only response: the file exceeds the inline cap
	// so Content is empty and the caller must fall back to a raw/download
	// endpoint. Currently emitted only by the public share file endpoint, which
	// has no line-window path to fall back on. (GetFile instead rejects with
	// FileTooLarge, because it has the line-window preview as an alternative.)
	TooLarge bool `json:"tooLarge,omitempty"`

	// Line-window metadata, present only on ?lineStart/?lineEnd responses.
	// TotalLines is the file's full line count (so the preview can compute how
	// much context remains above/below), while WindowStart/WindowEnd bound the
	// returned Content (1-based inclusive; WindowEnd < WindowStart means no lines
	// were captured, e.g. the requested range starts past EOF).
	TotalLines  int `json:"totalLines,omitempty"`
	WindowStart int `json:"windowStart,omitempty"`
	// WindowEnd is deliberately NOT omitempty: "no lines captured" is encoded as
	// WindowEnd == WindowStart-1, which is 0 when the window starts at line 1.
	// Omitting that 0 would make an empty window indistinguishable from a
	// response that carried no window metadata at all.
	WindowEnd       int  `json:"windowEnd"`
	WindowTruncated bool `json:"windowTruncated,omitempty"`
}

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
// Returns a "dir" entry when the target is a directory within the allowed base;
// otherwise a non-navigable file entry (dangling or escaping targets stay
// listed but cannot be entered). Always sets Symlink, and Broken for dangling.
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

	if target.IsDir() && within(fullPath) {
		return DirEntry{Name: name, Type: "dir", Symlink: true, Modified: target.ModTime().Format(time.RFC3339)}
	}

	// File symlink, or directory symlink escaping the allowed base (shown as a
	// non-navigable file, consistent with copy/archive handling).
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
	// binarySniffSize is how many bytes to inspect for null-byte detection.
	binarySniffSize = 8192
	// maxForceTextSize is the maximum bytes to return for large text files.
	maxForceTextSize = 512 * 1024 // 512KB
	// maxBinaryTextSize is the maximum bytes to return for binary files
	// opened as text (detected via null-byte sniffing).
	maxBinaryTextSize = 64 * 1024 // 64KB
)

// sanitizeTextContent prepares raw file bytes for text display.
// For binary files (detected via null-byte sniffing in the first 8KB):
// - Truncates to maxBinaryTextSize
// - Replaces non-printable characters with '.'
// For large text files:
// - Truncates to maxForceTextSize at a UTF-8 boundary
// Returns the sanitized content and whether truncation occurred.
func sanitizeTextContent(data []byte) ([]byte, bool) {
	if len(data) == 0 {
		return data, false
	}

	if hasBinaryContent(data) {
		return sanitizeBinaryContent(data)
	}

	if len(data) > maxForceTextSize {
		return truncateAtUTF8Boundary(data), true
	}

	return data, false
}

// hasBinaryContent checks the first 8KB for null bytes to detect binary content.
func hasBinaryContent(data []byte) bool {
	sniffEnd := len(data)
	if sniffEnd > binarySniffSize {
		sniffEnd = binarySniffSize
	}
	for i := range sniffEnd {
		if data[i] == 0 {
			return true
		}
	}
	return false
}

// sanitizeBinaryContent truncates binary content and replaces non-printable
// characters with '.', keeping \n, \r, \t, printable ASCII, and high bytes.
func sanitizeBinaryContent(data []byte) ([]byte, bool) {
	truncated := false
	if len(data) > maxBinaryTextSize {
		data = data[:maxBinaryTextSize]
		truncated = true
	}
	out := make([]byte, len(data))
	for i, b := range data {
		if b == '\n' || b == '\r' || b == '\t' || (b >= 0x20 && b < 0x7F) || b >= 0x80 {
			out[i] = b
		} else {
			out[i] = '.'
		}
	}
	return out, truncated
}

// truncateAtUTF8Boundary truncates data at maxForceTextSize, stepping back to
// avoid splitting a multi-byte UTF-8 character.
func truncateAtUTF8Boundary(data []byte) []byte {
	cut := maxForceTextSize
	for cut > 0 && cut < len(data) && !utf8.RuneStart(data[cut]) {
		cut--
	}
	return data[:cut]
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
