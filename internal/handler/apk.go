package handler

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
)

const apkPath = "public/assets/clawbench-android.apk"

// ServeAPK serves the Android APK file for download.
// No authentication required — APK is a public resource.
func ServeAPK(w http.ResponseWriter, r *http.Request) {
	absPath, err := filepath.Abs(apkPath)
	if err != nil {
		slog.Warn("apk: path resolution failed", "err", err)
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		slog.Debug("apk: file not found", "path", absPath)
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(absPath)
	if err != nil {
		slog.Warn("apk: open failed", "path", absPath, "err", err)
		http.NotFound(w, r)
		return
	}
	defer func() {
		if err := f.Close(); err != nil {
			slog.Warn("apk: close failed", "err", err)
		}
	}()

	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", `attachment; filename="clawbench-android.apk"`)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeContent(w, r, "clawbench-android.apk", info.ModTime(), f)
}
