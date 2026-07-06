package frontend

import (
	"embed"
	"io/fs"
	"log/slog"
	"os"
)

//go:embed all:dist
var embeddedFS embed.FS

// distFS is the embedded frontend with the "dist/" prefix stripped,
// so files are accessible at root level (e.g. "index.html", "assets/favicon.png").
var distFS, _ = fs.Sub(embeddedFS, "dist")

// GetFS returns the appropriate filesystem for serving frontend assets.
// Priority: disk public/ dir (if exists) > embedded dist/ content.
// This allows hot-swapping frontend files on disk without recompiling,
// while the embedded content serves as a fallback for single-binary deployment.
func GetFS() fs.FS {
	if fi, err := os.Stat("public"); err == nil && fi.IsDir() {
		slog.Info("frontend: serving from disk", slog.String("dir", "public/"))
		return os.DirFS("public")
	}
	slog.Info("frontend: serving from embedded binary")
	return distFS
}

// DiskPublicExists returns true if the public/ directory exists on disk.
// Used to determine whether ISS-055 path traversal guards are needed
// (embed.FS is inherently safe against traversal).
func DiskPublicExists() bool {
	fi, err := os.Stat("public")
	return err == nil && fi.IsDir()
}

// ModeLabel returns a human-readable label for the current frontend serving mode.
func ModeLabel() string {
	if DiskPublicExists() {
		return "disk (public/)"
	}
	return "embedded"
}
