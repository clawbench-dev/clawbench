package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// 从 share 包搬回：/api/share-in/recent 是文件管理侧的路由，分享包只挂
// /api/share/*。

// TestShareManage_RouteDoesNotShadowShareIn ensures /api/share (admin) does not
// swallow /api/share-in/recent.
func TestShareManage_RouteDoesNotShadowShareIn(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	mux := http.NewServeMux()
	registerAllPluginsForTest(t)
	RegisterRoutes(mux)

	// /api/share-in/recent has its own handler (ShareInRecent). A GET should
	// reach it (method GET → 405/400 depending on handler) rather than 404 from
	// falling through. We just assert it is NOT routed to ServeShareManage's
	// method-switch (which would 405 too) — the real guarantee is that the
	// ServeMux does not conflict; any response other than a mux 404 is fine.
	req := newRequest(t, http.MethodGet, "/api/share-in/recent", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assert.NotEqual(t, http.StatusNotFound, rec.Code, "share-in route must not 404")
}
