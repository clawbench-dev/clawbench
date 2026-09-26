package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
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
	"gopkg.in/yaml.v3"
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

// ── Bing state persistence ───────────────────────────────────────────────────

func TestPersistBingWallpaperState_WritesConfigAndMemory(t *testing.T) {
	themeDir, teardown := setupThemeTestEnv(t)
	defer teardown()

	err := PersistBingWallpaperState(wallpaper.BingState{
		File:            "bing-20260910.jpg",
		LastSuccessDate: "20260910",
		Copyright:       "© Someone",
		Title:           "A Title",
		Mkt:             "zh-CN",
		LastAttemptAt:   123,
	})
	require.NoError(t, err)

	b := model.ConfigInstance.Appearance.Bing
	assert.Equal(t, "bing-20260910.jpg", b.File)
	assert.Equal(t, "20260910", b.LastSuccessDate)
	assert.Equal(t, "© Someone", b.Copyright)
	assert.Equal(t, "A Title", b.Title)
	assert.Equal(t, "zh-CN", b.Mkt)
	assert.Empty(t, b.LastError)
	assert.Equal(t, int64(123), b.LastAttemptAt)

	// The state is persisted under the temp DataDir, not the repo fixture.
	cfgPath := filepath.Join(model.DataDir, "config", "config.yaml")
	data, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "bing-20260910.jpg")
	_ = themeDir
}

func TestPersistBingWallpaperState_FailurePreservesCachedAttribution(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	// A previously cached image with its photographer credit.
	model.ConfigInstance.Appearance.Bing.File = "bing-20260901.jpg"
	model.ConfigInstance.Appearance.Bing.LastSuccessDate = "20260901"
	model.ConfigInstance.Appearance.Bing.Copyright = "© Original Photographer"
	model.ConfigInstance.Appearance.Bing.Title = "Original Title"

	// A failed fetch reports no file and no attribution — only the error.
	require.NoError(t, PersistBingWallpaperState(wallpaper.BingState{
		LastError:     "network down",
		LastAttemptAt: 99,
	}))

	b := model.ConfigInstance.Appearance.Bing
	assert.Equal(t, "bing-20260901.jpg", b.File, "the cached image must be preserved")
	assert.Equal(t, "20260901", b.LastSuccessDate)
	assert.Equal(t, "© Original Photographer", b.Copyright, "attribution must not be wiped by a failed fetch")
	assert.Equal(t, "Original Title", b.Title)
	assert.Equal(t, "network down", b.LastError)
	assert.Equal(t, int64(99), b.LastAttemptAt)
}

func TestPersistBingWallpaperState_FailureKeepsCachedFile(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	// An existing cached image and a config dir that cannot be created.
	model.ConfigInstance.Appearance.Bing.File = "bing-20260901.jpg"

	model.DataDir = filepath.Join(t.TempDir(), "nested")
	require.NoError(t, os.MkdirAll(model.DataDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(model.DataDir, "config"), []byte("x"), 0o644))

	err := PersistBingWallpaperState(wallpaper.BingState{
		LastError:     "boom",
		LastAttemptAt: 5,
	})
	require.Error(t, err)
	// The in-memory config is restored, so the cached image is still referenced.
	assert.Equal(t, "bing-20260901.jpg", model.ConfigInstance.Appearance.Bing.File,
		"a failed persist must not drop the cached wallpaper")
	assert.Empty(t, model.ConfigInstance.Appearance.Bing.LastError,
		"a failed persist must not leave a half-applied in-memory state")
}

// ── Config DTO ───────────────────────────────────────────────────────────────

// ── Config DTO ───────────────────────────────────────────────────────────────

func TestBuildConfigAppearance_ExposesResolvedActiveFile(t *testing.T) {
	cfg := model.Config{}
	cfg.Appearance.WallpaperMode = "local"
	cfg.Appearance.WallpaperEnabled = true
	cfg.Appearance.Local.Selected = "local-1-a.png"
	cfg.Appearance.Local.Items = []model.LocalWallpaperItem{
		{File: "local-1-a.png", Name: "a.png", UploadedAt: 1, Size: 2},
	}

	out := buildConfigAppearance(cfg)
	assert.Equal(t, "local-1-a.png", out.ActiveFile)
	assert.Equal(t, "local", out.WallpaperMode)
	assert.True(t, out.WallpaperEnabled)
	require.Len(t, out.Local.Items, 1)
	assert.Equal(t, "a.png", out.Local.Items[0].Name)
}

func TestBuildConfigAppearance_DisabledHasEmptyActiveFile(t *testing.T) {
	cfg := model.Config{}
	cfg.Appearance.WallpaperMode = "bing"
	cfg.Appearance.WallpaperEnabled = false
	cfg.Appearance.Bing.File = "bing-20260910.jpg"

	out := buildConfigAppearance(cfg)
	assert.Empty(t, out.ActiveFile)
	assert.Equal(t, "bing-20260910.jpg", out.Bing.File, "the cached file is still reported")
}

func TestBuildConfigAppearance_ExposesBingStatus(t *testing.T) {
	cfg := model.Config{}
	cfg.Appearance.Bing.Enabled = true
	cfg.Appearance.Bing.Copyright = "© Someone"
	cfg.Appearance.Bing.LastError = "boom"

	out := buildConfigAppearance(cfg)
	assert.True(t, out.Bing.Enabled)
	assert.Equal(t, "© Someone", out.Bing.Copyright)
	assert.Equal(t, "boom", out.Bing.LastError)
}

func TestBuildConfigAppearance_EmptyItemsIsEmptySlice(t *testing.T) {
	// The client iterates items directly; a nil slice would serialize as null.
	out := buildConfigAppearance(model.Config{})
	assert.NotNil(t, out.Local.Items)
	assert.Empty(t, out.Local.Items)
}

// ── Absolute paths for thumbnail generation ──────────────────────────────────

// TestBuildConfigAppearance_ExposesAbsPathsForThumbnails covers the field the
// settings panel hands to GET /api/fs/thumb, which takes a path rather than a
// bare name and returns a small JPEG instead of the full-size image.

// ── Absolute paths for thumbnail generation ──────────────────────────────────

// TestBuildConfigAppearance_ExposesAbsPathsForThumbnails covers the field the
// settings panel hands to GET /api/fs/thumb, which takes a path rather than a
// bare name and returns a small JPEG instead of the full-size image.
func TestBuildConfigAppearance_ExposesAbsPathsForThumbnails(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	// The files must exist: FilePath resolves symlinks on the containing
	// directory, so a name whose directory is absent has no path to report.
	require.NoError(t, wallpaper.WriteAtomic(wallpaper.LocalDir(), "local-1-a.png", makePNG(8, 8)))
	require.NoError(t, wallpaper.WriteAtomic(wallpaper.BingDir(), "bing-20260910.jpg", makeJPEG(8, 8)))

	cfg := model.Config{}
	cfg.Appearance.WallpaperMode = "local"
	cfg.Appearance.WallpaperEnabled = true
	cfg.Appearance.Local.Items = []model.LocalWallpaperItem{
		{File: "local-1-a.png", Name: "a.png"},
	}
	cfg.Appearance.Bing.File = "bing-20260910.jpg"

	out := buildConfigAppearance(cfg)

	require.Len(t, out.Local.Items, 1)
	wantItem, ok := wallpaper.FilePath("local-1-a.png")
	require.True(t, ok, "test fixture name should resolve")
	assert.Equal(t, wantItem, out.Local.Items[0].AbsPath,
		"gallery item must carry the resolved absolute path")

	wantBing, ok := wallpaper.FilePath("bing-20260910.jpg")
	require.True(t, ok)
	assert.Equal(t, wantBing, out.Bing.AbsPath,
		"the Bing entry must carry the resolved absolute path")
}

// TestBuildConfigAppearance_AbsPathEmptyWhenUnresolvable guards the fallback:
// a name that fails containment/extension checks must yield no path at all
// rather than a partially-built one the client could still request.

// TestBuildConfigAppearance_AbsPathEmptyWhenUnresolvable guards the fallback:
// a name that fails containment/extension checks must yield no path at all
// rather than a partially-built one the client could still request.
func TestBuildConfigAppearance_AbsPathEmptyWhenUnresolvable(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	cfg := model.Config{}
	cfg.Appearance.Local.Items = []model.LocalWallpaperItem{
		{File: "../../etc/passwd", Name: "evil"},
		{File: "notes.txt", Name: "not an image"},
	}
	cfg.Appearance.Bing.File = "../escape.jpg"

	out := buildConfigAppearance(cfg)

	require.Len(t, out.Local.Items, 2)
	for _, it := range out.Local.Items {
		assert.Empty(t, it.AbsPath, "unresolvable name %q must not expose a path", it.File)
	}
	assert.Empty(t, out.Bing.AbsPath)
}

func TestAbsWallpaperPath_EmptyName(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	assert.Empty(t, absWallpaperPath(""))
}

// ── Startup appearance persistence (B1) + Bing-mode self-heal ────────────────

// ── Startup appearance persistence (B1) + Bing-mode self-heal ────────────────

func TestPersistStartupAppearance_FreshInstallWritesDefaultsToDisk(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	model.FirstRun = true
	t.Cleanup(func() { model.FirstRun = false })

	// The defaults ApplyDefaults derived for a fresh install: the Bing source is
	// pre-selected with its fetch switch on, but the wallpaper itself is off.
	model.ConfigInstance.Appearance.WallpaperMode = "bing"
	model.ConfigInstance.Appearance.WallpaperEnabled = false
	model.ConfigInstance.Appearance.Bing.Enabled = true
	model.ConfigInstance.Appearance.Bing.Mkt = "zh-CN"

	require.NoError(t, PersistStartupAppearance())

	// The file must record them, otherwise a restart before any other write
	// would classify the install as pre-existing and drop the factory wallpaper.
	cfgPath := filepath.Join(model.DataDir, "config", "config.yaml")
	data, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	s := string(data)
	assert.Contains(t, s, "wallpaper_mode: bing")
	assert.Contains(t, s, "wallpaper_enabled: false")
	assert.Contains(t, s, "enabled: true")
	assert.Contains(t, s, "mkt: zh-CN")
}

func TestPersistStartupAppearance_DoesNotWritePlaintextPassword(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	model.FirstRun = true
	t.Cleanup(func() { model.FirstRun = false })

	// A fresh install has an auto-generated plaintext password in memory; it
	// already lives in the 0600 auto-password file and must not be copied into
	// the 0644 config.yaml.
	model.ConfigInstance.Password = "super-secret-auto-password"
	model.ConfigInstance.Appearance.WallpaperMode = "bing"
	model.ConfigInstance.Appearance.Bing.Enabled = true

	require.NoError(t, PersistStartupAppearance())

	cfgPath := filepath.Join(model.DataDir, "config", "config.yaml")
	data, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "super-secret-auto-password",
		"the auto-generated password must not be persisted in plaintext")
	// The in-memory password is restored for the rest of startup.
	assert.Equal(t, "super-secret-auto-password", model.ConfigInstance.Password)
}

func TestPersistStartupAppearance_FreshInstallSurvivesReload(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	model.FirstRun = true
	t.Cleanup(func() { model.FirstRun = false })

	model.ConfigInstance.Appearance.WallpaperMode = "bing"
	model.ConfigInstance.Appearance.WallpaperEnabled = false
	model.ConfigInstance.Appearance.Bing.Enabled = true
	model.ConfigInstance.Appearance.Bing.Mkt = "zh-CN"
	require.NoError(t, PersistStartupAppearance())

	// Simulate the restart: reload config.yaml into a zero-value Config with a
	// non-nil presence map (the file now exists) and apply defaults again.
	cfgPath := filepath.Join(model.DataDir, "config", "config.yaml")
	data, err := os.ReadFile(cfgPath)
	require.NoError(t, err)

	reloaded := model.Config{}
	require.NoError(t, yaml.Unmarshal(data, &reloaded))
	model.ApplyDefaults(&reloaded, map[string]bool{"appearance": true})

	assert.Equal(t, "bing", reloaded.Appearance.WallpaperMode,
		"the factory wallpaper source must survive a restart")
	assert.False(t, reloaded.Appearance.WallpaperEnabled,
		"the wallpaper must stay off across a restart")
	assert.True(t, reloaded.Appearance.Bing.Enabled)
}

// TestPersistStartupAppearance_HealsBingModeWithFetchDisabled covers the
// unrepresentable state configs could reach before the mode endpoint kept the
// two in step: mode says Bing while the fetch switch is off, so the worker
// silently refused to fetch and the sync button did nothing. The settings UI
// cannot reach this state, so startup must repair it.

// TestPersistStartupAppearance_HealsBingModeWithFetchDisabled covers the
// unrepresentable state configs could reach before the mode endpoint kept the
// two in step: mode says Bing while the fetch switch is off, so the worker
// silently refused to fetch and the sync button did nothing. The settings UI
// cannot reach this state, so startup must repair it.
func TestPersistStartupAppearance_HealsBingModeWithFetchDisabled(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	model.FirstRun = false
	model.HealedBingFetch = true
	t.Cleanup(func() { model.HealedBingFetch = false })
	model.ConfigInstance.Appearance.WallpaperMode = "bing"
	model.ConfigInstance.Appearance.WallpaperEnabled = true
	model.ConfigInstance.Appearance.Bing.Enabled = true // already healed in memory

	require.NoError(t, PersistStartupAppearance())

	cfgPath := filepath.Join(model.DataDir, "config", "config.yaml")
	data, err := os.ReadFile(cfgPath)
	require.NoError(t, err)

	reloaded := model.Config{}
	require.NoError(t, yaml.Unmarshal(data, &reloaded))
	assert.True(t, reloaded.Appearance.Bing.Enabled,
		"the healed switch must be on disk, not just in memory")
}

func TestPersistStartupAppearance_OrdinaryInstallWritesNothing(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	model.FirstRun = false
	// A normal existing install that never touched a wallpaper setting.
	model.ConfigInstance.Appearance.WallpaperMode = ""
	model.ConfigInstance.Appearance.Bing.Enabled = false

	require.NoError(t, PersistStartupAppearance())

	assert.NoFileExists(t, filepath.Join(model.DataDir, "config", "config.yaml"),
		"an ordinary install must not get a config.yaml created for it")
}

func TestPersistStartupAppearance_LocalModeDoesNotEnableBing(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	model.FirstRun = false
	model.ConfigInstance.Appearance.WallpaperMode = "local"
	model.ConfigInstance.Appearance.Local.Selected = "local-1-a.png"
	model.ConfigInstance.Appearance.Bing.Enabled = false

	require.NoError(t, PersistStartupAppearance())

	assert.False(t, model.ConfigInstance.Appearance.Bing.Enabled,
		"a local-mode install must not have Bing fetching enabled")
}

// ── Mode switch keeps the Bing fetch switch in step ──────────────────────────

// ── Mode switch keeps the Bing fetch switch in step ──────────────────────────

func TestThemeWallpaperMode_SelectingBingEnablesFetch(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	origTrigger := triggerBingSync
	triggered := 0
	triggerBingSync = func() { triggered++ }
	defer func() { triggerBingSync = origTrigger }()

	// Start from local with the Bing switch off — the state a user is in before
	// choosing Bing, and the one that used to leave the sync button inert.
	model.ConfigInstance.Appearance.WallpaperMode = "local"
	model.ConfigInstance.Appearance.Bing.Enabled = false

	req := httptest.NewRequest(http.MethodPost, "/api/theme/wallpaper", strings.NewReader(`{"mode":"bing"}`))
	req.Header.Set("Content-Type", "application/json")
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeWallpaperMode, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.True(t, model.ConfigInstance.Appearance.Bing.Enabled,
		"selecting the Bing source must enable its fetch switch")
	assert.Equal(t, 1, triggered, "selecting Bing should kick off a fetch immediately")

	// And it must be on disk so a restart keeps fetching.
	cfgPath := filepath.Join(model.DataDir, "config", "config.yaml")
	data, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	reloaded := model.Config{}
	require.NoError(t, yaml.Unmarshal(data, &reloaded))
	assert.True(t, reloaded.Appearance.Bing.Enabled)
}

func TestThemeWallpaperMode_SelectingLocalDisablesBingFetch(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	origTrigger := triggerBingSync
	triggered := 0
	triggerBingSync = func() { triggered++ }
	defer func() { triggerBingSync = origTrigger }()

	model.ConfigInstance.Appearance.WallpaperMode = "bing"
	model.ConfigInstance.Appearance.Bing.Enabled = true

	req := httptest.NewRequest(http.MethodPost, "/api/theme/wallpaper", strings.NewReader(`{"mode":"local"}`))
	req.Header.Set("Content-Type", "application/json")
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeWallpaperMode, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.False(t, model.ConfigInstance.Appearance.Bing.Enabled,
		"switching away from Bing should stop its fetching")
	assert.Zero(t, triggered, "switching to local must not trigger a Bing fetch")
}

func TestThemeWallpaperMode_EnabledToggleLeavesModeCouplingAlone(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	origTrigger := triggerBingSync
	triggerBingSync = func() {}
	defer func() { triggerBingSync = origTrigger }()

	model.ConfigInstance.Appearance.WallpaperMode = "bing"
	model.ConfigInstance.Appearance.Bing.Enabled = true

	// Toggling the global switch must not be mistaken for a mode change.
	req := httptest.NewRequest(http.MethodPost, "/api/theme/wallpaper", strings.NewReader(`{"enabled":false}`))
	req.Header.Set("Content-Type", "application/json")
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeWallpaperMode, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.False(t, model.ConfigInstance.Appearance.WallpaperEnabled)
	assert.True(t, model.ConfigInstance.Appearance.Bing.Enabled,
		"the mode/Bing coupling must only change on an actual mode switch")
}

// ── Delete rollback integrity (I2) ───────────────────────────────────────────

// ── Delete rollback integrity (I2) ───────────────────────────────────────────

func TestThemeLocalDelete_PersistFailureRestoresGalleryExactly(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	a := seedGalleryItem(t, "local-1-a.png")
	b := seedGalleryItem(t, "local-2-b.png")
	c := seedGalleryItem(t, "local-3-c.png")
	model.ConfigInstance.Appearance.Local.Items = []model.LocalWallpaperItem{a, b, c}
	model.ConfigInstance.Appearance.Local.Selected = "local-2-b.png"
	model.ConfigInstance.Appearance.WallpaperMode = "local"
	model.ConfigInstance.Appearance.WallpaperEnabled = true

	// Force the config write to fail: make the config dir uncreatable.
	model.DataDir = filepath.Join(t.TempDir(), "nested")
	require.NoError(t, os.MkdirAll(model.DataDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(model.DataDir, "config"), []byte("x"), 0o644))

	req := httptest.NewRequest(http.MethodDelete, "/api/theme/local/item?name=local-2-b.png", http.NoBody)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeLocalUpload, req)
	require.Equal(t, http.StatusInternalServerError, w.Code)

	// The gallery must be restored element-for-element — a shallow snapshot
	// sharing the backing array would leave a duplicated entry behind.
	got := model.ConfigInstance.Appearance.Local.Items
	require.Len(t, got, 3, "gallery must be fully restored")
	assert.Equal(t, "local-1-a.png", got[0].File)
	assert.Equal(t, "local-2-b.png", got[1].File)
	assert.Equal(t, "local-3-c.png", got[2].File)
	assert.Equal(t, "local-2-b.png", model.ConfigInstance.Appearance.Local.Selected)
}

// ── Upload capacity under the lock (S5) ──────────────────────────────────────

func TestAppendGalleryItems_EnforcesCapacityUnderLock(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	// Fill to one below the cap.
	for i := range wallpaper.MaxGalleryItems - 1 {
		model.ConfigInstance.Appearance.Local.Items = append(
			model.ConfigInstance.Appearance.Local.Items, seedGalleryItem(t, fmt.Sprintf("local-%d-a.png", i)),
		)
	}

	// Two items requested with only one slot free: one is accepted, not two.
	added, err := appendGalleryItems([]galleryItemView{
		{File: "local-new1-a.png", Name: "n1.png"},
		{File: "local-new2-a.png", Name: "n2.png"},
	})
	require.NoError(t, err)
	assert.Len(t, added, 1, "only the available slot may be filled")
	assert.Len(t, model.ConfigInstance.Appearance.Local.Items, wallpaper.MaxGalleryItems)
}

func TestAppendGalleryItems_FullGalleryReturnsError(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	for i := range wallpaper.MaxGalleryItems {
		model.ConfigInstance.Appearance.Local.Items = append(
			model.ConfigInstance.Appearance.Local.Items, seedGalleryItem(t, fmt.Sprintf("local-%d-a.png", i)),
		)
	}

	added, err := appendGalleryItems([]galleryItemView{{File: "local-new-a.png", Name: "n.png"}})
	require.ErrorIs(t, err, errGalleryFull)
	assert.Empty(t, added)
	assert.Len(t, model.ConfigInstance.Appearance.Local.Items, wallpaper.MaxGalleryItems)
}

// ── Orphan reconciliation (S4) ───────────────────────────────────────────────

// ── Orphan reconciliation (S4) ───────────────────────────────────────────────

func TestReconcileLocalGallery_RemovesUnreferencedFiles(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	require.NoError(t, wallpaper.WriteAtomic(wallpaper.LocalDir(), "local-keep-a.png", makePNG(8, 8)))
	require.NoError(t, wallpaper.WriteAtomic(wallpaper.LocalDir(), "local-orphan-b.png", makePNG(8, 8)))

	removed := wallpaper.ReconcileLocalGallery([]string{"local-keep-a.png"})

	assert.Equal(t, []string{"local-orphan-b.png"}, removed)
	assert.FileExists(t, filepath.Join(wallpaper.LocalDir(), "local-keep-a.png"))
	assert.NoFileExists(t, filepath.Join(wallpaper.LocalDir(), "local-orphan-b.png"))
}

func TestReconcileLocalGallery_LeavesNonGalleryFilesAlone(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	// A legacy unprefixed wallpaper in the theme root, and a Bing cache file,
	// must both survive: only the local gallery is swept.
	require.NoError(t, wallpaper.WriteAtomic(wallpaper.ThemeDir(), "background.png", makePNG(8, 8)))
	require.NoError(t, wallpaper.WriteAtomic(wallpaper.BingDir(), "bing-20260910.jpg", []byte("x")))

	removed := wallpaper.ReconcileLocalGallery(nil)

	assert.Empty(t, removed)
	assert.FileExists(t, filepath.Join(wallpaper.ThemeDir(), "background.png"))
	assert.FileExists(t, filepath.Join(wallpaper.BingDir(), "bing-20260910.jpg"))
}

// ── Bing mkt persistence (S6) ────────────────────────────────────────────────

// ── Bing mkt persistence (S6) ────────────────────────────────────────────────

func TestThemeBingSync_PersistsMktEvenWhenDisabled(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	origTrigger := triggerBingSync
	triggerBingSync = func() {}
	defer func() { triggerBingSync = origTrigger }()

	// Disabled: the worker returns early and would never persist the market.
	model.ConfigInstance.Appearance.Bing.Enabled = false

	req := httptest.NewRequest(http.MethodPost, "/api/theme/bing/sync", http.NoBody)
	req.Header.Set("X-Locale", "en")
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeBingSync, req)
	require.Equal(t, http.StatusAccepted, w.Code)

	cfgPath := filepath.Join(model.DataDir, "config", "config.yaml")
	data, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "mkt: en-US",
		"the market must reach disk, not just memory")
}

// ── Sanity: uploaded bytes are a decodable image ─────────────────────────────

// ── Sanity: uploaded bytes are a decodable image ─────────────────────────────

func TestThemeLocalUpload_StoredImageIsDecodable(t *testing.T) {
	_, teardown := setupThemeTestEnv(t)
	defer teardown()

	body, ct := makeMultiFileBody(map[string][]byte{"p.png": makePNG(24, 16)})
	req := httptest.NewRequest(http.MethodPost, "/api/theme/local/upload", body)
	req.Header.Set("Content-Type", ct)
	req = withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeThemeLocalUpload, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp galleryUploadResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Items, 1)

	abs, ok := wallpaper.FilePath(resp.Items[0].File)
	require.True(t, ok)
	data, err := os.ReadFile(abs)
	require.NoError(t, err)
	img, _, err := image.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, 24, img.Bounds().Dx())
	assert.Equal(t, 16, img.Bounds().Dy())
	_ = png.Encode
}
