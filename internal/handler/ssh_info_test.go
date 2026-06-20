package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/ssh"
)

func TestServeSSHInfo_Disabled(t *testing.T) {
	// No SSH server reference set
	origSSH := sshServerRef
	sshServerRef = nil
	defer func() { sshServerRef = origSSH }()

	req := httptest.NewRequest(http.MethodGet, "/api/ssh/info", http.NoBody)
	w := httptest.NewRecorder()
	ServeSSHInfo(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if result["enabled"] != false {
		t.Errorf("expected enabled=false, got %v", result["enabled"])
	}
}

func TestServeSSHInfo_Enabled(t *testing.T) {
	// Set up a ProxyService with a registered port
	origProxy := service.ProxyService
	service.ProxyService = service.NewProxyRegistry(0)
	defer func() {
		service.ProxyService.Stop()
		service.ProxyService = origProxy
	}()
	_, _ = service.ProxyService.RegisterPort(5173, "", "Vite Dev", "http")
	_, _ = service.ProxyService.RegisterPort(8080, "", "API", "http")

	// Create and set an SSH server reference
	srv := ssh.NewServer(model.PortForwardConfig{Enabled: true, Port: 20001}, 20000, "test-password", service.ProxyService)
	if err := srv.InitHostKey(); err != nil {
		t.Fatalf("failed to init host key: %v", err)
	}
	origSSH := sshServerRef
	sshServerRef = srv
	defer func() { sshServerRef = origSSH }()

	req := httptest.NewRequest(http.MethodGet, "/api/ssh/info", http.NoBody)
	req.Host = "myserver.com:20000"
	w := httptest.NewRecorder()
	ServeSSHInfo(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	// Check basic fields
	if result["enabled"] != true {
		t.Errorf("expected enabled=true, got %v", result["enabled"])
	}
	if result["host"] != "myserver.com" {
		t.Errorf("expected host=myserver.com, got %v", result["host"])
	}
	if result["port"] != float64(20001) {
		t.Errorf("expected port=20001, got %v", result["port"])
	}
	if result["username"] != "clawbench" {
		t.Errorf("expected username=clawbench, got %v", result["username"])
	}

	// Check fingerprint is non-empty
	fingerprint, ok := result["fingerprint"].(string)
	if !ok || fingerprint == "" {
		t.Error("expected non-empty fingerprint")
	}

	// Check command contains both ports
	cmd, ok := result["command"].(string)
	if !ok {
		t.Fatalf("expected command to be string, got %v", result["command"])
	}
	if cmd == "" {
		t.Error("expected non-empty command")
	}
	// Command should contain both port forwards
	if !containsStr(cmd, "-L 5173:localhost:5173") {
		t.Errorf("command should contain -L 5173:localhost:5173, got: %s", cmd)
	}
	if !containsStr(cmd, "-L 8080:localhost:8080") {
		t.Errorf("command should contain -L 8080:localhost:8080, got: %s", cmd)
	}
	if !containsStr(cmd, "clawbench@myserver.com") {
		t.Errorf("command should contain clawbench@myserver.com, got: %s", cmd)
	}
	if !containsStr(cmd, "-p 20001") {
		t.Errorf("command should contain -p 20001, got: %s", cmd)
	}
}

func TestServeSSHInfo_NonLocalhostTarget_UsesReverseProxyRoute(t *testing.T) {
	// When a non-localhost target has a reverse proxy, the SSH -L command should
	// route through the reverse proxy (127.0.0.1:{localPort}) instead of directly
	// to the remote host, so that the Host header is rewritten correctly.
	origProxy := service.ProxyService
	service.ProxyService = service.NewProxyRegistry(0)
	defer func() {
		service.ProxyService.Stop()
		service.ProxyService = origProxy
	}()

	// Register a localhost port (direct connection)
	_, _ = service.ProxyService.RegisterPort(5173, "", "Vite Dev", "http")
	// Register a non-localhost port (routed through reverse proxy)
	localPort, _ := service.ProxyService.RegisterPort(8080, "192.168.100.1", "Remote Router", "http")

	// Check if reverse proxy started successfully — it may fail on some platforms
	// (e.g., Windows CI where the port might be in use by another service)
	ports := service.ProxyService.ListPorts()
	hasReverseProxy := false
	for _, p := range ports {
		if p.LocalPort == localPort && p.HasReverseProxy {
			hasReverseProxy = true
		}
	}
	if !hasReverseProxy {
		t.Skip("reverse proxy did not start (port likely in use), skipping reverse proxy assertions")
	}

	srv := ssh.NewServer(model.PortForwardConfig{Enabled: true, Port: 20001}, 20000, "test-password", service.ProxyService)
	if err := srv.InitHostKey(); err != nil {
		t.Fatalf("failed to init host key: %v", err)
	}
	origSSH := sshServerRef
	sshServerRef = srv
	defer func() { sshServerRef = origSSH }()

	req := httptest.NewRequest(http.MethodGet, "/api/ssh/info", http.NoBody)
	req.Host = "myserver.com:20000"
	w := httptest.NewRecorder()
	ServeSSHInfo(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	cmd, ok := result["command"].(string)
	if !ok {
		t.Fatalf("expected command to be string, got %v", result["command"])
	}

	// Localhost port should use direct connection: -L 5173:localhost:5173
	if !containsStr(cmd, "-L 5173:localhost:5173") {
		t.Errorf("command should contain -L 5173:localhost:5173 for localhost target, got: %s", cmd)
	}

	// Non-localhost port should route through reverse proxy: -L 8080:127.0.0.1:8080
	// (NOT -L 8080:192.168.100.1:8080 which would bypass the reverse proxy)
	if !containsStr(cmd, "-L 8080:127.0.0.1:8080") {
		t.Errorf("command should contain -L 8080:127.0.0.1:8080 for reverse proxy route, got: %s", cmd)
	}
	if containsStr(cmd, "192.168.100.1") {
		t.Errorf("command should NOT contain the remote host IP directly (should go through reverse proxy), got: %s", cmd)
	}
}

func TestServeSSHInfo_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/ssh/info", http.NoBody)
	w := httptest.NewRecorder()
	ServeSSHInfo(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

func TestServeSSHInfo_AutoPort(t *testing.T) {
	// Test that port 0 auto-assigns to mainPort+1
	origProxy := service.ProxyService
	service.ProxyService = service.NewProxyRegistry(0)
	defer func() {
		service.ProxyService.Stop()
		service.ProxyService = origProxy
	}()

	srv := ssh.NewServer(model.PortForwardConfig{Enabled: true, Port: 0}, 30000, "test", service.ProxyService)
	if err := srv.InitHostKey(); err != nil {
		t.Fatalf("failed to init host key: %v", err)
	}
	origSSH := sshServerRef
	sshServerRef = srv
	defer func() { sshServerRef = origSSH }()

	req := httptest.NewRequest(http.MethodGet, "/api/ssh/info", http.NoBody)
	req.Host = "server:30000"
	w := httptest.NewRecorder()
	ServeSSHInfo(w, req)

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if result["port"] != float64(30001) {
		t.Errorf("expected auto-assigned port 30001, got %v", result["port"])
	}
}

func TestServeSSHInfo_HostFromHeader(t *testing.T) {
	// Test that host is correctly extracted from various Host header formats
	origProxy := service.ProxyService
	service.ProxyService = service.NewProxyRegistry(0)
	defer func() {
		service.ProxyService.Stop()
		service.ProxyService = origProxy
	}()

	srv := ssh.NewServer(model.PortForwardConfig{Enabled: true, Port: 20001}, 20000, "test", service.ProxyService)
	origSSH := sshServerRef
	sshServerRef = srv
	defer func() { sshServerRef = origSSH }()

	tests := []struct {
		name     string
		host     string
		expected string
	}{
		{"host_with_port", "example.com:20000", "example.com"},
		{"host_without_port", "example.com", "example.com"},
		{"ip_with_port", "192.168.1.1:20000", "192.168.1.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/ssh/info", http.NoBody)
			req.Host = tt.host
			w := httptest.NewRecorder()
			ServeSSHInfo(w, req)

			var result map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatalf("failed to parse JSON: %v", err)
			}
			if result["host"] != tt.expected {
				t.Errorf("expected host=%s, got %v", tt.expected, result["host"])
			}
		})
	}
}

func TestServeSSHInfo_EmptyPortList(t *testing.T) {
	// When no ports are registered, command should be empty
	origProxy := service.ProxyService
	service.ProxyService = service.NewProxyRegistry(0)
	defer func() {
		service.ProxyService.Stop()
		service.ProxyService = origProxy
	}()

	srv := ssh.NewServer(model.PortForwardConfig{Enabled: true, Port: 20001}, 20000, "test", service.ProxyService)
	origSSH := sshServerRef
	sshServerRef = srv
	defer func() { sshServerRef = origSSH }()

	req := httptest.NewRequest(http.MethodGet, "/api/ssh/info", http.NoBody)
	req.Host = "server:20000"
	w := httptest.NewRecorder()
	ServeSSHInfo(w, req)

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if result["command"] != "" {
		t.Errorf("expected empty command when no ports registered, got: %v", result["command"])
	}
}

func TestServeSSHInfo_ConnectionStats_Disabled(t *testing.T) {
	// When SSH is disabled, connectionStats should be nil
	origSSH := sshServerRef
	sshServerRef = nil
	defer func() { sshServerRef = origSSH }()

	req := httptest.NewRequest(http.MethodGet, "/api/ssh/info", http.NoBody)
	w := httptest.NewRecorder()
	ServeSSHInfo(w, req)

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if result["connectionStats"] != nil {
		t.Errorf("expected connectionStats=nil when SSH disabled, got %v", result["connectionStats"])
	}
}

func TestServeSSHInfo_ConnectionStats_Enabled(t *testing.T) {
	// When SSH is enabled but no clients connected, connectionStats should reflect that
	origProxy := service.ProxyService
	service.ProxyService = service.NewProxyRegistry(0)
	defer func() {
		service.ProxyService.Stop()
		service.ProxyService = origProxy
	}()

	srv := ssh.NewServer(model.PortForwardConfig{Enabled: true, Port: 20001}, 20000, "test", service.ProxyService)
	if err := srv.InitHostKey(); err != nil {
		t.Fatalf("failed to init host key: %v", err)
	}
	origSSH := sshServerRef
	sshServerRef = srv
	defer func() { sshServerRef = origSSH }()

	req := httptest.NewRequest(http.MethodGet, "/api/ssh/info", http.NoBody)
	req.Host = "server:20000"
	w := httptest.NewRecorder()
	ServeSSHInfo(w, req)

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	stats, ok := result["connectionStats"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected connectionStats to be a map, got %v", result["connectionStats"])
	}
	if stats["connected"] != false {
		t.Errorf("expected connected=false, got %v", stats["connected"])
	}
	if stats["clientCount"] != float64(0) {
		t.Errorf("expected clientCount=0, got %v", stats["clientCount"])
	}
	if stats["activeChannels"] != float64(0) {
		t.Errorf("expected activeChannels=0, got %v", stats["activeChannels"])
	}
}

// helper
func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || sub == "" ||
		(s != "" && sub != "" && findSubstr(s, sub)))
}

func findSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
