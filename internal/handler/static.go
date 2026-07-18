package handler

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"clawbench/internal/frontend"
)

// hashedAssetExts lists extensions for Vite hash-named assets that are
// safe to cache aggressively (immutable — hash changes when content changes).
//
//nolint:goconst // ".png" appears in multiple unrelated string maps; extracting is overkill
var hashedAssetExts = map[string]bool{
	".js": true, ".css": true, ".mjs": true,
	".woff2": true, ".woff": true, ".ttf": true,
	".ico": true, ".png": true, ".svg": true, ".webp": true,
}

// isHashedAsset returns true if the filename follows Vite's hash-naming pattern
// (e.g. "index-CaOuUlWb.js", "pdf-D-oSvAqu.js", "index-C_GAucyY.css").
// These assets are immutable — the hash changes when content changes.
func isHashedAsset(name string) bool {
	base := name
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	ext, hash := splitHashedName(base)
	if ext == "" {
		return false
	}
	return isValidViteHash(hash) && hashedAssetExts[ext]
}

// splitHashedName splits "name-HASH.ext" into (".ext", "HASH").
// Returns ("", "") if the pattern doesn't match.
func splitHashedName(base string) (ext, hash string) {
	for i := len(base) - 1; i >= 0; i-- {
		if base[i] == '.' {
			ext = base[i:]
			name := base[:i]
			dashIdx := strings.LastIndex(name, "-")
			if dashIdx < 0 || dashIdx == len(name)-1 {
				return "", ""
			}
			hash = name[dashIdx+1:]
			return ext, hash
		}
	}
	return "", ""
}

// isValidViteHash checks that the hash portion is 6+ alphanumeric/underscore chars
// (Vite uses 8 alphanumeric chars by default).
func isValidViteHash(hash string) bool {
	if len(hash) < 6 {
		return false
	}
	for _, c := range hash {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

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

	urlPath := r.URL.Path

	// ISS-055: Clean the path to prevent path traversal (e.g. /../etc/passwd)
	// Use path.Clean (not filepath.Clean) to keep forward slashes —
	// fs.FS.Open requires "/" separators on all platforms including Windows.
	urlPath = path.Clean(urlPath)

	fsys := frontend.GetFS()

	// Serve index for root — auth is handled by the Vue app itself
	if urlPath == "/" || urlPath == "." {
		// index.html must never be strongly cached — it references hash-named
		// assets that change on every build. A stale index.html pointing to
		// outdated chunk hashes causes "Failed to fetch dynamically imported
		// module" errors.
		w.Header().Set("Cache-Control", "no-cache")
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
	cleanRelPath := strings.TrimPrefix(urlPath, "/")

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
		// Vite outputs hash-named assets (e.g. pdf-D-oSvAqu.js, index-CaOuUlWb.js).
		// The hash in the filename changes when content changes, making them
		// immutable — safe to cache aggressively for 1 year.
		if isHashedAsset(cleanRelPath) {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		frontend.ServeFileFromFS(w, r, fsys, cleanRelPath)
		return
	}

	// For /css/* paths, also try web/css/ (dev mode fallback)
	if strings.HasPrefix(urlPath, "/css/") {
		fallback := filepath.Join("web", urlPath)
		if _, err := os.Stat(fallback); err == nil {
			http.ServeFile(w, r, fallback)
			return
		}
	}

	http.NotFound(w, r)
}
