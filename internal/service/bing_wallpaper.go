package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/wallpaper"
)

// bingArchiveURL is the Bing daily-image metadata endpoint. It returns a small
// JSON document describing the current image for the requested market; the
// image itself is fetched separately from the URL it advertises.
//
// These are variables (not constants) so tests can point them at a stub server.
var (
	bingArchiveURL = "https://www.bing.com/HPImageArchive.aspx?format=js&idx=0&n=1&mkt="
	bingBaseURL    = "https://www.bing.com"
)

// bingFetchTimeout bounds a single fetch (metadata + image download).
const bingFetchTimeout = 30 * time.Second

// bingHTTPClient performs the fetches. It is a package variable so tests can
// substitute a stub transport.
var bingHTTPClient = http.DefaultClient

// persistBingStateFn persists the fetch outcome into config. It is injected by
// main.go (handler.PersistBingWallpaperState) because the service package must
// not import the handler package.
var persistBingStateFn func(wallpaper.BingState) error

// SetPersistBingStateFn wires the config persistence callback for the Bing
// wallpaper worker. Called from main.go during startup.
func SetPersistBingStateFn(fn func(wallpaper.BingState) error) {
	persistBingStateFn = fn
}

// bingArchiveImage is one entry in Bing's HPImageArchive payload.
type bingArchiveImage struct {
	URL       string `json:"url"`
	URLBase   string `json:"urlbase"`
	Copyright string `json:"copyright"`
	Title     string `json:"title"`
}

// bingArchiveResponse is the subset of Bing's HPImageArchive payload we use.
type bingArchiveResponse struct {
	Images []bingArchiveImage `json:"images"`
}

// BingWallpaperWorker fetches the Bing daily wallpaper once per day and caches
// it on disk, so every client sees the same image without each one reaching out
// to Bing.
//
// The worker runs for the lifetime of the process even when the feature is
// disabled: work() re-reads the enabled flag on every run, so toggling the
// setting only needs Trigger() to take effect immediately rather than a
// stop/start cycle.
type BingWallpaperWorker struct {
	stopCh   chan struct{}
	doneCh   chan struct{}
	trigger  chan struct{}
	mu       sync.Mutex
	running  bool
	startup  time.Duration // delay before the first run
	interval time.Duration // interval between runs
}

// NewBingWallpaperWorker creates the production worker.
func NewBingWallpaperWorker() *BingWallpaperWorker {
	return &BingWallpaperWorker{
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
		trigger:  make(chan struct{}, 1),
		startup:  1 * time.Minute,
		interval: 24 * time.Hour,
	}
}

// newBingWallpaperWorkerForTest creates a worker with short timings so lifecycle
// tests do not wait a minute.
func newBingWallpaperWorkerForTest() *BingWallpaperWorker {
	return &BingWallpaperWorker{
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
		trigger:  make(chan struct{}, 1),
		startup:  10 * time.Millisecond,
		interval: 1 * time.Hour,
	}
}

// Start begins the fetch loop in a goroutine. Idempotent.
func (w *BingWallpaperWorker) Start() {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.mu.Unlock()

	go w.run()
	slog.Info("bing wallpaper worker started")
}

// Stop halts the loop and waits for the current run to finish.
func (w *BingWallpaperWorker) Stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	close(w.stopCh)
	w.mu.Unlock()

	<-w.doneCh

	w.mu.Lock()
	w.running = false
	w.mu.Unlock()
	slog.Info("bing wallpaper worker stopped")
}

// Trigger requests an immediate fetch without waiting for the daily tick. It
// never blocks: if a trigger is already queued it is coalesced.
func (w *BingWallpaperWorker) Trigger() {
	select {
	case w.trigger <- struct{}{}:
	default:
	}
}

func (w *BingWallpaperWorker) run() {
	defer close(w.doneCh)

	// The startup delay keeps the first fetch off the critical path of server
	// boot, but a manual "sync now" must not wait it out — so the trigger is
	// honored here too, and the loop below starts either way.
	select {
	case <-time.After(w.startup):
		w.work()
	case <-w.trigger:
		w.work()
	case <-w.stopCh:
		return
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			w.work()
		case <-w.trigger:
			w.work()
		case <-w.stopCh:
			return
		}
	}
}

// work performs one fetch attempt. Every run re-reads the live config so a
// settings change is picked up without restarting the worker.
func (w *BingWallpaperWorker) work() {
	// Read the live config the same way other service code does (see
	// session_runtime.go); config writes are serialized by the handler's mutex.
	bing := model.ConfigInstance.Appearance.Bing
	if !bing.Enabled {
		return
	}

	today := time.Now().Format("20060102")
	if bing.LastSuccessDate == today && bing.File != "" {
		return // already have today's image
	}

	mkt := bing.Mkt
	if mkt == "" {
		mkt = "zh-CN"
	}

	state, err := fetchBingWallpaper(mkt, today)
	state.Mkt = mkt
	state.LastAttemptAt = time.Now().Unix()
	if err != nil {
		// Keep the previously cached File untouched so the last good image
		// keeps serving; surface the failure to the settings panel instead.
		state.File = ""
		state.LastSuccessDate = ""
		state.LastError = err.Error()
		slog.Warn("bing wallpaper fetch failed", slog.String("err", err.Error()))
	} else {
		state.LastError = ""
		slog.Info("bing wallpaper updated", slog.String("file", state.File))
	}

	if persistBingStateFn != nil {
		if perr := persistBingStateFn(state); perr != nil {
			slog.Warn("bing wallpaper state persist failed", slog.String("err", perr.Error()))
			// Do not prune: the on-disk config may still reference an older
			// cached image (this write failed, so the in-memory rollback means
			// the reference is unchanged), and the sweep would delete it.
			return
		}
	}
	if err == nil {
		// Also retain whatever the live config still references, in case the
		// persisted state lags behind (e.g. another writer updated the config
		// between our persist and this sweep).
		wallpaper.RemoveOldBingFiles(state.File, model.ConfigInstance.Appearance.Bing.File)
	}
}

// fetchBingWallpaper resolves and downloads the current Bing image for the
// given market, returning the state to record on success. On failure the
// returned state carries only what is known (copyright/title may be empty).
func fetchBingWallpaper(mkt, today string) (wallpaper.BingState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), bingFetchTimeout)
	defer cancel()

	meta, err := fetchBingMetadata(ctx, mkt)
	if err != nil {
		return wallpaper.BingState{}, err
	}

	imgURL := meta.URL
	if imgURL == "" {
		// Fall back to the documented 1920x1080 variant of urlbase.
		imgURL = meta.URLBase + "_1920x1080.jpg"
	}
	if imgURL == "" {
		return wallpaper.BingState{}, fmt.Errorf("bing response contained no image url")
	}
	if strings.HasPrefix(imgURL, "/") {
		imgURL = bingBaseURL + imgURL
	}

	data, err := downloadBingImage(ctx, imgURL)
	if err != nil {
		return wallpaper.BingState{}, err
	}

	// Reuse the shared pipeline so the Bing image gets the same validation and
	// downscaling as an upload.
	processed, err := wallpaper.Process(data, "bing.jpg")
	if err != nil {
		return wallpaper.BingState{}, fmt.Errorf("invalid bing image: %w", err)
	}

	fileName := "bing-" + today + processed.Ext
	if err := wallpaper.WriteAtomic(wallpaper.BingDir(), fileName, processed.Data); err != nil {
		return wallpaper.BingState{}, err
	}

	return wallpaper.BingState{
		File:            fileName,
		LastSuccessDate: today,
		Copyright:       meta.Copyright,
		Title:           meta.Title,
	}, nil
}

// fetchBingMetadata retrieves the daily image descriptor for a market.
func fetchBingMetadata(ctx context.Context, mkt string) (bingArchiveImage, error) {
	var empty bingArchiveImage

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, bingArchiveURL+mkt, http.NoBody)
	if err != nil {
		return empty, fmt.Errorf("cannot build bing request: %w", err)
	}
	resp, err := bingHTTPClient.Do(req)
	if err != nil {
		return empty, fmt.Errorf("cannot reach bing: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return empty, fmt.Errorf("bing returned status %d", resp.StatusCode)
	}

	var archive bingArchiveResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&archive); err != nil {
		return empty, fmt.Errorf("cannot parse bing response: %w", err)
	}
	if len(archive.Images) == 0 {
		return empty, fmt.Errorf("bing returned no images")
	}
	return archive.Images[0], nil
}

// downloadBingImage fetches the image bytes, capped at the wallpaper size limit
// so a hostile or malformed response cannot exhaust memory.
func downloadBingImage(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("cannot build image request: %w", err)
	}
	resp, err := bingHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot download bing image: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bing image returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, wallpaper.MaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read bing image: %w", err)
	}
	if int64(len(data)) > wallpaper.MaxBytes {
		return nil, fmt.Errorf("bing image too large")
	}
	return data, nil
}

// ── Global singleton ─────────────────────────────────────────────────────────

var (
	globalBingWallpaper *BingWallpaperWorker
	bingWallpaperMu     sync.Mutex
)

// StartBingWallpaperWorker starts the process-wide worker.
func StartBingWallpaperWorker() {
	bingWallpaperMu.Lock()
	defer bingWallpaperMu.Unlock()
	if globalBingWallpaper != nil {
		return
	}
	w := NewBingWallpaperWorker()
	w.Start()
	globalBingWallpaper = w
}

// StopBingWallpaperWorker stops the process-wide worker, if running.
func StopBingWallpaperWorker() {
	bingWallpaperMu.Lock()
	w := globalBingWallpaper
	globalBingWallpaper = nil
	bingWallpaperMu.Unlock()
	if w != nil {
		w.Stop()
	}
}

// TriggerBingSync asks the process-wide worker for an immediate fetch. Safe to
// call when the worker is not running (no-op).
func TriggerBingSync() {
	bingWallpaperMu.Lock()
	w := globalBingWallpaper
	bingWallpaperMu.Unlock()
	if w != nil {
		w.Trigger()
	}
}
