package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Every WebSocket endpoint shares this allow-list, so a mistake here either
// breaks the dev server (the proxy talks to localhost) or opens the sockets to
// any third-party page. Both directions are pinned.

func TestWSOriginPatterns_AdmitsOwnHostAndLoopback(t *testing.T) {
	req := httptest.NewRequest("GET", "http://192.168.1.7:20000/api/ws", http.NoBody)
	req.Host = "192.168.1.7:20000"

	patterns := wsOriginPatterns(req)

	assert.Contains(t, patterns, "http://192.168.1.7:20000")
	assert.Contains(t, patterns, "https://192.168.1.7:20000")
	// The dev server proxies through localhost, and the Android/desktop shells
	// may dial 127.0.0.1 on any port — hence the wildcard port.
	assert.Contains(t, patterns, "http://localhost:*")
	assert.Contains(t, patterns, "http://127.0.0.1:*")
}

// A cross-origin page must not be admitted. The list is host-derived, so the
// only way an attacker's origin appears is if it equals r.Host — which the
// browser's same-origin rules already prevent it from claiming.
func TestWSOriginPatterns_DoesNotAdmitArbitraryOrigin(t *testing.T) {
	req := httptest.NewRequest("GET", "http://192.168.1.7:20000/api/ws", http.NoBody)
	req.Host = "192.168.1.7:20000"

	for _, p := range wsOriginPatterns(req) {
		assert.NotContains(t, p, "evil.example.com")
		assert.NotEqual(t, "*", p, "a bare wildcard would defeat the check")
	}
}

// The pattern must follow the Host header rather than a hard-coded port: the
// server is reachable on several ports and behind port-forwards.
func TestWSOriginPatterns_FollowsHostHeader(t *testing.T) {
	req := httptest.NewRequest("GET", "http://example.test:8080/api/ws", http.NoBody)
	req.Host = "example.test:8080"

	patterns := wsOriginPatterns(req)

	assert.Contains(t, patterns, "http://example.test:8080")
	assert.NotContains(t, patterns, "http://example.test:20000")
}
