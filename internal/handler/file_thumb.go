package handler

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"clawbench/internal/model"

	// Register image decoders for image.Decode (init() side-effects)
	_ "image/gif"
	_ "image/png"
)

const (
	thumbDefaultWidth = 200
	thumbMinWidth     = 50
	thumbMaxWidth     = 800
	thumbMaxFileSize  = 50 * 1024 * 1024 // 50 MB
	thumbJPEGQuality  = 75
)

// thumbDecodeExts lists extensions that Go's image.Decode can handle
// (standard library: png, jpeg, gif). BMP and TIFF require golang.org/x/image.
// SVG is explicitly excluded because it's vector, not raster.
//
//nolint:goconst // ".png" appears in multiple unrelated string maps; extracting is overkill
var thumbDecodeExts = []string{
	".png", ".jpg", ".jpeg", ".gif",
}

// FileThumb handles GET /api/file/thumb?path=<path>&w=<width>
// Returns a JPEG thumbnail of the image file at the given path.
func FileThumb(w http.ResponseWriter, r *http.Request) { //nolint:gocyclo // multi-format thumbnail generation
	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		model.WriteError(w, model.NotFound(nil, "path required"))
		return
	}

	absPath, ok := resolveAbsPath(w, r, relPath)
	if !ok {
		return
	}

	// Must be a regular file
	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		model.WriteError(w, model.NotFound(nil, "file not found"))
		return
	}

	// Skip files that are too large
	if info.Size() > thumbMaxFileSize {
		model.WriteError(w, model.NotFound(nil, "file too large for thumbnail"))
		return
	}

	// Only attempt to decode supported image formats
	if !model.IsImageFile(absPath) || !isThumbDecodable(absPath) {
		model.WriteError(w, model.NotFound(nil, "unsupported image format"))
		return
	}

	// Parse width parameter
	widthStr := r.URL.Query().Get("w")
	targetWidth := thumbDefaultWidth
	if widthStr != "" {
		if w, err := strconv.Atoi(widthStr); err == nil { //nolint:govet // shadowed err, scoped to if-block
			targetWidth = clampInt(w, thumbMinWidth, thumbMaxWidth)
		}
	}

	// Open and decode
	f, err := os.Open(absPath)
	if err != nil {
		slog.Debug("thumb: failed to open file", slog.String("path", absPath), slog.String("err", err.Error()))
		model.WriteError(w, model.NotFound(nil, "cannot open file"))
		return
	}
	defer func() { _ = f.Close() }()

	img, _, err := image.Decode(f)
	if err != nil {
		slog.Debug("thumb: failed to decode image", slog.String("path", absPath), slog.String("err", err.Error()))
		model.WriteError(w, model.NotFound(nil, "cannot decode image"))
		return
	}

	// Scale image maintaining aspect ratio, no square canvas padding
	bounds := img.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()
	if srcW <= 0 || srcH <= 0 {
		model.WriteError(w, model.NotFound(nil, "invalid image dimensions"))
		return
	}

	// Calculate scaled dimensions: width = targetWidth, height proportional
	var scaledW, scaledH int
	ratio := float64(targetWidth) / float64(srcW)
	scaledW = targetWidth
	scaledH = int(float64(srcH) * ratio)
	if scaledH < 1 {
		scaledH = 1
	}

	// Scale image using nearest-neighbor
	dst := scaleImage(img, scaledW, scaledH)

	// Encode as JPEG to buffer first to avoid partial response on encode error
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: thumbJPEGQuality}); err != nil {
		slog.Debug("thumb: failed to encode JPEG", slog.String("path", absPath), slog.String("err", err.Error()))
		model.WriteError(w, model.Internal(fmt.Errorf("jpeg encode: %w", err)))
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	_, _ = buf.WriteTo(w)
}

// scaleImage resizes an image to the target dimensions using nearest-neighbor
// interpolation. This uses only the standard library — no third-party deps.
func scaleImage(src image.Image, dstW, dstH int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	srcW, srcH := src.Bounds().Dx(), src.Bounds().Dy()
	for y := range dstH {
		for x := range dstW {
			// Map destination pixel to source pixel (nearest neighbor)
			sx := (x * srcW) / dstW
			sy := (y * srcH) / dstH
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

// isThumbDecodable checks if the file extension is one we can decode with Go's
// standard image package. SVG and PDF are explicitly excluded.
func isThumbDecodable(path string) bool {
	lower := strings.ToLower(path)
	for _, ext := range thumbDecodeExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// clampInt returns v clamped to [lo, hi].
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
