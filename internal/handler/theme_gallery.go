//nolint:goconst // response field names are domain strings
package handler

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/wallpaper"
)

// ── Request / response types ─────────────────────────────────────────────────

// galleryItemView is one gallery image as exposed to the client.
type galleryItemView struct {
	File       string `json:"file"`
	Name       string `json:"name"`
	UploadedAt int64  `json:"uploaded_at"`
	Size       int64  `json:"size"`
}

// galleryUploadError reports one file that failed during a multi-file upload.
type galleryUploadError struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

// galleryUploadResponse is the body of POST /api/theme/local/upload. Uploads are
// best-effort per file: successful files are returned in Items, failures in
// Errors, so one bad image does not discard the rest of the batch.
type galleryUploadResponse struct {
	Items  []galleryItemView    `json:"items"`
	Errors []galleryUploadError `json:"errors"`
}

// gallerySelectRequest is the JSON body of POST /api/theme/local/select.
type gallerySelectRequest struct {
	Name string `json:"name"`
}

// wallpaperModeRequest is the JSON body of POST /api/theme/wallpaper. Both
// fields are optional; only the ones present are applied.
type wallpaperModeRequest struct {
	Mode    *string `json:"mode"`
	Enabled *bool   `json:"enabled"`
}

// wallpaperStateResponse is the common state body returned by the wallpaper
// mutation endpoints, so the client can refresh in one round-trip.
type wallpaperStateResponse struct {
	Mode       string `json:"mode"`
	Enabled    bool   `json:"enabled"`
	ActiveFile string `json:"active_file"`
	Selected   string `json:"selected"`
}

// bingStatusResponse is the body of GET /api/theme/bing/status.
type bingStatusResponse struct {
	Enabled         bool   `json:"enabled"`
	File            string `json:"file"`
	LastSuccessDate string `json:"last_success_date"`
	Copyright       string `json:"copyright"`
	Title           string `json:"title"`
	Mkt             string `json:"mkt"`
	LastError       string `json:"last_error"`
	LastAttemptAt   int64  `json:"last_attempt_at"`
}

// ── Gallery endpoints ────────────────────────────────────────────────────────

// ServeThemeLocalUpload handles POST /api/theme/local/upload — adds one or more
// images to the local gallery via multipart form field "files".
func ServeThemeLocalUpload(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost, http.MethodDelete) {
		return
	}
	if r.Method == http.MethodDelete {
		serveThemeLocalDelete(w, r)
		return
	}
	serveThemeLocalUploadPost(w, r)
}

func serveThemeLocalUploadPost(w http.ResponseWriter, r *http.Request) {
	// +1MB of multipart framing overhead on top of the per-file budget; the
	// per-file cap is enforced again below on the decoded bytes.
	r.Body = http.MaxBytesReader(w, r.Body, int64(wallpaper.MaxBytes)*wallpaper.MaxUploadsPerRequest+1<<20)
	if err := r.ParseMultipartForm(int64(wallpaper.MaxBytes) * wallpaper.MaxUploadsPerRequest); err != nil { //nolint:gosec // MaxBytesReader bounds the parse
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "FileTooLargeOrInvalid")
		return
	}
	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "NoFileProvided")
		return
	}
	if len(files) > wallpaper.MaxUploadsPerRequest {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
		return
	}

	// The authoritative capacity check happens inside appendGalleryItems, under
	// the same lock as the append, so concurrent uploads cannot both pass a
	// stale count and push the gallery past its limit. This early check only
	// avoids writing files that would certainly be rejected.
	configMutex.Lock()
	existing := len(model.ConfigInstance.Appearance.Local.Items)
	configMutex.Unlock()
	if existing+len(files) > wallpaper.MaxGalleryItems {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
		return
	}

	resp := galleryUploadResponse{Items: []galleryItemView{}, Errors: []galleryUploadError{}}
	for _, header := range files {
		item, err := storeGalleryUpload(r, header)
		if err != nil {
			resp.Errors = append(resp.Errors, galleryUploadError{Name: header.Filename, Error: err.Error()})
			continue
		}
		resp.Items = append(resp.Items, *item)
	}

	// Append the accepted items to the gallery and, when nothing was selected
	// yet, auto-select the first new image so the upload takes effect
	// immediately and the mode switches to local.
	if len(resp.Items) > 0 {
		added, err := appendGalleryItems(resp.Items)
		if err != nil {
			if errors.Is(err, errGalleryFull) {
				// Another upload filled the gallery while these files were being
				// processed — drop the files we just wrote and report it.
				for _, it := range resp.Items {
					wallpaper.RemoveFile(it.File)
				}
				writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
				return
			}
			// Config write failed; the files are on disk but unreferenced. Remove
			// them so a failed upload does not leak orphans.
			for _, it := range resp.Items {
				wallpaper.RemoveFile(it.File)
			}
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "WriteFailed", map[string]any{"Error": err.Error()})
			return
		}
		resp.Items = added
	}
	writeJSON(w, http.StatusOK, resp)
}

// storeGalleryUpload validates and writes one uploaded file, returning its
// gallery entry. The stored name is generated (never client-controlled) and
// carries the local- prefix so it resolves inside <DataDir>/theme/local.
func storeGalleryUpload(r *http.Request, header *multipart.FileHeader) (*galleryItemView, error) {
	file, err := header.Open()
	if err != nil {
		return nil, fmt.Errorf("cannot read upload")
	}
	defer func() { _ = file.Close() }()

	if header.Size > wallpaper.MaxBytes {
		return nil, fmt.Errorf("image too large")
	}
	// Read with a hard limit so a lying Content-Length cannot exhaust memory.
	buf, err := io.ReadAll(io.LimitReader(file, wallpaper.MaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read upload")
	}
	if int64(len(buf)) > wallpaper.MaxBytes {
		return nil, fmt.Errorf("image too large")
	}

	// Bound concurrent decodes: each one may briefly hold a large RGBA buffer.
	if !acquireDecodeSlot(r) {
		return nil, fmt.Errorf("upload cancelled")
	}
	processed, err := wallpaper.Process(buf, header.Filename)
	releaseDecodeSlot()
	if err != nil {
		return nil, err
	}

	name := newGalleryFileName(processed.Ext)
	if err := wallpaper.WriteAtomic(wallpaper.LocalDir(), name, processed.Data); err != nil {
		return nil, err
	}
	return &galleryItemView{
		File:       name,
		Name:       filepath.Base(header.Filename),
		UploadedAt: time.Now().Unix(),
		Size:       int64(len(processed.Data)),
	}, nil
}

// newGalleryFileName builds a collision-resistant stored name: a nanosecond
// timestamp plus random suffix, both so that two uploads in the same instant
// never collide and so a name cannot be guessed from the original file name.
func newGalleryFileName(ext string) string {
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	return fmt.Sprintf("local-%d-%s%s", time.Now().UnixNano(), hex.EncodeToString(b), ext)
}

// errGalleryFull reports that the gallery is at capacity. Checked under the
// same lock as the append so concurrent uploads cannot exceed the limit.
var errGalleryFull = errors.New("gallery is full")

// galleryDecodeSem bounds how many uploads decode images at once. A single
// decoded image may occupy ~256MB (MaxDimension² × 4 bytes), so without a bound
// a handful of concurrent uploads could exhaust memory on a small host.
var galleryDecodeSem = make(chan struct{}, 2)

// acquireDecodeSlot blocks until a decode slot is free, returning false if the
// request was cancelled first.
func acquireDecodeSlot(r *http.Request) bool {
	select {
	case galleryDecodeSem <- struct{}{}:
		return true
	case <-r.Context().Done():
		return false
	}
}

func releaseDecodeSlot() { <-galleryDecodeSem }

// appendGalleryItems records newly stored files in the gallery config. When no
// image was selected before, the first new item becomes the selection and the
// mode switches to local, so an upload is visible immediately.
//
// Returns the items actually added, which may be fewer than requested when the
// gallery fills up; errGalleryFull is returned when none could be added.
func appendGalleryItems(items []galleryItemView) ([]galleryItemView, error) {
	configMutex.Lock()
	defer configMutex.Unlock()

	app := &model.ConfigInstance.Appearance

	// Capacity is enforced here, inside the lock, so two concurrent uploads
	// cannot both observe spare room and jointly exceed the limit.
	room := wallpaper.MaxGalleryItems - len(app.Local.Items)
	if room <= 0 {
		return nil, errGalleryFull
	}
	accepted := items
	if len(accepted) > room {
		accepted = accepted[:room]
	}

	// Deep-copy before mutating so a failed persist can restore the original
	// gallery exactly (a shallow snapshot would share the backing array).
	snapshot := model.ConfigInstance
	snapshot.Appearance.Local.Items = append([]model.LocalWallpaperItem(nil), app.Local.Items...)

	for _, it := range accepted {
		app.Local.Items = append(app.Local.Items, model.LocalWallpaperItem{
			File:       it.File,
			Name:       it.Name,
			UploadedAt: it.UploadedAt,
			Size:       it.Size,
		})
	}
	if app.Local.Selected == "" && len(accepted) > 0 {
		app.Local.Selected = accepted[0].File
		app.WallpaperMode = "local"
		app.WallpaperEnabled = true
	}
	if err := persistAppearanceLocked(snapshot); err != nil {
		return nil, err
	}
	return accepted, nil
}

// serveThemeLocalDelete handles DELETE /api/theme/local/item?name= — removes one
// gallery image. Deleting the selected image reselects a neighbor (preferring
// the previous one) so the wallpaper does not silently disappear.
func serveThemeLocalDelete(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" || !model.IsThemeAllowedExt(name) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
		return
	}

	configMutex.Lock()
	// Deep-copy the gallery before mutating it. A shallow snapshot would share
	// the backing array with ConfigInstance, so the in-place delete below would
	// corrupt the snapshot and a rollback would restore a duplicated list.
	snapshot := model.ConfigInstance
	snapshot.Appearance.Local.Items = append([]model.LocalWallpaperItem(nil), model.ConfigInstance.Appearance.Local.Items...)

	app := &model.ConfigInstance.Appearance

	idx := -1
	for i, it := range app.Local.Items {
		if it.File == name {
			idx = i
			break
		}
	}
	if idx == -1 {
		configMutex.Unlock()
		writeLocalizedError(w, r, model.NotFound(nil, "FileNotFoundShort"))
		return
	}

	// Build a new slice rather than deleting in place, so the snapshot above
	// keeps pointing at the original contents.
	app.Local.Items = append(append([]model.LocalWallpaperItem(nil), app.Local.Items[:idx]...),
		app.Local.Items[idx+1:]...)
	if app.Local.Selected == name {
		app.Local.Selected = ""
		if len(app.Local.Items) > 0 {
			// Prefer the previous item so repeated deletes walk backwards
			// predictably rather than jumping to the end.
			next := idx - 1
			if next < 0 {
				next = 0
			}
			app.Local.Selected = app.Local.Items[next].File
		}
	}
	// The legacy single-file field may still name this image (it is retained
	// through the upgrade migration). Clear it too, otherwise the client's
	// legacy fallback would keep showing a wallpaper the user just deleted.
	if app.WallpaperFile == name {
		app.WallpaperFile = ""
	}
	err := persistAppearanceLocked(snapshot)
	configMutex.Unlock()

	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "WriteFailed", map[string]any{"Error": err.Error()})
		return
	}
	// Config no longer references the file — safe to unlink.
	wallpaper.RemoveFile(name)
	writeJSON(w, http.StatusOK, currentWallpaperState())
}

// ServeThemeLocalSelect handles POST /api/theme/local/select — makes a gallery
// image the active wallpaper, switching the mode to local and enabling it.
func ServeThemeLocalSelect(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req gallerySelectRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" || !model.IsThemeAllowedExt(req.Name) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
		return
	}

	configMutex.Lock()
	snapshot := model.ConfigInstance
	app := &model.ConfigInstance.Appearance

	found := false
	for _, it := range app.Local.Items {
		if it.File == req.Name {
			found = true
			break
		}
	}
	if !found {
		configMutex.Unlock()
		writeLocalizedError(w, r, model.NotFound(nil, "FileNotFoundShort"))
		return
	}
	app.Local.Selected = req.Name
	app.WallpaperMode = "local"
	app.WallpaperEnabled = true
	err := persistAppearanceLocked(snapshot)
	configMutex.Unlock()

	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "WriteFailed", map[string]any{"Error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, currentWallpaperState())
}

// ServeThemeWallpaperMode handles POST /api/theme/wallpaper — switches the
// active wallpaper source and/or toggles the global wallpaper switch.
func ServeThemeWallpaperMode(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req wallpaperModeRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	configMutex.Lock()
	snapshot := model.ConfigInstance
	app := &model.ConfigInstance.Appearance

	if req.Mode != nil {
		if *req.Mode != "local" && *req.Mode != "bing" {
			configMutex.Unlock()
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
			return
		}
		app.WallpaperMode = *req.Mode
		// Selecting the Bing source means "fetch and show the Bing image", so the
		// fetch switch must follow. It has no UI of its own — leaving it false
		// here produced a dead state where the mode said Bing while the worker
		// silently returned without fetching, so the sync button did nothing and
		// no error was ever reported.
		app.Bing.Enabled = *req.Mode == "bing"
	}
	if req.Enabled != nil {
		app.WallpaperEnabled = *req.Enabled
	}
	err := persistAppearanceLocked(snapshot)
	configMutex.Unlock()

	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "WriteFailed", map[string]any{"Error": err.Error()})
		return
	}
	// Switching to Bing starts a fetch immediately rather than waiting for the
	// worker's next tick, so the preview appears without a manual sync.
	if req.Mode != nil && *req.Mode == "bing" {
		triggerBingSync()
	}
	writeJSON(w, http.StatusOK, currentWallpaperState())
}

// ServeThemeWallpaperGet handles GET /api/file/theme-wallpaper?name= — streams
// one gallery or Bing image by bare file name. Used for gallery thumbnails and
// for the Bing preview in the settings panel. The name is never trusted: it
// must be a plain whitelisted file name contained by its theme subdirectory.
func ServeThemeWallpaperGet(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet, http.MethodHead) {
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest")
		return
	}
	serveWallpaperByName(w, r, name)
}

// ── Bing endpoints ───────────────────────────────────────────────────────────

// ServeThemeBingSync handles POST /api/theme/bing/sync — triggers an immediate
// Bing fetch and returns 202. The fetch runs in the background worker; the
// client polls GET /api/theme/bing/status for the outcome.
//
// The request's locale is persisted as the Bing market so that a later
// scheduled fetch keeps following the UI language.
func ServeThemeBingSync(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	// Persist the request locale as the Bing market. It must be written to disk
	// rather than only held in memory: when the feature is disabled the worker
	// returns early and never persists anything, so an in-memory-only change
	// would be silently lost on restart.
	if mkt := wallpaper.BingMktForLocale(localeFromRequest(r)); mkt != "" {
		configMutex.Lock()
		if model.ConfigInstance.Appearance.Bing.Mkt != mkt {
			snapshot := model.ConfigInstance
			model.ConfigInstance.Appearance.Bing.Mkt = mkt
			patch := map[string]any{
				"appearance": map[string]any{"bing": map[string]any{"mkt": mkt}},
			}
			if err := writeConfigYAML(patch); err != nil {
				model.ConfigInstance = snapshot
				configMutex.Unlock()
				writeLocalizedErrorf(w, r, http.StatusInternalServerError, "WriteFailed", map[string]any{"Error": err.Error()})
				return
			}
		}
		configMutex.Unlock()
	}

	triggerBingSync()
	writeJSON(w, http.StatusAccepted, currentBingStatus())
}

// ServeThemeBingStatus handles GET /api/theme/bing/status — reports the Bing
// fetch state, including the photographer credit and the last error.
func ServeThemeBingStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, currentBingStatus())
}

// triggerBingSync is the hook that asks the Bing worker for an immediate fetch.
// main.go wires it to service.TriggerBingSync; it defaults to a no-op so tests
// and any handler usage without the worker stay safe.
var triggerBingSync = func() {}

// SetTriggerBingSyncFunc wires the immediate-sync hook. Called from main.go.
func SetTriggerBingSyncFunc(fn func()) {
	triggerBingSync = fn
}

// ── Shared helpers ───────────────────────────────────────────────────────────

// PersistStartupAppearance writes the appearance values that ApplyDefaults
// derived or normalized in memory and that must survive a restart.
//
// Two cases need this:
//
//   - A fresh install derives the factory defaults (Bing wallpaper enabled)
//     that exist only in memory at this point. The fresh-install marker is the
//     absence of the database file, which InitDB creates moments from now, so a
//     restart before the first config write would classify the install as
//     pre-existing and silently drop the factory wallpaper.
//   - An install whose mode is Bing while the fetch switch is off (configs
//     written before the two were coupled) is normalized back to enabled.
//
// Nothing is written for an ordinary existing install, so this does not create
// a config.yaml for users who have never changed a setting.
//
// Called once from main.go. A write failure is reported but not fatal: the
// in-memory values still apply for this process.
func PersistStartupAppearance() error {
	configMutex.Lock()
	defer configMutex.Unlock()

	app := model.ConfigInstance.Appearance
	freshInstall := model.FirstRun

	var bingPatch map[string]any
	switch {
	case freshInstall:
		bingPatch = map[string]any{
			"enabled": app.Bing.Enabled,
			"mkt":     app.Bing.Mkt,
		}
	case model.HealedBingFetch:
		// Heal the unrepresentable state on disk: mode says Bing, switch says off.
		bingPatch = map[string]any{"enabled": true}
	default:
		return nil // nothing to persist
	}

	snapshot := model.ConfigInstance

	// writeConfigYAML seeds the file from the whole ConfigInstance when no
	// config.yaml exists yet, which would write the auto-generated password in
	// plaintext into a 0644 file. The password already lives in the 0600
	// auto-password file and is re-read from there on startup, so blank it for
	// the duration of this write rather than widening its exposure.
	savedPassword := model.ConfigInstance.Password
	model.ConfigInstance.Password = ""
	defer func() { model.ConfigInstance.Password = savedPassword }()

	patch := map[string]any{
		"appearance": map[string]any{
			"wallpaper_mode":    app.WallpaperMode,
			"wallpaper_enabled": app.WallpaperEnabled,
			"bing":              bingPatch,
		},
	}
	if err := writeConfigYAML(patch); err != nil {
		model.ConfigInstance = snapshot
		return err
	}
	return nil
}

// persistAppearanceLocked writes the appearance section to config.yaml after
// the caller has already mutated model.ConfigInstance under configMutex. On a
// disk-write failure the supplied snapshot is restored so config never diverges
// from disk. Caller must hold configMutex.
func persistAppearanceLocked(snapshot model.Config) error {
	app := model.ConfigInstance.Appearance
	patch := map[string]any{
		"appearance": map[string]any{
			"wallpaper_mode":    app.WallpaperMode,
			"wallpaper_enabled": app.WallpaperEnabled,
			// Written so clearing the legacy field (when its image is deleted)
			// is persisted; writeConfigYAML is not subject to the PATCH
			// whitelist that restricts this key to empty values.
			"wallpaper_file": app.WallpaperFile,
			"local": map[string]any{
				"selected": app.Local.Selected,
				"items":    galleryItemsToMaps(app.Local.Items),
			},
		},
	}
	if err := writeConfigYAML(patch); err != nil {
		model.ConfigInstance = snapshot
		return err
	}
	return nil
}

// galleryItemsToMaps renders gallery items for the YAML patch writer. Items are
// sent as a whole list because mergePatchIntoRaw replaces lists wholesale.
func galleryItemsToMaps(items []model.LocalWallpaperItem) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		out = append(out, map[string]any{
			"file":        it.File,
			"name":        it.Name,
			"uploaded_at": it.UploadedAt,
			"size":        it.Size,
		})
	}
	return out
}

// currentWallpaperState snapshots the wallpaper state for a mutation response.
func currentWallpaperState() wallpaperStateResponse {
	configMutex.RLock()
	cfg := model.ConfigInstance
	configMutex.RUnlock()

	active, _ := wallpaper.ResolveActive(&cfg)
	return wallpaperStateResponse{
		Mode:       cfg.Appearance.WallpaperMode,
		Enabled:    cfg.Appearance.WallpaperEnabled,
		ActiveFile: active,
		Selected:   cfg.Appearance.Local.Selected,
	}
}

// currentBingStatus snapshots the Bing fetch state for a response.
func currentBingStatus() bingStatusResponse {
	configMutex.RLock()
	b := model.ConfigInstance.Appearance.Bing
	configMutex.RUnlock()

	return bingStatusResponse{
		Enabled:         b.Enabled,
		File:            b.File,
		LastSuccessDate: b.LastSuccessDate,
		Copyright:       b.Copyright,
		Title:           b.Title,
		Mkt:             b.Mkt,
		LastError:       b.LastError,
		LastAttemptAt:   b.LastAttemptAt,
	}
}

// localeFromRequest extracts the request locale using the same priority chain
// as the i18n localizer (X-Locale header, then the locale cookie).
func localeFromRequest(r *http.Request) string {
	if v := r.Header.Get("X-Locale"); v != "" {
		return v
	}
	if c, err := r.Cookie(model.ScopedCookieName("clawbench-locale")); err == nil && c.Value != "" {
		return c.Value
	}
	return ""
}
