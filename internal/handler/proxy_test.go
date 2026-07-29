package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
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

	_, _ = service.ProxyService.RegisterPort(8080, "", "test", "")

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
	_, _ = service.ProxyService.RegisterPort(8080, "", "local-api", "http")

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

	localPort1, _ := service.ProxyService.RegisterPort(3000, "", "first", "")

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

	_, _ = service.ProxyService.RegisterPort(9090, "", "metrics", "")

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

	_, _ = service.ProxyService.RegisterPort(3000, "", "app", "")
	_, _ = service.ProxyService.RegisterPort(5173, "", "vite", "")
	_, _ = service.ProxyService.RegisterPort(8080, "", "api", "")

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
	_, _ = service.ProxyService.RegisterPort(8080, "", "api", "http")

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

	_, _ = service.ProxyService.RegisterPort(8080, "", "api", "http")

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

	_, _ = service.ProxyService.RegisterPort(3500, "", "app", "")

	req := newRequest(t, http.MethodPut, "/api/proxy/ports", map[string]interface{}{
		"localPort": 3500,
		"port":      8080,
		"name":      "updated",
	})
	w := callHandler(ServeProxyPortAction, req)

	assertStatus(t, w, http.StatusForbidden)
}
