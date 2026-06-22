//nolint:goconst // JSON response field names are domain strings, not config constants
package handler

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"clawbench/internal/model"
	"clawbench/internal/platform"
)

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

	items := buildDirEntries(entries)

	relFromBase, _ := filepath.Rel(basePath, absPath)
	var parent *string
	if relFromBase != "." {
		parentDir := filepath.Dir(relFromBase)
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

// ListFiles returns all files in the project directory recursively.
func ListFiles(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	var files []FileInfo
	err := filepath.Walk(projectPath, func(fullPath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip inaccessible files
		}
		if info.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(projectPath, fullPath)
		if err != nil {
			return nil //nolint:nilerr // skip files with invalid relative paths
		}
		entryType := "file"
		if model.IsImageFile(info.Name()) {
			entryType = "image"
		}
		files = append(files, FileInfo{
			Name:      info.Name(),
			Path:      filepath.ToSlash(relPath),
			Modified:  info.ModTime().Format("2006-01-02T15:04:05Z07:00"),
			Size:      info.Size(),
			Type:      entryType,
			Supported: model.IsSupportedFile(info.Name()),
		})
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Cannot access directory"})
		return
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Name < files[j].Name
	})

	writeJSON(w, http.StatusOK, files)
}

// resolveFilePath determines the absolute path and whether it's external to the
// project. Supports absolute paths via ?path= query param and project-relative
// paths via URL path.
func resolveFilePath(w http.ResponseWriter, r *http.Request, projectPath string) (absPath string, isExternal bool, ok bool) {
	if queryPath := r.URL.Query().Get("path"); queryPath != "" {
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
		return absPath, true, true
	}

	// Project-relative path from URL path
	filepathStr := r.URL.Path
	if !strings.HasPrefix(filepathStr, "/api/file/") {
		http.NotFound(w, r)
		return "", false, false
	}
	filepathStr = filepathStr[len("/api/file/"):]
	// Strip leading slashes to handle double-slash URLs (/api/file//path)
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
func GetFile(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	absPath, isExternal, ok := resolveFilePath(w, r, projectPath)
	if !ok {
		return
	}

	info, err := os.Stat(absPath)
	if err != nil {
		handleStatError(w, r, absPath, err)
		return
	}
	if info.IsDir() {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "NotAFile")
		return
	}

	if info.Size() > 10*1024*1024 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "FileTooLarge")
		return
	}

	// For non-text files, check if the content is actually binary (via null-byte
	// sniffing). If binary, return isBinary=true without the content — the
	// frontend shows a placeholder with "Open as text" button.
	// Use ?forceText=1 to override: returns sanitized content (truncated +
	// non-printable chars replaced) safe for DOM rendering.
	isText := model.IsTextFile(info.Name())
	forceText := r.URL.Query().Get("forceText") == "1"

	if !isText && !forceText {
		isBinary, sniffErr := sniffBinaryContent(absPath)
		if sniffErr != nil {
			model.WriteError(w, model.Internal(fmt.Errorf("cannot open file")))
			return
		}
		if isBinary {
			respPath := responsePath(absPath, projectPath, isExternal)
			writeJSON(w, http.StatusOK, FileContent{
				Content:   "",
				Name:      info.Name(),
				Path:      respPath,
				Supported: false,
				IsBinary:  true,
				Size:      info.Size(),
			})
			return
		}
	}

	content, err := os.ReadFile(absPath)
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

	respPath := responsePath(absPath, projectPath, isExternal)
	writeJSON(w, http.StatusOK, FileContent{
		Content:   string(content),
		Name:      info.Name(),
		Path:      respPath,
		Supported: model.IsSupportedFile(info.Name()),
		Size:      info.Size(),
		Truncated: truncated,
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

// ServeLocalFile serves a file directly (for images, PDFs, etc.).
func ServeLocalFile(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	filepathStr := r.URL.Path
	if !strings.HasPrefix(filepathStr, "/api/local-file/") {
		http.NotFound(w, r)
		return
	}
	filepathStr = filepathStr[len("/api/local-file/"):]
	// Strip leading slashes to handle double-slash URLs (/api/local-file//path)
	// caused by encodeURIComponent("/path") which encodes as %2Fpath.
	filepathStr = strings.TrimLeft(filepathStr, "/")
	filepathStr = path.Clean(filepathStr)

	if filepathStr == ".." || path.IsAbs(filepathStr) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidPath")
		return
	}

	basePath, _ := filepath.Abs(projectPath)
	absPath, ok := validateAndResolvePath(w, r, basePath, filepathStr)
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
		mime = "application/octet-stream"
	}

	// If ?download=1 is present, force download with Content-Disposition header.
	// Use http.ServeContent instead of http.ServeFile to avoid a 301 redirect
	// for files named "index.html" — http.ServeFile treats "index.html" as a
	// directory index and redirects to "./", which changes the URL path to
	// point at the parent directory, triggering a NotADirectory error.
	if r.URL.Query().Get("download") == "1" {
		fileName := sanitizeArchiveName(filepath.Base(absPath))
		w.Header().Set("Content-Disposition", "attachment; filename=\""+fileName+"\"")
		w.Header().Set("Content-Type", mime)
		f, err := os.Open(absPath)
		if err != nil {
			model.WriteError(w, model.Internal(fmt.Errorf("cannot open file")))
			return
		}
		defer func() { _ = f.Close() }()
		http.ServeContent(w, r, fileName, info.ModTime(), f)
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

	items := buildDirEntries(entries)

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
		p := filepath.Dir(absPath)
		parent = &p
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"path":   absPath,
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

// ── File-related DTOs ──────────────────────────────────────────────────────────

// DirEntry represents a directory entry in API responses
type DirEntry struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Modified  string `json:"modified,omitempty"`
	Size      int64  `json:"size"`
	Supported bool   `json:"supported"`
}

// FileInfo represents file information in API responses
type FileInfo struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Modified  string `json:"modified"`
	Size      int64  `json:"size"`
	Type      string `json:"type"`
	Supported bool   `json:"supported"`
}

// FileContent represents file content in API responses
type FileContent struct {
	Content   string `json:"content"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Supported bool   `json:"supported"`
	IsBinary  bool   `json:"isBinary,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Size      int64  `json:"size"`
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

func buildDirEntries(entries []os.DirEntry) []DirEntry {
	var items []DirEntry
	for _, entry := range entries {
		// Try to get file info with a timeout to avoid blocking on
		// unresponsive network mounts (e.g. NFS hard mounts).
		info, infoErr := fileInfoWithTimeout(entry)
		if infoErr != nil {
			// Timeout or error — use DirEntry.IsDir() as a fallback
			// so the entry still appears in the listing (without size/modTime).
			slog.Warn("failed to get file info, using fallback", slog.String("name", entry.Name()), slog.String("err", infoErr.Error()))
			if entry.IsDir() {
				items = append(items, DirEntry{Name: entry.Name(), Type: "dir"})
			} else {
				name := entry.Name()
				entryType := "file"
				if model.IsImageFile(name) {
					entryType = "image"
				}
				items = append(items, DirEntry{
					Name:      name,
					Type:      entryType,
					Supported: model.IsSupportedFile(name),
				})
			}
			continue
		}
		if entry.IsDir() {
			modified := info.ModTime().Format(time.RFC3339)
			items = append(items, DirEntry{Name: entry.Name(), Type: "dir", Modified: modified})
		} else {
			name := entry.Name()
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
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "path": newDirAbs})
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
