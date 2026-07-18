package handler

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
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
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		model.WriteError(w, model.NotFound(nil, "path required"))
		return
	}

	absPath, ok := validateAndResolvePath(w, r, projectPath, relPath)
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

	// Resize maintaining aspect ratio, fit inside a square canvas with dominant-color padding
	bounds := img.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()
	if srcW <= 0 || srcH <= 0 {
		model.WriteError(w, model.NotFound(nil, "invalid image dimensions"))
		return
	}

	// Calculate scaled dimensions to fit within targetWidth × targetWidth square
	var scaledW, scaledH int
	if srcW >= srcH {
		// Landscape or square: width fills the canvas, height is shorter
		ratio := float64(targetWidth) / float64(srcW)
		scaledW = targetWidth
		scaledH = int(float64(srcH) * ratio)
		if scaledH < 1 {
			scaledH = 1
		}
	} else {
		// Portrait: height fills the canvas, width is narrower
		ratio := float64(targetWidth) / float64(srcH)
		scaledH = targetWidth
		scaledW = int(float64(srcW) * ratio)
		if scaledW < 1 {
			scaledW = 1
		}
	}

	// Scale image using nearest-neighbor
	scaled := scaleImage(img, scaledW, scaledH)

	// Extract dominant color (average of sampled pixels)
	dominant := dominantColor(img)

	// Create square canvas filled with dominant color, center the scaled image
	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetWidth))
	drawDominant(dst, dominant)
	// Center the scaled image on the canvas
	offsetX := (targetWidth - scaledW) / 2
	offsetY := (targetWidth - scaledH) / 2
	for y := range scaledH {
		for x := range scaledW {
			dst.Set(offsetX+x, offsetY+y, scaled.At(x, y))
		}
	}

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

// dominantColor samples pixels from the image and returns the average color.
// Uses a grid sampling approach for performance (max ~400 samples).
func dominantColor(src image.Image) color.RGBA {
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return color.RGBA{R: 128, G: 128, B: 128, A: 255}
	}

	// Sample on a grid — step size chosen so we get at most ~20 samples per axis
	stepX := w / 20
	stepY := h / 20
	if stepX < 1 {
		stepX = 1
	}
	if stepY < 1 {
		stepY = 1
	}

	var rSum, gSum, bSum uint64
	var count uint64
	for y := bounds.Min.Y; y < bounds.Max.Y; y += stepY {
		for x := bounds.Min.X; x < bounds.Max.X; x += stepX {
			r, g, b, _ := src.At(x, y).RGBA()
			rSum += uint64(r >> 8)
			gSum += uint64(g >> 8)
			bSum += uint64(b >> 8)
			count++
		}
	}
	if count == 0 {
		return color.RGBA{R: 128, G: 128, B: 128, A: 255}
	}
	return color.RGBA{
		R: uint8(rSum / count), //nolint:gosec // average of uint8 values, cannot overflow
		G: uint8(gSum / count), //nolint:gosec // average of uint8 values, cannot overflow
		B: uint8(bSum / count), //nolint:gosec // average of uint8 values, cannot overflow
		A: 255,
	}
}

// drawDominant fills the destination image with the given color.
func drawDominant(dst *image.RGBA, c color.RGBA) {
	for y := range dst.Bounds().Dy() {
		for x := range dst.Bounds().Dx() {
			dst.SetRGBA(x, y, c)
		}
	}
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
