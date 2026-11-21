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

// /share-target 是系统分享面板的入口：它不走鉴权中间件（未登录时要重定向而
// 不是 JSON 401），但**必须**在路由表里登记为 authenticated——它写文件，
// 而且 handler 自己会重新校验登录态。登记成 public 就等于放出一个免登录的
// 写入口。
func TestShareTargetRoute_RegisteredAuthenticatedButUngated(t *testing.T) {
	mux := http.NewServeMux()
	registerAllPluginsForTest(t)
	RegisterRoutes(mux)

	var found *Route
	for i := range routeTable {
		if routeTable[i].Pattern == "/share-target" {
			found = &routeTable[i]
			break
		}
	}
	require.NotNil(t, found, "/share-target must be registered")
	assert.True(t, found.Authenticated,
		"/share-target bypasses middleware.Auth, so its authenticated flag is the only thing "+
			"telling the spec (and readers) that it is not a public endpoint")
}
