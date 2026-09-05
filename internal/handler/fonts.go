package handler

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"clawbench/internal/model"
)

// fontMimeTypes maps font file extensions to their MIME type for font serving.
var fontMimeTypes = map[string]string{
	".woff2": "font/woff2",
	".woff":  "font/woff",
	".ttf":   "font/ttf",
	".otf":   "font/otf",
	".eot":   "application/vnd.ms-fontobject",
}

// fontListResponse is the JSON payload of GET /api/fonts/list.
type fontListResponse struct {
	Dir   string           `json:"dir"` // Resolved custom font directory
	Fonts []model.FontFile `json:"fonts"`
}

// ServeFontsList handles GET /api/fonts/list: it resolves the configured
// custom font directory (creating it on first access so users can drop font
// files in) and returns the scanned font files.
func ServeFontsList(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	configMutex.RLock()
	cfg := model.ConfigInstance
	configMutex.RUnlock()

	dir := cfg.ResolveFontsDir()
	if dir == "" {
		// DataDir unset — should never happen in a running server.
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidPath")
		return
	}

	// Auto-create the directory so the very first "drop a font in" works
	// without the user manually creating it.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeLocalizedError(w, r, model.Internal(fmt.Errorf("cannot create font dir: %w", err)))
		return
	}

	files := model.ListFontFiles(dir)
	if files == nil {
		files = []model.FontFile{}
	}
	writeJSON(w, http.StatusOK, fontListResponse{Dir: dir, Fonts: files})
}

// ServeFontFile handles GET /api/fonts/file?name=<file>: streams a single font
// file from the configured custom font directory. The name must be a bare file
// name (no path separators / traversal), guaranteeing the response can never
// escape the font directory.
func ServeFontFile(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" || name != filepath.Base(name) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidPath")
		return
	}
	if strings.ContainsAny(name, "/\\") {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidPath")
		return
	}

	configMutex.RLock()
	cfg := model.ConfigInstance
	configMutex.RUnlock()

	dir := cfg.ResolveFontsDir()
	if dir == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidPath")
		return
	}

	absPath := filepath.Join(dir, name)
	info, err := os.Stat(absPath)
	if err != nil {
		writeLocalizedError(w, r, model.NotFound(nil, "FileNotFoundShort"))
		return
	}
	if info.IsDir() {
		writeLocalizedError(w, r, model.NotFound(nil, "FileNotFoundShort"))
		return
	}

	ext := strings.ToLower(filepath.Ext(name))
	mime := fontMimeTypes[ext]
	if mime == "" {
		mime = mimeOctetStream
	}

	f, err := os.Open(absPath)
	if err != nil {
		writeLocalizedError(w, r, model.Internal(fmt.Errorf("cannot open font file")))
		return
	}
	defer func() { _ = f.Close() }()

	w.Header().Set("Content-Type", mime)
	http.ServeContent(w, r, name, info.ModTime(), f)
}
