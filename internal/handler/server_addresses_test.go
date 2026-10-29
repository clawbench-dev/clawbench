package handler

import (
	"context"
	"net"
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

// A Host header without a port (plain `Host: example.test`) cannot be split, so
// the port must come from the listening socket instead of being reported as "".
func TestServerAddresses_FallsBackToLocalAddrWhenHostHasNoPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.test/api/server/addresses", http.NoBody)
	req.Host = "example.test"
	// `httptest.NewRequest` already sets LocalAddrContextKey to a 192.0.2.1:1234
	// TCP address; pin it explicitly so the assertion cannot depend on the
	// recorder's default.
	req = req.WithContext(context.WithValue(
		req.Context(),
		http.LocalAddrContextKey,
		&net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 34567},
	))
	w := httptest.NewRecorder()

	ServeServerAddresses(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"port":"34567"`,
		"the listening port must be used when the Host header carries none")
}

// A non-TCP listener (Unix socket) or a context without a LocalAddr must not
// panic: the handler reports port "0" rather than a type-assertion crash.
func TestServerAddresses_SurvivesNonTCPLocalAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.test/api/server/addresses", http.NoBody)
	req.Host = "example.test"
	req = req.WithContext(context.WithValue(
		req.Context(),
		http.LocalAddrContextKey,
		&net.UnixAddr{Name: "/tmp/sock", Net: "unix"},
	))
	w := httptest.NewRecorder()

	require.NotPanics(t, func() { ServeServerAddresses(w, req) })

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"port":"0"`)
}

// Same, but with no LocalAddr at all in the context.
func TestServerAddresses_SurvivesMissingLocalAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.test/api/server/addresses", http.NoBody)
	req.Host = "example.test"
	req = req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey, nil))
	w := httptest.NewRecorder()

	require.NotPanics(t, func() { ServeServerAddresses(w, req) })

	assert.Contains(t, w.Body.String(), `"port":"0"`)
}
