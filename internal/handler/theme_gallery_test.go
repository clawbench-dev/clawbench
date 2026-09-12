package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/wallpaper"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Helpers ──────────────────────────────────────────────────────────────────

// makeMultiFileBody builds a multipart/form-data body with several "files"
// fields, so a single request can carry a batch of uploads.
func makeMultiFileBody(files map[string][]byte) (*bytes.Buffer, string) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for name, content := range files {
		fw, err := mw.CreateFormFile("files", name)
		if err != nil {
			panic(err)
		}
		_, _ = fw.Write(content)
	}
	_ = mw.Close()
	return &buf, mw.FormDataContentType()
}

// makeOrderedFileBody is makeMultiFileBody for cases where order matters (the
// auto-select behavior depends on which file is first).
func makeOrderedFileBody(names []string, contents [][]byte) (*bytes.Buffer, string) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for i, name := range names {
		fw, err := mw.CreateFormFile("files", name)
		if err != nil {
			panic(err)
		}
		_, _ = fw.Write(contents[i])
	}
	_ = mw.Close()
	return &buf, mw.FormDataContentType()
}

// seedGalleryItem writes a gallery image to disk and returns its config entry.
func seedGalleryItem(t *testing.T, name string) model.LocalWallpaperItem {
	t.Helper()
	require.NoError(t, wallpaper.WriteAtomic(wallpaper.LocalDir(), name, makePNG(8, 8)))
	return model.LocalWallpaperItem{File: name, Name: name, UploadedAt: 1, Size: 10}
}

// ── Upload ───────────────────────────────────────────────────────────────────

func TestThemeLocalUpload_MultipleFiles(t *testing.T) {
	themeDir, teardown := setupThemeTestEnv(t)
	defer teardown()

	body, ct := makeMultiFileBody(map[string][]byte{
		"a.png": makePNG(20, 20),
		"b.png": makePNG(30, 30),
		"c.png": makePNG(40, 40),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/theme/local/upload", body)
	req.Header.Set("Content-Type", ct)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeLocalUpload, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp galleryUploadResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Items, 3)
	assert.Empty(t, resp.Errors)

	// Every uploaded image is on disk under theme/local, and recorded in config.
	assert.Len(t, model.ConfigInstance.Appearance.Local.Items, 3)
	for _, it := range resp.Items {
		assert.True(t, strings.HasPrefix(it.File, "local-"), "stored name %q must carry the local- prefix", it.File)
		_, err := os.Stat(filepath.Join(themeDir, "local", it.File))
		assert.NoError(t, err, "uploaded file must exist on disk")
	}

	// The first upload becomes the selection and switches the mode to local.
	assert.Equal(t, resp.Items[0].File, model.ConfigInstance.Appearance.Local.Selected)
	assert.Equal(t, "local", model.ConfigInstance.Appearance.WallpaperMode)
	assert.True(t, model.ConfigInstance.Appearance.WallpaperEnabled)
}

func TestThemeLocalUpload_PreservesOriginalNameForDisplay(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	body, ct := makeMultiFileBody(map[string][]byte{"holiday-photo.png": makePNG(10, 10)})
	req := httptest.NewRequest(http.MethodPost, "/api/theme/local/upload", body)
	req.Header.Set("Content-Type", ct)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeLocalUpload, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp galleryUploadResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Items, 1)
	assert.Equal(t, "holiday-photo.png", resp.Items[0].Name, "the original name is kept for display")
	assert.NotEqual(t, "holiday-photo.png", resp.Items[0].File, "the stored name must not be client-controlled")
}

func TestThemeLocalUpload_RejectsOverPerRequestLimit(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	names := make([]string, wallpaper.MaxUploadsPerRequest+1)
	contents := make([][]byte, len(names))
	for i := range names {
		names[i] = fmt.Sprintf("f%d.png", i)
		contents[i] = makePNG(8, 8)
	}
	body, ct := makeOrderedFileBody(names, contents)

	req := httptest.NewRequest(http.MethodPost, "/api/theme/local/upload", body)
	req.Header.Set("Content-Type", ct)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeLocalUpload, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Empty(t, model.ConfigInstance.Appearance.Local.Items, "nothing may be stored when the batch is rejected")
}

func TestThemeLocalUpload_RejectsOverGalleryLimit(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	// Fill the gallery to the cap, then attempt one more.
	for i := range wallpaper.MaxGalleryItems {
		model.ConfigInstance.Appearance.Local.Items = append(
			model.ConfigInstance.Appearance.Local.Items, seedGalleryItem(t, fmt.Sprintf("local-%d-a.png", i)),
		)
	}

	body, ct := makeMultiFileBody(map[string][]byte{"extra.png": makePNG(8, 8)})
	req := httptest.NewRequest(http.MethodPost, "/api/theme/local/upload", body)
	req.Header.Set("Content-Type", ct)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeLocalUpload, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Len(t, model.ConfigInstance.Appearance.Local.Items, wallpaper.MaxGalleryItems)
}

func TestThemeLocalUpload_PartialFailure(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	// One valid PNG and one spoofed extension: the good file must be kept and
	// the bad one reported, rather than failing the whole batch.
	body, ct := makeOrderedFileBody(
		[]string{"good.png", "evil.png"},
		[][]byte{makePNG(10, 10), []byte("<html>not an image</html>")},
	)
	req := httptest.NewRequest(http.MethodPost, "/api/theme/local/upload", body)
	req.Header.Set("Content-Type", ct)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeLocalUpload, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp galleryUploadResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Items, 1)
	assert.Len(t, resp.Errors, 1)
	assert.Equal(t, "evil.png", resp.Errors[0].Name)
	assert.Len(t, model.ConfigInstance.Appearance.Local.Items, 1)
}

func TestThemeLocalUpload_AllFailedReturnsEmptyItems(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	body, ct := makeMultiFileBody(map[string][]byte{"bad.bmp": []byte("not an image")})
	req := httptest.NewRequest(http.MethodPost, "/api/theme/local/upload", body)
	req.Header.Set("Content-Type", ct)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeLocalUpload, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp galleryUploadResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.Items)
	assert.Len(t, resp.Errors, 1)
	assert.Empty(t, model.ConfigInstance.Appearance.Local.Items)
}

func TestThemeLocalUpload_NoFilesField(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	require.NoError(t, mw.WriteField("note", "hello"))
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/theme/local/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeLocalUpload, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestThemeLocalUpload_MethodNotAllowed(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodPut, "/api/theme/local/upload", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeLocalUpload, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// ── Delete ───────────────────────────────────────────────────────────────────

func TestThemeLocalDelete_ReselectsNeighbor(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	items := []model.LocalWallpaperItem{
		seedGalleryItem(t, "local-1-a.png"),
		seedGalleryItem(t, "local-2-b.png"),
		seedGalleryItem(t, "local-3-c.png"),
	}
	model.ConfigInstance.Appearance.Local.Items = items
	model.ConfigInstance.Appearance.Local.Selected = "local-2-b.png"
	model.ConfigInstance.Appearance.WallpaperMode = "local"
	model.ConfigInstance.Appearance.WallpaperEnabled = true

	req := httptest.NewRequest(http.MethodDelete, "/api/theme/local/item?name=local-2-b.png", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeLocalUpload, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Len(t, model.ConfigInstance.Appearance.Local.Items, 2)
	// The previous item is preferred so repeated deletes walk backwards.
	assert.Equal(t, "local-1-a.png", model.ConfigInstance.Appearance.Local.Selected)
	// The file is removed from disk.
	assert.NoFileExists(t, filepath.Join(wallpaper.LocalDir(), "local-2-b.png"))
}

func TestThemeLocalDelete_FirstItemSelectsNext(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	model.ConfigInstance.Appearance.Local.Items = []model.LocalWallpaperItem{
		seedGalleryItem(t, "local-1-a.png"),
		seedGalleryItem(t, "local-2-b.png"),
	}
	model.ConfigInstance.Appearance.Local.Selected = "local-1-a.png"
	model.ConfigInstance.Appearance.WallpaperMode = "local"
	model.ConfigInstance.Appearance.WallpaperEnabled = true

	req := httptest.NewRequest(http.MethodDelete, "/api/theme/local/item?name=local-1-a.png", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeLocalUpload, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "local-2-b.png", model.ConfigInstance.Appearance.Local.Selected)
}

func TestThemeLocalDelete_NonSelectedKeepsSelection(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	model.ConfigInstance.Appearance.Local.Items = []model.LocalWallpaperItem{
		seedGalleryItem(t, "local-1-a.png"),
		seedGalleryItem(t, "local-2-b.png"),
	}
	model.ConfigInstance.Appearance.Local.Selected = "local-1-a.png"

	req := httptest.NewRequest(http.MethodDelete, "/api/theme/local/item?name=local-2-b.png", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeLocalUpload, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "local-1-a.png", model.ConfigInstance.Appearance.Local.Selected)
}

func TestThemeLocalDelete_LastItemClearsSelected(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	model.ConfigInstance.Appearance.Local.Items = []model.LocalWallpaperItem{
		seedGalleryItem(t, "local-1-a.png"),
	}
	model.ConfigInstance.Appearance.Local.Selected = "local-1-a.png"
	model.ConfigInstance.Appearance.WallpaperMode = "local"
	model.ConfigInstance.Appearance.WallpaperEnabled = true

	req := httptest.NewRequest(http.MethodDelete, "/api/theme/local/item?name=local-1-a.png", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeLocalUpload, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, model.ConfigInstance.Appearance.Local.Items)
	assert.Empty(t, model.ConfigInstance.Appearance.Local.Selected)
	// With nothing selected, no wallpaper resolves.
	_, ok := wallpaper.ResolveActive(&model.ConfigInstance)
	assert.False(t, ok)
}

func TestThemeLocalDelete_UnknownNameIs404(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodDelete, "/api/theme/local/item?name=local-999-x.png", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeLocalUpload, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestThemeLocalDelete_RejectsTraversalName(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodDelete, "/api/theme/local/item?name=../../etc/passwd", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeLocalUpload, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestThemeLocalDelete_MissingNameIs400(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodDelete, "/api/theme/local/item", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeLocalUpload, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ── Select ───────────────────────────────────────────────────────────────────

func TestThemeLocalSelect_SetsModeAndEnabled(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	model.ConfigInstance.Appearance.Local.Items = []model.LocalWallpaperItem{
		seedGalleryItem(t, "local-1-a.png"),
		seedGalleryItem(t, "local-2-b.png"),
	}
	// Start on Bing with the wallpaper globally disabled.
	model.ConfigInstance.Appearance.WallpaperMode = "bing"
	model.ConfigInstance.Appearance.WallpaperEnabled = false

	body := strings.NewReader(`{"name":"local-2-b.png"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/theme/local/select", body)
	req.Header.Set("Content-Type", "application/json")
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeLocalSelect, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "local-2-b.png", model.ConfigInstance.Appearance.Local.Selected)
	assert.Equal(t, "local", model.ConfigInstance.Appearance.WallpaperMode)
	assert.True(t, model.ConfigInstance.Appearance.WallpaperEnabled, "selecting an image re-enables the wallpaper")

	var resp wallpaperStateResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "local-2-b.png", resp.ActiveFile)
}

func TestThemeLocalSelect_NotInGalleryIs404(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	body := strings.NewReader(`{"name":"local-999-x.png"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/theme/local/select", body)
	req.Header.Set("Content-Type", "application/json")
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeLocalSelect, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestThemeLocalSelect_RejectsTraversalName(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	body := strings.NewReader(`{"name":"../../etc/passwd"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/theme/local/select", body)
	req.Header.Set("Content-Type", "application/json")
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeLocalSelect, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestThemeLocalSelect_MethodNotAllowed(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodGet, "/api/theme/local/select", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeLocalSelect, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// ── Mode / enable switch ─────────────────────────────────────────────────────

func TestThemeWallpaper_DisableHidesActiveFile(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	model.ConfigInstance.Appearance.Local.Items = []model.LocalWallpaperItem{
		seedGalleryItem(t, "local-1-a.png"),
	}
	model.ConfigInstance.Appearance.Local.Selected = "local-1-a.png"
	model.ConfigInstance.Appearance.WallpaperMode = "local"
	model.ConfigInstance.Appearance.WallpaperEnabled = true

	body := strings.NewReader(`{"enabled":false}`)
	req := httptest.NewRequest(http.MethodPost, "/api/theme/wallpaper", body)
	req.Header.Set("Content-Type", "application/json")
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeWallpaperMode, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp wallpaperStateResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp.Enabled)
	assert.Empty(t, resp.ActiveFile, "disabling must hide the wallpaper")
	// The gallery and its selection are retained so it can be re-enabled.
	assert.Equal(t, "local-1-a.png", resp.Selected)
	assert.Len(t, model.ConfigInstance.Appearance.Local.Items, 1)
}

func TestThemeWallpaper_ReEnableRestoresActiveFile(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	model.ConfigInstance.Appearance.Local.Items = []model.LocalWallpaperItem{
		seedGalleryItem(t, "local-1-a.png"),
	}
	model.ConfigInstance.Appearance.Local.Selected = "local-1-a.png"
	model.ConfigInstance.Appearance.WallpaperMode = "local"
	model.ConfigInstance.Appearance.WallpaperEnabled = false

	body := strings.NewReader(`{"enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/theme/wallpaper", body)
	req.Header.Set("Content-Type", "application/json")
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeWallpaperMode, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp wallpaperStateResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Enabled)
	assert.Equal(t, "local-1-a.png", resp.ActiveFile)
}

func TestThemeWallpaper_SwitchMode(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	model.ConfigInstance.Appearance.Bing.File = "bing-20260910.jpg"
	model.ConfigInstance.Appearance.WallpaperMode = "local"
	model.ConfigInstance.Appearance.WallpaperEnabled = true

	body := strings.NewReader(`{"mode":"bing"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/theme/wallpaper", body)
	req.Header.Set("Content-Type", "application/json")
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeWallpaperMode, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "bing", model.ConfigInstance.Appearance.WallpaperMode)
	var resp wallpaperStateResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "bing-20260910.jpg", resp.ActiveFile)
}

func TestThemeWallpaper_RejectsInvalidMode(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	body := strings.NewReader(`{"mode":"nonsense"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/theme/wallpaper", body)
	req.Header.Set("Content-Type", "application/json")
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeWallpaperMode, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestThemeWallpaper_MethodNotAllowed(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodGet, "/api/theme/wallpaper", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeWallpaperMode, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// ── Bing endpoints ───────────────────────────────────────────────────────────

func TestThemeBingStatus_ReportsState(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	model.ConfigInstance.Appearance.Bing.Enabled = true
	model.ConfigInstance.Appearance.Bing.File = "bing-20260910.jpg"
	model.ConfigInstance.Appearance.Bing.Copyright = "© Someone"
	model.ConfigInstance.Appearance.Bing.Title = "A Title"
	model.ConfigInstance.Appearance.Bing.LastError = "boom"
	model.ConfigInstance.Appearance.Bing.Mkt = "en-US"

	req := httptest.NewRequest(http.MethodGet, "/api/theme/bing/status", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBingStatus, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp bingStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Enabled)
	assert.Equal(t, "bing-20260910.jpg", resp.File)
	assert.Equal(t, "© Someone", resp.Copyright)
	assert.Equal(t, "A Title", resp.Title)
	assert.Equal(t, "boom", resp.LastError)
	assert.Equal(t, "en-US", resp.Mkt)
}

func TestThemeBingStatus_MethodNotAllowed(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodPost, "/api/theme/bing/status", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBingStatus, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestThemeBingSync_TriggersAndPersistsMktFromLocale(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	triggered := 0
	origTrigger := triggerBingSync
	triggerBingSync = func() { triggered++ }
	defer func() { triggerBingSync = origTrigger }()

	req := httptest.NewRequest(http.MethodPost, "/api/theme/bing/sync", http.NoBody)
	req.Header.Set("X-Locale", "en")
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBingSync, req)

	assert.Equal(t, http.StatusAccepted, w.Code)
	assert.Equal(t, 1, triggered, "sync must ask the worker for an immediate fetch")
	assert.Equal(t, "en-US", model.ConfigInstance.Appearance.Bing.Mkt, "the request locale must persist as the market")
}

func TestThemeBingSync_UsesLocaleCookieFallback(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	origTrigger := triggerBingSync
	triggerBingSync = func() {}
	defer func() { triggerBingSync = origTrigger }()

	req := httptest.NewRequest(http.MethodPost, "/api/theme/bing/sync", http.NoBody)
	req.AddCookie(&http.Cookie{Name: model.ScopedCookieName("clawbench-locale"), Value: "zh"})
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBingSync, req)

	assert.Equal(t, http.StatusAccepted, w.Code)
	assert.Equal(t, "zh-CN", model.ConfigInstance.Appearance.Bing.Mkt)
}

func TestThemeBingSync_MethodNotAllowed(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodGet, "/api/theme/bing/sync", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBingSync, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestSetTriggerBingSyncFunc(t *testing.T) {
	orig := triggerBingSync
	defer func() { triggerBingSync = orig }()

	called := false
	SetTriggerBingSyncFunc(func() { called = true })
	triggerBingSync()
	assert.True(t, called)
}

// ── Serving ──────────────────────────────────────────────────────────────────

func TestServeThemeWallpaper_ByName(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	png := makePNG(12, 12)
	require.NoError(t, wallpaper.WriteAtomic(wallpaper.LocalDir(), "local-1-a.png", png))

	req := httptest.NewRequest(http.MethodGet, "/api/file/theme-wallpaper?name=local-1-a.png", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeWallpaperGet, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, png, w.Body.Bytes())
}

func TestServeThemeWallpaper_TraversalRejected(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	for _, name := range []string{"../../etc/passwd", "sub/a.png", "..%2F..%2Fetc%2Fpasswd"} {
		req := httptest.NewRequest(http.MethodGet, "/api/file/theme-wallpaper?name="+name, http.NoBody)
		req = withAuthCookie(req, model.SessionToken)
		w := callHandler(ServeThemeWallpaperGet, req)
		assert.Equal(t, http.StatusNotFound, w.Code, "name %q must not resolve", name)
	}
}

func TestServeThemeWallpaper_MissingNameIs400(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodGet, "/api/file/theme-wallpaper", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeWallpaperGet, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeThemeWallpaper_MethodNotAllowed(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodPost, "/api/file/theme-wallpaper", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeWallpaperGet, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
