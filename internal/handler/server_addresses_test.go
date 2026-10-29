package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The share dialogs build links from this endpoint instead of
// window.location.origin, which only knows the one address the browser used.

func TestServerAddresses_ReportsPortFromHost(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.test/api/server/addresses", http.NoBody)
	req.Host = "192.168.1.7:20000"
	w := httptest.NewRecorder()

	ServeServerAddresses(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	// The port must come from the request, not a package variable: behind a
	// port-forward the public port differs from the listening one.
	assert.Contains(t, w.Body.String(), `"port":"20000"`)
}

func TestServerAddresses_AlwaysReturnsAddressesField(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.test/api/server/addresses", http.NoBody)
	req.Host = "example.test:20000"
	w := httptest.NewRecorder()

	ServeServerAddresses(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	// The field must always be present (possibly empty): the client
	// distinguishes "no extra addresses" from "request failed" by it.
	assert.Contains(t, w.Body.String(), `"addresses"`)
}

func TestServerAddresses_RejectsNonGet(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://example.test/api/server/addresses", http.NoBody)
	w := httptest.NewRecorder()

	ServeServerAddresses(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
