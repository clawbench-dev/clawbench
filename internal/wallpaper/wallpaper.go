// Package wallpaper owns the custom-wallpaper image pipeline shared by the
// HTTP handlers and the background Bing-fetch worker.
//
// It lives in its own package because internal/service (the Bing worker) may
// not import internal/handler (the theme endpoints), yet both need the same
// validation, downscaling and on-disk layout. Only internal/model is imported
// here, so there is no import cycle.
//
// On-disk layout under <DataDir>/theme:
//
//	background.png          legacy single-file wallpaper (no prefix → theme root)
//	local/local-<nano>-<hex>.<ext>
//	bing/bing-<yyyymmdd>.jpg
//
// The file-name prefix selects the subdirectory, so a bare name is all the
// caller needs to store in config while still resolving to a contained path.
//
//nolint:goconst // file extensions are clearer as literals at each switch case
package wallpaper

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"clawbench/internal/model"

	"golang.org/x/image/draw"

	// Register image decoders for image.Decode / image.DecodeConfig (init() side-effects)
	_ "golang.org/x/image/webp"
	_ "image/gif"
)

// Processing limits and gallery caps.
const (
	// MaxLongEdge is the target longest-edge pixel size for raster wallpapers
	// (png/jpeg) supplied by a user. Larger sources are downscaled to keep
	// multi-device decode cost low while remaining sharp on hi-DPI screens.
	//
	// Only uploads use this: they are user-triggered and may run concurrently, so
	// their cost must stay bounded. The Bing fetch passes an explicit, higher cap
	// (see ProcessWithMaxEdge) because it runs once a day in the background and
	// the daily image is served at 3840x2160.
	MaxLongEdge = 2048

	// BingMaxLongEdge is the longest-edge cap for the Bing daily wallpaper. The
	// source is 3840x2160, and keeping it native is both sharper and cheaper than
	// downscaling: the Catmull-Rom reduction to 2048 costs more CPU and RAM than
	// encoding the original 4K frame.
	BingMaxLongEdge = 3840

	// MaxDimension is the hard upper bound on a source image's width/height.
	// Enforced via image.DecodeConfig BEFORE full decode so a small-but-huge
	// image (decompression bomb) can never allocate megabytes per pixel in RAM.
	MaxDimension = 8000

	// MaxBytes is the byte-size cap for non-SVG wallpapers.
	MaxBytes = 10 * 1024 * 1024

	// MaxSVGBytes is the byte-size cap for SVG wallpapers. SVG has no intrinsic
	// pixel size and is rasterized at viewport scale, so oversized files (huge
	// paths, embedded payloads) can stall a WebView render.
	MaxSVGBytes = 1 * 1024 * 1024

	// JPEGQuality is the encode quality for re-encoded JPEG wallpapers.
	JPEGQuality = 88

	// MaxGalleryItems caps how many local gallery images are retained. Each item
	// may be up to MaxBytes, so this bounds total disk use at ~500MB.
	MaxGalleryItems = 50

	// MaxUploadsPerRequest caps how many files a single upload request may carry.
	MaxUploadsPerRequest = 10

	// maxBingCacheFiles is how many cached Bing wallpapers are retained
	// (the current one plus the most recent others), so a failed fetch still has
	// a fallback and disk use stays bounded.
	maxBingCacheFiles = 3

	// localPrefix / bingPrefix select the subdirectory for a bare file name.
	localPrefix = "local-"
	bingPrefix  = "bing-"
)

// writeMu serializes wallpaper file writes so concurrent set operations
// (two tabs, upload vs Bing fetch) never interleave temp-file renames.
var writeMu sync.Mutex

// WithWriteLock runs fn while holding the package write lock, so a sequence of
// steps that must not interleave (write file → record in config → delete stale
// files) stays atomic with respect to other wallpaper writers. Callers that
// only need a single atomic file write should use WriteAtomic instead.
func WithWriteLock(fn func() error) error {
	writeMu.Lock()
	defer writeMu.Unlock()
	return fn()
}

// MimeFor maps a wallpaper extension to its MIME type. Unknown extensions
// return an empty string.
func MimeFor(ext string) string {
	switch strings.ToLower(ext) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	}
	return ""
}

// ThemeDir returns <DataDir>/theme, or "" when DataDir is unset.
func ThemeDir() string {
	return model.DefaultThemeDir()
}

// LocalDir returns <DataDir>/theme/local, or "" when DataDir is unset.
func LocalDir() string {
	dir := ThemeDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "local")
}

// BingDir returns <DataDir>/theme/bing, or "" when DataDir is unset.
func BingDir() string {
	dir := ThemeDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "bing")
}

// dirForName selects the directory a bare wallpaper file name lives in based on
// its prefix. Unprefixed names are the legacy single-file wallpaper, which lives
// directly in the theme root.
func dirForName(name string) string {
	switch {
	case strings.HasPrefix(name, bingPrefix):
		return BingDir()
	case strings.HasPrefix(name, localPrefix):
		return LocalDir()
	default:
		return ThemeDir()
	}
}

// FilePath resolves a bare wallpaper file name to an absolute path, refusing
// anything that is not a plain whitelisted file name contained by its theme
// subdirectory. The bool is false when the name is malformed or escapes the
// directory, so callers must treat false as "not found" rather than an error.
func FilePath(name string) (string, bool) {
	if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) {
		return "", false
	}
	if !model.IsThemeAllowedExt(name) {
		return "", false
	}
	dir := dirForName(name)
	if dir == "" {
		return "", false
	}
	abs, ok := model.ValidatePath(dir, name)
	if !ok {
		return "", false
	}
	return abs, true
}

// ResolveActive returns the bare name of the wallpaper that should currently be
// displayed, and whether one is active. A globally disabled wallpaper resolves
// to none even though the gallery and its selection are retained.
func ResolveActive(cfg *model.Config) (string, bool) {
	// A config with no mode predates the enabled switch and the gallery: back
	// then wallpaper_file was the only way to set a wallpaper, so its presence
	// alone means the user wants it shown. Gating this on WallpaperEnabled would
	// hide wallpapers that were set before the switch existed.
	if cfg.Appearance.WallpaperMode == "" {
		if cfg.Appearance.WallpaperFile != "" {
			return cfg.Appearance.WallpaperFile, true
		}
		return "", false
	}

	if !cfg.Appearance.WallpaperEnabled {
		return "", false
	}
	switch cfg.Appearance.WallpaperMode {
	case "bing":
		if cfg.Appearance.Bing.File != "" {
			return cfg.Appearance.Bing.File, true
		}
	case "local":
		if cfg.Appearance.Local.Selected != "" {
			return cfg.Appearance.Local.Selected, true
		}
	}
	return "", false
}

// BingMktForLocale maps a UI locale to the Bing market parameter. Chinese maps
// to mainland China; every other locale (including empty/unknown) maps to the
// US market, which always has a daily image.
func BingMktForLocale(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return "zh-CN"
	}
	return "en-US"
}

// Processed is the final on-disk representation of a wallpaper source.
type Processed struct {
	Data []byte // encoded bytes to write
	Ext  string // lower-cased extension with dot, e.g. ".png"
}

// Process validates srcBytes against the wallpaper format whitelist, enforces
// size/dimension limits, downscales png/jpeg, and returns the final bytes.
// Content is verified by decode/sniff — never by trusting the file extension
// alone, so extension spoofing is impossible.
//
// Uses MaxLongEdge as the downscale target. Callers that legitimately handle
// larger images (the Bing daily wallpaper) should use ProcessWithMaxEdge.
func Process(srcBytes []byte, srcName string) (*Processed, error) {
	return ProcessWithMaxEdge(srcBytes, srcName, MaxLongEdge)
}

// ProcessWithMaxEdge is Process with an explicit longest-edge downscale target.
//
// The cap is a parameter rather than a global because the two sources have
// different cost profiles: uploads are user-triggered and may run concurrently
// (keep them at MaxLongEdge), while the Bing fetch runs once a day in the
// background and is served at 3840x2160 (BingMaxLongEdge). Passing the wrong cap
// here silently changes image quality, so callers should name the constant that
// matches their source rather than inlining a number.
func ProcessWithMaxEdge(srcBytes []byte, srcName string, maxEdge int) (*Processed, error) {
	ext := strings.ToLower(filepath.Ext(srcName))
	if !model.IsThemeAllowedExt(srcName) {
		return nil, fmt.Errorf("unsupported image format: %s", ext)
	}
	if maxEdge <= 0 {
		maxEdge = MaxLongEdge
	}

	// Per-format size caps.
	maxBytes := int64(MaxBytes)
	if ext == ".svg" {
		maxBytes = MaxSVGBytes
	}
	if int64(len(srcBytes)) > maxBytes {
		return nil, fmt.Errorf("image too large")
	}

	switch ext {
	case ".png", ".jpg", ".jpeg":
		return processRaster(srcBytes, ext, maxEdge)

	case ".gif", ".webp":
		// Stored verbatim (no re-encode — the Go stdlib has no GIF/WebP
		// encoder). Verify decodability + dimension ceiling via DecodeConfig.
		if err := VerifyRasterConfig(srcBytes); err != nil {
			return nil, err
		}
		return &Processed{Data: srcBytes, Ext: ext}, nil

	case ".svg":
		if !SVGLooksSafe(srcBytes) {
			return nil, fmt.Errorf("unsupported svg content")
		}
		return &Processed{Data: srcBytes, Ext: ".svg"}, nil
	}
	return nil, fmt.Errorf("unsupported image format")
}

// Scale resizes an image to the target dimensions using high-quality
// Catmull-Rom interpolation via golang.org/x/image/draw. This produces a much
// sharper downscale than nearest-neighbor, which aliases fine detail into a
// blurry mess when reducing large images.
func Scale(src image.Image, dstW, dstH int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}

// processRaster decodes a png/jpeg, downscales it to maxEdge on the long edge,
// and re-encodes it (PNG stays PNG to preserve alpha; JPEG is re-encoded).
func processRaster(srcBytes []byte, ext string, maxEdge int) (*Processed, error) {
	// Full decode doubles as content verification: a "png" that is not really a
	// PNG fails here, so extension spoofing is impossible.
	img, err := decodeRaster(srcBytes)
	if err != nil {
		return nil, fmt.Errorf("cannot decode image: %w", err)
	}

	bounds := img.Bounds()
	dst := img
	if bounds.Dx() > maxEdge || bounds.Dy() > maxEdge {
		ratio := float64(maxEdge) / float64(max(bounds.Dx(), bounds.Dy()))
		dstW := max(1, int(float64(bounds.Dx())*ratio))
		dstH := max(1, int(float64(bounds.Dy())*ratio))
		dst = Scale(img, dstW, dstH)
	}

	var buf bytes.Buffer
	if ext == ".png" {
		// Keep PNG as PNG to preserve alpha transparency (JPEG has none).
		if err := png.Encode(&buf, dst); err != nil {
			return nil, fmt.Errorf("cannot encode png: %w", err)
		}
		return &Processed{Data: buf.Bytes(), Ext: ".png"}, nil
	}
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: JPEGQuality}); err != nil {
		return nil, fmt.Errorf("cannot encode jpeg: %w", err)
	}
	return &Processed{Data: buf.Bytes(), Ext: ".jpg"}, nil
}

// decodeRaster fully decodes a png/jpeg/gif source after checking its
// dimensions via DecodeConfig (decompression-bomb guard).
func decodeRaster(srcBytes []byte) (image.Image, error) {
	if err := VerifyRasterConfig(srcBytes); err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(srcBytes))
	return img, err
}

// VerifyRasterConfig reads only the image header and enforces the dimension
// ceiling before any full-decode work happens.
func VerifyRasterConfig(srcBytes []byte) error {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(srcBytes))
	if err != nil {
		return fmt.Errorf("cannot decode image: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxDimension || cfg.Height > MaxDimension {
		return fmt.Errorf("image dimensions out of range (%dx%d)", cfg.Width, cfg.Height)
	}
	return nil
}

// SVGLooksSafe rejects SVG wallpapers that embed scripts, foreign objects,
// external references, or raster <image> loads — content that is inert when
// rendered as a CSS background but can leak data or stall rendering. Uses a
// case-insensitive byte scan on the raw source (SVG is XML; tags are text).
func SVGLooksSafe(src []byte) bool {
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

// WriteAtomic writes data to dir/name via a temp file + rename so readers never
// observe a partially written wallpaper. Serialized by writeMu.
func WriteAtomic(dir, name string, data []byte) error {
	writeMu.Lock()
	defer writeMu.Unlock()
	return writeAtomicLocked(dir, name, data)
}

// writeAtomicLocked is WriteAtomic without taking the lock, for callers already
// holding it via WithWriteLock.
func writeAtomicLocked(dir, name string, data []byte) error {
	if dir == "" {
		return fmt.Errorf("wallpaper directory unavailable")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create wallpaper directory: %w", err)
	}
	tmpPath := filepath.Join(dir, name+".tmp")
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write wallpaper: %w", err)
	}
	if err := os.Rename(tmpPath, filepath.Join(dir, name)); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write wallpaper: %w", err)
	}
	return nil
}

// WriteAtomicWithinLock is WriteAtomic for callers that already hold the write
// lock through WithWriteLock. Calling WriteAtomic from inside that lock would
// deadlock, since the lock is not reentrant.
func WriteAtomicWithinLock(dir, name string, data []byte) error {
	return writeAtomicLocked(dir, name, data)
}

// RemoveFile deletes a wallpaper file, ignoring a missing file so callers can
// treat deletion as idempotent.
func RemoveFile(name string) {
	abs, ok := FilePath(name)
	if !ok {
		return
	}
	_ = os.Remove(abs)
}

// RemoveOldBingFiles deletes cached Bing wallpapers beyond the most recent
// RemoveOldBingFiles deletes cached Bing wallpapers beyond the most recent
// maxBingCacheFiles. Every name passed in keep is always retained, so callers
// can protect the image the config still references. Names embed yyyymmdd, so a
// lexical descending sort is chronological.
func RemoveOldBingFiles(keep ...string) {
	dir := BingDir()
	if dir == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, bingPrefix) || !model.IsThemeAllowedExt(name) {
			continue
		}
		names = append(names, name)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))

	kept := make(map[string]bool, len(keep)+maxBingCacheFiles)
	for _, k := range keep {
		if k != "" {
			kept[k] = true
		}
	}
	for _, name := range names {
		if len(kept) >= maxBingCacheFiles {
			break
		}
		kept[name] = true
	}
	for _, name := range names {
		if kept[name] {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
}

// BingState is the subset of Bing wallpaper state the fetch worker reports back
// to the config layer. It is defined here (rather than in service or handler)
// so both can reference it without importing each other.
type BingState struct {
	File            string
	Copyright       string
	Title           string
	Mkt             string
	LastError       string
	LastSuccessDate string
	LastAttemptAt   int64
}

// ReconcileLocalGallery deletes files in <DataDir>/theme/local that the gallery
// no longer references, and reports the names removed. An upload that fails
// between writing the file and recording it in config (or a crash in that
// window) leaves such orphans behind, and nothing else ever reclaims them.
//
// Only the local gallery is swept: the legacy theme root may hold files the
// config still points at, and the Bing cache prunes itself.
func ReconcileLocalGallery(known []string) []string {
	dir := LocalDir()
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	keep := make(map[string]bool, len(known))
	for _, name := range known {
		keep[name] = true
	}

	var removed []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Skip a temp file from an interrupted write; it carries no extension
		// match anyway, but being explicit avoids deleting in-flight writes.
		if strings.HasSuffix(name, ".tmp") {
			continue
		}
		if !strings.HasPrefix(name, localPrefix) || !model.IsThemeAllowedExt(name) {
			continue
		}
		if keep[name] {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err == nil {
			removed = append(removed, name)
		}
	}
	return removed
}
