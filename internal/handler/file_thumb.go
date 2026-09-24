package handler

import (
	"bytes"
	"container/list"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/image/draw"

	"clawbench/internal/model"

	// Register image decoders for image.Decode (init() side-effects)
	_ "image/gif"
	_ "image/png"
)

const (
	thumbDefaultWidth = 200
	thumbMinWidth     = 50
	thumbMaxWidth     = 1600             // hi-DPI displays upscale an 800px thumb → blurry; allow larger
	thumbMaxFileSize  = 50 * 1024 * 1024 // 50 MB
	thumbJPEGQuality  = 85

	// thumbCacheMaxEntries / thumbCacheMaxBytes bound the decoded-thumbnail
	// cache. Both ceilings are enforced so a directory of many small images
	// cannot blow up memory via entry count, nor a few large ones via bytes.
	thumbCacheMaxEntries = 512
	thumbCacheMaxBytes   = 32 * 1024 * 1024 // 32 MB
)

// thumbMaxConcurrent caps how many image decodes run at once.
//
// Decoding is CPU-bound (full-resolution decode + CatmullRom rescale), so an
// unbounded fan-in — e.g. a file-manager directory holding dozens of images,
// which mounts every thumbnail in one tick — saturates every core and starves
// unrelated endpoints. Measured before this cap: 32 concurrent decodes drove
// the process to 638% CPU and pushed the (DB-free) /api/dir from 16ms to 72ms.
//
// The ceiling is min(4, NumCPU): a quarter of a large machine, but never more
// than the core count on a small one. It is a var so tests can pin it to 1 and
// observe the serialization.
var thumbMaxConcurrent = min(4, runtime.NumCPU())

// thumbDecodeSem bounds concurrent decodes.
var thumbDecodeSem = make(chan struct{}, thumbMaxConcurrent)

// resetThumbSemForTesting swaps in a semaphore with the given ceiling so a test
// can make the bound deterministic rather than depending on the package default.
func resetThumbSemForTesting(n int) {
	thumbDecodeSem = make(chan struct{}, n)
}

// ─── Thumbnail cache ─────────────────────────────────────────────────────────

// thumbCacheKey identifies a cached thumbnail. It deliberately includes the
// source file's size and mtime: rewriting an image (even to the same
// dimensions) changes at least one of them, producing a different key, so a
// stale entry can never be served after the source changes. That makes
// invalidation automatic and immediate — no watcher or TTL required.
type thumbCacheKey struct {
	path  string
	size  int64
	mtime int64
	width int
}

type thumbCacheEntry struct {
	key thumbCacheKey
	jpg []byte
}

// thumbCache is a bounded LRU of encoded thumbnails. Keys carry the source
// mtime/size, so entries for a superseded file version simply become
// unreachable and age out; nothing has to invalidate them explicitly.
type thumbCache struct {
	mu      sync.Mutex
	entries map[thumbCacheKey]*list.Element
	order   *list.List // front = most recently used
	bytes   int
}

func newThumbCache() *thumbCache {
	return &thumbCache{
		entries: make(map[thumbCacheKey]*list.Element),
		order:   list.New(),
	}
}

var thumbCacheStore = newThumbCache()

func (c *thumbCache) get(key thumbCacheKey) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(el)
	entry, ok := el.Value.(*thumbCacheEntry)
	if !ok {
		return nil, false
	}
	return entry.jpg, true
}

func (c *thumbCache) put(key thumbCacheKey, jpg []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key]; ok {
		c.order.MoveToFront(el)
		entry, ok := el.Value.(*thumbCacheEntry)
		if !ok {
			return
		}
		c.bytes += len(jpg) - len(entry.jpg)
		entry.jpg = jpg
	} else {
		el := c.order.PushFront(&thumbCacheEntry{key: key, jpg: jpg})
		c.entries[key] = el
		c.bytes += len(jpg)
	}
	for c.order.Len() > thumbCacheMaxEntries || c.bytes > thumbCacheMaxBytes {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		c.order.Remove(oldest)
		entry, ok := oldest.Value.(*thumbCacheEntry)
		if !ok {
			continue
		}
		delete(c.entries, entry.key)
		c.bytes -= len(entry.jpg)
	}
}

// resetThumbCacheForTesting clears the process-wide cache. Only tests use it.
func resetThumbCacheForTesting() {
	thumbCacheStore = newThumbCache()
}

// acquireThumbSlot blocks until a decode slot is free or ctx is done. Returns
// false when the client went away while waiting, so the caller can bail without
// doing the work.
func acquireThumbSlot(ctx context.Context) bool {
	select {
	case thumbDecodeSem <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func releaseThumbSlot() {
	<-thumbDecodeSem
}

// thumbDecodeTracker observes real decode work so tests can prove two
// properties that are otherwise invisible from the outside: that a cache hit
// skips decoding, and that the semaphore actually bounds concurrency. Costs two
// atomic ops per decode.
var thumbDecodeTracker struct {
	decodes  atomic.Int64
	inFlight atomic.Int64
	maxSeen  atomic.Int64
}

func trackDecodeStart() {
	thumbDecodeTracker.decodes.Add(1)
	cur := thumbDecodeTracker.inFlight.Add(1)
	for {
		seen := thumbDecodeTracker.maxSeen.Load()
		if cur <= seen || thumbDecodeTracker.maxSeen.CompareAndSwap(seen, cur) {
			break
		}
	}
}

func trackDecodeEnd() {
	thumbDecodeTracker.inFlight.Add(-1)
}

func resetThumbDecodeTracker() {
	thumbDecodeTracker.decodes.Store(0)
	thumbDecodeTracker.inFlight.Store(0)
	thumbDecodeTracker.maxSeen.Store(0)
}

// thumbDecodeExts lists extensions that Go's image.Decode can handle
// (standard library: png, jpeg, gif). BMP and TIFF require golang.org/x/image.
// SVG is explicitly excluded because it's vector, not raster.
//
//nolint:goconst // ".png" appears in multiple unrelated string maps; extracting is overkill
var thumbDecodeExts = []string{
	".png", ".jpg", ".jpeg", ".gif",
}

// FileThumb handles GET /api/fs/thumb?target=<path>&w=<width>
// Returns a JPEG thumbnail of the image file at the given path.
func FileThumb(w http.ResponseWriter, r *http.Request) { //nolint:gocyclo // multi-format thumbnail generation
	relPath := r.URL.Query().Get("target")
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

	// Revalidation: derive a validator from the source file's metadata so the
	// thumbnail refreshes immediately when the source image changes, and returns
	// a cheap 304 when it hasn't. Checked BEFORE decode/encode to avoid wasted work.
	// HTTP dates only have 1s precision, so truncate modTime for Last-Modified /
	// If-Modified-Since; keep raw precision in the ETag for exact matching.
	modTime := info.ModTime().UTC()
	lastMod := modTime.Truncate(time.Second)
	etag := fmt.Sprintf(`"%x-%x"`, modTime.UnixNano(), info.Size())

	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if ims := r.Header.Get("If-Modified-Since"); ims != "" {
		if imsTime, parseErr := http.ParseTime(ims); parseErr == nil && !lastMod.After(imsTime) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	// Parse width parameter. Done before the cache lookup because the target
	// width is part of the cache key — the same source image yields a different
	// thumbnail per width.
	widthStr := r.URL.Query().Get("w")
	targetWidth := thumbDefaultWidth
	if widthStr != "" {
		if w, err := strconv.Atoi(widthStr); err == nil { //nolint:govet // shadowed err, scoped to if-block
			targetWidth = clampInt(w, thumbMinWidth, thumbMaxWidth)
		}
	}

	// Serve a cached thumbnail when the source file is byte-for-byte the same
	// version we last decoded. The key carries mtime+size, so an updated image
	// misses the cache and is re-decoded immediately — the same property the
	// ETag above relies on, applied to the encoded body.
	cacheKey := thumbCacheKey{
		path:  absPath,
		size:  info.Size(),
		mtime: modTime.UnixNano(),
		width: targetWidth,
	}
	if cached, ok := thumbCacheStore.get(cacheKey); ok {
		writeThumbResponse(w, cached, etag, lastMod)
		return
	}

	// Bound concurrent decodes: a directory of images mounts every thumbnail in
	// one tick, and unbounded parallel decode+rescale saturates all cores,
	// stalling unrelated endpoints (see thumbMaxConcurrent).
	if !acquireThumbSlot(r.Context()) {
		// Client went away while queued — nothing to send.
		return
	}
	defer releaseThumbSlot()

	trackDecodeStart()
	defer trackDecodeEnd()

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

	// Scale image using Catmull-Rom resampling
	dst := scaleImage(img, scaledW, scaledH)

	// Encode as JPEG to buffer first to avoid partial response on encode error
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: thumbJPEGQuality}); err != nil {
		slog.Debug("thumb: failed to encode JPEG", slog.String("path", absPath), slog.String("err", err.Error()))
		model.WriteError(w, model.Internal(fmt.Errorf("jpeg encode: %w", err)))
		return
	}

	jpg := buf.Bytes()
	thumbCacheStore.put(cacheKey, jpg)
	writeThumbResponse(w, jpg, etag, lastMod)
}

// writeThumbResponse emits a thumbnail body with its validators and caching
// headers. Shared by the cache-hit and freshly-encoded paths so the two can
// never drift.
func writeThumbResponse(w http.ResponseWriter, jpg []byte, etag string, lastMod time.Time) {
	w.Header().Set("Content-Type", "image/jpeg")
	// no-cache: the browser must revalidate against the source file's ETag
	// before reusing a thumbnail, so an updated image shows immediately.
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Last-Modified", lastMod.Format(http.TimeFormat))
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Length", strconv.Itoa(len(jpg)))
	_, _ = w.Write(jpg)
}

// scaleImage resizes an image to the target dimensions using high-quality
// Catmull-Rom interpolation via golang.org/x/image/draw. This produces a much
// sharper downscale than nearest-neighbor, which aliases fine detail into a
// blurry mess when reducing large images to thumbnail size.
func scaleImage(src image.Image, dstW, dstH int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
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
