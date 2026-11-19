package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServeDesktopLatest(t *testing.T) {
	orig := fetchDesktopLatest
	defer func() { fetchDesktopLatest = orig }()

	fetchDesktopLatest = func() (*service.DesktopLatestResult, error) {
		return &service.DesktopLatestResult{
			Version:       "v0.98.0",
			ClientVersion: "v0.97.0",
			Protocol:      3,
			Tag:           "v0.98.0",
			Downloads:     map[string][]string{"win32-x64": {"https://github.com/o/r/releases/download/v0.98.0/x.zip"}},
			Checksums:     map[string]string{"x.zip": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/desktop/latest", http.NoBody)
	rec := httptest.NewRecorder()
	ServeDesktopLatest(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rawBody := rec.Body.Bytes()
	var body service.DesktopLatestResult
	require.NoError(t, json.Unmarshal(rawBody, &body))
	assert.Equal(t, "v0.98.0", body.Version)
	assert.Equal(t, "v0.98.0", body.Tag)
	// The desktop gate compares `protocol`, and the installer keys its directory
	// off `clientVersion` — both must survive the JSON round-trip or the client
	// silently falls back to comparing version strings / installs under the
	// wrong version.
	assert.Equal(t, 3, body.Protocol)
	assert.Equal(t, "v0.97.0", body.ClientVersion)
	require.Len(t, body.Downloads["win32-x64"], 1)
	assert.Equal(t, "https://github.com/o/r/releases/download/v0.98.0/x.zip", body.Downloads["win32-x64"][0])
	// The checksum map must survive the JSON round-trip; the desktop client
	// looks it up by asset filename.
	assert.Equal(t, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", body.Checksums["x.zip"])
	// The removed payload field must not reappear in the response.
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rawBody, &raw))
	_, hasPayloads := raw["payloads"]
	assert.False(t, hasPayloads, "payloads must not be in the response")
}

// A dev server has no matching release, so it must answer 200 with an empty
// download list rather than erroring — the web UI hides the download row on an
// empty list, and a 502 would be logged as a failure on every dev instance.
func TestServeDesktopLatest_DevBuildReturnsEmptyDownloads(t *testing.T) {
	orig := fetchDesktopLatest
	defer func() { fetchDesktopLatest = orig }()

	fetchDesktopLatest = func() (*service.DesktopLatestResult, error) {
		return &service.DesktopLatestResult{
			Version:   "dev",
			Tag:       "",
			Downloads: map[string][]string{},
		}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/desktop/latest", http.NoBody)
	rec := httptest.NewRecorder()
	ServeDesktopLatest(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var body service.DesktopLatestResult
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "dev", body.Version)
	assert.Empty(t, body.Downloads)
	assert.Empty(t, body.Checksums)
}

func TestServeDesktopLatest_FetchError(t *testing.T) {
	orig := fetchDesktopLatest
	defer func() { fetchDesktopLatest = orig }()

	fetchDesktopLatest = func() (*service.DesktopLatestResult, error) {
		return nil, fmt.Errorf("network error")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/desktop/latest", http.NoBody)
	rec := httptest.NewRecorder()
	ServeDesktopLatest(rec, req)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
}
