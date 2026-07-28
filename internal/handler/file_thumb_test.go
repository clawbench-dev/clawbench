package handler

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

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
		assert.Equal(t, "public, max-age=86400", w.Header().Get("Cache-Control"))
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

	t.Run("TallImage_OutputIsSquareWithDominantColorPadding", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a tall 100x400 image (2:1 height:width ratio)
		// All pixels are the same solid color (R=255, G=100, B=50)
		createTestPNG(t, env.ProjectDir, "tall.png", 100, 400)

		req := newRequest(t, http.MethodGet, "/api/file/thumb?path=tall.png&w=50", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(FileThumb, req)
		assert.Equal(t, http.StatusOK, w.Code)

		// Decode the JPEG response
		thumb, err := jpeg.Decode(bytes.NewReader(w.Body.Bytes()))
		assert.NoError(t, err)

		// Thumbnail should be square (50x50) — image fits inside, padded with dominant color
		bounds := thumb.Bounds()
		assert.Equal(t, 50, bounds.Dx(), "thumbnail should be square (width)")
		assert.Equal(t, 50, bounds.Dy(), "thumbnail should be square (height)")

		// The top-left corner should be the dominant color (padding area)
		paddingColor := thumb.At(0, 0)
		r, g, _, _ := paddingColor.RGBA()
		// Dominant color is RGB(255, 100, 50) — allow some JPEG compression tolerance
		assert.Greater(t, r, uint32(60000), "padding red channel should be close to 255")
		assert.Greater(t, g, uint32(20000), "padding green channel should be close to 100")
		assert.Less(t, g, uint32(30000), "padding green channel should be close to 100")
	})

	t.Run("WideImage_OutputIsSquareWithDominantColorPadding", func(t *testing.T) {
		env, teardown := setupTestEnv(t)
		defer teardown()

		// Create a wide 400x100 image
		createTestPNG(t, env.ProjectDir, "wide.png", 400, 100)

		req := newRequest(t, http.MethodGet, "/api/file/thumb?path=wide.png&w=50", nil)
		withProjectCookie(req, env.ProjectDir)

		w := callHandler(FileThumb, req)
		assert.Equal(t, http.StatusOK, w.Code)

		thumb, err := jpeg.Decode(bytes.NewReader(w.Body.Bytes()))
		assert.NoError(t, err)

		bounds := thumb.Bounds()
		assert.Equal(t, 50, bounds.Dx(), "thumbnail should be square (width)")
		assert.Equal(t, 50, bounds.Dy(), "thumbnail should be square (height)")

		// Bottom area is padding with dominant color
		paddingColor := thumb.At(25, 49)
		r, _, _, _ := paddingColor.RGBA()
		assert.Greater(t, r, uint32(60000), "padding red channel should be close to 255")
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
