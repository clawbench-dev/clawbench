package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isProxyPortRegistered is a test helper that checks if a port is registered via ListPorts.
func isProxyPortRegistered(r *service.ProxyRegistry, port int) bool {
	for _, p := range r.ListPorts() {
		if p.Port == port {
			return true
		}
	}
	return false
}

// getProxyPortProtocol is a test helper that returns the protocol for a registered port.
func getProxyPortProtocol(r *service.ProxyRegistry, port int) string {
	for _, p := range r.ListPorts() {
		if p.Port == port {
			return p.Protocol
		}
	}
	return "http"
}

// setupProxyTest creates a ProxyService for testing and returns a teardown func.
func setupProxyTest(t *testing.T) func() {
	t.Helper()
	origProxy := service.ProxyService
	service.ProxyService = service.NewProxyRegistry(0)
	return func() {
		service.ProxyService.Stop()
		service.ProxyService = origProxy
	}
}

func TestServeProxyPorts_ListEmpty(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/proxy/ports", nil)
	w := callHandler(ServeProxyPortAction, req)

	assertOK(t, w)
	assertJSONField(t, w, "ports", []interface{}{})
}

func TestServeProxyPorts_AfterRegister(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	_, _ = service.ProxyService.RegisterPort(8080, "", "test", "", "")

	req := newRequest(t, http.MethodGet, "/api/proxy/ports", nil)
	w := callHandler(ServeProxyPortAction, req)

	assertOK(t, w)

	// Verify ports list contains the registered port
	var result map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	ports, ok := result["ports"].([]interface{})
	assert.True(t, ok)
	assert.Len(t, ports, 1)
}

func TestRegisterPort_Valid(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/proxy/ports", map[string]interface{}{
		"port": 5173,
		"name": "Vite",
	})
	w := callHandler(ServeProxyPortAction, req)

	assertOK(t, w)
	assertJSONField(t, w, "status", "ok")
	assertJSONField(t, w, "localPort", float64(5173)) // JSON numbers decode as float64
	assert.True(t, isProxyPortRegistered(service.ProxyService, 5173))
}

func TestRegisterPort_ReturnsAutoAssignedLocalPort(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	// Register port 8080 first
	_, _ = service.ProxyService.RegisterPort(8080, "", "local-api", "http", "")

	// Register same target port with different host — localPort should be auto-assigned
	req := newRequest(t, http.MethodPost, "/api/proxy/ports", map[string]interface{}{
		"port": 8080,
		"host": "192.168.1.100",
		"name": "remote-api",
	})
	w := callHandler(ServeProxyPortAction, req)

	assertOK(t, w)
	assertJSONField(t, w, "status", "ok")
	assertJSONField(t, w, "localPort", float64(8081)) // auto-assigned next free port
}

func TestRegisterPort_InvalidPort(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	tests := []struct {
		name string
		port int
	}{
		{"zero", 0},
		{"negative", -1},
		{"too large", 70000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := newRequest(t, http.MethodPost, "/api/proxy/ports", map[string]interface{}{
				"port": tt.port,
				"name": "",
			})
			w := callHandler(ServeProxyPortAction, req)
			assertStatus(t, w, http.StatusBadRequest)
		})
	}
}

func TestRegisterPort_Duplicate(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	localPort1, _ := service.ProxyService.RegisterPort(3000, "", "first", "", "")

	req := newRequest(t, http.MethodPost, "/api/proxy/ports", map[string]interface{}{
		"port": 3000,
		"name": "second",
	})
	w := callHandler(ServeProxyPortAction, req)

	// Duplicate registration is now idempotent — returns existing localPort
	assertOK(t, w)
	assertJSONField(t, w, "status", "ok")
	assertJSONField(t, w, "localPort", float64(localPort1))
}

func TestRegisterPort_DisallowedRange(t *testing.T) {
	origProxy := service.ProxyService
	service.ProxyService = service.NewProxyRegistry(0)
	service.ProxyService.SetAllowedPorts("3000-4000")
	defer func() {
		service.ProxyService.Stop()
		service.ProxyService = origProxy
	}()

	req := newRequest(t, http.MethodPost, "/api/proxy/ports", map[string]interface{}{
		"port": 8080,
		"name": "",
	})
	w := callHandler(ServeProxyPortAction, req)

	assertStatus(t, w, http.StatusForbidden)
}

func TestUnregisterPort_Valid(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	_, _ = service.ProxyService.RegisterPort(9090, "", "metrics", "", "")

	req := httptest.NewRequest(http.MethodDelete, "/api/proxy/ports?port=9090", http.NoBody)
	w := callHandler(ServeProxyPortAction, req)

	assertOK(t, w)
	assertJSONField(t, w, "status", "ok")
	assert.False(t, isProxyPortRegistered(service.ProxyService, 9090))
}

func TestUnregisterPort_NotRegistered(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodDelete, "/api/proxy/ports?port=9999", http.NoBody)
	w := callHandler(ServeProxyPortAction, req)

	assertStatus(t, w, http.StatusNotFound)
}

func TestUnregisterPort_InvalidQuery(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	tests := []struct {
		name  string
		query string
	}{
		{"missing", ""},
		{"non-numeric", "port=abc"},
		{"negative", "port=-1"},
		{"zero", "port=0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, "/api/proxy/ports?"+tt.query, http.NoBody)
			w := callHandler(ServeProxyPortAction, req)
			assertStatus(t, w, http.StatusBadRequest)
		})
	}
}

func TestServeProxyPortAction_MethodNotAllowed(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	req := newRequest(t, http.MethodPatch, "/api/proxy/ports", map[string]interface{}{
		"port": 8080,
	})
	w := callHandler(ServeProxyPortAction, req)

	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestServeProxyDetect(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/proxy/detect", nil)
	w := callHandler(ServeProxyDetect, req)

	assertOK(t, w)
}

func TestRegisterPort_EmptyName(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/proxy/ports", map[string]interface{}{
		"port": 4000,
		"name": "",
	})
	w := callHandler(ServeProxyPortAction, req)

	assertOK(t, w)
	assert.True(t, isProxyPortRegistered(service.ProxyService, 4000))
}

func TestRegisterPort_MissingBody(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodPost, "/api/proxy/ports", http.NoBody)
	w := callHandler(ServeProxyPortAction, req)

	assertStatus(t, w, http.StatusBadRequest)
}

func TestRegisterAndListMultiple(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	_, _ = service.ProxyService.RegisterPort(3000, "", "app", "", "")
	_, _ = service.ProxyService.RegisterPort(5173, "", "vite", "", "")
	_, _ = service.ProxyService.RegisterPort(8080, "", "api", "", "")

	req := newRequest(t, http.MethodGet, "/api/proxy/ports", nil)
	w := callHandler(ServeProxyPortAction, req)

	assertOK(t, w)
	// Verify we get 3 ports back (sorted by port number)
	ports := service.ProxyService.ListPorts()
	assert.Len(t, ports, 3)
	assert.Equal(t, 3000, ports[0].Port)
	assert.Equal(t, 5173, ports[1].Port)
	assert.Equal(t, 8080, ports[2].Port)
}

func TestRegisterPort_WithProtocol(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/proxy/ports", map[string]interface{}{
		"port":     4443,
		"name":     "secure",
		"protocol": "https",
	})
	w := callHandler(ServeProxyPortAction, req)

	assertOK(t, w)
	assert.Equal(t, "https", getProxyPortProtocol(service.ProxyService, 4443))
}

func TestRegisterPort_DefaultProtocol(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/proxy/ports", map[string]interface{}{
		"port": 8080,
		"name": "plain",
	})
	w := callHandler(ServeProxyPortAction, req)

	assertOK(t, w)
	assert.Equal(t, "http", getProxyPortProtocol(service.ProxyService, 8080))
}

// --- Register with host ---

func TestRegisterPort_WithHost(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/proxy/ports", map[string]interface{}{
		"port": 8080,
		"host": "192.168.1.100",
		"name": "remote-api",
	})
	w := callHandler(ServeProxyPortAction, req)

	assertOK(t, w)
	assertJSONField(t, w, "status", "ok")
	assert.True(t, isProxyPortRegistered(service.ProxyService, 8080))
}

func TestRegisterPort_EmptyHost(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/proxy/ports", map[string]interface{}{
		"port": 3000,
		"host": "",
		"name": "local",
	})
	w := callHandler(ServeProxyPortAction, req)

	assertOK(t, w)
	assertJSONField(t, w, "status", "ok")
}

// --- UpdatePort (PUT) ---

func TestUpdatePort_Valid(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	// Register a port first
	_, _ = service.ProxyService.RegisterPort(8080, "", "api", "http", "")

	req := newRequest(t, http.MethodPut, "/api/proxy/ports", map[string]interface{}{
		"localPort": 8080,
		"port":      8080,
		"host":      "",
		"name":      "api-v2",
		"protocol":  "https",
	})
	w := callHandler(ServeProxyPortAction, req)

	assertOK(t, w)
	assertJSONField(t, w, "status", "ok")
	assert.Equal(t, "https", getProxyPortProtocol(service.ProxyService, 8080))
}

func TestUpdatePort_WithHost(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	_, _ = service.ProxyService.RegisterPort(8080, "", "api", "http", "")

	req := newRequest(t, http.MethodPut, "/api/proxy/ports", map[string]interface{}{
		"localPort": 8080,
		"port":      9090,
		"host":      "192.168.1.100",
		"name":      "remote-api",
		"protocol":  "http",
	})
	w := callHandler(ServeProxyPortAction, req)

	assertOK(t, w)
	assertJSONField(t, w, "status", "ok")
}

func TestUpdatePort_InvalidLocalPort(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	tests := []struct {
		name      string
		localPort int
	}{
		{"zero", 0},
		{"negative", -1},
		{"too large", 70000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := newRequest(t, http.MethodPut, "/api/proxy/ports", map[string]interface{}{
				"localPort": tt.localPort,
				"port":      8080,
				"name":      "test",
			})
			w := callHandler(ServeProxyPortAction, req)
			assertStatus(t, w, http.StatusBadRequest)
		})
	}
}

func TestUpdatePort_MissingBody(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodPut, "/api/proxy/ports", http.NoBody)
	w := callHandler(ServeProxyPortAction, req)

	assertStatus(t, w, http.StatusBadRequest)
}

func TestUpdatePort_NotRegistered(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	req := newRequest(t, http.MethodPut, "/api/proxy/ports", map[string]interface{}{
		"localPort": 9999,
		"port":      8080,
		"name":      "test",
	})
	w := callHandler(ServeProxyPortAction, req)

	// UpdatePort on non-existent localPort should return Forbidden (AccessDenied)
	assertStatus(t, w, http.StatusForbidden)
}

func TestUpdatePort_DisallowedPortRange(t *testing.T) {
	origProxy := service.ProxyService
	service.ProxyService = service.NewProxyRegistry(0)
	service.ProxyService.SetAllowedPorts("3000-4000")
	defer func() {
		service.ProxyService.Stop()
		service.ProxyService = origProxy
	}()

	_, _ = service.ProxyService.RegisterPort(3500, "", "app", "", "")

	req := newRequest(t, http.MethodPut, "/api/proxy/ports", map[string]interface{}{
		"localPort": 3500,
		"port":      8080,
		"name":      "updated",
	})
	w := callHandler(ServeProxyPortAction, req)

	assertStatus(t, w, http.StatusForbidden)
}

// --- SetPortEnabled (PUT /api/proxy/ports/enabled) ---

func TestServeProxySetPortEnabled_Disable(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	_, _ = service.ProxyService.RegisterPort(8080, "", "api", "http", "")

	req := newRequest(t, http.MethodPut, "/api/proxy/ports/enabled", map[string]interface{}{
		"localPort": 8080,
		"enabled":   false,
	})
	w := callHandler(ServeProxySetPortEnabled, req)

	assertOK(t, w)
	assertJSONField(t, w, "status", "ok")

	ports := service.ProxyService.ListPorts()
	assert.Len(t, ports, 1)
	assert.False(t, ports[0].Enabled)
	// Disabling marks the port inactive
	assert.False(t, ports[0].Active)
}

func TestServeProxySetPortEnabled_Enable(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	_, _ = service.ProxyService.RegisterPort(8080, "", "api", "http", "")
	_ = service.ProxyService.SetPortEnabled(8080, false)

	req := newRequest(t, http.MethodPut, "/api/proxy/ports/enabled", map[string]interface{}{
		"localPort": 8080,
		"enabled":   true,
	})
	w := callHandler(ServeProxySetPortEnabled, req)

	assertOK(t, w)

	ports := service.ProxyService.ListPorts()
	assert.Len(t, ports, 1)
	assert.True(t, ports[0].Enabled)
}

func TestServeProxySetPortEnabled_NotRegistered(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	req := newRequest(t, http.MethodPut, "/api/proxy/ports/enabled", map[string]interface{}{
		"localPort": 9999,
		"enabled":   false,
	})
	w := callHandler(ServeProxySetPortEnabled, req)

	assertStatus(t, w, http.StatusNotFound)
}

func TestServeProxySetPortEnabled_InvalidLocalPort(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	tests := []struct {
		name      string
		localPort int
	}{
		{"zero", 0},
		{"negative", -1},
		{"too large", 70000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := newRequest(t, http.MethodPut, "/api/proxy/ports/enabled", map[string]interface{}{
				"localPort": tt.localPort,
				"enabled":   false,
			})
			w := callHandler(ServeProxySetPortEnabled, req)
			assertStatus(t, w, http.StatusBadRequest)
		})
	}
}

func TestServeProxySetPortEnabled_MissingBody(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodPut, "/api/proxy/ports/enabled", http.NoBody)
	w := callHandler(ServeProxySetPortEnabled, req)

	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeProxySetPortEnabled_MethodNotAllowed(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/proxy/ports/enabled", nil)
	w := callHandler(ServeProxySetPortEnabled, req)

	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestRegisterPort_AcceptsDirectionReverse(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/proxy/ports", map[string]interface{}{
		"port":      3000,
		"name":      "local svc",
		"direction": "reverse",
	})
	w := callHandler(ServeProxyPortAction, req)

	assertOK(t, w)

	var result map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	serverPort := int(result["localPort"].(float64))

	var found *model.ForwardedPort
	for _, p := range service.ProxyService.ListPorts() {
		if p.LocalPort == serverPort {
			cp := p
			found = &cp
		}
	}
	assert.NotNil(t, found)
	assert.Equal(t, model.DirectionReverse, found.Direction)
}

func TestUpdatePort_AcceptsDirection(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	serverPort, err := service.ProxyService.RegisterPort(3000, "", "svc", "http", model.DirectionReverse)
	assert.NoError(t, err)

	req := newRequest(t, http.MethodPut, "/api/proxy/ports", map[string]interface{}{
		"localPort": serverPort,
		"port":      3001,
		"host":      "",
		"name":      "renamed",
		"protocol":  "http",
		"direction": "reverse",
	})
	w := callHandler(ServeProxyPortAction, req)
	assertOK(t, w)

	var found *model.ForwardedPort
	for _, p := range service.ProxyService.ListPorts() {
		if p.LocalPort == serverPort {
			cp := p
			found = &cp
		}
	}
	assert.NotNil(t, found)
	assert.Equal(t, 3001, found.Port)
	assert.Equal(t, "renamed", found.Name)
	assert.Equal(t, model.DirectionReverse, found.Direction)
}

func TestServeProxyPorts_IncludesDirection(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	_, _ = service.ProxyService.RegisterPort(5173, "", "fwd", "http", model.DirectionForward)
	_, _ = service.ProxyService.RegisterPort(3000, "", "rev", "http", model.DirectionReverse)

	req := newRequest(t, http.MethodGet, "/api/proxy/ports", nil)
	w := callHandler(ServeProxyPortAction, req)
	assertOK(t, w)

	var result map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	ports := result["ports"].([]interface{})
	assert.Len(t, ports, 2)

	directions := map[string]bool{}
	for _, raw := range ports {
		p := raw.(map[string]interface{})
		d, ok := p["direction"].(string)
		assert.True(t, ok, "every port entry must carry a direction field")
		directions[d] = true
	}
	assert.True(t, directions[model.DirectionForward])
	assert.True(t, directions[model.DirectionReverse])
}

// With no ProxyRegistry (manually nil'd here — no configuration produces this
// any more, since shouldCreateProxyRegistry is unconditional; see
// cmd/server/proxy_registry_gate.go), every handler on this surface must refuse
// with 503 rather than dereference a nil singleton. The nil guard remains
// load-bearing for a failed creation or a future refactor; before it existed
// these handlers could assume a non-nil registry, and the panic would surface
// as a middleware-recovered 500 rather than "port forwarding unavailable".
func TestProxyHandlers_NilRegistryReturns503(t *testing.T) {
	origProxy := service.ProxyService
	service.ProxyService = nil
	defer func() { service.ProxyService = origProxy }()

	tests := []struct {
		name    string
		handler http.HandlerFunc
		method  string
		target  string
		body    string
	}{
		{name: "list ports", handler: ServeProxyPortAction, method: http.MethodGet, target: "/api/proxy/ports"},
		{name: "register port", handler: ServeProxyPortAction, method: http.MethodPost, target: "/api/proxy/ports", body: `{"port":8080}`},
		{name: "update port", handler: ServeProxyPortAction, method: http.MethodPut, target: "/api/proxy/ports", body: `{"localPort":8080,"port":9090}`},
		{name: "unregister port", handler: ServeProxyPortAction, method: http.MethodDelete, target: "/api/proxy/ports?port=8080"},
		{name: "toggle enabled", handler: ServeProxySetPortEnabled, method: http.MethodPut, target: "/api/proxy/ports/enabled", body: `{"localPort":8080,"enabled":true}`},
		{name: "detect ports", handler: ServeProxyDetect, method: http.MethodGet, target: "/api/proxy/detect"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req *http.Request
			if tt.body != "" {
				req = httptest.NewRequest(tt.method, tt.target, strings.NewReader(tt.body))
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = httptest.NewRequest(tt.method, tt.target, http.NoBody)
			}

			require.NotPanics(t, func() {
				w := callHandler(tt.handler, req)
				assert.Equal(t, http.StatusServiceUnavailable, w.Code)
			})
		})
	}
}

// ── POST /api/proxy/ports/rebind ──

func TestServeProxyRebind_MovesKey(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	local, _ := service.ProxyService.RegisterPort(5173, "", "Vite", "http", "")

	req := newRequest(t, http.MethodPost, "/api/proxy/ports/rebind", map[string]interface{}{
		"localPort":    local,
		"newLocalPort": local + 1,
	})
	w := callHandler(ServeProxyRebind, req)

	assertOK(t, w)
	assertJSONField(t, w, "status", "ok")
	// The response echoes the new key so the client does not have to assume it.
	assertJSONField(t, w, "localPort", float64(local+1))
	// isProxyPortRegistered keys off the TARGET port (unchanged by a rebind), so
	// assert on the local key directly.
	var found bool
	for _, p := range service.ProxyService.ListPorts() {
		if p.LocalPort == local+1 {
			found = true
		}
		if p.LocalPort == local {
			t.Fatalf("old local port %d must no longer be registered", local)
		}
	}
	assert.True(t, found, "the mapping must be registered under the new local port")
}

func TestServeProxyRebind_UnknownSourceIs404(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/proxy/ports/rebind", map[string]interface{}{
		"localPort":    9999,
		"newLocalPort": 10000,
	})
	w := callHandler(ServeProxyRebind, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeProxyRebind_TargetTakenIs409(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	first, _ := service.ProxyService.RegisterPort(5173, "", "one", "http", "")
	second, _ := service.ProxyService.RegisterPort(8080, "", "two", "http", "")

	req := newRequest(t, http.MethodPost, "/api/proxy/ports/rebind", map[string]interface{}{
		"localPort":    first,
		"newLocalPort": second,
	})
	w := callHandler(ServeProxyRebind, req)

	// 409 (not 400/500) is what lets the client retry with another port.
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestServeProxyRebind_InvalidPortIs400(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/proxy/ports/rebind", map[string]interface{}{
		"localPort":    8080,
		"newLocalPort": 70000,
	})
	w := callHandler(ServeProxyRebind, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeProxyRebind_RejectsGet(t *testing.T) {
	teardown := setupProxyTest(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/proxy/ports/rebind", nil)
	w := callHandler(ServeProxyRebind, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
