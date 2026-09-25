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
			Version:   "v0.98.0",
			Tag:       "v0.98.0",
			Downloads: map[string][]string{"win32-x64": {"https://github.com/o/r/releases/download/v0.98.0/x.zip"}},
			Payloads:  map[string][]string{"win32-x64": {"https://github.com/o/r/releases/download/v0.98.0/p.zip"}},
		}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/desktop/latest", http.NoBody)
	rec := httptest.NewRecorder()
	ServeDesktopLatest(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var body service.DesktopLatestResult
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "v0.98.0", body.Version)
	assert.Equal(t, "v0.98.0", body.Tag)
	require.Len(t, body.Downloads["win32-x64"], 1)
	assert.Equal(t, "https://github.com/o/r/releases/download/v0.98.0/x.zip", body.Downloads["win32-x64"][0])
	// The payload list must survive the JSON round-trip; the desktop client
	// reads it to decide whether it can skip the full download.
	require.Len(t, body.Payloads["win32-x64"], 1)
	assert.Equal(t, "https://github.com/o/r/releases/download/v0.98.0/p.zip", body.Payloads["win32-x64"][0])
	// macOS has no payload, so the key must be absent rather than an empty list.
	_, hasDarwin := body.Payloads["darwin-arm64"]
	assert.False(t, hasDarwin, "macOS must have no payload key")
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
			Payloads:  map[string][]string{},
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
	assert.Empty(t, body.Payloads)
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
