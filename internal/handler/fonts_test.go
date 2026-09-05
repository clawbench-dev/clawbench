package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupFontsTestEnv configures model.DataDir so ResolveFontsDir() resolves to
// a fresh temp dir, and sets ConfigInstance.Fonts.Dir to the same value so the
// handler uses it directly. Returns the fonts dir and teardown.
func setupFontsTestEnv(t *testing.T) (string, func()) {
	_, teardown := setupTestEnv(t)

	origConfig := model.ConfigInstance
	origDataDir := model.DataDir

	fontsDir := filepath.Join(t.TempDir(), "fonts")
	cfg := model.Config{}
	cfg.Fonts.Dir = fontsDir
	model.ConfigInstance = cfg
	model.DataDir = filepath.Dir(fontsDir)

	cleanup := func() {
		model.ConfigInstance = origConfig
		model.DataDir = origDataDir
		teardown()
	}
	return fontsDir, cleanup
}

func TestServeFontsList_CreatesDirAndReturnsFonts(t *testing.T) {
	fontsDir, teardown := setupFontsTestEnv(t)
	defer teardown()

	// Directory does not exist yet — the list handler must auto-create it.
	require.NoError(t, os.MkdirAll(fontsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fontsDir, "Sarasa.woff2"), []byte("woff2-data"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(fontsDir, "sub"), 0o755))

	req := httptest.NewRequest(http.MethodGet, "/api/fonts/list", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeFontsList, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp fontListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, fontsDir, resp.Dir)
	require.Len(t, resp.Fonts, 1)
	assert.Equal(t, "Sarasa", resp.Fonts[0].Family)
	assert.Equal(t, "Sarasa.woff2", resp.Fonts[0].File)
	assert.Equal(t, ".woff2", resp.Fonts[0].Ext)
}

func TestServeFontsList_CreatesMissingDir(t *testing.T) {
	fontsDir, teardown := setupFontsTestEnv(t)
	defer teardown()

	// Remove the dir created by setup to simulate first access.
	require.NoError(t, os.RemoveAll(fontsDir))

	req := httptest.NewRequest(http.MethodGet, "/api/fonts/list", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeFontsList, req)

	assert.Equal(t, http.StatusOK, w.Code)
	_, err := os.Stat(fontsDir)
	assert.NoError(t, err, "fonts dir should be auto-created")
}

func TestServeFontsList_Unauthorized(t *testing.T) {
	_, teardown := setupFontsTestEnv(t)
	defer teardown()

	model.SessionToken = "test-token"
	req := httptest.NewRequest(http.MethodGet, "/api/fonts/list", http.NoBody)
	w := callHandlerWithAuth(ServeFontsList, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestServeFontFile_ServesContent(t *testing.T) {
	fontsDir, teardown := setupFontsTestEnv(t)
	defer teardown()

	content := []byte("fake-woff2-glyphs")
	require.NoError(t, os.MkdirAll(fontsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fontsDir, "Sarasa Mono.woff2"), content, 0o644))

	req := httptest.NewRequest(http.MethodGet, "/api/fonts/file?name="+url.QueryEscape("Sarasa Mono.woff2"), http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeFontFile, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "font/woff2", w.Header().Get("Content-Type"))
	assert.Equal(t, content, w.Body.Bytes())
}

func TestServeFontFile_RejectsTraversal(t *testing.T) {
	_, teardown := setupFontsTestEnv(t)
	defer teardown()

	cases := []string{
		"../etc/passwd",
		"/etc/passwd",
		"sub/dir/file.ttf",
		"",
	}
	for _, name := range cases {
		req := httptest.NewRequest(http.MethodGet, "/api/fonts/file?name="+url.QueryEscape(name), http.NoBody)
		withAuthCookie(req, model.SessionToken)
		w := callHandler(ServeFontFile, req)
		assert.Equal(t, http.StatusBadRequest, w.Code, "name=%q should be rejected", name)
	}
}

func TestServeFontFile_MissingFile(t *testing.T) {
	fontsDir, teardown := setupFontsTestEnv(t)
	defer teardown()
	require.NoError(t, os.MkdirAll(fontsDir, 0o755))

	req := httptest.NewRequest(http.MethodGet, "/api/fonts/file?name=ghost.ttf", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeFontFile, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeFontFile_DirectoryRejected(t *testing.T) {
	fontsDir, teardown := setupFontsTestEnv(t)
	defer teardown()
	require.NoError(t, os.MkdirAll(filepath.Join(fontsDir, "afolder"), 0o755))

	req := httptest.NewRequest(http.MethodGet, "/api/fonts/file?name=afolder", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeFontFile, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeFontFile_Unauthorized(t *testing.T) {
	_, teardown := setupFontsTestEnv(t)
	defer teardown()

	model.SessionToken = "test-token"
	req := httptest.NewRequest(http.MethodGet, "/api/fonts/file?name=x.ttf", http.NoBody)
	w := callHandlerWithAuth(ServeFontFile, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestServeFontFile_MethodNotAllowed(t *testing.T) {
	_, teardown := setupFontsTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodPost, "/api/fonts/file?name=x.ttf", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeFontFile, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
