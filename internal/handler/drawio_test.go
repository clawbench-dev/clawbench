package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServeDrawioStatus_ReportsIdle(t *testing.T) {
	defer func() { drawioGetStatus = service.GetDrawioStatus }()
	drawioGetStatus = func() service.DrawioStatus {
		return service.DrawioStatus{Version: "v1", State: "idle"}
	}

	req := newRequest(t, http.MethodGet, "/api/drawio/status", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeDrawioStatus, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, false, resp["installed"])
	assert.Equal(t, "idle", resp["state"])
}

func TestServeDrawioStatus_ReportsBasePathWhenInstalled(t *testing.T) {
	defer func() { drawioGetStatus = service.GetDrawioStatus }()
	drawioGetStatus = func() service.DrawioStatus {
		return service.DrawioStatus{Installed: true, State: "ready", BasePath: "/vendor/drawio/v1/"}
	}

	req := newRequest(t, http.MethodGet, "/api/drawio/status", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeDrawioStatus, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["installed"])
	// The client builds the iframe URL from this, so it must be the served path
	// rather than a constant duplicated in the frontend.
	assert.Equal(t, "/vendor/drawio/v1/", resp["basePath"])
}

func TestServeDrawioInstall_AlreadyInstalledSkipsTheDownload(t *testing.T) {
	defer func() {
		drawioIsReady = service.DrawioReady
		drawioStartInstall = service.StartDrawioInstall
		drawioGetStatus = service.GetDrawioStatus
	}()
	drawioIsReady = func() bool { return true }
	started := false
	drawioStartInstall = func() error { started = true; return nil }
	drawioGetStatus = func() service.DrawioStatus {
		return service.DrawioStatus{Installed: true, State: "ready"}
	}

	req := newRequest(t, http.MethodPost, "/api/drawio/install", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeDrawioInstall, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.False(t, started, "an installed editor must not trigger a second download")
}

func TestServeDrawioInstall_ConflictWhenAlreadyRunning(t *testing.T) {
	defer func() {
		drawioIsReady = service.DrawioReady
		drawioStartInstall = service.StartDrawioInstall
		drawioGetStatus = service.GetDrawioStatus
	}()
	drawioIsReady = func() bool { return false }
	drawioStartInstall = func() error { return assert.AnError }
	drawioGetStatus = func() service.DrawioStatus {
		return service.DrawioStatus{State: "downloading", Downloaded: 10, Total: 100}
	}

	req := newRequest(t, http.MethodPost, "/api/drawio/install", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeDrawioInstall, req)

	require.Equal(t, http.StatusConflict, w.Code)
	// The conflict body is the live status, so the client can start polling
	// instead of showing a bare error.
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "downloading", resp["state"])
}

func TestServeDrawioAsset_ServesInstalledEditor(t *testing.T) {
	orig := model.DataDir
	model.DataDir = t.TempDir()
	t.Cleanup(func() { model.DataDir = orig })

	dir := service.DrawioInstallDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "js"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>editor</html>"), 0o644))

	// The directory URL is what the iframe loads; http.FileServer serves
	// index.html for it (a path ending in /index.html would redirect instead).
	req := newRequest(t, http.MethodGet, service.DrawioBasePath(), nil)
	w := callHandler(ServeDrawioAsset, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "editor")
	// drawio ships fixed filenames, so the version in the path is what makes
	// these files immutable.
	assert.Contains(t, w.Header().Get("Cache-Control"), "immutable")
}

func TestServeDrawioAsset_NotFoundWhenNotInstalled(t *testing.T) {
	orig := model.DataDir
	model.DataDir = t.TempDir()
	t.Cleanup(func() { model.DataDir = orig })

	req := newRequest(t, http.MethodGet, service.DrawioBasePath()+"index.html", nil)
	w := callHandler(ServeDrawioAsset, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeDrawioAsset_RefusesPathsOutsideThePinnedVersion(t *testing.T) {
	orig := model.DataDir
	model.DataDir = t.TempDir()
	t.Cleanup(func() { model.DataDir = orig })

	dir := service.DrawioInstallDir()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("x"), 0o644))

	// A previous version's files must not be reachable by guessing a path.
	req := newRequest(t, http.MethodGet, "/vendor/drawio/v0.0.1/index.html", nil)
	w := callHandler(ServeDrawioAsset, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestValidatePatchValues_DrawioDownloadURL(t *testing.T) {
	// Only an absolute http(s) URL or empty: a typo has to fail here, while the
	// user is still looking at the field, instead of minutes later as an opaque
	// download error.
	bad := []string{"://bad", "ftp://mirror/draw.war", "mirror/draw.war"}
	for _, v := range bad {
		err := validatePatchValues(map[string]any{
			"drawio": map[string]any{"download_url": v},
		})
		assert.Errorf(t, err, "expected %q to be rejected", v)
	}

	for _, v := range []string{"", "https://mirror.internal/draw.war", "http://192.168.1.9/draw.war"} {
		assert.NoErrorf(t, validatePatchValues(map[string]any{
			"drawio": map[string]any{"download_url": v},
		}), "expected %q to be accepted", v)
	}
}

func TestApplyConfigPatch_DrawioDownloadURL(t *testing.T) {
	orig := model.ConfigInstance.Drawio.DownloadURL
	t.Cleanup(func() { model.ConfigInstance.Drawio.DownloadURL = orig })

	applyConfigPatch(map[string]any{
		"drawio": map[string]any{"download_url": "  https://mirror.internal/draw.war  "},
	})

	// Surrounding whitespace is stripped on write: a pasted URL with a trailing
	// space would otherwise fail the request the next time it is used.
	assert.Equal(t, "https://mirror.internal/draw.war", model.ConfigInstance.Drawio.DownloadURL)
}

// multipartUpload builds a request carrying one form file.
func multipartUpload(t *testing.T, field, name string, content []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(field, name)
	require.NoError(t, err)
	_, err = fw.Write(content)
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/drawio/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func TestServeDrawioUpload_InstallsTheProvidedArchive(t *testing.T) {
	defer func() { drawioInstallFrom = service.InstallDrawioFromReader }()
	var gotSize int64
	drawioInstallFrom = func(r io.Reader, total int64) error {
		gotSize = total
		_, err := io.ReadAll(r)
		return err
	}
	drawioGetStatus = func() service.DrawioStatus {
		return service.DrawioStatus{Installed: true, State: "ready"}
	}
	defer func() { drawioGetStatus = service.GetDrawioStatus }()

	w := callHandler(ServeDrawioUpload, multipartUpload(t, "file", "draw.war", []byte("archive-bytes")))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, int64(len("archive-bytes")), gotSize, "the size hint must reach the installer")
}

func TestServeDrawioUpload_RejectsAMissingFile(t *testing.T) {
	// A form without the expected field is a client bug, not a server failure.
	req := httptest.NewRequest(http.MethodPost, "/api/drawio/upload", bytes.NewReader(nil))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")

	w := callHandler(ServeDrawioUpload, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeDrawioUpload_ReportsARejectedArchive(t *testing.T) {
	defer func() { drawioInstallFrom = service.InstallDrawioFromReader }()
	drawioInstallFrom = func(io.Reader, int64) error {
		return errors.New("checksum mismatch: got aa, want bb")
	}

	w := callHandler(ServeDrawioUpload, multipartUpload(t, "file", "draw.war", []byte("x")))

	require.Equal(t, http.StatusBadRequest, w.Code)
	// The reason has to reach the user: "upload failed" alone would leave them
	// guessing whether the file was wrong or the server broke.
	assert.Contains(t, w.Body.String(), "checksum mismatch")
}

func TestServeDrawioUpload_ConflictWhileAnInstallRuns(t *testing.T) {
	defer func() { drawioInstallFrom = service.InstallDrawioFromReader }()
	drawioInstallFrom = func(io.Reader, int64) error { return service.ErrDrawioInstallBusy }
	drawioGetStatus = func() service.DrawioStatus {
		return service.DrawioStatus{State: "downloading"}
	}
	defer func() { drawioGetStatus = service.GetDrawioStatus }()

	w := callHandler(ServeDrawioUpload, multipartUpload(t, "file", "draw.war", []byte("x")))

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestDrawioEndpoints_RejectTheWrongMethod(t *testing.T) {
	// Every endpoint asserts its method up front: without the guard a GET on
	// /api/drawio/install would start a 118MB download.
	cases := []struct {
		name   string
		fn     http.HandlerFunc
		method string
	}{
		{"status", ServeDrawioStatus, http.MethodPost},
		{"install", ServeDrawioInstall, http.MethodGet},
		{"upload", ServeDrawioUpload, http.MethodGet},
		{"asset", ServeDrawioAsset, http.MethodPost},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := newRequest(t, tc.method, "/api/drawio/"+tc.name, nil)
			withAuthCookie(req, model.SessionToken)
			w := callHandler(tc.fn, req)
			assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
		})
	}
}
