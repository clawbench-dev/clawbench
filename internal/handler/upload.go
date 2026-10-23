//nolint:goconst,govet // JSON response field names are domain strings; shadowed err is acceptable in sequential blocks
package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "UploadSaveFailed", map[string]any{"Error": err.Error()})
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
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "UploadSaveFailed", map[string]any{"Error": err.Error()})
		return
	}
	defer func() { _ = dst.Close() }()

	// Copy file content
	if _, err := io.Copy(dst, file); err != nil {
		_ = os.Remove(dstPath)
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "UploadSaveFailed", map[string]any{"Error": err.Error()})
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

	// Remember uploads that land OUTSIDE .clawbench/uploads/ (file-manager
	// uploads into the project tree). "最近上传" lists the uploads directory by
	// scanning it, which cannot see arbitrary project paths — without this
	// record a screenshot uploaded to the project root never shows up there
	// (issue #573). Files inside the uploads directory are skipped by the
	// recorder itself, so they are not listed twice.
	recordUploadedFile(projectPath, relativePath)

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
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "UploadSaveFailed", map[string]any{"Error": err.Error()})
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
	// Origin tells the client what a delete on this row means. "uploads" is a
	// file the app owns inside .clawbench/uploads/ (delete removes it from
	// disk); "project" is an ordinary project file recorded by the upload
	// ledger, where delete only stops listing it. Empty for the share-in list,
	// which has a single origin.
	Origin string `json:"origin,omitempty"`
}

// Upload origins reported in recentFile.Origin.
const (
	uploadOriginUploads = "uploads"
	uploadOriginProject = "project"
)

// listRecentFiles lists up to limit files in dirPath (under projectPath),
// sorted by modification time descending. Returns empty slice if dir doesn't exist.
func listRecentFiles(projectPath, dirPath string, limit int) []recentFile {
	fullDir := filepath.Join(projectPath, dirPath)

	entries, err := os.ReadDir(fullDir)
	if err != nil {
		return []recentFile{}
	}

	// Non-nil empty slice so the JSON body is `[]` rather than `null` when the
	// directory exists but is empty. A `null` body makes the frontend
	// AttachDrawer crash on `recentShares.length` when the Shares/Uploads tab is
	// clicked.
	files := make([]recentFile, 0)
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
		// All clears the whole scoped directory instead of one file. It is a
		// separate flag rather than "empty path means all" so a client that
		// forgets the field can never wipe the directory by accident.
		All bool `json:"all"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.All {
		clearAllRecent(w, projectPath, subDir)
		return
	}
	if req.Path == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MissingPath")
		return
	}

	// A ledger row is an ordinary project file; deleting it only un-lists it.
	if subDir == "uploads" && deleteLedgerEntry(w, r, projectPath, req.Path) {
		return
	}

	absPath, done := resolveRecentDeleteTarget(w, r, projectPath, subDir, req.Path)
	if done {
		return
	}

	if err := os.Remove(absPath); err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("delete failed: %w", err)))
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// resolveRecentDeleteTarget resolves a DELETE path to the file to remove and
// verifies it lives inside .clawbench/<subDir>. done=true means a response was
// already written and the caller must return.
//
// Unlike resolveAbsPath, a non-existent path is not rejected here so the caller
// can answer a proper 404 (which is what a stale row means).
func resolveRecentDeleteTarget(w http.ResponseWriter, r *http.Request, projectPath, subDir, reqPath string) (absPath string, done bool) {
	var resolved string
	if filepath.IsAbs(reqPath) {
		ap, err := filepath.Abs(reqPath)
		if err != nil || !isPathUnderAnyRoot(ap) {
			writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
			return "", true
		}
		resolved = ap
	} else {
		baseAbs, err := filepath.Abs(projectPath)
		if err != nil {
			model.WriteError(w, model.Internal(fmt.Errorf("failed to resolve project path: %w", err)))
			return "", true
		}
		validated, ok := validateAndResolvePath(w, r, baseAbs, reqPath)
		if !ok {
			return "", true
		}
		resolved = validated
	}

	// Check existence first so a missing file returns 404 regardless of scope.
	info, err := os.Stat(resolved)
	if err != nil {
		writeLocalizedError(w, r, model.NotFound(nil, "FileNotFoundShort"))
		return "", true
	}
	if info.IsDir() {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "NotAFile")
		return "", true
	}

	// Scope check: the file must live inside .clawbench/<subDir>.
	baseDir := filepath.Join(projectPath, ".clawbench", subDir)
	if !isPathUnderBase(resolved, baseDir) {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return "", true
	}
	return resolved, false
}

// JSON field names for the clear-all response, shared by both endpoints.
const (
	fieldDeleted = "deleted"
	fieldFailed  = "failed"
)

// clearAllRecent handles the `{all:true}` branch of a recent-list DELETE.
//
// The "最近上传" list is a merge of the uploads directory and the upload ledger
// (which records file-manager uploads into the project tree), so clearing it
// must empty both. The ledger only holds references to ordinary project files,
// so those are forgotten without deleting anything from the user's tree —
// while the uploads directory, which the app owns, is emptied for real.
//
// The directory is cleared FIRST: it is the half that can fail for reasons
// outside our control (a permission change, an unmounted volume), and doing it
// after the ledger would leave the user staring at a "failed" toast with the
// project rows already gone — a retry could not bring them back.
//
// The reported count is what the user watched disappear from the tab, so the
// ledger rows count too: counting only the directory would answer "cleared 0
// files" for a list that visibly emptied.
func clearAllRecent(w http.ResponseWriter, projectPath, subDir string) {
	deleted, failed, err := clearRecentDir(projectPath, subDir)
	if err != nil {
		model.WriteError(w, model.Internal(err))
		return
	}
	if subDir != "uploads" {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":         true,
			fieldDeleted: deleted,
			fieldFailed:  failed,
		})
		return
	}

	ledgerCleared, ledgerRemaining, ledgerErr := clearUploadLedger(projectPath)
	if ledgerErr != nil {
		// The directory half already succeeded and cannot be undone. Report the
		// truth: those files are gone, and every ledger row the user can still
		// see is still listed — so `failed` counts the surviving rows, not the
		// single write attempt. A bare "1 failed" would read as one stray file
		// when the whole project half is untouched.
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":         true,
			fieldDeleted: deleted,
			fieldFailed:  failed + ledgerRemaining,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		fieldDeleted: deleted + ledgerCleared,
		fieldFailed:  failed,
	})
}

// clearRecentDir empties .clawbench/<subDir> entirely, returning the number of
// files removed and the number that could not be.
//
// Scope is the directory itself, so the traversal concerns that apply to a
// caller-supplied path do not arise: the target is computed here from the
// project root and the fixed subDir. Sub-directories are left alone — a
// directory upload can hold a whole tree the user still expects, and nothing
// in this UI ever created one under uploads/ or share-in/.
//
// A file that cannot be removed (permissions, in use) is counted and reported
// rather than aborting the sweep: the user asked to clear the list, and
// stopping at the first failure would leave it looking half-cleared with no
// indication why.
//
// Returns err only when the directory could not even be read; the caller
// writes the error. A missing directory is success with zero counts, not an
// error: the UI may hold a stale list from before it was removed.
func clearRecentDir(projectPath, subDir string) (deleted, failed int, err error) {
	dir := filepath.Join(projectPath, ".clawbench", subDir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("read %s: %w", subDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		target := filepath.Join(dir, entry.Name())
		// Re-check containment: ReadDir cannot return a path outside dir, but
		// this is the same guard the single-file delete uses, and a symlinked
		// entry must never be followed out of the directory.
		if !isPathUnderBase(target, dir) {
			failed++
			continue
		}
		if rmErr := os.Remove(target); rmErr != nil {
			if os.IsNotExist(rmErr) {
				continue
			}
			slog.Warn("clear recent dir: failed to remove file",
				slog.String("path", target), slog.String("err", rmErr.Error()))
			failed++
			continue
		}
		deleted++
	}

	return deleted, failed, nil
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
//
// GET returns the 20 most recently modified uploads, merging two sources: the
// chat uploads directory (.clawbench/uploads/, scanned) and the upload ledger
// (.clawbench/upload-log.json, which records uploads that went into the
// project tree through the file manager). Both are "uploads the user just
// made", and the tab exists to get a just-uploaded file into the chat quickly —
// which path it was saved to is not something the user chose to care about.
func UploadRecent(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		deleteRecentFile(w, r, "uploads")
		return
	}

	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	files := listRecentUploads(projectPath, 20)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(files)
}
