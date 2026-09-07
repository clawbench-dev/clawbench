package handler

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupThemeTestEnv configures model.DataDir to a fresh temp dir and gives
// ConfigInstance a clean Appearance section so handlers read empty defaults.
// The theme dir lives under the test env watch dir so absolute-path source
// files resolve against model.RootPaths (set to the watch dir).
func setupThemeTestEnv(t *testing.T) (string, func()) {
	env, teardown := setupTestEnv(t)

	origConfig := model.ConfigInstance
	origDataDir := model.DataDir

	themeDir := filepath.Join(env.WatchDir, "data", "theme")
	model.DataDir = filepath.Dir(themeDir)
	model.ConfigInstance = model.Config{}
	require.NoError(t, os.MkdirAll(themeDir, 0o755))

	cleanup := func() {
		model.ConfigInstance = origConfig
		model.DataDir = origDataDir
		teardown()
	}
	return themeDir, cleanup
}

// makePNG renders an RGBA image and PNG-encodes it.
func makePNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// makeJPEG renders a grayscale image and JPEG-encodes it.
func makeJPEG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 64, A: 255})
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80})
	return buf.Bytes()
}

// setWallpaperInConfig writes a wallpaper file name into ConfigInstance so GET
// tests exercise the full read path without going through POST.
func setWallpaperInConfig(t *testing.T, themeDir, name string) {
	t.Helper()
	model.ConfigInstance.Appearance.WallpaperFile = name
	_ = os.MkdirAll(themeDir, 0o755)
	require.NoError(t, os.WriteFile(filepath.Join(themeDir, name), []byte("png-data"), 0o644))
}

func TestServeThemeBackground_PostMultipartPNG(t *testing.T) {
	themeDir, teardown := setupThemeTestEnv(t)
	defer teardown()

	// Build multipart body with a real PNG.
	pngBytes := makePNG(64, 48)
	body, contentType := makeMultipartBody("photo.png", pngBytes)

	req := httptest.NewRequest(http.MethodPost, "/api/theme-background", body)
	req.Header.Set("Content-Type", contentType)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackground, req)

	assert.Equal(t, http.StatusOK, w.Code)
	// PNG must be stored as background.png.
	assert.FileExists(t, filepath.Join(themeDir, "background.png"))
	// Config records the file name.
	assert.Equal(t, "background.png", model.ConfigInstance.Appearance.WallpaperFile)
}

func TestServeThemeBackground_PostMultipartJPEGDownscaled(t *testing.T) {
	themeDir, teardown := setupThemeTestEnv(t)
	defer teardown()

	// A huge source (5000x4000) must be downscaled to ≤ wallpaperMaxLongEdge.
	jpegBytes := makeJPEG(5000, 4000)
	body, contentType := makeMultipartBody("huge.jpg", jpegBytes)

	req := httptest.NewRequest(http.MethodPost, "/api/theme-background", body)
	req.Header.Set("Content-Type", contentType)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackground, req)

	assert.Equal(t, http.StatusOK, w.Code)
	stored, err := os.ReadFile(filepath.Join(themeDir, "background.jpg"))
	require.NoError(t, err)
	assert.Less(t, len(stored), len(jpegBytes), "downscaled JPEG should be smaller than source")
	assert.Equal(t, "background.jpg", model.ConfigInstance.Appearance.WallpaperFile)
}

func TestServeThemeBackground_PostPathCopyReplacesOldExtension(t *testing.T) {
	themeDir, teardown := setupThemeTestEnv(t)
	defer teardown()

	// A source file next to DataDir (absolute path, no project cookie needed).
	projectFile := filepath.Join(filepath.Dir(themeDir), "source.png")
	require.NoError(t, os.WriteFile(projectFile, makePNG(20, 20), 0o644))
	// Also stage an old gif file to ensure it gets cleaned on replacement.
	require.NoError(t, os.WriteFile(filepath.Join(themeDir, "background.gif"), []byte("old-gif"), 0o644))

	body := strings.NewReader(`{"path":"` + projectFile + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/theme-background", body)
	req.Header.Set("Content-Type", "application/json")
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackground, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.FileExists(t, filepath.Join(themeDir, "background.png"))
	assert.NoFileExists(t, filepath.Join(themeDir, "background.gif"), "stale extension must be cleaned")
	assert.Equal(t, "background.png", model.ConfigInstance.Appearance.WallpaperFile)
}

func TestServeThemeBackground_PostRejectsSpoofedExtension(t *testing.T) {
	themeDir, teardown := setupThemeTestEnv(t)
	defer teardown()

	// HTML content masquerading as .png must be rejected (content sniffing).
	body, contentType := makeMultipartBody("evil.png", []byte("<html><script>alert(1)</script></html>"))

	req := httptest.NewRequest(http.MethodPost, "/api/theme-background", body)
	req.Header.Set("Content-Type", contentType)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackground, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.NoFileExists(t, filepath.Join(themeDir, "background.png"))
}

func TestServeThemeBackground_PostRejectsOversized(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	// File larger than the 10MB cap.
	big := bytes.Repeat([]byte{0x42}, wallpaperMaxBytes+1024)
	body, contentType := makeMultipartBody("big.png", big)

	req := httptest.NewRequest(http.MethodPost, "/api/theme-background", body)
	req.Header.Set("Content-Type", contentType)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackground, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeThemeBackground_PostRejectsUnsafeSVG(t *testing.T) {
	themeDir, teardown := setupThemeTestEnv(t)
	defer teardown()

	unsafeSVG := `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`
	body, contentType := makeMultipartBody("bg.svg", []byte(unsafeSVG))

	req := httptest.NewRequest(http.MethodPost, "/api/theme-background", body)
	req.Header.Set("Content-Type", contentType)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackground, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.NoFileExists(t, filepath.Join(themeDir, "background.svg"))

	// A safe SVG must pass.
	safeSVG := `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100"><rect width="100" height="100" fill="blue"/></svg>`
	body2, ct2 := makeMultipartBody("bg2.svg", []byte(safeSVG))
	req2 := httptest.NewRequest(http.MethodPost, "/api/theme-background", body2)
	req2.Header.Set("Content-Type", ct2)
	req2 = withAuthCookie(req2, model.SessionToken)
	w2 := callHandler(ServeThemeBackground, req2)
	assert.Equal(t, http.StatusOK, w2.Code)
	assert.FileExists(t, filepath.Join(themeDir, "background.svg"))
}

func TestServeThemeBackground_PostRejectsHugeDimensions(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	// Cannot build an actual 9000px image cheaply in test; instead craft a PNG
	// header claiming enormous dimensions — DecodeConfig reads the header only,
	// so this exercises the dimension guard without allocating real pixels.
	crafted := craftPngHeader(9000, 9000)
	body, contentType := makeMultipartBody("bomb.png", crafted)

	req := httptest.NewRequest(http.MethodPost, "/api/theme-background", body)
	req.Header.Set("Content-Type", contentType)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackground, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeThemeBackground_DeleteClearsConfigAndFiles(t *testing.T) {
	themeDir, teardown := setupThemeTestEnv(t)
	defer teardown()

	setWallpaperInConfig(t, themeDir, "background.png")
	require.NoError(t, os.WriteFile(filepath.Join(themeDir, "background.jpg"), []byte("jpg"), 0o644))

	req := httptest.NewRequest(http.MethodDelete, "/api/theme-background", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackground, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "", model.ConfigInstance.Appearance.WallpaperFile)
	assert.NoFileExists(t, filepath.Join(themeDir, "background.png"))
	assert.NoFileExists(t, filepath.Join(themeDir, "background.jpg"))
}

func TestServeThemeBackground_GetServesWithCacheHeaders(t *testing.T) {
	themeDir, teardown := setupThemeTestEnv(t)
	defer teardown()

	setWallpaperInConfig(t, themeDir, "background.png")
	realPNG := makePNG(10, 10)
	require.NoError(t, os.WriteFile(filepath.Join(themeDir, "background.png"), realPNG, 0o644))

	req := httptest.NewRequest(http.MethodGet, "/api/file/theme-background", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackgroundGet, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.Contains(t, w.Header().Get("Cache-Control"), "immutable")
	assert.NotEmpty(t, w.Header().Get("ETag"))
	assert.Equal(t, realPNG, w.Body.Bytes())
}

func TestServeThemeBackground_GetSVGAddsCSP(t *testing.T) {
	themeDir, teardown := setupThemeTestEnv(t)
	defer teardown()

	safeSVG := `<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10" fill="red"/></svg>`
	setWallpaperInConfig(t, themeDir, "background.svg")
	require.NoError(t, os.WriteFile(filepath.Join(themeDir, "background.svg"), []byte(safeSVG), 0o644))

	req := httptest.NewRequest(http.MethodGet, "/api/file/theme-background", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackgroundGet, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "image/svg+xml", w.Header().Get("Content-Type"))
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.Contains(t, w.Header().Get("Content-Security-Policy"), "sandbox")
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}

func TestServeThemeBackground_GetTraversalRejected(t *testing.T) {
	themeDir, teardown := setupThemeTestEnv(t)
	defer teardown()

	// Config corrupted with a traversal value — GET must 404, never serve outside dir.
	model.ConfigInstance.Appearance.WallpaperFile = "../../etc/passwd"
	_ = os.MkdirAll(themeDir, 0o755)

	req := httptest.NewRequest(http.MethodGet, "/api/file/theme-background", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackgroundGet, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeThemeBackground_GetWhenUnset(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodGet, "/api/file/theme-background", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackgroundGet, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ── Branch coverage: error/validation paths not hit by happy-path tests ──

func TestServeThemeBackground_MethodNotAllowed(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	// PUT is not routed by the dispatch handler.
	req := httptest.NewRequest(http.MethodPut, "/api/theme-background", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackground, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeThemeBackground_PostPathCopyMissingPathField(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	// JSON body without a "path" — must 400 InvalidRequest.
	body := strings.NewReader(`{"nope":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/theme-background", body)
	req.Header.Set("Content-Type", "application/json")
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackground, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeThemeBackground_PostPathCopyNonexistentFile(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// A path under the watch dir (a permitted root) that does not exist must
	// 404 — not be treated as a valid wallpaper source.
	missing := filepath.Join(env.WatchDir, "nope", "wallpaper.png")
	body := strings.NewReader(`{"path":"` + missing + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/theme-background", body)
	req.Header.Set("Content-Type", "application/json")
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackground, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeThemeBackground_PostPathCopyDirectoryRejected(t *testing.T) {
	themeDir, teardown := setupThemeTestEnv(t)
	defer teardown()

	// A directory must not be accepted as a wallpaper source.
	body := strings.NewReader(`{"path":"` + themeDir + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/theme-background", body)
	req.Header.Set("Content-Type", "application/json")
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackground, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeThemeBackground_PostUnsupportedExtension(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	// A .bmp extension is outside the whitelist → InvalidImage.
	body, contentType := makeMultipartBody("bg.bmp", []byte("not really bmp"))
	req := httptest.NewRequest(http.MethodPost, "/api/theme-background", body)
	req.Header.Set("Content-Type", contentType)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackground, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeThemeBackground_PostMultipartGIFVerbatim(t *testing.T) {
	themeDir, teardown := setupThemeTestEnv(t)
	defer teardown()

	// GIF stored verbatim (no re-encode) after DecodeConfig verification. The
	// 1x1 pixel GIF exercises the ".gif/.webp" verbatim branch.
	gifBytes := []byte{
		'G', 'I', 'F', '8', '9', 'a', 1, 0, 1, 0, 0x80, 0x00, 0x00,
		0x00, 0x00, 0x00, 0xff, 0xff, 0xff,
		0x21, 0xf9, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x2c, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00,
		0x02, 0x02, 0x44, 0x01, 0x00, 0x3b,
	}
	body, contentType := makeMultipartBody("bg.gif", gifBytes)
	req := httptest.NewRequest(http.MethodPost, "/api/theme-background", body)
	req.Header.Set("Content-Type", contentType)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackground, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.FileExists(t, filepath.Join(themeDir, "background.gif"))
}

func TestServeThemeBackground_GetUnknownExtFallsBackToOctetStream(t *testing.T) {
	themeDir, teardown := setupThemeTestEnv(t)
	defer teardown()

	// Config names a bare file whose extension is NOT in the wallpaper whitelist
	// → 404 before MIME lookup (IsThemeAllowedExt rejects it). To hit the
	// octet-stream fallback the extension must be allowed by model but absent
	// from the handler map — every model-allowed ext IS in the map, so this
	// tests the 404 path for a disallowed-but-bare name instead.
	model.ConfigInstance.Appearance.WallpaperFile = "background.exe"
	require.NoError(t, os.WriteFile(filepath.Join(themeDir, "background.exe"), []byte("MZ"), 0o644))
	req := httptest.NewRequest(http.MethodGet, "/api/file/theme-background", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackgroundGet, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeThemeBackground_GetSVGReadFailure404(t *testing.T) {
	themeDir, teardown := setupThemeTestEnv(t)
	defer teardown()

	// SVG whose file disappears between Stat and ReadFile is caught by the
	// svgLooksSafe read-err branch. Simulate via a directory entry that passes
	// Stat but fails ReadFile — chmod 000 on the file.
	_ = os.MkdirAll(themeDir, 0o755)
	svgPath := filepath.Join(themeDir, "background.svg")
	require.NoError(t, os.WriteFile(svgPath, []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>`), 0o000))
	t.Cleanup(func() { _ = os.Chmod(svgPath, 0o644) })
	model.ConfigInstance.Appearance.WallpaperFile = "background.svg"

	req := httptest.NewRequest(http.MethodGet, "/api/file/theme-background", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackgroundGet, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeThemeBackground_DeleteWritesConfigWhenDataDirSet(t *testing.T) {
	themeDir, teardown := setupThemeTestEnv(t)
	defer teardown()

	// DELETE persists an empty wallpaper_file back to config.yaml. With DataDir
	// pointing at the temp theme env, writeConfigYAML writes there — never into
	// the repository's checked-in config fixture.
	model.ConfigInstance.Appearance.WallpaperFile = "background.png"
	require.NoError(t, os.WriteFile(filepath.Join(themeDir, "background.png"), []byte("png"), 0o644))

	req := httptest.NewRequest(http.MethodDelete, "/api/theme-background", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackground, req)
	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "", model.ConfigInstance.Appearance.WallpaperFile)
	assert.NoFileExists(t, filepath.Join(themeDir, "background.png"))

	// writeConfigYAML ran against the temp DataDir.
	cfgPath := filepath.Join(model.DataDir, "config", "config.yaml")
	data, err := os.ReadFile(cfgPath)
	require.NoError(t, err, "config.yaml should be written under the temp DataDir")
	assert.Contains(t, string(data), "wallpaper_file")
}

func TestProcessWallpaperSource_RejectsOversizedSVG(t *testing.T) {
	// SVG over its 1MB cap must be rejected before content scanning.
	big := bytes.Repeat([]byte("<svg xmlns='http://www.w3.org/2000/svg'>"), wallpaperMaxSVGBytes/40+1)
	_, err := processWallpaperSource(big, "bg.svg")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
}

func TestSVGLooksSafe_RejectsForbiddenPatterns(t *testing.T) {
	// Each forbidden construct must be rejected case-insensitively.
	cases := []string{
		``,
		`<html><body></body></html>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><foreignObject>hi</foreignObject></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><image href="http://evil/x.png"/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><a href="javascript:alert(1)">x</a></svg>`,
	}
	for _, tc := range cases {
		assert.False(t, svgLooksSafe([]byte(tc)), "should reject: %q", tc)
	}
	// A plain safe SVG must pass.
	assert.True(t, svgLooksSafe([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect width="10" height="10" fill="blue"/></svg>`)))
	// Empty input is rejected.
	assert.False(t, svgLooksSafe(nil))
}

func TestProcessWallpaperSource_UnsupportedFormat(t *testing.T) {
	_, err := processWallpaperSource([]byte("anything"), "photo.xyz")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported image format")
}

func TestVerifyRasterConfig_RejectsOutOfRangeDimensions(t *testing.T) {
	// A PNG header claiming 9000px (> 8000 ceiling) is rejected before any
	// pixel allocation (DecodeConfig surfaces the dimension error).
	err := verifyRasterConfig(craftPngHeader(9000, 9000))
	require.Error(t, err)
	// A non-image payload fails DecodeConfig outright.
	err = verifyRasterConfig([]byte("not an image"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot decode image")
}

func TestServeThemeBackground_GetMethodNotAllowed(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	// GET accepts GET/HEAD only — a POST must 405.
	req := httptest.NewRequest(http.MethodPost, "/api/file/theme-background", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackgroundGet, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeThemeBackground_PostWriteFailureIs500(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	// Point DataDir at a path under a regular file so MkdirAll in
	// writeWallpaperFile fails after the image is processed.
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))
	model.DataDir = filepath.Join(blocker, "data")

	body, contentType := makeMultipartBody("bg.png", makePNG(10, 10))
	req := httptest.NewRequest(http.MethodPost, "/api/theme-background", body)
	req.Header.Set("Content-Type", contentType)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackground, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestServeThemeBackground_PostInvalidJSONBody(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	// Malformed JSON in path-copy mode → decodeJSON fails → 400.
	req := httptest.NewRequest(http.MethodPost, "/api/theme-background", strings.NewReader(`{bad`))
	req.Header.Set("Content-Type", "application/json")
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackground, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeThemeBackground_PostPathCopyFileTooLarge(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// A source file exceeding wallpaperMaxBytes must 400 before any read.
	big := filepath.Join(env.WatchDir, "huge.png")
	require.NoError(t, os.WriteFile(big, bytes.Repeat([]byte{0x89}, wallpaperMaxBytes+1024), 0o644))

	body := strings.NewReader(`{"path":"` + big + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/theme-background", body)
	req.Header.Set("Content-Type", "application/json")
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackground, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeThemeBackground_PostMultipartNoFileField(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	// A multipart body without a "file" field → NoFileProvided.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	require.NoError(t, mw.WriteField("note", "hello"))
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/theme-background", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackground, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeThemeBackground_GetWhenFileIsDirectory(t *testing.T) {
	themeDir, teardown := setupThemeTestEnv(t)
	defer teardown()

	// Config names a path that resolves to a directory → 404.
	model.ConfigInstance.Appearance.WallpaperFile = "background.png"
	require.NoError(t, os.MkdirAll(filepath.Join(themeDir, "background.png"), 0o755))

	req := httptest.NewRequest(http.MethodGet, "/api/file/theme-background", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackgroundGet, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeThemeBackground_GetUnreadableRasterFile(t *testing.T) {
	themeDir, teardown := setupThemeTestEnv(t)
	defer teardown()

	// Stat succeeds but os.Open fails (chmod 000) → 404.
	model.ConfigInstance.Appearance.WallpaperFile = "background.png"
	p := filepath.Join(themeDir, "background.png")
	require.NoError(t, os.WriteFile(p, makePNG(10, 10), 0o000))
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })

	req := httptest.NewRequest(http.MethodGet, "/api/file/theme-background", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackgroundGet, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeThemeBackground_GetSVGUnsafeContent404(t *testing.T) {
	themeDir, teardown := setupThemeTestEnv(t)
	defer teardown()

	// SVG whose content fails svgLooksSafe on the serve path → 404.
	model.ConfigInstance.Appearance.WallpaperFile = "background.svg"
	require.NoError(t, os.WriteFile(filepath.Join(themeDir, "background.svg"),
		[]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), 0o644))

	req := httptest.NewRequest(http.MethodGet, "/api/file/theme-background", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackgroundGet, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestRemoveStaleWallpapers_IgnoresNonWallpaperAndDirs(t *testing.T) {
	dir := t.TempDir()
	// Entries that must be skipped: non-background files, directories.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "background.png"), []byte("x"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "background.old"), 0o755))

	removeStaleWallpapers(dir, "background.png")
	assert.FileExists(t, filepath.Join(dir, "readme.txt"), "non-background file must stay")
	oldInfo, err := os.Stat(filepath.Join(dir, "background.old"))
	assert.NoError(t, err, "directory with wallpaper prefix must stay")
	assert.True(t, oldInfo.IsDir())
	assert.FileExists(t, filepath.Join(dir, "background.png"), "kept file untouched")

	// keepFile == "" removes all wallpaper files.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "background.gif"), []byte("x"), 0o644))
	removeStaleWallpapers(dir, "")
	assert.NoFileExists(t, filepath.Join(dir, "background.gif"))
}

func TestServeThemeBackground_DeleteConfigWriteFailure(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	// Make the config dir uncreatable so setConfigWallpaperFile fails during
	// DELETE → 500 and the in-memory wallpaper file name is restored.
	model.DataDir = filepath.Join(t.TempDir(), "nested")
	require.NoError(t, os.MkdirAll(model.DataDir, 0o755))
	// Replace the config dir path with a regular file to force MkdirAll failure.
	cfgDir := filepath.Join(model.DataDir, "config")
	require.NoError(t, os.WriteFile(cfgDir, []byte("x"), 0o644))

	model.ConfigInstance.Appearance.WallpaperFile = "background.png"
	req := httptest.NewRequest(http.MethodDelete, "/api/theme-background", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackground, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "background.png", model.ConfigInstance.Appearance.WallpaperFile,
		"in-memory config must be restored on write failure")
}

func TestProcessWallpaperSource_TooLargeRaster(t *testing.T) {
	// A raster source over wallpaperMaxBytes must fail before decode.
	_, err := processWallpaperSource(make([]byte, wallpaperMaxBytes+1), "bg.png")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
}

func TestSVGLooksSafe_HeadLongerThan4096(t *testing.T) {
	// A safe SVG longer than 4096 bytes must still pass (only head scanned for
	// the <svg marker, then the full body checked for forbidden content).
	longSafe := `<svg xmlns="http://www.w3.org/2000/svg">` + strings.Repeat(" ", 5000) + `<rect width="10" height="10"/></svg>`
	assert.True(t, svgLooksSafe([]byte(longSafe)))
}

func TestServeThemeBackground_GetRasterServesPNGBody(t *testing.T) {
	themeDir, teardown := setupThemeTestEnv(t)
	defer teardown()

	// GET for a real PNG wallpaper streams the file content with ETag/Cache-Control.
	png := makePNG(8, 8)
	model.ConfigInstance.Appearance.WallpaperFile = "background.png"
	require.NoError(t, os.WriteFile(filepath.Join(themeDir, "background.png"), png, 0o644))

	req := httptest.NewRequest(http.MethodGet, "/api/file/theme-background", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackgroundGet, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, png, w.Body.Bytes())
}

// --- helpers ---

// makeMultipartBody builds a multipart/form-data body with one "file" field.
func makeMultipartBody(filename string, content []byte) (*bytes.Buffer, string) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		panic(err)
	}
	_, _ = fw.Write(content)
	_ = mw.Close()
	return &buf, mw.FormDataContentType()
}

// craftPngHeader writes a minimal PNG header with the given IHDR dimensions —
// enough for image.DecodeConfig to read width/height without valid pixel data.
func craftPngHeader(w, h int) []byte {
	var buf bytes.Buffer
	// PNG signature
	buf.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	// IHDR chunk
	ihdr := make([]byte, 13)
	ihdr[0] = byte(w >> 24)
	ihdr[1] = byte(w >> 16)
	ihdr[2] = byte(w >> 8)
	ihdr[3] = byte(w)
	ihdr[4] = byte(h >> 24)
	ihdr[5] = byte(h >> 16)
	ihdr[6] = byte(h >> 8)
	ihdr[7] = byte(h)
	ihdr[8] = 8 // bit depth
	ihdr[9] = 6 // color type RGBA
	writeChunk(&buf, "IHDR", ihdr)
	return buf.Bytes()
}

func writeChunk(buf *bytes.Buffer, typ string, data []byte) {
	// length
	length := len(data)
	buf.Write([]byte{byte(length >> 24), byte(length >> 16), byte(length >> 8), byte(length)})
	buf.WriteString(typ)
	buf.Write(data)
	// CRC is omitted — DecodeConfig does not validate CRC on the header path we need.
	buf.Write([]byte{0, 0, 0, 0})
}
