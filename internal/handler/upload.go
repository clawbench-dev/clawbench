//nolint:goconst,govet // JSON response field names are domain strings; shadowed err is acceptable in sequential blocks
package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"clawbench/internal/model"
)

// maxUploadSize returns the maximum allowed upload size in bytes from config.
func maxUploadSize() int64 {
	mb := model.UploadMaxSizeMB
	if mb <= 0 {
		mb = 10
	}
	return int64(mb) * 1024 * 1024
}

// UploadFile handles POST /api/upload/file
// Accepts an optional "dir" form field. When provided, the file is saved to
// that directory (validated and resolved against the project root). When
// omitted, the file is saved to .clawbench/uploads/ (chat attachment flow).
func UploadFile(w http.ResponseWriter, r *http.Request) { //nolint:gocyclo // multi-path upload handler
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	if r.Method != http.MethodPost {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}

	// Limit request body size
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize())

	// Parse multipart form
	if err := r.ParseMultipartForm(maxUploadSize()); err != nil { //nolint:gosec // MaxBytesReader limits parsed size
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "FileTooLargeOrInvalid")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "NoFileProvided")
		return
	}
	defer func() { _ = file.Close() }()

	// No extension validation — all file types are allowed.
	// This is intentional: users upload code, configs, binaries, and arbitrary
	// project files, including extensionless files common in source trees
	// (LICENSE, Makefile, .env). Content safety is enforced by the downstream
	// consumer (AI agent / file viewer), not the upload endpoint.

	// Determine target directory: custom dir or default .clawbench/uploads/
	var targetDir string
	var customDir bool
	dir := r.FormValue("dir")
	if dir != "" {
		// Custom directory: validate and resolve
		customDir = true
		dirAbs, ok := resolveAbsPath(w, r, dir)
		if !ok {
			return
		}
		// Auto-create the target directory if it doesn't exist (same behavior
		// as the default uploads dir). This is necessary for share-in uploads
		// where the directory is created on first use.
		if err := os.MkdirAll(dirAbs, 0o755); err != nil {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "DirectoryNotFound")
			return
		}
		dirInfo, err := os.Stat(dirAbs)
		if err != nil {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "DirectoryNotFound")
			return
		}
		if !dirInfo.IsDir() {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "NotADirectory")
			return
		}
		targetDir = dirAbs
	} else {
		// Default: .clawbench/uploads/ (project-relative, not server DataDir)
		customDir = false
		targetDir = filepath.Join(projectPath, ".clawbench", "uploads")
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			model.WriteError(w, model.Internal(fmt.Errorf("failed to create uploads directory")))
			return
		}
	}

	// Optional relative sub-path for directory uploads (preserves nested folder
	// structure). When present, the file is saved into targetDir/<relpath>/,
	// creating the intermediate directories as needed.
	targetDir = resolveRelPathDir(w, r, targetDir)
	if targetDir == "" {
		return
	}

	// Generate filename: use original name, append sequential number if exists
	baseName := filepath.Base(header.Filename)
	ext := strings.ToLower(filepath.Ext(baseName))
	nameWithoutExt := strings.TrimSuffix(baseName, ext)
	// Replace spaces with underscores for safety
	nameWithoutExt = strings.ReplaceAll(nameWithoutExt, " ", "_")
	filename := nameWithoutExt + ext
	dstPath := filepath.Join(targetDir, filename)
	if _, err := os.Stat(dstPath); err == nil {
		for i := 1; i <= 9999; i++ {
			filename = fmt.Sprintf("%s_%d%s", nameWithoutExt, i, ext)
			dstPath = filepath.Join(targetDir, filename)
			if _, err := os.Stat(dstPath); err != nil {
				break
			}
		}
	}

	// Validate the final destination path is under a root path
	// (defense-in-depth: resolveAbsPath already validated dir, but filepath.Join
	// could theoretically produce unexpected results)
	if !isPathUnderAnyRoot(dstPath) {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	// Create destination file
	dst, err := os.Create(dstPath)
	if err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("failed to create file")))
		return
	}
	defer func() { _ = dst.Close() }()

	// Copy file content
	if _, err := io.Copy(dst, file); err != nil {
		_ = os.Remove(dstPath)
		model.WriteError(w, model.Internal(fmt.Errorf("failed to save file")))
		return
	}

	// Return relative path (always use forward slashes for frontend)
	var relativePath string
	if customDir {
		relPath, err := filepath.Rel(projectPath, dstPath)
		if err != nil {
			relPath = filepath.Join(dir, filename)
		}
		relativePath = filepath.ToSlash(relPath)
	} else {
		relativePath = ".clawbench/uploads/" + filename
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":   true,
		"path": relativePath,
	})
}

// resolveRelPathDir applies the optional "relpath" form value to targetDir,
// creating the nested directory structure it describes. Returns the new target
// directory, or the original targetDir when relpath is absent/empty. On a
// validation or filesystem error it writes the response and returns "".
func resolveRelPathDir(w http.ResponseWriter, r *http.Request, targetDir string) string {
	relRaw := r.FormValue("relpath")
	if relRaw == "" {
		return targetDir
	}
	rel := strings.Trim(filepath.ToSlash(filepath.Clean(relRaw)), "/")
	if rel == "" || rel == "." {
		return targetDir
	}
	// Reject absolute paths and parent-directory traversal.
	if filepath.IsAbs(relRaw) || containsParentTraversal(rel) {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return ""
	}
	subAbs := filepath.Join(targetDir, filepath.FromSlash(rel))
	// Defense-in-depth: the joined path must stay under the target dir.
	if !isPathUnderBase(subAbs, targetDir) {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return ""
	}
	if err := os.MkdirAll(subAbs, 0o755); err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("failed to create upload subdirectory: %w", err)))
		return ""
	}
	return subAbs
}

// containsParentTraversal reports whether a slash-normalized relative path
// contains a ".." segment, which would escape the intended target directory.
func containsParentTraversal(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// recentFile represents a file in an uploads directory.
type recentFile struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"`
}

// listRecentFiles lists up to limit files in dirPath (under projectPath),
// sorted by modification time descending. Returns empty slice if dir doesn't exist.
func listRecentFiles(projectPath, dirPath string, limit int) []recentFile {
	fullDir := filepath.Join(projectPath, dirPath)

	entries, err := os.ReadDir(fullDir)
	if err != nil {
		return []recentFile{}
	}

	var files []recentFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		relPath := filepath.ToSlash(filepath.Join(dirPath, entry.Name()))
		files = append(files, recentFile{
			Name:    entry.Name(),
			Path:    relPath,
			Size:    info.Size(),
			ModTime: info.ModTime().Format(time.RFC3339),
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime > files[j].ModTime
	})

	if len(files) > limit {
		files = files[:limit]
	}

	return files
}

// deleteRecentFile handles DELETE requests scoped to a .clawbench/<subDir>
// directory. It only deletes a regular file that lives inside that directory
// (symlink-safe via isPathUnderBase), preventing the attachment drawer from
// removing arbitrary project files. Writes an error and returns false on
// failure.
func deleteRecentFile(w http.ResponseWriter, r *http.Request, subDir string) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodDelete {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}

	var req struct {
		Path string `json:"path"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Path == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MissingPath")
		return
	}

	// Resolve the path against the project root. Unlike resolveAbsPath, we
	// don't fail on non-existent paths here so we can return a proper 404.
	var absPath string
	if filepath.IsAbs(req.Path) {
		ap, err := filepath.Abs(req.Path)
		if err != nil || !isPathUnderAnyRoot(ap) {
			writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
			return
		}
		absPath = ap
	} else {
		baseAbs, err := filepath.Abs(projectPath)
		if err != nil {
			model.WriteError(w, model.Internal(fmt.Errorf("failed to resolve project path: %w", err)))
			return
		}
		absPath, ok = validateAndResolvePath(w, r, baseAbs, req.Path)
		if !ok {
			return
		}
	}

	// Check existence first so a missing file returns 404 regardless of scope.
	info, err := os.Stat(absPath)
	if err != nil {
		writeLocalizedError(w, r, model.NotFound(nil, "FileNotFoundShort"))
		return
	}
	if info.IsDir() {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "NotAFile")
		return
	}

	// Scope check: the file must live inside .clawbench/<subDir>.
	baseDir := filepath.Join(projectPath, ".clawbench", subDir)
	if !isPathUnderBase(absPath, baseDir) {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	if err := os.Remove(absPath); err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("delete failed: %w", err)))
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ShareInRecent handles GET /api/share-in/recent (list) and
// DELETE /api/share-in/recent (delete a file in .clawbench/share-in/).
// GET returns the 20 most recently modified files in .clawbench/share-in/.
func ShareInRecent(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		deleteRecentFile(w, r, "share-in")
		return
	}

	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	files := listRecentFiles(projectPath, filepath.Join(".clawbench", "share-in"), 20)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(files)
}

// UploadRecent handles GET /api/upload/recent (list) and
// DELETE /api/upload/recent (delete a file in .clawbench/uploads/).
// GET returns the 20 most recently modified files in .clawbench/uploads/.
func UploadRecent(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		deleteRecentFile(w, r, "uploads")
		return
	}

	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	files := listRecentFiles(projectPath, filepath.Join(".clawbench", "uploads"), 20)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(files)
}
