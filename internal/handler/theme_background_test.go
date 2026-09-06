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
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
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
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
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
	body, contentType := makeMultipartBody("file", "photo.png", pngBytes)

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
	body, contentType := makeMultipartBody("file", "huge.jpg", jpegBytes)

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
	body, contentType := makeMultipartBody("file", "evil.png", []byte("<html><script>alert(1)</script></html>"))

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
	body, contentType := makeMultipartBody("file", "big.png", big)

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
	body, contentType := makeMultipartBody("file", "bg.svg", []byte(unsafeSVG))

	req := httptest.NewRequest(http.MethodPost, "/api/theme-background", body)
	req.Header.Set("Content-Type", contentType)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBackground, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.NoFileExists(t, filepath.Join(themeDir, "background.svg"))

	// A safe SVG must pass.
	safeSVG := `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100"><rect width="100" height="100" fill="blue"/></svg>`
	body2, ct2 := makeMultipartBody("file", "bg2.svg", []byte(safeSVG))
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
	body, contentType := makeMultipartBody("file", "bomb.png", crafted)

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

// --- helpers ---

// makeMultipartBody builds a multipart/form-data body with one file field.
func makeMultipartBody(field, filename string, content []byte) (*bytes.Buffer, string) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(field, filename)
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
