package frontend

import (
	"embed"
	"io/fs"
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
		return os.DirFS("public")
	}
	return distFS
}

// DiskPublicExists returns true if the public/ directory exists on disk.
// Used to determine whether ISS-055 path traversal guards are needed
// (embed.FS is inherently safe against traversal).
func DiskPublicExists() bool {
	fi, err := os.Stat("public")
	return err == nil && fi.IsDir()
}
