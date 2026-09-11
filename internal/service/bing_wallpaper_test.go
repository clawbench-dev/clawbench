package service

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/wallpaper"

	"github.com/stretchr/testify/assert"
)

// bigJPEG encodes a solid-color JPEG of the given size, used to assert how the
// pipeline resizes an oversized source.
func bigJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y += 7 {
		for x := 0; x < w; x += 7 {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 70}); err != nil {
		t.Fatalf("encode test jpeg: %v", err)
	}
	return buf.Bytes()
}

// bingTestEnv points model.DataDir at a temp dir and stubs the HTTP client plus
// the config-persistence callback, returning the recorded Bing states.
func bingTestEnv(t *testing.T) (*[]wallpaper.BingState, func()) {
	t.Helper()

	origDataDir := model.DataDir
	origConfig := model.ConfigInstance
	origClient := bingHTTPClient
	origPersist := persistBingStateFn

	model.DataDir = t.TempDir()
	model.ConfigInstance = model.Config{}

	var persisted []wallpaper.BingState
	persistBingStateFn = func(s wallpaper.BingState) error {
		persisted = append(persisted, s)
		// Mirror the handler: apply to the live config so follow-up work() runs
		// observe the outcome.
		b := &model.ConfigInstance.Appearance.Bing
		if s.File != "" {
			b.File = s.File
		}
		if s.LastSuccessDate != "" {
			b.LastSuccessDate = s.LastSuccessDate
		}
		b.Copyright = s.Copyright
		b.Title = s.Title
		if s.Mkt != "" {
			b.Mkt = s.Mkt
		}
		b.LastError = s.LastError
		b.LastAttemptAt = s.LastAttemptAt
		return nil
	}

	cleanup := func() {
		model.DataDir = origDataDir
		model.ConfigInstance = origConfig
		bingHTTPClient = origClient
		persistBingStateFn = origPersist
	}
	return &persisted, cleanup
}

// bingTestImage is a tiny valid PNG used as the downloaded image.
func bingTestImage(t *testing.T) []byte {
	t.Helper()
	// Reuse the shared package's expectations: a 1x1 PNG is enough for Process.
	return []byte{
		0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n',
		0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89,
		0x00, 0x00, 0x00, 0x0a, 'I', 'D', 'A', 'T',
		0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01,
		0x0d, 0x0a, 0x2d, 0xb4,
		0x00, 0x00, 0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
	}
}

// stubBingServer serves the metadata endpoint and the image, recording the
// requested mkt so tests can assert locale following.
func stubBingServer(t *testing.T, image []byte, seenMkt *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "HPImageArchive"):
			if seenMkt != nil {
				*seenMkt = r.URL.Query().Get("mkt")
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"images":[{"url":"%s","urlbase":"/th?id=x","copyright":"© Test Photographer","title":"Test Title"}]}`, "/img.png")
		case r.URL.Path == "/img.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(image)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// pointBingAt rewrites the package URLs at the test server. The metadata URL and
// base URL are both derived from the same server.
func pointBingAt(t *testing.T, srv *httptest.Server) {
	t.Helper()
	origArchive := bingArchiveURL
	origBase := bingBaseURL
	// The archive URL is built by concatenating the market, so keep the suffix.
	bingArchiveURL = srv.URL + "/HPImageArchive.aspx?format=js&idx=0&n=1&mkt="
	bingBaseURL = srv.URL
	t.Cleanup(func() {
		bingArchiveURL = origArchive
		bingBaseURL = origBase
	})
}

func TestBingWorker_FetchSuccess_WritesFileAndState(t *testing.T) {
	persisted, cleanup := bingTestEnv(t)
	defer cleanup()

	img := bingTestImage(t)
	srv := stubBingServer(t, img, nil)
	pointBingAt(t, srv)

	model.ConfigInstance.Appearance.Bing.Enabled = true
	model.ConfigInstance.Appearance.Bing.Mkt = "zh-CN"

	w := NewBingWallpaperWorker()
	w.work()

	if len(*persisted) != 1 {
		t.Fatalf("persisted %d states, want 1", len(*persisted))
	}
	state := (*persisted)[0]
	if state.LastError != "" {
		t.Errorf("LastError = %q, want empty", state.LastError)
	}
	if state.Copyright != "© Test Photographer" {
		t.Errorf("Copyright = %q, want the credit from the API", state.Copyright)
	}
	if state.Title != "Test Title" {
		t.Errorf("Title = %q, want Test Title", state.Title)
	}
	if state.LastSuccessDate == "" {
		t.Error("LastSuccessDate is empty, want today's date")
	}
	if state.File == "" {
		t.Fatal("File is empty, want a cached file name")
	}
	if state.Mkt != "zh-CN" {
		t.Errorf("Mkt = %q, want zh-CN", state.Mkt)
	}

	// The image must actually be on disk.
	abs, ok := wallpaper.FilePath(state.File)
	if !ok {
		t.Fatalf("cached file %q does not resolve", state.File)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Errorf("cached image missing on disk: %v", err)
	}
}

func TestBingWorker_SkipsWhenDisabled(t *testing.T) {
	persisted, cleanup := bingTestEnv(t)
	defer cleanup()

	// A server that fails the test if it is ever reached.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("worker must not fetch while disabled")
	}))
	defer srv.Close()
	pointBingAt(t, srv)

	model.ConfigInstance.Appearance.Bing.Enabled = false

	w := NewBingWallpaperWorker()
	w.work()

	if len(*persisted) != 0 {
		t.Errorf("persisted %d states while disabled, want 0", len(*persisted))
	}
}

func TestBingWorker_SkipsWhenAlreadySyncedToday(t *testing.T) {
	persisted, cleanup := bingTestEnv(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("worker must not refetch when today's image is cached")
	}))
	defer srv.Close()
	pointBingAt(t, srv)

	model.ConfigInstance.Appearance.Bing.Enabled = true
	model.ConfigInstance.Appearance.Bing.LastSuccessDate = time.Now().Format("20060102")
	model.ConfigInstance.Appearance.Bing.File = "bing-" + time.Now().Format("20060102") + ".jpg"

	w := NewBingWallpaperWorker()
	w.work()

	if len(*persisted) != 0 {
		t.Errorf("persisted %d states, want 0 (already synced today)", len(*persisted))
	}
}

func TestBingWorker_FetchFailure_KeepsCachedFileAndSetsError(t *testing.T) {
	persisted, cleanup := bingTestEnv(t)
	defer cleanup()

	// Every request fails.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	pointBingAt(t, srv)

	model.ConfigInstance.Appearance.Bing.Enabled = true
	// A previously cached image from an earlier successful fetch.
	model.ConfigInstance.Appearance.Bing.File = "bing-20260901.jpg"
	model.ConfigInstance.Appearance.Bing.LastSuccessDate = "20260901"

	w := NewBingWallpaperWorker()
	w.work()

	if len(*persisted) != 1 {
		t.Fatalf("persisted %d states, want 1", len(*persisted))
	}
	state := (*persisted)[0]
	if state.LastError == "" {
		t.Error("LastError is empty, want the failure reported")
	}
	// The failure must not clear the cached file, so the last good image keeps
	// serving. The worker signals "keep" by leaving File empty in the patch.
	if state.File != "" {
		t.Errorf("File = %q, want empty so the cached image is preserved", state.File)
	}
	if model.ConfigInstance.Appearance.Bing.File != "bing-20260901.jpg" {
		t.Errorf("cached File = %q, want the previous image preserved",
			model.ConfigInstance.Appearance.Bing.File)
	}
}

func TestBingWorker_UsesPersistedMkt(t *testing.T) {
	_, cleanup := bingTestEnv(t)
	defer cleanup()

	var seenMkt string
	img := bingTestImage(t)
	srv := stubBingServer(t, img, &seenMkt)
	pointBingAt(t, srv)

	model.ConfigInstance.Appearance.Bing.Enabled = true
	model.ConfigInstance.Appearance.Bing.Mkt = "en-US"

	w := NewBingWallpaperWorker()
	w.work()

	if seenMkt != "en-US" {
		t.Errorf("requested mkt = %q, want en-US", seenMkt)
	}
}

func TestBingWorker_DefaultsMktWhenUnset(t *testing.T) {
	_, cleanup := bingTestEnv(t)
	defer cleanup()

	var seenMkt string
	img := bingTestImage(t)
	srv := stubBingServer(t, img, &seenMkt)
	pointBingAt(t, srv)

	model.ConfigInstance.Appearance.Bing.Enabled = true
	model.ConfigInstance.Appearance.Bing.Mkt = ""

	w := NewBingWallpaperWorker()
	w.work()

	if seenMkt != "zh-CN" {
		t.Errorf("requested mkt = %q, want the zh-CN fallback", seenMkt)
	}
}

func TestBingWorker_FallsBackToUrlBase(t *testing.T) {
	persisted, cleanup := bingTestEnv(t)
	defer cleanup()

	img := bingTestImage(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "HPImageArchive") {
			// No "url" field — the worker must use urlbase + "_1920x1080.jpg".
			fmt.Fprint(w, `{"images":[{"urlbase":"/th?id=fallback","copyright":"c","title":"t"}]}`)
			return
		}
		if r.URL.Path == "/th" {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(img)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	pointBingAt(t, srv)

	model.ConfigInstance.Appearance.Bing.Enabled = true

	w := NewBingWallpaperWorker()
	w.work()

	if len(*persisted) != 1 {
		t.Fatalf("persisted %d states, want 1", len(*persisted))
	}
	if (*persisted)[0].LastError != "" {
		t.Errorf("LastError = %q, want empty (urlbase fallback should succeed)", (*persisted)[0].LastError)
	}
}

// ── Download URL selection ───────────────────────────────────────────────────

func TestBingImageURLs_PrefersUHD(t *testing.T) {
	origBase := bingBaseURL
	bingBaseURL = "https://example.test"
	t.Cleanup(func() { bingBaseURL = origBase })

	got := bingImageURLs(bingArchiveImage{
		URL:     "/th?id=OHR.X_1920x1080.jpg&pid=hp",
		URLBase: "/th?id=OHR.X",
	})

	// Bing's `url` field is only 1920x1080; the same image is served at
	// 3840x2160 from urlbase + "_UHD.jpg", which must be tried first.
	if len(got) == 0 {
		t.Fatal("no candidate URLs")
	}
	if got[0] != "https://example.test/th?id=OHR.X_UHD.jpg" {
		t.Errorf("first candidate = %q, want the UHD rendition", got[0])
	}
	// The lower-resolution forms stay as fallbacks.
	wantFallbacks := []string{
		"https://example.test/th?id=OHR.X_1920x1080.jpg&pid=hp",
		"https://example.test/th?id=OHR.X_1920x1080.jpg",
	}
	for _, want := range wantFallbacks {
		found := false
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Errorf("candidate list %v is missing fallback %q", got, want)
		}
	}
}

func TestBingImageURLs_WithoutURLBaseUsesURL(t *testing.T) {
	origBase := bingBaseURL
	bingBaseURL = "https://example.test"
	t.Cleanup(func() { bingBaseURL = origBase })

	got := bingImageURLs(bingArchiveImage{URL: "/th?id=only"})

	if len(got) != 1 || got[0] != "https://example.test/th?id=only" {
		t.Errorf("candidates = %v, want just the url field", got)
	}
}

func TestBingImageURLs_EmptyWhenNoImageFields(t *testing.T) {
	if got := bingImageURLs(bingArchiveImage{}); len(got) != 0 {
		t.Errorf("candidates = %v, want none", got)
	}
}

func TestBingImageURLs_DeduplicatesIdenticalForms(t *testing.T) {
	origBase := bingBaseURL
	bingBaseURL = "https://example.test"
	t.Cleanup(func() { bingBaseURL = origBase })

	// url already is the 1080p variant, so it must not be added twice.
	got := bingImageURLs(bingArchiveImage{
		URL:     "/th?id=OHR.X_1920x1080.jpg",
		URLBase: "/th?id=OHR.X",
	})
	seen := map[string]int{}
	for _, u := range got {
		seen[u]++
	}
	for u, n := range seen {
		if n > 1 {
			t.Errorf("url %q appears %d times, want once", u, n)
		}
	}
}

// TestBingWorker_FallsBackWhenUHDFails covers an entry with no UHD rendition:
// the worker must fall through to the lower-resolution URL rather than losing
// the whole day's wallpaper.
func TestBingWorker_FallsBackWhenUHDFails(t *testing.T) {
	persisted, cleanup := bingTestEnv(t)
	defer cleanup()

	img := bingTestImage(t)
	var triedUHD, triedFallback bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "HPImageArchive") {
			fmt.Fprint(w, `{"images":[{"url":"/img.png","urlbase":"/th?id=nouhd","copyright":"c","title":"t"}]}`)
			return
		}
		if strings.Contains(r.URL.RawQuery, "_UHD") {
			triedUHD = true
			http.NotFound(w, r) // no UHD rendition available
			return
		}
		triedFallback = true
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(img)
	}))
	defer srv.Close()
	pointBingAt(t, srv)

	model.ConfigInstance.Appearance.Bing.Enabled = true

	w := NewBingWallpaperWorker()
	w.work()

	if !triedUHD {
		t.Error("the UHD rendition was never attempted")
	}
	if !triedFallback {
		t.Error("the worker did not fall back to a lower-resolution URL")
	}
	if len(*persisted) != 1 || (*persisted)[0].LastError != "" {
		t.Errorf("state = %+v, want a successful fetch via fallback", *persisted)
	}
}

// TestBingWorker_KeepsNativeResolution pins option A: the Bing image is stored
// at its native 4K size, while uploads keep the smaller upload cap.
func TestBingWorker_KeepsNativeResolution(t *testing.T) {
	_, cleanup := bingTestEnv(t)
	defer cleanup()

	// A 4000x2000 source: wider than the upload cap (2048) but within the Bing
	// cap (3840), so it must be downscaled to 3840 — not 2048.
	img := bigJPEG(t, 4000, 2000)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "HPImageArchive") {
			fmt.Fprint(w, `{"images":[{"url":"/img.jpg","urlbase":"/th?id=big","copyright":"c","title":"t"}]}`)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(img)
	}))
	defer srv.Close()
	pointBingAt(t, srv)

	model.ConfigInstance.Appearance.Bing.Enabled = true

	w := NewBingWallpaperWorker()
	w.work()

	entries, err := os.ReadDir(wallpaper.BingDir())
	if err != nil || len(entries) == 0 {
		t.Fatalf("no cached bing image: %v", err)
	}
	f, err := os.Open(filepath.Join(wallpaper.BingDir(), entries[0].Name()))
	if err != nil {
		t.Fatalf("open cached image: %v", err)
	}
	defer func() { _ = f.Close() }()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		t.Fatalf("decode cached image: %v", err)
	}

	if cfg.Width != wallpaper.BingMaxLongEdge {
		t.Errorf("cached width = %d, want %d (native-ish 4K, not the upload cap %d)",
			cfg.Width, wallpaper.BingMaxLongEdge, wallpaper.MaxLongEdge)
	}
	if cfg.Width == wallpaper.MaxLongEdge {
		t.Error("the Bing image was downscaled to the upload cap — option A regressed")
	}
}

func TestBingWorker_EmptyImageListIsError(t *testing.T) {
	persisted, cleanup := bingTestEnv(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"images":[]}`)
	}))
	defer srv.Close()
	pointBingAt(t, srv)

	model.ConfigInstance.Appearance.Bing.Enabled = true

	w := NewBingWallpaperWorker()
	w.work()

	if len(*persisted) != 1 {
		t.Fatalf("persisted %d states, want 1", len(*persisted))
	}
	if !strings.Contains((*persisted)[0].LastError, "no images") {
		t.Errorf("LastError = %q, want a 'no images' error", (*persisted)[0].LastError)
	}
}

func TestBingWorker_MalformedJSONIsError(t *testing.T) {
	persisted, cleanup := bingTestEnv(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{not json`)
	}))
	defer srv.Close()
	pointBingAt(t, srv)

	model.ConfigInstance.Appearance.Bing.Enabled = true

	w := NewBingWallpaperWorker()
	w.work()

	if len(*persisted) != 1 {
		t.Fatalf("persisted %d states, want 1", len(*persisted))
	}
	if !strings.Contains((*persisted)[0].LastError, "parse") {
		t.Errorf("LastError = %q, want a parse error", (*persisted)[0].LastError)
	}
}

func TestBingWorker_RejectsNonImagePayload(t *testing.T) {
	persisted, cleanup := bingTestEnv(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "HPImageArchive") {
			fmt.Fprint(w, `{"images":[{"url":"/evil.png"}]}`)
			return
		}
		// Serve HTML where an image was advertised — the shared pipeline must
		// reject it rather than caching a non-image.
		fmt.Fprint(w, "<html>not an image</html>")
	}))
	defer srv.Close()
	pointBingAt(t, srv)

	model.ConfigInstance.Appearance.Bing.Enabled = true

	w := NewBingWallpaperWorker()
	w.work()

	if len(*persisted) != 1 {
		t.Fatalf("persisted %d states, want 1", len(*persisted))
	}
	if !strings.Contains((*persisted)[0].LastError, "invalid bing image") {
		t.Errorf("LastError = %q, want an invalid-image error", (*persisted)[0].LastError)
	}
	if (*persisted)[0].File != "" {
		t.Errorf("File = %q, want empty on failure", (*persisted)[0].File)
	}
}

func TestBingWorker_DownloadSizeGuard(t *testing.T) {
	persisted, cleanup := bingTestEnv(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "HPImageArchive") {
			fmt.Fprint(w, `{"images":[{"url":"/big.png"}]}`)
			return
		}
		// Stream more than the wallpaper cap; the worker must stop reading.
		w.Header().Set("Content-Type", "image/png")
		chunk := make([]byte, 1<<20)
		for range (wallpaper.MaxBytes >> 20) + 2 {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	pointBingAt(t, srv)

	model.ConfigInstance.Appearance.Bing.Enabled = true

	w := NewBingWallpaperWorker()
	w.work()

	if len(*persisted) != 1 {
		t.Fatalf("persisted %d states, want 1", len(*persisted))
	}
	if (*persisted)[0].LastError == "" {
		t.Error("LastError is empty, want the oversize download rejected")
	}
}

func TestBingWorker_PrunesOldBingFiles(t *testing.T) {
	_, cleanup := bingTestEnv(t)
	defer cleanup()

	img := bingTestImage(t)
	srv := stubBingServer(t, img, nil)
	pointBingAt(t, srv)

	// Pre-seed old cached images.
	require := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	for _, n := range []string{"bing-20260801.jpg", "bing-20260802.jpg", "bing-20260803.jpg"} {
		require(wallpaper.WriteAtomic(wallpaper.BingDir(), n, []byte("x")))
	}

	model.ConfigInstance.Appearance.Bing.Enabled = true

	w := NewBingWallpaperWorker()
	w.work()

	// Only the newest few survive.
	entries, err := os.ReadDir(wallpaper.BingDir())
	require(err)
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "bing-") && strings.HasSuffix(e.Name(), ".jpg") {
			names = append(names, e.Name())
		}
	}
	if len(names) > 3 {
		t.Errorf("bing cache has %d files (%v), want at most 3", len(names), names)
	}
	// The oldest must be gone.
	if _, err := os.Stat(filepath.Join(wallpaper.BingDir(), "bing-20260801.jpg")); err == nil {
		t.Error("oldest cached bing image was not pruned")
	}
}

func TestBingWorker_TriggerRunsImmediately(t *testing.T) {
	persisted, cleanup := bingTestEnv(t)
	defer cleanup()

	img := bingTestImage(t)
	srv := stubBingServer(t, img, nil)
	pointBingAt(t, srv)

	model.ConfigInstance.Appearance.Bing.Enabled = true

	w := newBingWallpaperWorkerForTest()
	w.Start()
	defer w.Stop()

	// Wait for the startup run to complete, then clear the record and trigger.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(*persisted) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if len(*persisted) == 0 {
		t.Fatal("startup run did not persist a state")
	}

	// Force a re-run by clearing today's marker, then trigger.
	model.ConfigInstance.Appearance.Bing.LastSuccessDate = ""
	model.ConfigInstance.Appearance.Bing.File = ""
	*persisted = nil

	w.Trigger()

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(*persisted) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if len(*persisted) == 0 {
		t.Error("Trigger did not cause an immediate run")
	}
}

// TestBingWorker_TriggerBypassesStartupDelay is the regression guard for the
// first-fetch delay: the worker used to sit out its whole startup wait before
// looking at the trigger channel, so a manual "sync now" right after boot was
// silently ignored for up to a minute.
func TestBingWorker_TriggerBypassesStartupDelay(t *testing.T) {
	persisted, cleanup := bingTestEnv(t)
	defer cleanup()

	img := bingTestImage(t)
	srv := stubBingServer(t, img, nil)
	pointBingAt(t, srv)

	model.ConfigInstance.Appearance.Bing.Enabled = true

	w := newBingWallpaperWorkerForTest()
	// A startup delay far longer than the test's patience: only the trigger can
	// produce a fetch in time.
	w.startup = time.Hour

	w.Start()
	defer w.Stop()

	w.Trigger()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(*persisted) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if len(*persisted) == 0 {
		t.Error("Trigger did not bypass the startup delay")
	}
}

func TestBingWorker_Lifecycle_StartStopIdempotent(t *testing.T) {
	_, cleanup := bingTestEnv(t)
	defer cleanup()

	// Disabled so the startup run is a no-op and never touches the network.
	model.ConfigInstance.Appearance.Bing.Enabled = false

	w := newBingWallpaperWorkerForTest()
	w.Start()
	w.Start() // second start must be a no-op, not a double goroutine
	w.Stop()
	w.Stop() // second stop must be a no-op

	// Restarting after a stop must work.
	w2 := newBingWallpaperWorkerForTest()
	w2.Start()
	w2.Stop()
}

func TestBingWorker_GlobalSingletonLifecycle(t *testing.T) {
	_, cleanup := bingTestEnv(t)
	defer cleanup()

	model.ConfigInstance.Appearance.Bing.Enabled = false

	StartBingWallpaperWorker()
	StartBingWallpaperWorker() // idempotent

	// Triggering must not panic while running.
	TriggerBingSync()

	StopBingWallpaperWorker()
	// Triggering after stop must be a safe no-op.
	TriggerBingSync()
	StopBingWallpaperWorker() // idempotent
}

func TestBingWorker_PersistErrorDoesNotPanic(t *testing.T) {
	persisted, cleanup := bingTestEnv(t)
	defer cleanup()

	img := bingTestImage(t)
	srv := stubBingServer(t, img, nil)
	pointBingAt(t, srv)

	// Persistence fails — the fetch itself must still be reported and not panic.
	persistBingStateFn = func(wallpaper.BingState) error {
		return fmt.Errorf("disk full")
	}

	model.ConfigInstance.Appearance.Bing.Enabled = true

	w := NewBingWallpaperWorker()
	w.work()

	if len(*persisted) != 0 {
		t.Errorf("persisted %d states via the failing callback, want 0", len(*persisted))
	}
}

// TestBingWorker_DoesNotPruneWhenPersistFails guards the cached image the
// on-disk config still references: if the state write fails, the config keeps
// pointing at an older image, so pruning to "today" would delete the very file
// being served.
func TestBingWorker_DoesNotPruneWhenPersistFails(t *testing.T) {
	_, cleanup := bingTestEnv(t)
	defer cleanup()

	img := bingTestImage(t)
	srv := stubBingServer(t, img, nil)
	pointBingAt(t, srv)

	// Seed several older cached images plus the one config references.
	for _, n := range []string{"bing-20260801.jpg", "bing-20260802.jpg", "bing-20260803.jpg", "bing-20260804.jpg"} {
		if err := wallpaper.WriteAtomic(wallpaper.BingDir(), n, []byte("x")); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	model.ConfigInstance.Appearance.Bing.File = "bing-20260801.jpg"

	persistBingStateFn = func(wallpaper.BingState) error {
		return fmt.Errorf("config unwritable")
	}
	model.ConfigInstance.Appearance.Bing.Enabled = true

	w := NewBingWallpaperWorker()
	w.work()

	// Nothing may have been pruned while the config still references the old image.
	assert.FileExists(t, filepath.Join(wallpaper.BingDir(), "bing-20260801.jpg"),
		"the config-referenced image must not be pruned when the state write failed")
}

func TestBingWorker_PruneRetainsConfigReferencedFile(t *testing.T) {
	_, cleanup := bingTestEnv(t)
	defer cleanup()

	img := bingTestImage(t)
	srv := stubBingServer(t, img, nil)
	pointBingAt(t, srv)

	// Many old images, plus one the config references but the fetch will NOT
	// replace (simulating a config whose state lags the cache).
	for _, n := range []string{"bing-20260101.jpg", "bing-20260102.jpg", "bing-20260103.jpg", "bing-20260104.jpg", "bing-20260105.jpg"} {
		if err := wallpaper.WriteAtomic(wallpaper.BingDir(), n, []byte("x")); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// Persistence keeps the config pointing at the old image (as it would if
	// another writer had just set it), so the sweep must not delete it.
	persistBingStateFn = func(s wallpaper.BingState) error {
		model.ConfigInstance.Appearance.Bing.File = "bing-20260101.jpg"
		return nil
	}
	model.ConfigInstance.Appearance.Bing.Enabled = true
	model.ConfigInstance.Appearance.Bing.File = "bing-20260101.jpg"

	w := NewBingWallpaperWorker()
	w.work()

	assert.FileExists(t, filepath.Join(wallpaper.BingDir(), "bing-20260101.jpg"),
		"an image the live config still references must survive the sweep")
}

func TestSetPersistBingStateFn(t *testing.T) {
	orig := persistBingStateFn
	defer func() { persistBingStateFn = orig }()

	called := false
	SetPersistBingStateFn(func(wallpaper.BingState) error {
		called = true
		return nil
	})
	if persistBingStateFn == nil {
		t.Fatal("SetPersistBingStateFn did not install the callback")
	}
	_ = persistBingStateFn(wallpaper.BingState{})
	if !called {
		t.Error("installed callback was not invoked")
	}
}
