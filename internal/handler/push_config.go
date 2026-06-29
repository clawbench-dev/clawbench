package handler

import (
	"net/http"
	"sync"

	"clawbench/internal/push"
)

// pushClientRef holds a reference to the JPush client, set from main.go.
// Access is protected by pushClientMu for hot-reload safety.
var (
	pushClientRef *push.JPushClient
	pushClientMu  sync.RWMutex
)

// SetPushClient stores a reference to the JPush client for handler access.
// Goroutine-safe: concurrent reads are protected by RWMutex.
func SetPushClient(c *push.JPushClient) {
	pushClientMu.Lock()
	pushClientRef = c
	pushClientMu.Unlock()
}

// GetPushClient returns the current JPush client (may be nil if not configured).
// Goroutine-safe for concurrent reads.
func GetPushClient() *push.JPushClient {
	pushClientMu.RLock()
	c := pushClientRef
	pushClientMu.RUnlock()
	return c
}

// ServePushConfig returns JPush configuration for the Android app.
// GET /api/push/config
//
// Unauthenticated: the Android native layer calls this before WebView loads
// (no cookies available) to discover the JPush AppKey at runtime.
// Only exposes: jpush_enabled (bool) and jpush_app_key (string) — no secrets.
func ServePushConfig(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	result := map[string]any{
		"jpush_enabled": false,
		"jpush_app_key": "",
	}

	client := GetPushClient()
	if client != nil {
		appKey := client.AppKey()
		if client.Enabled() && appKey != "" {
			result["jpush_enabled"] = true
			result["jpush_app_key"] = appKey
		}
	}

	writeJSON(w, http.StatusOK, result)
}
