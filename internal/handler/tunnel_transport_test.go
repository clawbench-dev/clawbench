package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withTunnelTransport points the live config at a transport for one test and
// installs a registry, so the transport guard is the only thing that can refuse
// the request.
func withTunnelTransport(t *testing.T, transport string) {
	t.Helper()
	origCfg := model.ConfigInstance
	origProxy := service.ProxyService
	t.Cleanup(func() {
		model.ConfigInstance = origCfg
		if service.ProxyService != nil && service.ProxyService != origProxy {
			service.ProxyService.Stop()
		}
		service.ProxyService = origProxy
	})

	service.ProxyService = service.NewProxyRegistry(0)
	cfg := model.Config{}
	cfg.PortForward.Transport = transport
	model.ConfigInstance = cfg
}

// With transport "ssh" the server carries forwards over the SSH listener only,
// so both h2 endpoints must refuse. Without this the operator's choice would be
// a hint: a client could keep dialing /api/tunnel/* regardless.
func TestTunnelEndpoints_RefuseWhenTransportIsSSH(t *testing.T) {
	withTunnelTransport(t, model.TransportSSH)

	streamReq := httptest.NewRequest(http.MethodPost, "/api/tunnel/stream?host=127.0.0.1&port=8080", http.NoBody)
	streamW := httptest.NewRecorder()
	TunnelStream(streamW, streamReq)
	assert.Equal(t, http.StatusServiceUnavailable, streamW.Code)
	assert.Contains(t, streamW.Body.String(), "TunnelTransportDisabled")

	controlReq := httptest.NewRequest(http.MethodPost, "/api/tunnel/control", http.NoBody)
	controlW := httptest.NewRecorder()
	TunnelControl(controlW, controlReq)
	assert.Equal(t, http.StatusServiceUnavailable, controlW.Code)
	assert.Contains(t, controlW.Body.String(), "TunnelTransportDisabled")
}

// With transport "h2" the guard must let the request through: the missing
// target is rejected by the next check (400), which is how we tell "the gate
// opened" apart from "the gate refused with the same status".
func TestTunnelStream_PassesTransportGateWhenH2(t *testing.T) {
	withTunnelTransport(t, model.TransportH2)

	req := httptest.NewRequest(http.MethodPost, "/api/tunnel/stream", http.NoBody)
	w := httptest.NewRecorder()
	TunnelStream(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.NotContains(t, w.Body.String(), "TunnelTransportDisabled")
}

// The retired "both" must still open the h2 endpoints: an install that has not
// been rewritten yet keeps working until ApplyDefaults normalizes it.
func TestTunnelStream_RetiredBothStillServesH2(t *testing.T) {
	withTunnelTransport(t, model.TransportBoth)

	req := httptest.NewRequest(http.MethodPost, "/api/tunnel/stream", http.NoBody)
	w := httptest.NewRecorder()
	TunnelStream(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.NotContains(t, w.Body.String(), "TunnelTransportDisabled")
}
