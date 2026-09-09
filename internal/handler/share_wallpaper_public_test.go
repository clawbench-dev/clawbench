package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupShareWallpaperEnv is setupThemeTestEnvFull (theme dir + project under a
// shared DB env) plus a share token for a file inside the project — everything
// the token-scoped wallpaper public endpoints need.
func setupShareWallpaperEnv(t *testing.T) (themeDir, projectDir, token string, teardown func()) {
	t.Helper()
	themeDir, projectDir, teardown = setupThemeTestEnvFull(t)
	shareFile := filepath.Join(projectDir, "shared.md")
	require.NoError(t, os.WriteFile(shareFile, []byte("# doc"), 0o644))
	token = createShareViaAPI(t, shareFile)
	return themeDir, projectDir, token, teardown
}

// getShareWallpaperResponse hits a public token-scoped endpoint without auth.
func getShareWallpaperResponse(t *testing.T, token, rest string) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequest(t, http.MethodGet, "/api/share/"+token+"/"+rest, nil)
	return callHandler(ServeSharePublic, req)
}

func TestSharePublic_Appearance_WithWallpaper(t *testing.T) {
	themeDir, _, token, teardown := setupShareWallpaperEnv(t)
	defer teardown()

	setWallpaperInConfig(t, themeDir, "background.png")
	model.ConfigInstance.Appearance.PanelOpacity = 0.72

	w := getShareWallpaperResponse(t, token, "appearance")
	assert.Equal(t, http.StatusOK, w.Code)

	var resp shareAppearanceResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "background.png", resp.Appearance.WallpaperFile)
	assert.InDelta(t, 0.72, resp.Appearance.PanelOpacity, 0.0001)
}

func TestSharePublic_Appearance_NoWallpaper(t *testing.T) {
	_, _, token, teardown := setupShareWallpaperEnv(t)
	defer teardown()

	// No wallpaper configured — file is empty, opacity stays at the default.
	w := getShareWallpaperResponse(t, token, "appearance")
	assert.Equal(t, http.StatusOK, w.Code)

	var resp shareAppearanceResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "", resp.Appearance.WallpaperFile)
}

func TestSharePublic_Appearance_StaleConfigReportsEmpty(t *testing.T) {
	themeDir, _, token, teardown := setupShareWallpaperEnv(t)
	defer teardown()

	// Config names a wallpaper whose file has been deleted out-of-band. The
	// appearance endpoint must report "" (not a dangling file name) but stay 200.
	model.ConfigInstance.Appearance.WallpaperFile = "background.png"
	require.NoError(t, os.WriteFile(filepath.Join(themeDir, "background.png"), []byte("x"), 0o644))
	require.NoError(t, os.Remove(filepath.Join(themeDir, "background.png")))

	w := getShareWallpaperResponse(t, token, "appearance")
	assert.Equal(t, http.StatusOK, w.Code)
	var resp shareAppearanceResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "", resp.Appearance.WallpaperFile)
}

func TestSharePublic_Appearance_InvalidToken_404(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/share/ffffffffffffffffffffffffffffffff/appearance", nil)
	w := callHandler(ServeSharePublic, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestSharePublic_ThemeBackground_ServesImage(t *testing.T) {
	themeDir, _, token, teardown := setupShareWallpaperEnv(t)
	defer teardown()

	png := makePNG(8, 8)
	model.ConfigInstance.Appearance.WallpaperFile = "background.png"
	require.NoError(t, os.WriteFile(filepath.Join(themeDir, "background.png"), png, 0o644))

	w := getShareWallpaperResponse(t, token, "theme-background")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.NotEmpty(t, w.Header().Get("ETag"))
	assert.Contains(t, w.Header().Get("Cache-Control"), "immutable")
	assert.Equal(t, png, w.Body.Bytes())
}

func TestSharePublic_ThemeBackground_SVGAddsCSP(t *testing.T) {
	themeDir, _, token, teardown := setupShareWallpaperEnv(t)
	defer teardown()

	safeSVG := `<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10" fill="red"/></svg>`
	model.ConfigInstance.Appearance.WallpaperFile = "background.svg"
	require.NoError(t, os.WriteFile(filepath.Join(themeDir, "background.svg"), []byte(safeSVG), 0o644))

	w := getShareWallpaperResponse(t, token, "theme-background")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "image/svg+xml", w.Header().Get("Content-Type"))
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.Contains(t, w.Header().Get("Content-Security-Policy"), "sandbox")
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}

func TestSharePublic_ThemeBackground_Unset_404(t *testing.T) {
	_, _, token, teardown := setupShareWallpaperEnv(t)
	defer teardown()

	w := getShareWallpaperResponse(t, token, "theme-background")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestSharePublic_ThemeBackground_InvalidToken_404(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/share/ffffffffffffffffffffffffffffffff/theme-background", nil)
	w := callHandler(ServeSharePublic, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestSharePublic_ThemeBackground_TraversalConfig_404(t *testing.T) {
	themeDir, _, token, teardown := setupShareWallpaperEnv(t)
	defer teardown()

	// A corrupted config value must not escape the theme dir via the public
	// share endpoint either — containment runs on the shared serve path.
	model.ConfigInstance.Appearance.WallpaperFile = "../../etc/passwd"
	_ = os.MkdirAll(themeDir, 0o755)

	w := getShareWallpaperResponse(t, token, "theme-background")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestSharePublic_WallpaperRoutes_MethodNotAllowed(t *testing.T) {
	_, _, token, teardown := setupShareWallpaperEnv(t)
	defer teardown()

	for _, rest := range []string{"appearance", "theme-background"} {
		req := newRequest(t, http.MethodPost, "/api/share/"+token+"/"+rest, map[string]string{})
		w := callHandler(ServeSharePublic, req)
		assert.Equal(t, http.StatusMethodNotAllowed, w.Code, "POST %s must 405", rest)
	}
}
