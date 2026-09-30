package handler

import (
	"net/http"
	"strconv"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// ServeProxyPorts returns the list of registered forwarded ports with health status.
func ServeProxyPorts(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if !requireProxyRegistry(w, r) {
		return
	}
	ports := service.ProxyService.ListPorts()
	writeJSON(w, http.StatusOK, map[string]any{"ports": ports})
}

// requireProxyRegistry writes a 503 and reports false when no registry exists.
//
// The registry is created whenever a transport could carry traffic (see
// cmd/server/proxy_registry_gate.go), so nil is only reachable with
// `port_forward.transport: ssh` and `enabled: false` — the configuration where
// port forwarding is provably off. Every handler on this surface used to
// dereference service.ProxyService directly, which was safe only while the
// registry was created for every non-SSH-disabled install; now that a
// configuration exists where it is absent, each entry point must answer with a
// refusal instead of a panic (the panic would be a 500 recovered by middleware,
// not a crash, but "port forwarding unavailable" is the honest answer).
func requireProxyRegistry(w http.ResponseWriter, r *http.Request) bool {
	if service.ProxyService == nil {
		writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "PortForwardUnavailable")
		return false
	}
	return true
}

// ServeProxyPortAction handles GET (list), POST (register), PUT (update) and DELETE (unregister)
// for proxy ports. DELETE uses query parameter: /api/proxy/ports?port=5173
func ServeProxyPortAction(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ServeProxyPorts(w, r)
	case http.MethodPost:
		registerPort(w, r)
	case http.MethodPut:
		updatePort(w, r)
	case http.MethodDelete:
		unregisterPortByQuery(w, r)
	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

func registerPort(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Port      int    `json:"port"`
		Host      string `json:"host"`
		Name      string `json:"name"`
		Protocol  string `json:"protocol"`
		Direction string `json:"direction"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	if req.Port <= 0 || req.Port > 65535 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidPortNumber", map[string]any{"Port": req.Port})
		return
	}

	if !requireProxyRegistry(w, r) {
		return
	}

	localPort, err := service.ProxyService.RegisterPort(req.Port, req.Host, req.Name, req.Protocol, req.Direction)
	if err != nil {
		writeLocalizedError(w, r, model.Forbidden(err, "AccessDenied"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{jsonKeyStatus: "ok", "localPort": localPort})
}

func updatePort(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LocalPort int    `json:"localPort"`
		Port      int    `json:"port"`
		Host      string `json:"host"`
		Name      string `json:"name"`
		Protocol  string `json:"protocol"`
		Direction string `json:"direction"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	if req.LocalPort <= 0 || req.LocalPort > 65535 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidPortNumber", map[string]any{"Port": req.LocalPort})
		return
	}

	if !requireProxyRegistry(w, r) {
		return
	}

	if err := service.ProxyService.UpdatePort(req.LocalPort, req.Port, req.Host, req.Name, req.Protocol, req.Direction); err != nil {
		writeLocalizedError(w, r, model.Forbidden(err, "AccessDenied"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{jsonKeyStatus: "ok"})
}

func unregisterPortByQuery(w http.ResponseWriter, r *http.Request) {
	portStr := r.URL.Query().Get("port")
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidPortInQuery")
		return
	}

	if !requireProxyRegistry(w, r) {
		return
	}

	if err := service.ProxyService.UnregisterPort(port); err != nil {
		writeLocalizedError(w, r, model.NotFound(err, "FileNotFoundShort"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{jsonKeyStatus: "ok"})
}

// ServeProxySetPortEnabled toggles a forwarded port's user-controlled enabled state.
// Body: {"localPort": 8080, "enabled": false}
func ServeProxySetPortEnabled(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPut) {
		return
	}
	var req struct {
		LocalPort int  `json:"localPort"`
		Enabled   bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.LocalPort <= 0 || req.LocalPort > 65535 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidPortNumber", map[string]any{"Port": req.LocalPort})
		return
	}
	if !requireProxyRegistry(w, r) {
		return
	}
	if err := service.ProxyService.SetPortEnabled(req.LocalPort, req.Enabled); err != nil {
		writeLocalizedError(w, r, model.NotFound(err, "FileNotFoundShort"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{jsonKeyStatus: "ok"})
}

// ServeProxyDetect returns auto-detected listening ports on the server.
func ServeProxyDetect(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if !requireProxyRegistry(w, r) {
		return
	}
	ports := service.ProxyService.DetectListeningPorts()
	writeJSON(w, http.StatusOK, map[string]any{"ports": ports})
}
