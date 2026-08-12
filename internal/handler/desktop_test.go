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
		return &service.DesktopLatestResult{Version: "0.2.0", Downloads: map[string]string{"win32-x64": "https://npm/t.tgz"}}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/desktop/latest", http.NoBody)
	rec := httptest.NewRecorder()
	ServeDesktopLatest(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var body service.DesktopLatestResult
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "0.2.0", body.Version)
	assert.Equal(t, "https://npm/t.tgz", body.Downloads["win32-x64"])
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
