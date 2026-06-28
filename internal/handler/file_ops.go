//nolint:govet // shadowed err is acceptable in sequential blocks
package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"clawbench/internal/middleware"
	"clawbench/internal/model"
)

// ServeFileRename handles file and directory rename operations.
func ServeFileRename(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var req struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Path == "" || req.Name == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MissingPathOrName")
		return
	}

	absOld, ok := resolveAbsPath(w, r, req.Path)
	if !ok {
		return
	}

	// New path = same directory, new name
	newPath := filepath.Join(filepath.Dir(absOld), req.Name)
	absNew, err := filepath.Abs(newPath)
	if err != nil {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}
	// Validate new path is still under a root path
	if !isPathUnderAnyRoot(absNew) {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	slog.Info("rename attempt", slog.String("old", absOld), slog.String("new", absNew))
	if err := os.Rename(absOld, absNew); err != nil {
		slog.Error("rename failed", slog.String("old", absOld), slog.String("new", absNew), slog.String("err", err.Error()))
		model.WriteError(w, model.Internal(fmt.Errorf("rename failed: %w", err)))
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ServeFileWrite handles writing full file content (used for revert/restore).
func ServeFileWrite(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize())
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Path == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MissingPath")
		return
	}

	absPath, ok := resolveAbsPath(w, r, req.Path)
	if !ok {
		return
	}

	// Atomic write: write to temp file then rename to prevent corruption on crash
	dir := filepath.Dir(absPath)
	tmpFile, err := os.CreateTemp(dir, ".clawbench-write-*")
	if err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("cannot create temp file")))
		return
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.WriteString(req.Content); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		model.WriteError(w, model.Internal(fmt.Errorf("cannot write file")))
		return
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		model.WriteError(w, model.Internal(fmt.Errorf("cannot close temp file")))
		return
	}
	if err := os.Rename(tmpPath, absPath); err != nil {
		_ = os.Remove(tmpPath)
		model.WriteError(w, model.Internal(fmt.Errorf("cannot rename temp file")))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ServeFileDelete handles file and directory deletion.
func ServeFileDelete(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
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

	absPath, ok := resolveAbsPath(w, r, req.Path)
	if !ok {
		return
	}

	info, err := os.Stat(absPath)
	if err != nil {
		slog.Warn("delete: file not found", slog.String("requestPath", req.Path), slog.String("absPath", absPath), slog.String("err", err.Error()))
		writeLocalizedError(w, r, model.NotFound(nil, "FileNotFoundShort"))
		return
	}

	if info.IsDir() {
		if err := safeRemoveAll(absPath); err != nil {
			model.WriteError(w, model.Internal(fmt.Errorf("delete failed: %w", err)))
			return
		}
	} else {
		if err := os.Remove(absPath); err != nil {
			model.WriteError(w, model.Internal(fmt.Errorf("delete failed: %w", err)))
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ServeFileBatchDelete handles deleting multiple files/directories in a single request.
func ServeFileBatchDelete(w http.ResponseWriter, r *http.Request) { //nolint:gocyclo // batch delete with per-item error handling
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

	deleted := 0
	var errs []string
	for _, p := range req.Paths {
		// Resolve each path: absolute validated directly, relative resolved against project cookie
		var absPath string
		if filepath.IsAbs(p) {
			ap, err := filepath.Abs(p)
			if err != nil || !isPathUnderAnyRoot(ap) {
				errs = append(errs, p+": access denied")
				continue
			}
			absPath = ap
		} else {
			projectPath := middleware.GetProjectFromCookie(r)
			if projectPath == "" {
				errs = append(errs, p+": no project")
				continue
			}
			baseAbs, err := filepath.Abs(projectPath)
			if err != nil {
				errs = append(errs, p+": access denied")
				continue
			}
			ap, ok := model.ValidatePath(baseAbs, p)
			if !ok || !isPathUnderAnyRoot(ap) {
				errs = append(errs, p+": access denied")
				continue
			}
			absPath = ap
		}

		info, err := os.Stat(absPath)
		if err != nil {
			errs = append(errs, p+": not found")
			continue
		}
		if info.IsDir() {
			if err := safeRemoveAll(absPath); err != nil {
				errs = append(errs, p+": delete failed: "+err.Error())
				continue
			}
		} else {
			if err := os.Remove(absPath); err != nil {
				errs = append(errs, p+": delete failed: "+err.Error())
				continue
			}
		}
		deleted++
	}

	result := map[string]interface{}{"ok": true, "deleted": deleted} //nolint:goconst // response key
	if len(errs) > 0 {
		result["errors"] = errs
	}
	writeJSON(w, http.StatusOK, result)
}

// ServeFileCreate handles file creation.
func ServeFileCreate(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var req struct {
		Path string `json:"path"` // directory to create in (absolute or relative)
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MissingName")
		return
	}

	absPath := validateCreatePath(w, r, req.Path, req.Name)
	if absPath == "" {
		return
	}

	if _, err := os.Stat(absPath); err == nil {
		writeLocalizedErrorf(w, r, http.StatusConflict, "FileAlreadyExists")
		return
	}

	if err := os.WriteFile(absPath, []byte{}, 0o644); err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("create file failed: %w", err)))
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ServeDirCreate handles directory creation.
func ServeDirCreate(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var req struct {
		Path string `json:"path"` // directory to create in (absolute or relative)
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MissingName")
		return
	}

	absPath := validateCreatePath(w, r, req.Path, req.Name)
	if absPath == "" {
		return
	}

	if err := os.MkdirAll(absPath, 0o755); err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("create directory failed: %w", err)))
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// validateCreatePath validates the path for file/directory creation operations.
// dirPath can be absolute (validated against root paths) or relative (resolved against projectPath).
// Returns the absolute path of the item to create, or empty string on error (response already written).
func validateCreatePath(w http.ResponseWriter, r *http.Request, dirPath, name string) string {
	var absDir string
	if dirPath == "" {
		// No directory specified — use project root from cookie
		projectPath, ok := requireProject(w, r)
		if !ok {
			return ""
		}
		var err error
		absDir, err = filepath.Abs(projectPath)
		if err != nil {
			writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
			return ""
		}
	} else if filepath.IsAbs(dirPath) {
		// Absolute directory path — validate against root paths
		if !isPathUnderAnyRoot(dirPath) {
			writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
			return ""
		}
		absDir = dirPath
	} else {
		// Relative directory path — resolve against projectPath
		projectPath, ok := requireProject(w, r)
		if !ok {
			return ""
		}
		baseAbs, err := filepath.Abs(projectPath)
		if err != nil {
			writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
			return ""
		}
		resolved, ok := model.ValidatePath(baseAbs, dirPath)
		if !ok {
			writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
			return ""
		}
		absDir = resolved
	}

	fullPath := filepath.Join(absDir, name)
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return ""
	}
	// Ensure the final path is under a root path
	if !isPathUnderAnyRoot(absPath) {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return ""
	}
	return absPath
}

// ServeFileMove handles file and directory move operations.
func ServeFileMove(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var req struct {
		Path string `json:"path"`
		Dest string `json:"dest"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Path == "" || req.Dest == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MissingPathOrDest")
		return
	}

	srcAbsPath, ok := resolveAbsPath(w, r, req.Path)
	if !ok {
		return
	}
	destAbsPath, ok := resolveAbsPath(w, r, req.Dest)
	if !ok {
		return
	}

	// Check if source and destination are the same (no-op move).
	if srcAbsPath == destAbsPath {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}

	// Check if destination already exists (ISS-041).
	// os.Rename atomically replaces the destination on Unix, silently
	// destroying the target file. This check prevents accidental data loss.
	if _, err := os.Stat(destAbsPath); err == nil {
		writeLocalizedErrorf(w, r, http.StatusConflict, "FileAlreadyExists")
		return
	}

	if err := os.Rename(srcAbsPath, destAbsPath); err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("move failed: %w", err)))
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ServeFileCopy handles file and directory copy operations.
func ServeFileCopy(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var req struct {
		Path string `json:"path"`
		Dest string `json:"dest"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Path == "" || req.Dest == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MissingPathOrDest")
		return
	}

	srcAbsPath, ok := resolveAbsPath(w, r, req.Path)
	if !ok {
		return
	}
	destAbsPath, ok := resolveAbsPath(w, r, req.Dest)
	if !ok {
		return
	}

	// Check if source and destination are the same (no-op copy).
	if srcAbsPath == destAbsPath {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}

	// Check if destination already exists
	if _, err := os.Stat(destAbsPath); err == nil {
		writeLocalizedErrorf(w, r, http.StatusConflict, "FileAlreadyExists")
		return
	}

	// Copy file or directory
	srcInfo, err := os.Stat(srcAbsPath)
	if err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("source not found: %w", err)))
		return
	}

	if srcInfo.IsDir() {
		err = copyDir(srcAbsPath, destAbsPath)
	} else {
		err = copyFile(srcAbsPath, destAbsPath)
	}

	if err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("copy failed: %w", err)))
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// copyFile copies a single file from src to dst.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = srcFile.Close() }()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = dstFile.Close() }()

	_, err = dstFile.ReadFrom(srcFile)
	return err
}

// copyDir copies a directory recursively from src to dst.
// Symlinks whose targets escape root paths are skipped to prevent data exfiltration.
func copyDir(src, dst string) error { //nolint:gocognit // recursive directory copy
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		// Skip symlinks that escape root paths (prevent data exfiltration via symlink)
		if entry.Type()&os.ModeSymlink != 0 {
			target, linkErr := filepath.EvalSymlinks(srcPath)
			if linkErr != nil || !isPathUnderAnyRoot(target) {
				slog.Warn("copyDir: skip symlink escaping root paths", "path", srcPath)
				continue
			}
			// Symlink target is within root paths — copy the actual file/directory it points to
			targetInfo, statErr := os.Stat(srcPath)
			if statErr != nil {
				continue
			}
			if targetInfo.IsDir() {
				if err := copyDir(srcPath, dstPath); err != nil {
					return err
				}
			} else {
				if err := copyFile(srcPath, dstPath); err != nil {
					return err
				}
			}
			continue
		}

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// safeRemoveAll removes a directory tree without following symlinks that point
// outside root paths. This prevents os.RemoveAll from traversing symlinks and
// deleting files outside the project directory (ISS-048).
func safeRemoveAll(dir string) error {
	// Walk the tree and remove entries bottom-up, skipping symlink targets
	// that escape watchBase. We use a two-pass approach:
	// 1. Walk to collect paths and check symlinks
	// 2. Remove entries from deepest to shallowest
	var paths []string

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}

		// Check if this entry is a symlink pointing outside root paths
		if info.Mode()&os.ModeSymlink != 0 {
			target, linkErr := filepath.EvalSymlinks(path)
			if linkErr == nil && !isPathUnderAnyRoot(target) {
				// Symlink escapes root paths — remove just the symlink, don't follow it
				slog.Warn("safeRemoveAll: symlink escapes root paths, removing symlink only", "path", path, "target", target)
				_ = os.Remove(path) //nolint:gosec // symlink validated above, root-scoped removal
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return err
	}

	// Remove paths from deepest to shallowest (bottom-up)
	for i := len(paths) - 1; i >= 0; i-- {
		p := paths[i]
		if _, err := os.Lstat(p); err != nil {
			continue // already gone
		}
		_ = os.Remove(p) // file or directory
	}
	return nil
}
