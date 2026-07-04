package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"clawbench/internal/frontend"
)

// ServeProjectDialog serves the project dialog HTML template.
func ServeProjectDialog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}
	tmplPath := filepath.Join("web", "project-dialog.html")
	http.ServeFile(w, r, tmplPath)
}

// ServeIndex serves the main index page and static assets.
func ServeIndex(w http.ResponseWriter, r *http.Request) {
	// Only serve GET/HEAD requests; reject other methods
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}

	path := r.URL.Path

	// ISS-055: Clean the path to prevent path traversal (e.g. /../etc/passwd)
	path = filepath.Clean(path)

	fsys := frontend.GetFS()

	// Serve index for root — auth is handled by the Vue app itself
	if path == "/" || path == "." {
		if fi, err := fsys.Open("index.html"); err == nil {
			_ = fi.Close()
			frontend.ServeFileFromFS(w, r, fsys, "index.html")
			return
		}
		// Dev fallback: serve from web/ directory
		http.ServeFile(w, r, filepath.Join("web", "index.html"))
		return
	}

	// For other paths (e.g. /index-*.css, /index-*.js), serve from frontend FS
	cleanRelPath := strings.TrimPrefix(path, "/")

	// ISS-055: When serving from disk, ensure the cleaned path stays within public/
	if frontend.DiskPublicExists() {
		absPublic, _ := filepath.Abs("public")
		absTarget := filepath.Join("public", cleanRelPath)
		absTarget, _ = filepath.Abs(absTarget)
		if !strings.HasPrefix(absTarget, absPublic+string(filepath.Separator)) && absTarget != absPublic {
			http.NotFound(w, r)
			return
		}
	}

	// Try serving from frontend filesystem (disk public/ or embed)
	if fi, err := fsys.Open(cleanRelPath); err == nil {
		_ = fi.Close()
		frontend.ServeFileFromFS(w, r, fsys, cleanRelPath)
		return
	}

	// For /css/* paths, also try web/css/ (dev mode fallback)
	if strings.HasPrefix(path, "/css/") {
		fallback := filepath.Join("web", path)
		if _, err := os.Stat(fallback); err == nil {
			http.ServeFile(w, r, fallback)
			return
		}
	}

	http.NotFound(w, r)
}
