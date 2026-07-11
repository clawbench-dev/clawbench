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

	// Validate file extension — all file types are allowed.
	// This is intentional: users upload code, configs, binaries, and arbitrary
	// project files. We only require a non-empty extension so the file can be
	// identified and served correctly. Content safety is enforced by the
	// downstream consumer (AI agent / file viewer), not the upload endpoint.
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "FileMustHaveExtension")
		return
	}

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

	// Generate filename: use original name, append sequential number if exists
	baseName := filepath.Base(header.Filename)
	nameWithoutExt := strings.TrimSuffix(baseName, filepath.Ext(baseName))
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

// ShareInRecent handles GET /api/share-in/recent
// Returns the 20 most recently modified files in .clawbench/share-in/.
func ShareInRecent(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	files := listRecentFiles(projectPath, filepath.Join(".clawbench", "share-in"), 20)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(files)
}

// UploadRecent handles GET /api/upload/recent
// Returns the 20 most recently modified files in .clawbench/uploads/.
func UploadRecent(w http.ResponseWriter, r *http.Request) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	files := listRecentFiles(projectPath, filepath.Join(".clawbench", "uploads"), 20)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(files)
}
