package handler

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 从 share 包搬回：它检查的是 handler 的 routeTable（路由表是 handler 的
// 全局状态），分享包看不到。

// /api/share/session must be matched as an exact route ahead of the public
// /api/share/ subtree, and must stay auth-protected.
func TestSessionShareRoute_RegisteredAuthenticated(t *testing.T) {
	// routeTable is only populated by RegisterRoutes, so build a fresh mux first.
	mux := http.NewServeMux()
	registerAllPluginsForTest(t)
	RegisterRoutes(mux)

	var found *Route
	for i := range routeTable {
		if routeTable[i].Pattern == "/api/share/session" {
			found = &routeTable[i]
			break
		}
	}
	require.NotNil(t, found, "/api/share/session must be registered")
	assert.True(t, found.Authenticated, "management endpoints must require auth")

	var publicFound *Route
	for i := range routeTable {
		if routeTable[i].Pattern == "/api/share/" {
			publicFound = &routeTable[i]
			break
		}
	}
	require.NotNil(t, publicFound)
	assert.False(t, publicFound.Authenticated, "the token subtree stays public")
}
