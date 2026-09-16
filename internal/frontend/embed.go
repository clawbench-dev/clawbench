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

// DiskDirName is the CWD-relative directory the frontend build is written to,
// consulted at request time so a rebuild can be hot-swapped without recompiling.
//
// It is deliberately not named "public": this is resolved against the process's
// working directory, and a generic name collides with unrelated directories. On
// macOS the default APFS volume is case-insensitive, so `os.Stat("public")` from
// a home-directory CWD matched the system-provided ~/Public, and the server then
// served that empty directory instead of the embedded frontend — every page 404
// (issue #461). A distinctive name also means build output left behind by older
// installs is never picked up again.
const DiskDirName = ".clawbench-web"

// GetFS returns the appropriate filesystem for serving frontend assets.
// Priority: disk DiskDirName/ dir (if exists) > embedded dist/ content.
// This allows hot-swapping frontend files on disk without recompiling,
// while the embedded content serves as a fallback for single-binary deployment.
func GetFS() fs.FS {
	if DiskDirExists() {
		slog.Info("frontend: serving from disk", slog.String("dir", DiskDirName+"/"))
		return os.DirFS(DiskDirName)
	}
	slog.Info("frontend: serving from embedded binary")
	return distFS
}

// DiskDirExists reports whether the disk build directory exists in the CWD.
// The static handlers use it to apply their ISS-055 traversal guards exactly
// when GetFS() resolves to the disk filesystem (embed.FS is inherently safe
// against traversal).
func DiskDirExists() bool {
	fi, err := os.Stat(DiskDirName)
	return err == nil && fi.IsDir()
}

// EmbeddedFS returns the build-time embedded frontend filesystem (go:embed
// dist/) unconditionally. Unlike GetFS(), it never consults the working
// directory, so build-time artifacts that must match the running binary —
// notably the Android APK — stay consistent no matter where the server was
// started. Callers needing the hot-swappable disk override should use GetFS().
func EmbeddedFS() fs.FS {
	return distFS
}

// ModeLabel returns a human-readable label for the current frontend serving mode.
func ModeLabel() string {
	if DiskDirExists() {
		return "disk (" + DiskDirName + "/)"
	}
	return "embedded"
}
