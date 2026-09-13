package wallpaper

import (
	"bytes"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// makeJPEG renders an image and JPEG-encodes it.
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

// craftPngHeader writes a minimal PNG header with the given IHDR dimensions and
// a valid CRC — enough for image.DecodeConfig to read width/height (and reach
// the dimension guard) without valid pixel data.
func craftPngHeader(w, h int) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	ihdr := make([]byte, 13)
	ihdr[0] = byte(w >> 24)
	ihdr[1] = byte(w >> 16)
	ihdr[2] = byte(w >> 8)
	ihdr[3] = byte(w)
	ihdr[4] = byte(h >> 24)
	ihdr[5] = byte(h >> 16)
	ihdr[6] = byte(h >> 8)
	ihdr[7] = byte(h)
	ihdr[8] = 8
	ihdr[9] = 6
	writeChunk(&buf, "IHDR", ihdr)
	return buf.Bytes()
}

func writeChunk(buf *bytes.Buffer, typ string, data []byte) {
	length := len(data)
	buf.Write([]byte{byte(length >> 24), byte(length >> 16), byte(length >> 8), byte(length)})
	buf.WriteString(typ)
	buf.Write(data)

	// The CRC covers the chunk type and data; DecodeConfig validates it.
	crc := crc32.NewIEEE()
	_, _ = crc.Write([]byte(typ))
	_, _ = crc.Write(data)
	buf.Write([]byte{
		byte(crc.Sum32() >> 24), byte(crc.Sum32() >> 16),
		byte(crc.Sum32() >> 8), byte(crc.Sum32()),
	})
}

// setupThemeDir points model.DataDir at a temp dir so FilePath/WriteAtomic
// resolve inside the test sandbox.
func setupThemeDir(t *testing.T) string {
	t.Helper()
	origDataDir := model.DataDir
	model.DataDir = t.TempDir()
	t.Cleanup(func() { model.DataDir = origDataDir })
	return model.DataDir
}

func TestProcess_PNGResizedToMaxLongEdge(t *testing.T) {
	// A source larger than MaxLongEdge must be downscaled on the long edge.
	out, err := Process(makePNG(3000, 1000), "big.png")
	require.NoError(t, err)
	assert.Equal(t, ".png", out.Ext)

	cfg, _, err := image.DecodeConfig(bytes.NewReader(out.Data))
	require.NoError(t, err)
	assert.Equal(t, MaxLongEdge, cfg.Width)
	assert.Less(t, cfg.Height, MaxLongEdge)
}

func TestProcess_JPEGReencodedAndSmaller(t *testing.T) {
	src := makeJPEG(4000, 3000)
	out, err := Process(src, "photo.jpg")
	require.NoError(t, err)
	assert.Equal(t, ".jpg", out.Ext)
	assert.Less(t, len(out.Data), len(src), "downscaled JPEG should be smaller than the source")

	cfg, _, err := image.DecodeConfig(bytes.NewReader(out.Data))
	require.NoError(t, err)
	assert.LessOrEqual(t, cfg.Width, MaxLongEdge)
	assert.LessOrEqual(t, cfg.Height, MaxLongEdge)
}

// TestProcessWithMaxEdge_UsesCallerCap pins the explicit-cap API that lets the
// Bing fetch keep its native 4K while uploads stay at the smaller upload cap.
func TestProcessWithMaxEdge_UsesCallerCap(t *testing.T) {
	// 3000 wide: above MaxLongEdge (2048) but below BingMaxLongEdge (3840).
	src := makePNG(3000, 1000)

	// The default cap still downscales it.
	byDefault, err := Process(src, "big.png")
	require.NoError(t, err)
	defCfg, _, err := image.DecodeConfig(bytes.NewReader(byDefault.Data))
	require.NoError(t, err)
	assert.Equal(t, MaxLongEdge, defCfg.Width)

	// An explicit larger cap keeps it at 3000 (not upscaled to the cap).
	kept, err := ProcessWithMaxEdge(src, "big.png", BingMaxLongEdge)
	require.NoError(t, err)
	keptCfg, _, err := image.DecodeConfig(bytes.NewReader(kept.Data))
	require.NoError(t, err)
	assert.Equal(t, 3000, keptCfg.Width, "a source under the cap must not be resized")

	// An explicit smaller cap downscales further.
	small, err := ProcessWithMaxEdge(src, "big.png", 1024)
	require.NoError(t, err)
	smallCfg, _, err := image.DecodeConfig(bytes.NewReader(small.Data))
	require.NoError(t, err)
	assert.Equal(t, 1024, smallCfg.Width)
}

// TestProcessWithMaxEdge_NonPositiveFallsBackToDefault guards the zero value:
// a caller passing 0 must get the safe upload cap rather than no limit at all.
func TestProcessWithMaxEdge_NonPositiveFallsBackToDefault(t *testing.T) {
	out, err := ProcessWithMaxEdge(makePNG(3000, 1000), "big.png", 0)
	require.NoError(t, err)

	cfg, _, err := image.DecodeConfig(bytes.NewReader(out.Data))
	require.NoError(t, err)
	assert.Equal(t, MaxLongEdge, cfg.Width)
}

func TestProcess_SmallImageNotUpscaled(t *testing.T) {
	out, err := Process(makePNG(64, 48), "small.png")
	require.NoError(t, err)
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out.Data))
	require.NoError(t, err)
	assert.Equal(t, 64, cfg.Width)
	assert.Equal(t, 48, cfg.Height)
}

func TestProcess_GIFWebpVerbatim(t *testing.T) {
	// A 1x1 GIF must be stored byte-for-byte (no re-encode available).
	gifBytes := []byte{
		'G', 'I', 'F', '8', '9', 'a', 1, 0, 1, 0, 0x80, 0x00, 0x00,
		0x00, 0x00, 0x00, 0xff, 0xff, 0xff,
		0x21, 0xf9, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x2c, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00,
		0x02, 0x02, 0x44, 0x01, 0x00, 0x3b,
	}
	out, err := Process(gifBytes, "anim.gif")
	require.NoError(t, err)
	assert.Equal(t, ".gif", out.Ext)
	assert.Equal(t, gifBytes, out.Data, "GIF must be stored verbatim")
}

func TestProcess_RejectsUnsafeSVG(t *testing.T) {
	_, err := Process([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), "bg.svg")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported svg content")

	// A safe SVG is accepted verbatim.
	safe := `<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10"/></svg>`
	out, err := Process([]byte(safe), "bg.svg")
	require.NoError(t, err)
	assert.Equal(t, safe, string(out.Data))
}

func TestProcess_RejectsOversizeBytes(t *testing.T) {
	_, err := Process(make([]byte, MaxBytes+1), "bg.png")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too large")

	_, err = Process(bytes.Repeat([]byte("<svg xmlns='http://www.w3.org/2000/svg'>"), MaxSVGBytes/40+1), "bg.svg")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
}

func TestProcess_RejectsDecompressionBomb(t *testing.T) {
	// A PNG header claiming 9000px exceeds MaxDimension and must be rejected
	// before any pixel allocation.
	_, err := Process(craftPngHeader(9000, 9000), "bomb.png")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

func TestProcess_RejectsSpoofedExtension(t *testing.T) {
	// HTML content named .png must fail the decode-based content check.
	_, err := Process([]byte("<html><script>alert(1)</script></html>"), "evil.png")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot decode image")
}

func TestProcess_RejectsUnsupportedExtension(t *testing.T) {
	_, err := Process([]byte("anything"), "photo.xyz")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported image format")
}

func TestFilePath_RejectsTraversal(t *testing.T) {
	setupThemeDir(t)

	for _, name := range []string{
		"../etc/passwd",
		"../../etc/passwd",
		"sub/background.png",
		`sub\background.png`,
		"/etc/passwd",
		"",
	} {
		_, ok := FilePath(name)
		assert.False(t, ok, "must reject %q", name)
	}
}

func TestFilePath_RejectsDisallowedExtension(t *testing.T) {
	setupThemeDir(t)

	_, ok := FilePath("background.exe")
	assert.False(t, ok)
}

func TestFilePath_RoutesByPrefix(t *testing.T) {
	dir := setupThemeDir(t)

	// FilePath resolves real paths, so the target directories must exist — a
	// file cannot live in a directory that is not there.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "theme"), 0o755))
	require.NoError(t, os.MkdirAll(LocalDir(), 0o755))
	require.NoError(t, os.MkdirAll(BingDir(), 0o755))

	themeRoot, ok := FilePath("background.png")
	require.True(t, ok)
	assert.Equal(t, filepath.Join(dir, "theme", "background.png"), themeRoot)

	local, ok := FilePath("local-123-abc.jpg")
	require.True(t, ok)
	assert.Equal(t, filepath.Join(dir, "theme", "local", "local-123-abc.jpg"), local)

	bing, ok := FilePath("bing-20260910.jpg")
	require.True(t, ok)
	assert.Equal(t, filepath.Join(dir, "theme", "bing", "bing-20260910.jpg"), bing)
}

func TestFilePath_MissingDirIsNotOk(t *testing.T) {
	setupThemeDir(t)

	// With no theme directories created yet, a well-formed name still cannot
	// resolve — callers must treat this as "not found".
	_, ok := FilePath("local-123-abc.jpg")
	assert.False(t, ok)
}

func TestResolveActive_DisabledReturnsNotOk(t *testing.T) {
	cfg := &model.Config{}
	cfg.Appearance.WallpaperMode = "local"
	cfg.Appearance.WallpaperEnabled = false
	cfg.Appearance.Local.Selected = "local-1-a.png"

	name, ok := ResolveActive(cfg)
	assert.False(t, ok, "disabled wallpaper must resolve to none")
	assert.Empty(t, name)
}

func TestResolveActive_BingThenLocalPrecedence(t *testing.T) {
	// Each mode resolves to its own source.
	bingCfg := &model.Config{}
	bingCfg.Appearance.WallpaperMode = "bing"
	bingCfg.Appearance.WallpaperEnabled = true
	bingCfg.Appearance.Bing.File = "bing-20260910.jpg"
	bingCfg.Appearance.Local.Selected = "local-1-a.png"

	name, ok := ResolveActive(bingCfg)
	require.True(t, ok)
	assert.Equal(t, "bing-20260910.jpg", name)

	localCfg := &model.Config{}
	localCfg.Appearance.WallpaperMode = "local"
	localCfg.Appearance.WallpaperEnabled = true
	localCfg.Appearance.Bing.File = "bing-20260910.jpg"
	localCfg.Appearance.Local.Selected = "local-1-a.png"

	name, ok = ResolveActive(localCfg)
	require.True(t, ok)
	assert.Equal(t, "local-1-a.png", name)
}

func TestResolveActive_EmptySourceReturnsNotOk(t *testing.T) {
	// Mode set but nothing cached yet (e.g. first Bing fetch still pending).
	cfg := &model.Config{}
	cfg.Appearance.WallpaperMode = "bing"
	cfg.Appearance.WallpaperEnabled = true

	name, ok := ResolveActive(cfg)
	assert.False(t, ok)
	assert.Empty(t, name)
}

func TestResolveActive_LegacyEmptyReturnsNotOk(t *testing.T) {
	cfg := &model.Config{}
	_, ok := ResolveActive(cfg)
	assert.False(t, ok)
}

func TestBingMktForLocale(t *testing.T) {
	cases := map[string]string{
		"zh":      "zh-CN",
		"zh-CN":   "zh-CN",
		"zh-Hans": "zh-CN",
		"ZH":      "zh-CN",
		"en":      "en-US",
		"en-US":   "en-US",
		"fr":      "en-US",
		"":        "en-US",
		"unknown": "en-US",
	}
	for in, want := range cases {
		assert.Equal(t, want, BingMktForLocale(in), "locale %q", in)
	}
}

func TestMimeFor(t *testing.T) {
	assert.Equal(t, "image/png", MimeFor(".png"))
	assert.Equal(t, "image/jpeg", MimeFor(".jpg"))
	assert.Equal(t, "image/jpeg", MimeFor(".JPEG"))
	assert.Equal(t, "image/svg+xml", MimeFor(".svg"))
	assert.Equal(t, "image/webp", MimeFor(".webp"))
	assert.Equal(t, "image/gif", MimeFor(".gif"))
	assert.Equal(t, "", MimeFor(".bmp"))
}

func TestWriteAtomic_CreatesDirAndWritesFile(t *testing.T) {
	setupThemeDir(t)
	dir := LocalDir()
	require.NoError(t, WriteAtomic(dir, "local-1-ab.png", []byte("data")))

	got, err := os.ReadFile(filepath.Join(dir, "local-1-ab.png"))
	require.NoError(t, err)
	assert.Equal(t, "data", string(got))
	// The temp file must not survive the rename.
	assert.NoFileExists(t, filepath.Join(dir, "local-1-ab.png.tmp"))
}

func TestWriteAtomic_OverwritesAtomically(t *testing.T) {
	setupThemeDir(t)
	dir := LocalDir()
	require.NoError(t, WriteAtomic(dir, "local-1-ab.png", []byte("first")))
	require.NoError(t, WriteAtomic(dir, "local-1-ab.png", []byte("second")))

	got, err := os.ReadFile(filepath.Join(dir, "local-1-ab.png"))
	require.NoError(t, err)
	assert.Equal(t, "second", string(got))
}

func TestWriteAtomic_EmptyDirFails(t *testing.T) {
	origDataDir := model.DataDir
	model.DataDir = ""
	t.Cleanup(func() { model.DataDir = origDataDir })

	err := WriteAtomic("", "local-1-ab.png", []byte("x"))
	require.Error(t, err)
}

func TestRemoveFile(t *testing.T) {
	setupThemeDir(t)
	require.NoError(t, WriteAtomic(LocalDir(), "local-1-ab.png", []byte("x")))
	assert.FileExists(t, filepath.Join(LocalDir(), "local-1-ab.png"))

	RemoveFile("local-1-ab.png")
	assert.NoFileExists(t, filepath.Join(LocalDir(), "local-1-ab.png"))

	// Removing a malformed name must be a no-op, never a delete outside the dir.
	RemoveFile("../../etc/passwd")
	RemoveFile("local-1-ab.png") // already gone — idempotent
}

func TestRemoveOldBingFiles_KeepsCurrentAndRecent(t *testing.T) {
	setupThemeDir(t)
	dir := BingDir()

	names := []string{
		"bing-20260901.jpg", "bing-20260902.jpg", "bing-20260903.jpg",
		"bing-20260904.jpg", "bing-20260905.jpg",
	}
	for _, n := range names {
		require.NoError(t, WriteAtomic(dir, n, []byte("x")))
	}
	// A non-Bing file must never be touched.
	require.NoError(t, WriteAtomic(dir, "readme.txt", []byte("keep")))

	RemoveOldBingFiles("bing-20260905.jpg")

	// The kept file plus the next two most recent survive.
	assert.FileExists(t, filepath.Join(dir, "bing-20260905.jpg"))
	assert.FileExists(t, filepath.Join(dir, "bing-20260904.jpg"))
	assert.FileExists(t, filepath.Join(dir, "bing-20260903.jpg"))
	assert.NoFileExists(t, filepath.Join(dir, "bing-20260902.jpg"))
	assert.NoFileExists(t, filepath.Join(dir, "bing-20260901.jpg"))
	assert.FileExists(t, filepath.Join(dir, "readme.txt"), "non-bing file must stay")
}

func TestRemoveOldBingFiles_MissingDirIsNoop(t *testing.T) {
	setupThemeDir(t)
	// No bing dir exists yet — must not panic.
	RemoveOldBingFiles("bing-20260910.jpg")
}

func TestRemoveOldBingFiles_RetainsEveryExplicitKeep(t *testing.T) {
	setupThemeDir(t)
	dir := BingDir()

	names := []string{
		"bing-20260901.jpg", "bing-20260902.jpg", "bing-20260903.jpg",
		"bing-20260904.jpg", "bing-20260905.jpg",
	}
	for _, n := range names {
		require.NoError(t, WriteAtomic(dir, n, []byte("x")))
	}

	// Both the fresh image and the one config still references must survive,
	// even though the older one falls outside the "newest few" window.
	RemoveOldBingFiles("bing-20260905.jpg", "bing-20260901.jpg")

	assert.FileExists(t, filepath.Join(dir, "bing-20260905.jpg"))
	assert.FileExists(t, filepath.Join(dir, "bing-20260901.jpg"),
		"an explicitly retained image must survive")
	assert.NoFileExists(t, filepath.Join(dir, "bing-20260902.jpg"))
}

func TestReconcileLocalGallery(t *testing.T) {
	setupThemeDir(t)

	require.NoError(t, WriteAtomic(LocalDir(), "local-keep-a.png", []byte("x")))
	require.NoError(t, WriteAtomic(LocalDir(), "local-orphan-b.png", []byte("x")))
	// Non-gallery files must never be swept.
	require.NoError(t, WriteAtomic(ThemeDir(), "background.png", []byte("x")))
	require.NoError(t, WriteAtomic(BingDir(), "bing-20260910.jpg", []byte("x")))

	removed := ReconcileLocalGallery([]string{"local-keep-a.png"})

	assert.Equal(t, []string{"local-orphan-b.png"}, removed)
	assert.FileExists(t, filepath.Join(LocalDir(), "local-keep-a.png"))
	assert.NoFileExists(t, filepath.Join(LocalDir(), "local-orphan-b.png"))
	assert.FileExists(t, filepath.Join(ThemeDir(), "background.png"), "legacy wallpaper must stay")
	assert.FileExists(t, filepath.Join(BingDir(), "bing-20260910.jpg"), "bing cache must stay")
}

func TestReconcileLocalGallery_SkipsTempFiles(t *testing.T) {
	setupThemeDir(t)

	// An in-flight write's temp file must not be reclaimed.
	require.NoError(t, os.MkdirAll(LocalDir(), 0o755))
	tmp := filepath.Join(LocalDir(), "local-inflight-a.png.tmp")
	require.NoError(t, os.WriteFile(tmp, []byte("x"), 0o644))

	removed := ReconcileLocalGallery(nil)

	assert.Empty(t, removed)
	assert.FileExists(t, tmp, "a temp file from an in-flight write must be left alone")
}

func TestReconcileLocalGallery_EmptyDataDirIsNoop(t *testing.T) {
	origDataDir := model.DataDir
	model.DataDir = ""
	t.Cleanup(func() { model.DataDir = origDataDir })

	assert.Nil(t, ReconcileLocalGallery([]string{"local-1-a.png"}))
}

func TestSVGLooksSafe_RejectsForbiddenPatterns(t *testing.T) {
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
		assert.False(t, SVGLooksSafe([]byte(tc)), "should reject: %q", tc)
	}
	assert.True(t, SVGLooksSafe([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect width="10" height="10" fill="blue"/></svg>`)))
	assert.False(t, SVGLooksSafe(nil))
}

func TestSVGLooksSafe_HeadLongerThan4096(t *testing.T) {
	longSafe := `<svg xmlns="http://www.w3.org/2000/svg">` + strings.Repeat(" ", 5000) + `<rect width="10" height="10"/></svg>`
	assert.True(t, SVGLooksSafe([]byte(longSafe)))
}

func TestVerifyRasterConfig_RejectsOutOfRangeDimensions(t *testing.T) {
	err := VerifyRasterConfig(craftPngHeader(9000, 9000))
	require.Error(t, err)

	err = VerifyRasterConfig([]byte("not an image"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot decode image")
}

func TestDirs_EmptyWhenDataDirUnset(t *testing.T) {
	origDataDir := model.DataDir
	model.DataDir = ""
	t.Cleanup(func() { model.DataDir = origDataDir })

	assert.Equal(t, "", ThemeDir())
	assert.Equal(t, "", LocalDir())
	assert.Equal(t, "", BingDir())
}
