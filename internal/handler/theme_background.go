//nolint:goconst // response field names are domain strings
package handler

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"clawbench/internal/model"

	// Register image decoders for image.Decode / image.DecodeConfig (init() side-effects)
	_ "golang.org/x/image/webp"
	_ "image/gif"
)

// Wallpaper path/mime helpers and processing limits.
const (
	// wallpaperBaseName is the fixed file name (without extension) used for the
	// active wallpaper in <DataDir>/theme. Only one wallpaper is kept at a time.
	wallpaperBaseName = "background"

	// wallpaperMaxLongEdge is the target longest-edge pixel size for raster
	// wallpapers (png/jpeg). Larger sources are downscaled to keep multi-device
	// decode cost low while remaining sharp on hi-DPI screens.
	wallpaperMaxLongEdge = 2048

	// wallpaperMaxDimension is the hard upper bound on a source image's
	// width/height. Enforced via image.DecodeConfig BEFORE full decode so a
	// small-but-huge image (decompression bomb) can never allocate megabytes
	// per pixel in RAM.
	wallpaperMaxDimension = 8000

	// wallpaperMaxBytes is the byte-size cap for non-SVG wallpapers.
	wallpaperMaxBytes = 10 * 1024 * 1024

	// wallpaperMaxSVGBytes is the byte-size cap for SVG wallpapers. SVG has no
	// intrinsic pixel size and is rasterized at viewport scale, so oversized
	// files (huge paths, embedded payloads) can stall a WebView render.
	wallpaperMaxSVGBytes = 1 * 1024 * 1024

	// wallpaperJPEGQuality is the encode quality for re-encoded JPEG wallpapers.
	wallpaperJPEGQuality = 88
)

// themeMutex serializes wallpaper file writes so concurrent set operations
// (two tabs, path-copy vs multipart) never interleave rename + old-file cleanup.
var themeMutex sync.Mutex

// wallpaperMimeTypes maps the allowed wallpaper extensions to MIME types.
var wallpaperMimeTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
}

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
	themeMutex.Lock()
	defer themeMutex.Unlock()

	var (
		srcBytes []byte
		srcName  string // original file name (for extension detection)
	)

	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/json") {
		var req themeBackgroundSetRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.Path == "" {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
			return
		}
		// Path-copy mode: read the server-side file. resolveAbsPath allows any
		// absolute path under a root (Linux/macOS root = "/") as well as
		// project-relative paths — the same permission domain the file viewer
		// uses to open external absolute-path files.
		absPath, ok := resolveAbsPath(w, r, req.Path)
		if !ok {
			return
		}
		info, err := os.Stat(absPath)
		if err != nil || info.IsDir() {
			writeLocalizedError(w, r, model.NotFound(nil, "FileNotFoundShort"))
			return
		}
		// Read with a size guard so a huge source can't be slurped before the
		// per-format limit is applied below.
		if info.Size() > wallpaperMaxBytes {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "FileTooLargeShort")
			return
		}
		srcBytes, err = os.ReadFile(absPath)
		if err != nil {
			writeLocalizedError(w, r, model.Internal(fmt.Errorf("cannot read source file")))
			return
		}
		srcName = filepath.Base(absPath)
	} else {
		// Multipart mode: read the uploaded file.
		r.Body = http.MaxBytesReader(w, r.Body, wallpaperMaxBytes+1<<20)        // +1MB multipart overhead
		if err := r.ParseMultipartForm(wallpaperMaxBytes + 1<<20); err != nil { //nolint:gosec // MaxBytesReader limits parsed size
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "FileTooLargeOrInvalid")
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "NoFileProvided")
			return
		}
		defer func() { _ = file.Close() }()

		if header.Size > wallpaperMaxBytes {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "FileTooLargeShort")
			return
		}
		buf, err := io.ReadAll(io.LimitReader(file, wallpaperMaxBytes+1))
		if err != nil || int64(len(buf)) > wallpaperMaxBytes {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "FileTooLargeShort")
			return
		}
		srcBytes = buf
		srcName = header.Filename
	}

	// Resolve the intended target file + process (validate, downscale, encode).
	target, err := processWallpaperSource(srcBytes, srcName)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidImage", map[string]any{"Error": err.Error()})
		return
	}

	themeDir := model.DefaultThemeDir()
	if themeDir == "" {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	if err := os.MkdirAll(themeDir, 0o755); err != nil {
		writeLocalizedError(w, r, model.Internal(fmt.Errorf("cannot create theme directory")))
		return
	}

	// Atomic write: temp file + rename. Config is updated BEFORE any stale
	// file cleanup, so a config-write failure leaves the previous wallpaper
	// file + its config entry fully intact (only the brand-new file is removed
	// on rollback below), and a successful set never 404s in the interim.
	finalPath := filepath.Join(themeDir, target.fileName)
	tmpPath := filepath.Join(themeDir, target.fileName+".tmp")
	if err := os.WriteFile(tmpPath, target.data, 0o644); err != nil {
		_ = os.Remove(tmpPath)
		writeLocalizedError(w, r, model.Internal(fmt.Errorf("failed to write wallpaper")))
		return
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		writeLocalizedError(w, r, model.Internal(fmt.Errorf("failed to write wallpaper")))
		return
	}

	// Persist the file name in config (empty write removes nothing else).
	if err := setConfigWallpaperFile(wallpaperBaseName + target.ext); err != nil {
		// Config write failed — remove the brand-new file. The old wallpaper
		// file + its config entry were left untouched (no cleanup ran yet), so
		// the previous background remains active.
		_ = os.Remove(finalPath)
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "WriteFailed", map[string]any{"Error": err.Error()})
		return
	}

	// Config now names the new file — safe to remove stale old-extension files.
	removeStaleWallpapers(themeDir, target.fileName)

	writeJSON(w, http.StatusOK, themeBackgroundSetResponse{File: wallpaperBaseName + target.ext})
}

// serveThemeBackgroundClear handles DELETE /api/theme-background — removes the
// active wallpaper: clears appearance.wallpaper_file in config and deletes all
// background.* files in <DataDir>/theme.
func serveThemeBackgroundClear(w http.ResponseWriter, r *http.Request) {
	themeMutex.Lock()
	defer themeMutex.Unlock()

	themeDir := model.DefaultThemeDir()
	if err := setConfigWallpaperFile(""); err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "WriteFailed", map[string]any{"Error": err.Error()})
		return
	}
	if themeDir != "" {
		removeStaleWallpapers(themeDir, "")
	}
	w.WriteHeader(http.StatusNoContent)
}

// ServeThemeBackgroundGet handles GET /api/file/theme-background — streams the
// active wallpaper file from <DataDir>/theme. The config value is trusted only
// after containment checks (bare name, extension whitelist, symlink-safe
// under-dir guard) so a corrupted config value can never escape the theme dir.
func ServeThemeBackgroundGet(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet, http.MethodHead) {
		return
	}

	configMutex.RLock()
	cfg := model.ConfigInstance
	configMutex.RUnlock()

	name := cfg.Appearance.WallpaperFile
	if name == "" {
		writeLocalizedError(w, r, model.NotFound(nil, "FileNotFoundShort"))
		return
	}

	// Strict containment: must be a bare file name with a whitelisted ext.
	if name != filepath.Base(name) || strings.ContainsAny(name, "/\\") {
		writeLocalizedError(w, r, model.NotFound(nil, "FileNotFoundShort"))
		return
	}
	ext := strings.ToLower(filepath.Ext(name))
	if !model.IsThemeAllowedExt(name) {
		writeLocalizedError(w, r, model.NotFound(nil, "FileNotFoundShort"))
		return
	}

	themeDir := model.DefaultThemeDir()
	if themeDir == "" {
		writeLocalizedError(w, r, model.NotFound(nil, "FileNotFoundShort"))
		return
	}
	absPath := filepath.Join(themeDir, name)
	if !isPathUnderBase(absPath, themeDir) {
		writeLocalizedError(w, r, model.NotFound(nil, "FileNotFoundShort"))
		return
	}

	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		writeLocalizedError(w, r, model.NotFound(nil, "FileNotFoundShort"))
		return
	}

	mime := wallpaperMimeTypes[ext]
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
		svgBytes, err := os.ReadFile(absPath)
		if err != nil || !svgLooksSafe(svgBytes) {
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
// from disk. Caller must hold themeMutex.
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

// processedWallpaper is the final on-disk representation of a wallpaper source.
type processedWallpaper struct {
	data     []byte // encoded bytes to write
	fileName string // full file name, e.g. "background.png"
	ext      string // lower-cased extension with dot, e.g. ".png"
}

// processWallpaperSource validates srcBytes against the wallpaper format
// whitelist, enforces size/dimension limits, downscales png/jpeg, and returns
// the final bytes + file name. Content is verified by decode/sniff — never by
// trusting the file extension alone.
func processWallpaperSource(srcBytes []byte, srcName string) (*processedWallpaper, error) {
	ext := strings.ToLower(filepath.Ext(srcName))
	if !model.IsThemeAllowedExt(srcName) {
		return nil, fmt.Errorf("unsupported image format: %s", ext)
	}

	// Per-format size caps.
	maxBytes := int64(wallpaperMaxBytes)
	if ext == ".svg" {
		maxBytes = wallpaperMaxSVGBytes
	}
	if int64(len(srcBytes)) > maxBytes {
		return nil, fmt.Errorf("image too large")
	}

	switch ext {
	case ".png", ".jpg", ".jpeg":
		// Full decode doubles as content verification: a "png" that is not
		// really a PNG fails here, so extension spoofing is impossible.
		img, err := decodeRaster(srcBytes)
		if err != nil {
			return nil, fmt.Errorf("cannot decode image: %w", err)
		}
		bounds := img.Bounds()
		dst := img
		if bounds.Dx() > wallpaperMaxLongEdge || bounds.Dy() > wallpaperMaxLongEdge {
			ratio := float64(wallpaperMaxLongEdge) / float64(max(bounds.Dx(), bounds.Dy()))
			dstW := max(1, int(float64(bounds.Dx())*ratio))
			dstH := max(1, int(float64(bounds.Dy())*ratio))
			scaled := scaleImage(img, dstW, dstH)
			dst = scaled
		}

		var buf bytes.Buffer
		var outExt string
		if ext == ".png" {
			// Keep PNG as PNG to preserve alpha transparency (JPEG has none).
			if err := png.Encode(&buf, dst); err != nil {
				return nil, fmt.Errorf("cannot encode png: %w", err)
			}
			outExt = ".png"
		} else {
			if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: wallpaperJPEGQuality}); err != nil {
				return nil, fmt.Errorf("cannot encode jpeg: %w", err)
			}
			outExt = ".jpg"
		}
		return &processedWallpaper{data: buf.Bytes(), ext: outExt, fileName: wallpaperBaseName + outExt}, nil

	case ".gif", ".webp":
		// Stored verbatim (no re-encode — the Go stdlib has no GIF/WebP
		// encoder). Verify decodability + dimension ceiling via DecodeConfig.
		if err := verifyRasterConfig(srcBytes); err != nil {
			return nil, err
		}
		return &processedWallpaper{data: srcBytes, ext: ext, fileName: wallpaperBaseName + ext}, nil

	case ".svg":
		if !svgLooksSafe(srcBytes) {
			return nil, fmt.Errorf("unsupported svg content")
		}
		return &processedWallpaper{data: srcBytes, ext: ".svg", fileName: wallpaperBaseName + ".svg"}, nil
	}
	return nil, fmt.Errorf("unsupported image format")
}

// decodeRaster fully decodes a png/jpeg/gif source after checking its
// dimensions via DecodeConfig (decompression-bomb guard).
func decodeRaster(srcBytes []byte) (image.Image, error) {
	if err := verifyRasterConfig(srcBytes); err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(srcBytes))
	return img, err
}

// verifyRasterConfig reads only the image header and enforces the dimension
// ceiling before any full-decode work happens.
func verifyRasterConfig(srcBytes []byte) error {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(srcBytes))
	if err != nil {
		return fmt.Errorf("cannot decode image: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > wallpaperMaxDimension || cfg.Height > wallpaperMaxDimension {
		return fmt.Errorf("image dimensions out of range (%dx%d)", cfg.Width, cfg.Height)
	}
	return nil
}

// svgLooksSafe rejects SVG wallpapers that embed scripts, foreign objects,
// external references, or raster <image> loads — content that is inert when
// rendered as a CSS background but can leak data or stall rendering. Uses a
// case-insensitive byte scan on the raw source (SVG is XML; tags are text).
func svgLooksSafe(src []byte) bool {
	if len(src) == 0 {
		return false
	}
	// Quick structural sanity: must look like an XML/SVG document.
	head := src
	if len(head) > 4096 {
		head = head[:4096]
	}
	if !bytes.Contains(bytes.ToLower(head), []byte("<svg")) {
		return false
	}

	lower := bytes.ToLower(src)
	for _, forbidden := range [][]byte{
		[]byte("<script"),
		[]byte("foreignobject"),
		[]byte("</foreignobject>"),
		[]byte("xlink:href"),
		[]byte("href="),
		[]byte("<image"),
		[]byte("onload="),
		[]byte("onerror="),
		[]byte("javascript:"),
	} {
		if bytes.Contains(lower, forbidden) {
			return false
		}
	}
	return true
}
