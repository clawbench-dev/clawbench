package handler

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type encoderFunc func(w io.Writer, m image.Image) error

// createTestPNG creates a real PNG file of the given dimensions at relPath under projectDir.
func createTestPNG(t *testing.T, projectDir, relPath string, width, height int) {
	t.Helper()
	createTestImage(t, projectDir, relPath, width, height, png.Encode)
}

// createTestJPG creates a real JPEG file of the given dimensions at relPath under projectDir.
func createTestJPG(t *testing.T, projectDir, relPath string, width, height int) {
	t.Helper()
	encode := func(w io.Writer, m image.Image) error {
		return jpeg.Encode(w, m, &jpeg.Options{Quality: 90})
	}
	createTestImage(t, projectDir, relPath, width, height, encode)
}

func createTestImage(t *testing.T, projectDir, relPath string, width, height int, encode encoderFunc) {
	t.Helper()
	fullPath := filepath.Join(projectDir, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("failed to create directories: %v", err)
	}
	f, err := os.Create(fullPath)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	defer func() { _ = f.Close() }()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{R: 255, G: 100, B: 50, A: 255})
		}
	}
	if err := encode(f, img); err != nil {
		t.Fatalf("failed to encode image: %v", err)
	}
}

func TestFileThumb(t *testing.T) {
	t.Run("ValidImage_ReturnsJPEG", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a 100x80 PNG
		createTestPNG(t, env.ProjectDir, "photo.png", 100, 80)

		req := newRequest(t, http.MethodGet, "/api/file/thumb?path=photo.png&w=50", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(FileThumb, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/jpeg", w.Header().Get("Content-Type"))
		assert.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
		// Thumbnail should carry validators derived from the source file so the
		// browser can revalidate and get fresh content when the file changes.
		assert.NotEmpty(t, w.Header().Get("ETag"))
		assert.NotEmpty(t, w.Header().Get("Last-Modified"))
		// Response body should be non-empty (valid JPEG data)
		assert.Greater(t, w.Body.Len(), 0)
	})

	t.Run("WidthParameterClamped", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestPNG(t, env.ProjectDir, "img.png", 100, 100)

		// Width too small → should clamp to 50
		req := newRequest(t, http.MethodGet, "/api/file/thumb?path=img.png&w=10", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(FileThumb, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/jpeg", w.Header().Get("Content-Type"))

		// Width too large → should clamp to 800
		req2 := newRequest(t, http.MethodGet, "/api/file/thumb?path=img.png&w=9999", nil)
		withProjectCookie(req2, env.ProjectDir)

		w2 := callHandler(FileThumb, req2)
		assert.Equal(t, http.StatusOK, w2.Code)
	})

	t.Run("MissingWidth_DefaultsTo200", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestPNG(t, env.ProjectDir, "img.png", 300, 200)

		req := newRequest(t, http.MethodGet, "/api/file/thumb?path=img.png", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(FileThumb, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/jpeg", w.Header().Get("Content-Type"))
	})

	t.Run("NonImageFile_Returns404", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "readme.md", "# Hello")

		req := newRequest(t, http.MethodGet, "/api/file/thumb?path=readme.md", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(FileThumb, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("UnchangedFile_IfNoneMatch_Returns304", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestPNG(t, env.ProjectDir, "photo.png", 100, 80)

		req := newRequest(t, http.MethodGet, "/api/file/thumb?path=photo.png&w=50", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(FileThumb, req)
		require.Equal(t, http.StatusOK, w.Code)
		etag := w.Header().Get("ETag")
		require.NotEmpty(t, etag)

		// Revalidate with the same ETag → 304 (no re-encode).
		req2 := newRequest(t, http.MethodGet, "/api/file/thumb?path=photo.png&w=50", nil)
		withProjectCookie(req2, env.ProjectDir)
		req2.Header.Set("If-None-Match", etag)

		w2 := callHandler(FileThumb, req2)
		assert.Equal(t, http.StatusNotModified, w2.Code)
		assert.Equal(t, 0, w2.Body.Len())
	})

	t.Run("ChangedFile_IfNoneMatch_ReturnsFreshThumbnail", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		rel := "photo.png"
		createTestPNG(t, env.ProjectDir, rel, 100, 80)

		getThumb := func() (int, string) {
			req := newRequest(t, http.MethodGet, "/api/file/thumb?path="+rel+"&w=50", nil)
			withProjectCookie(req, env.ProjectDir)
			w := callHandler(FileThumb, req)
			return w.Code, w.Header().Get("ETag")
		}

		code, oldEtag := getThumb()
		require.Equal(t, http.StatusOK, code)
		require.NotEmpty(t, oldEtag)

		// Overwrite the source image with different dimensions → new mtime/size.
		createTestPNG(t, env.ProjectDir, rel, 200, 160)

		code, newEtag := getThumb()
		assert.Equal(t, http.StatusOK, code)
		assert.NotEmpty(t, newEtag)
		assert.NotEqual(t, oldEtag, newEtag, "thumbnail validator must change when the source file changes")
	})

	t.Run("UnchangedFile_IfModifiedSince_Returns304", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestPNG(t, env.ProjectDir, "photo.png", 100, 80)

		req := newRequest(t, http.MethodGet, "/api/file/thumb?path=photo.png&w=50", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(FileThumb, req)
		require.Equal(t, http.StatusOK, w.Code)
		lastMod := w.Header().Get("Last-Modified")
		require.NotEmpty(t, lastMod)

		// Revalidate with a Last-Modified equal to (or after) the file's mtime → 304.
		req2 := newRequest(t, http.MethodGet, "/api/file/thumb?path=photo.png&w=50", nil)
		withProjectCookie(req2, env.ProjectDir)
		req2.Header.Set("If-Modified-Since", lastMod)

		w2 := callHandler(FileThumb, req2)
		assert.Equal(t, http.StatusNotModified, w2.Code)
		assert.Equal(t, 0, w2.Body.Len())
	})

	t.Run("JPGImage_ReturnsJPEG", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestJPG(t, env.ProjectDir, "photo.jpg", 200, 150)

		req := newRequest(t, http.MethodGet, "/api/file/thumb?path=photo.jpg", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(FileThumb, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/jpeg", w.Header().Get("Content-Type"))
		assert.Greater(t, w.Body.Len(), 0)
	})

	t.Run("SVGImage_Returns404", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestFile(t, env.ProjectDir, "logo.svg", `<svg xmlns="http://www.w3.org/2000/svg"><circle r="10"/></svg>`)

		req := newRequest(t, http.MethodGet, "/api/file/thumb?path=logo.svg", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(FileThumb, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("FileNotFound_Returns404", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodGet, "/api/file/thumb?path=missing.png", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(FileThumb, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("NoProjectCookie_Returns403", func(t *testing.T) {
		_, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodGet, "/api/file/thumb?path=img.png", nil)

		w := callHandler(FileThumb, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("PathTraversal_Returns403", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		req := newRequest(t, http.MethodGet, "/api/file/thumb?path=../../../etc/passwd", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(FileThumb, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("DirectoryPath_Returns404", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		_ = os.MkdirAll(filepath.Join(env.ProjectDir, "subdir"), 0o755)

		req := newRequest(t, http.MethodGet, "/api/file/thumb?path=subdir", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(FileThumb, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("TallImage_OutputMaintainsAspectRatio", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a tall 100x400 image (4:1 height:width ratio)
		// All pixels are the same solid color (R=255, G=100, B=50)
		createTestPNG(t, env.ProjectDir, "tall.png", 100, 400)

		req := newRequest(t, http.MethodGet, "/api/file/thumb?path=tall.png&w=50", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(FileThumb, req)
		assert.Equal(t, http.StatusOK, w.Code)

		// Decode the JPEG response
		thumb, err := jpeg.Decode(bytes.NewReader(w.Body.Bytes()))
		assert.NoError(t, err)

		// Thumbnail should maintain aspect ratio: 50 wide, 200 tall
		bounds := thumb.Bounds()
		assert.Equal(t, 50, bounds.Dx(), "thumbnail width should be 50")
		assert.Equal(t, 200, bounds.Dy(), "thumbnail height should be 200 (4:1 ratio)")
	})

	t.Run("WideImage_OutputMaintainsAspectRatio", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a wide 400x100 image (4:1 width:height ratio)
		createTestPNG(t, env.ProjectDir, "wide.png", 400, 100)

		req := newRequest(t, http.MethodGet, "/api/file/thumb?path=wide.png&w=50", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(FileThumb, req)
		assert.Equal(t, http.StatusOK, w.Code)

		thumb, err := jpeg.Decode(bytes.NewReader(w.Body.Bytes()))
		assert.NoError(t, err)

		// Thumbnail should maintain aspect ratio: width=50, height=12 (400:100 → 50:12.5→12)
		bounds := thumb.Bounds()
		assert.Equal(t, 50, bounds.Dx(), "thumbnail width should be 50")
		assert.Equal(t, 12, bounds.Dy(), "thumbnail height should be 12 (4:1 ratio)")
	})

	t.Run("SquareImage_OutputIsSquare_NoPaddingNeeded", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		createTestPNG(t, env.ProjectDir, "square.png", 200, 200)

		req := newRequest(t, http.MethodGet, "/api/file/thumb?path=square.png&w=50", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(FileThumb, req)
		assert.Equal(t, http.StatusOK, w.Code)

		thumb, err := jpeg.Decode(bytes.NewReader(w.Body.Bytes()))
		assert.NoError(t, err)

		bounds := thumb.Bounds()
		assert.Equal(t, 50, bounds.Dx())
		assert.Equal(t, 50, bounds.Dy())
	})

	t.Run("AbsolutePath_ReturnsJPEG", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a PNG in an uploads subdirectory (mimics .clawbench/uploads/)
		createTestPNG(t, env.ProjectDir, ".clawbench/uploads/photo.png", 100, 80)

		// Request thumbnail using absolute path (as stored in DB by chat handler)
		absPath := filepath.Join(env.ProjectDir, ".clawbench/uploads/photo.png")
		req := newRequest(t, http.MethodGet, "/api/file/thumb?path="+absPath+"&w=50", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(FileThumb, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/jpeg", w.Header().Get("Content-Type"))
		assert.Greater(t, w.Body.Len(), 0)
	})

	t.Run("AbsolutePathOutsideProject_Returns403", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Absolute path outside project — must work on all platforms including Windows.
		// model.RootPaths is set to watchDir (a temp dir). Use a sibling path outside it.
		outsidePath := filepath.Join(filepath.Dir(env.ProjectDir), "..", "outside-project", "file.txt")
		absOutside, err := filepath.Abs(outsidePath)
		require.NoError(t, err)

		req := newRequest(t, http.MethodGet, "/api/file/thumb?path="+url.QueryEscape(absOutside), nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(FileThumb, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

// ─── Caching ────────────────────────────────────────────────────────────────

// TestFileThumb_CacheHitSkipsDecode pins the cache's whole purpose: a second
// request for an unchanged source must not decode again. The decode counter is
// the only way to observe this — both responses are byte-identical either way.
func TestFileThumb_CacheHitSkipsDecode(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	resetThumbCacheForTesting()
	resetThumbDecodeTracker()

	createTestPNG(t, env.ProjectDir, "photo.png", 100, 80)

	request := func() []byte {
		req := newRequest(t, http.MethodGet, "/api/file/thumb?path=photo.png&w=50", nil)
		withProjectCookie(req, env.ProjectDir)
		w := callHandler(FileThumb, req)
		require.Equal(t, http.StatusOK, w.Code)
		return w.Body.Bytes()
	}

	first := request()
	require.Equal(t, int64(1), thumbDecodeTracker.decodes.Load(), "first request must decode")

	second := request()
	assert.Equal(t, int64(1), thumbDecodeTracker.decodes.Load(),
		"second request for an unchanged file must be served from cache without decoding")
	assert.Equal(t, first, second, "cached body must be identical to the freshly encoded one")
}

// TestFileThumb_CacheInvalidatedWhenSourceChanges is the guarantee the user
// asked for: when the image file itself is updated, the stale thumbnail must
// never be served. Overwriting the file changes mtime (and here also size), so
// the cache key changes and the next request re-decodes.
func TestFileThumb_CacheInvalidatedWhenSourceChanges(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	resetThumbCacheForTesting()
	resetThumbDecodeTracker()

	rel := "photo.png"
	createTestPNG(t, env.ProjectDir, rel, 100, 80)

	request := func() (int, []byte, string) {
		req := newRequest(t, http.MethodGet, "/api/file/thumb?path="+rel+"&w=50", nil)
		withProjectCookie(req, env.ProjectDir)
		w := callHandler(FileThumb, req)
		return w.Code, w.Body.Bytes(), w.Header().Get("ETag")
	}

	code, firstBody, firstEtag := request()
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, int64(1), thumbDecodeTracker.decodes.Load())

	// Overwrite with different dimensions → different mtime and size.
	// Sleep a hair so the mtime advances even on filesystems with coarse
	// timestamp granularity.
	time.Sleep(10 * time.Millisecond)
	createTestPNG(t, env.ProjectDir, rel, 240, 180)

	code, secondBody, secondEtag := request()
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, int64(2), thumbDecodeTracker.decodes.Load(),
		"an updated source image must force a re-decode, never a cached body")
	assert.NotEqual(t, firstEtag, secondEtag, "validator must change with the source file")
	assert.NotEqual(t, firstBody, secondBody, "thumbnail bytes must reflect the updated source image")

	// A 100x80 source at w=50 yields 50x40; 240x180 yields 50x37. Verify the
	// served bytes really are the new geometry rather than a stale copy.
	thumb, err := jpeg.Decode(bytes.NewReader(secondBody))
	require.NoError(t, err)
	assert.Equal(t, 50, thumb.Bounds().Dx())
	assert.Equal(t, 37, thumb.Bounds().Dy(), "must encode the updated image's aspect ratio")
}

// TestFileThumb_CacheKeyedByWidth guards against serving a thumbnail encoded
// for one width to a request asking for another.
func TestFileThumb_CacheKeyedByWidth(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	resetThumbCacheForTesting()
	resetThumbDecodeTracker()

	createTestPNG(t, env.ProjectDir, "photo.png", 400, 300)

	request := func(width string) image.Image {
		req := newRequest(t, http.MethodGet, "/api/file/thumb?path=photo.png&w="+width, nil)
		withProjectCookie(req, env.ProjectDir)
		w := callHandler(FileThumb, req)
		require.Equal(t, http.StatusOK, w.Code)
		thumb, err := jpeg.Decode(bytes.NewReader(w.Body.Bytes()))
		require.NoError(t, err)
		return thumb
	}

	assert.Equal(t, 50, request("50").Bounds().Dx())
	assert.Equal(t, 120, request("120").Bounds().Dx(),
		"a different width must not be served the other width's cached body")
	assert.Equal(t, int64(2), thumbDecodeTracker.decodes.Load())
}

// ─── Concurrency bound ──────────────────────────────────────────────────────

// TestFileThumb_ConcurrentDecodesAreBounded is the regression test for the
// CPU-saturation incident: a file-manager directory mounts every thumbnail at
// once, and unbounded parallel decode+rescale drove the process to 638% CPU and
// stalled unrelated endpoints. With the semaphore pinned to 2, peak concurrent
// decodes must never exceed 2 no matter how many requests arrive together.
func TestFileThumb_ConcurrentDecodesAreBounded(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	resetThumbCacheForTesting()
	resetThumbDecodeTracker()
	resetThumbSemForTesting(2)

	// Distinct files so every request is a cache miss and must decode.
	const files = 12
	for i := range files {
		createTestPNG(t, env.ProjectDir, fmt.Sprintf("img%d.png", i), 400, 300)
	}

	var wg sync.WaitGroup
	for i := range files {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := newRequest(t, http.MethodGet, fmt.Sprintf("/api/file/thumb?path=img%d.png&w=50", i), nil)
			withProjectCookie(req, env.ProjectDir)
			w := callHandler(FileThumb, req)
			assert.Equal(t, http.StatusOK, w.Code)
		}(i)
	}
	wg.Wait()

	assert.Equal(t, int64(files), thumbDecodeTracker.decodes.Load(), "every distinct file must be decoded once")
	assert.LessOrEqual(t, thumbDecodeTracker.maxSeen.Load(), int64(2),
		"concurrent decodes must never exceed the semaphore ceiling")
	assert.Equal(t, int64(0), thumbDecodeTracker.inFlight.Load(), "all decode slots must be released")
}

// TestFileThumb_SemaphoreReleasedOnDecodeFailure guards the slot release: a
// decode error must not leak a slot, or the semaphore would permanently shrink
// until every thumbnail request blocks forever.
func TestFileThumb_SemaphoreReleasedOnDecodeFailure(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	resetThumbCacheForTesting()
	resetThumbSemForTesting(1)

	// A .png that is not a valid image → decode fails.
	require.NoError(t, os.WriteFile(filepath.Join(env.ProjectDir, "broken.png"), []byte("not an image"), 0o644))

	req := newRequest(t, http.MethodGet, "/api/file/thumb?path=broken.png&w=50", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(FileThumb, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	assert.Equal(t, 0, len(thumbDecodeSem), "the decode slot must be released after a failure")
}

// TestFileThumb_UnchangedSource_StillReturns304 proves the new cache path did
// not bypass HTTP revalidation: a conditional request for an unchanged file
// must still answer 304 rather than a body.
func TestFileThumb_UnchangedSource_StillReturns304(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	resetThumbCacheForTesting()

	createTestPNG(t, env.ProjectDir, "photo.png", 100, 80)

	req := newRequest(t, http.MethodGet, "/api/file/thumb?path=photo.png&w=50", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(FileThumb, req)
	require.Equal(t, http.StatusOK, w.Code)
	etag := w.Header().Get("ETag")

	req2 := newRequest(t, http.MethodGet, "/api/file/thumb?path=photo.png&w=50", nil)
	withProjectCookie(req2, env.ProjectDir)
	req2.Header.Set("If-None-Match", etag)

	w2 := callHandler(FileThumb, req2)
	assert.Equal(t, http.StatusNotModified, w2.Code)
	assert.Equal(t, 0, w2.Body.Len())
}

// ─── Cache bounds ───────────────────────────────────────────────────────────

// TestThumbCache_EvictsByEntryCount pins the entry ceiling. Keys include the
// source mtime, so every edit of an image creates a NEW key while the old entry
// stays reachable only through the LRU — without a bound, memory would grow
// with edit count rather than with the number of images.
func TestThumbCache_EvictsByEntryCount(t *testing.T) {
	c := newThumbCache()

	// Insert more distinct keys than the ceiling allows.
	for i := range thumbCacheMaxEntries + 50 {
		key := thumbCacheKey{path: "/p/img.png", mtime: int64(i), width: 200}
		c.put(key, []byte("x"))
	}

	assert.LessOrEqual(t, c.order.Len(), thumbCacheMaxEntries, "entry count must stay within the ceiling")
	assert.LessOrEqual(t, len(c.entries), thumbCacheMaxEntries)

	// The most recently inserted entry must survive (LRU keeps the hot end).
	newest := thumbCacheKey{path: "/p/img.png", mtime: int64(thumbCacheMaxEntries + 49), width: 200}
	_, ok := c.get(newest)
	assert.True(t, ok, "the most recently used entry must not be evicted")
}

// TestThumbCache_EvictsByBytes guards the byte ceiling, which is what actually
// bounds memory when thumbnails are large (a wide `w=` produces a much bigger
// JPEG than the default 200px).
func TestThumbCache_EvictsByBytes(t *testing.T) {
	c := newThumbCache()

	// Each entry is a quarter of the byte budget, so more than four must evict.
	chunk := make([]byte, thumbCacheMaxBytes/4)
	for i := range 8 {
		key := thumbCacheKey{path: "/p/img.png", mtime: int64(i), width: 200}
		c.put(key, chunk)
	}

	assert.LessOrEqual(t, c.bytes, thumbCacheMaxBytes, "cached bytes must stay within the ceiling")
	assert.NotZero(t, c.order.Len(), "at least one entry must be retained")
}

// TestThumbCache_RePutSameKeyDoesNotDoubleCount guards the byte accounting: a
// re-put of an existing key replaces the body, so the old size must be
// subtracted or `bytes` would drift upward and evict healthy entries.
func TestThumbCache_RePutSameKeyDoesNotDoubleCount(t *testing.T) {
	c := newThumbCache()
	key := thumbCacheKey{path: "/p/img.png", mtime: 1, width: 200}

	c.put(key, []byte("12345"))
	c.put(key, []byte("1234567890"))

	assert.Equal(t, 1, c.order.Len(), "re-putting a key must not add an entry")
	assert.Equal(t, 10, c.bytes, "byte accounting must replace, not accumulate")
}
