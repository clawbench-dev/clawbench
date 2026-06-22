package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestServeHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/health", http.NoBody)
	w := httptest.NewRecorder()

	ServeHealth(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var resp healthResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	assert.NoError(t, err)
	assert.Equal(t, "clawbench", resp.App)
	assert.NotEmpty(t, resp.Version)
}
