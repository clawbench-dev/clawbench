//nolint:goconst // response field names are domain strings
package handler

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"clawbench/internal/model"
	"clawbench/internal/wallpaper"
)

// wallpaperBaseName is the fixed file name (without extension) of the legacy
// single-file wallpaper in <DataDir>/theme. Processing limits live in the
// shared wallpaper package.
const wallpaperBaseName = "background"

// themeBackgroundSetRequest is the JSON body for the path-copy mode of
// POST /api/theme-background.
type themeBackgroundSetRequest struct {
	Path string `json:"path"`
}

// themeBackgroundSetResponse is the JSON success body of POST /api/theme-background.
type themeBackgroundSetResponse struct {
	File string `json:"file"`
}

// ServeThemeBackground handles POST (set) and DELETE (clear) /api/theme-background.
//
// This is the legacy single-file wallpaper endpoint, retained for backward
// compatibility (e.g. the file viewer's "set as theme background" action). New
// clients use the gallery endpoints in theme_gallery.go.
func ServeThemeBackground(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		serveThemeBackgroundSet(w, r)
	case http.MethodDelete:
		serveThemeBackgroundClear(w, r)
	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

// serveThemeBackgroundSet handles POST /api/theme-background — sets the custom
// wallpaper from either a server file (path-copy mode) or an uploaded file
// (multipart mode). Writes the processed image into <DataDir>/theme as
// background.<ext> and records the file name in config (appearance.wallpaper_file).
func serveThemeBackgroundSet(w http.ResponseWriter, r *http.Request) {
	srcBytes, srcName, handled := readWallpaperSource(w, r)
	if handled {
		return
	}

	// Resolve the intended target file + process (validate, downscale, encode).
	processed, err := wallpaper.Process(srcBytes, srcName)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidImage", map[string]any{"Error": err.Error()})
		return
	}
	fileName := wallpaperBaseName + processed.Ext

	// Writes the processed file atomically and records it in config. On any
	// failure the previous wallpaper file + config entry remain intact.
	if err := writeWallpaperFile(processed.Data, processed.Ext, fileName); err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "WriteFailed", map[string]any{"Error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, themeBackgroundSetResponse{File: fileName})
}

// readWallpaperSource acquires the wallpaper bytes + source file name from the
// request: path-copy mode (JSON body {path}) or multipart upload. On success it
// returns handled=false and the caller proceeds; on any error it writes the
// response and returns handled=true.
func readWallpaperSource(w http.ResponseWriter, r *http.Request) (srcBytes []byte, srcName string, handled bool) {
	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/json") {
		var req themeBackgroundSetRequest
		if !decodeJSON(w, r, &req) {
			return nil, "", true
		}
		if req.Path == "" {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
			return nil, "", true
		}
		// Path-copy mode: read the server-side file. resolveAbsPath allows any
		// absolute path under a root (Linux/macOS root = "/") as well as
		// project-relative paths — the same permission domain the file viewer
		// uses to open external absolute-path files.
		absPath, ok := resolveAbsPath(w, r, req.Path)
		if !ok {
			return nil, "", true
		}
		info, err := os.Stat(absPath)
		if err != nil || info.IsDir() {
			writeLocalizedError(w, r, model.NotFound(nil, "FileNotFoundShort"))
			return nil, "", true
		}
		// Read with a size guard so a huge source can't be slurped before the
		// per-format limit is applied below.
		if info.Size() > wallpaper.MaxBytes {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "FileTooLargeShort")
			return nil, "", true
		}
		srcBytes, err = os.ReadFile(absPath)
		if err != nil {
			writeLocalizedError(w, r, model.Internal(fmt.Errorf("cannot read source file")))
			return nil, "", true
		}
		return srcBytes, filepath.Base(absPath), false
	}

	// Multipart mode: read the uploaded file.
	r.Body = http.MaxBytesReader(w, r.Body, wallpaper.MaxBytes+1<<20)        // +1MB multipart overhead
	if err := r.ParseMultipartForm(wallpaper.MaxBytes + 1<<20); err != nil { //nolint:gosec // MaxBytesReader limits parsed size
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "FileTooLargeOrInvalid")
		return nil, "", true
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "NoFileProvided")
		return nil, "", true
	}
	defer func() { _ = file.Close() }()

	if header.Size > wallpaper.MaxBytes {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "FileTooLargeShort")
		return nil, "", true
	}
	buf, err := io.ReadAll(io.LimitReader(file, wallpaper.MaxBytes+1))
	if err != nil || int64(len(buf)) > wallpaper.MaxBytes {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "FileTooLargeShort")
		return nil, "", true
	}
	return buf, header.Filename, false
}

// writeWallpaperFile atomically writes the legacy single-file wallpaper into the
// theme dir, persists its file name in config, and only then removes stale
// old-extension wallpaper files.
//
// The whole sequence runs under the wallpaper write lock: the stale-file sweep
// deletes every background.* except the one just written, so two concurrent
// sets would otherwise delete each other's file and leave config pointing at
// a missing image.
func writeWallpaperFile(data []byte, ext, fileName string) error {
	themeDir := wallpaper.ThemeDir()
	if themeDir == "" {
		return fmt.Errorf("theme directory unavailable")
	}

	return wallpaper.WithWriteLock(func() error {
		// Atomic write first, so a config-write failure leaves the previous
		// wallpaper file + its config entry fully intact (only the brand-new
		// file is removed on rollback below), and a successful set never 404s
		// in the interim. Config is updated BEFORE any stale file cleanup.
		if err := wallpaper.WriteAtomicWithinLock(themeDir, fileName, data); err != nil {
			return err
		}
		finalPath := filepath.Join(themeDir, fileName)

		// Persist the file name in config (empty write removes nothing else).
		if err := setConfigWallpaperFile(wallpaperBaseName + ext); err != nil {
			// Config write failed — remove the brand-new file. The old wallpaper
			// file + its config entry were left untouched (no cleanup ran yet),
			// so the previous background remains active.
			_ = os.Remove(finalPath)
			return err
		}

		// Config now names the new file — safe to remove stale old-extension files.
		removeStaleWallpapers(themeDir, fileName)
		return nil
	})
}

// serveThemeBackgroundClear handles DELETE /api/theme-background — removes the
// legacy single-file wallpaper: clears appearance.wallpaper_file in config and
// deletes all background.* files in <DataDir>/theme.
func serveThemeBackgroundClear(w http.ResponseWriter, r *http.Request) {
	themeDir := wallpaper.ThemeDir()
	// Hold the write lock across config-clear + file sweep so a concurrent set
	// cannot interleave (which would leave config cleared but a fresh file
	// deleted, or vice versa).
	err := wallpaper.WithWriteLock(func() error {
		if err := setConfigWallpaperFile(""); err != nil {
			return err
		}
		if themeDir != "" {
			removeStaleWallpapers(themeDir, "")
		}
		return nil
	})
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "WriteFailed", map[string]any{"Error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ServeThemeBackgroundGet handles GET /api/file/theme-background — streams the
// active wallpaper. The active file is resolved from the wallpaper mode and
// enabled switch, so this endpoint reflects the gallery/Bing selection too.
func ServeThemeBackgroundGet(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet, http.MethodHead) {
		return
	}

	configMutex.RLock()
	cfg := model.ConfigInstance
	configMutex.RUnlock()

	name, ok := wallpaper.ResolveActive(&cfg)
	if !ok {
		writeLocalizedError(w, r, model.NotFound(nil, "FileNotFoundShort"))
		return
	}
	serveWallpaperByName(w, r, name)
}

// serveWallpaperByName streams a wallpaper file identified by a bare name,
// applying strict containment and per-format cache/security headers. A name
// that is malformed, escapes its directory, or does not exist yields 404.
func serveWallpaperByName(w http.ResponseWriter, r *http.Request, name string) {
	absPath, ok := wallpaper.FilePath(name)
	if !ok {
		writeLocalizedError(w, r, model.NotFound(nil, "FileNotFoundShort"))
		return
	}

	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		writeLocalizedError(w, r, model.NotFound(nil, "FileNotFoundShort"))
		return
	}

	ext := strings.ToLower(filepath.Ext(name))
	mime := wallpaper.MimeFor(ext)
	if mime == "" {
		mime = mimeOctetStream
	}
	w.Header().Set("Content-Type", mime)
	// nosniff: never let a browser content-sniff a wallpaper into HTML/SVG.
	// The wallpaper is stored on the authenticated server origin, so sniffed
	// active content would be a session-level XSS vector.
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if ext == ".svg" {
		// SVG is re-validated on every serve against the FULL file content
		// (SVG is capped at 1MB at write time, so a full scan is cheap) and
		// served under a sandbox CSP that disables scripts and external fetches.
		svgBytes, readErr := os.ReadFile(absPath)
		if readErr != nil || !wallpaper.SVGLooksSafe(svgBytes) {
			writeLocalizedError(w, r, model.NotFound(nil, "FileNotFoundShort"))
			return
		}
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
		// Do not cache SVG wallpapers — content checks are cheap and the
		// response is not safe for shared caches.
		w.Header().Set("Cache-Control", "no-store")
	} else {
		// Raster wallpapers are immutable (a new upload replaces the file under
		// a versioned query string), so they can be cached aggressively.
		etag := fmt.Sprintf(`"%x-%x"`, info.ModTime().UnixNano(), info.Size())
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	}

	f, err := os.Open(absPath)
	if err != nil {
		writeLocalizedError(w, r, model.NotFound(nil, "FileNotFoundShort"))
		return
	}
	defer func() { _ = f.Close() }()
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// setConfigWallpaperFile updates appearance.wallpaper_file in the in-memory
// config AND persists it to config.yaml. On a disk-write failure the whole
// in-memory ConfigInstance is restored to its snapshot so config never diverges
// from disk.
func setConfigWallpaperFile(fileName string) error {
	configMutex.Lock()
	defer configMutex.Unlock()

	snapshot := model.ConfigInstance
	model.ConfigInstance.Appearance.WallpaperFile = fileName
	patch := map[string]any{
		"appearance": map[string]any{
			"wallpaper_file": fileName,
		},
	}
	if err := writeConfigYAML(patch); err != nil {
		// Disk write failed — restore the full in-memory snapshot so config
		// (and therefore the active wallpaper file name) never diverges.
		model.ConfigInstance = snapshot
		return err
	}
	return nil
}

// PersistBingWallpaperState records the Bing fetch worker's outcome in config.
// It is injected into the worker via service.SetPersistBingStateFn so the
// service package never has to import this one.
//
// On a disk-write failure the in-memory config is restored, so the worker's
// view of what is cached never diverges from disk.
func PersistBingWallpaperState(s wallpaper.BingState) error {
	configMutex.Lock()
	defer configMutex.Unlock()

	snapshot := model.ConfigInstance
	bing := &model.ConfigInstance.Appearance.Bing
	// A failure reports no File; only the error/attempt fields should change, so
	// the cached image and its attribution stay intact.
	if s.File != "" {
		bing.File = s.File
		bing.LastSuccessDate = s.LastSuccessDate
		bing.Copyright = s.Copyright
		bing.Title = s.Title
	}
	if s.Mkt != "" {
		bing.Mkt = s.Mkt
	}
	bing.LastError = s.LastError
	bing.LastAttemptAt = s.LastAttemptAt

	patch := map[string]any{
		"appearance": map[string]any{
			"bing": map[string]any{
				"file":              bing.File,
				"last_success_date": bing.LastSuccessDate,
				"copyright":         bing.Copyright,
				"title":             bing.Title,
				"mkt":               bing.Mkt,
				"last_error":        bing.LastError,
				"last_attempt_at":   bing.LastAttemptAt,
			},
		},
	}
	if err := writeConfigYAML(patch); err != nil {
		model.ConfigInstance = snapshot
		return err
	}
	return nil
}

// removeStaleWallpapers deletes every background.* file in dir except keepFile.
// Called after a successful rename (so the active file is never momentarily
// absent) and on clear (keepFile == "" removes everything).
func removeStaleWallpapers(dir, keepFile string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, wallpaperBaseName+".") {
			continue
		}
		if name == keepFile {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
}
