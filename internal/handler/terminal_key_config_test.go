package handler

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawbench/internal/service"

	_ "modernc.org/sqlite"
)

func TestServeKeyConfig_GetEmpty(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/terminal/key-config?type=key", nil)
	w := callHandler(ServeKeyConfig, req)
	assertStatus(t, w, http.StatusOK)

	var items []service.KeyConfigItem
	decodeRespJSON(t, w.Body, &items)
	if len(items) != 0 {
		t.Fatalf("expected empty, got %d items", len(items))
	}
}

func TestServeKeyConfig_PutAndGet(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Put key config
	putBody := map[string]any{
		"type":  "key",
		"items": []string{"esc", "tab", "ctrl"},
	}
	req := newRequest(t, http.MethodPut, "/api/terminal/key-config", putBody)
	w := callHandler(ServeKeyConfig, req)
	assertStatus(t, w, http.StatusOK)

	// Get key config
	req = newRequest(t, http.MethodGet, "/api/terminal/key-config?type=key", nil)
	w = callHandler(ServeKeyConfig, req)
	assertStatus(t, w, http.StatusOK)

	var items []service.KeyConfigItem
	decodeRespJSON(t, w.Body, &items)
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if items[0].KeyID != "esc" || items[1].KeyID != "tab" || items[2].KeyID != "ctrl" {
		t.Fatalf("unexpected order: %+v", items)
	}
}

func TestServeKeyConfig_InvalidType(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/terminal/key-config?type=invalid", nil)
	w := callHandler(ServeKeyConfig, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeKeyConfig_Replace(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Initial config
	putBody := map[string]any{
		"type":  "symbol",
		"items": []string{".", "/", "-"},
	}
	req := newRequest(t, http.MethodPut, "/api/terminal/key-config", putBody)
	w := callHandler(ServeKeyConfig, req)
	assertStatus(t, w, http.StatusOK)

	// Replace with different config
	putBody = map[string]any{
		"type":  "symbol",
		"items": []string{"$", "&"},
	}
	req = newRequest(t, http.MethodPut, "/api/terminal/key-config", putBody)
	w = callHandler(ServeKeyConfig, req)
	assertStatus(t, w, http.StatusOK)

	// Get should show only new config
	req = newRequest(t, http.MethodGet, "/api/terminal/key-config?type=symbol", nil)
	w = callHandler(ServeKeyConfig, req)
	assertStatus(t, w, http.StatusOK)

	var items []service.KeyConfigItem
	decodeRespJSON(t, w.Body, &items)
	if len(items) != 2 {
		t.Fatalf("expected 2 items after replace, got %d", len(items))
	}
	if items[0].KeyID != "$" || items[1].KeyID != "&" {
		t.Fatalf("unexpected items after replace: %+v", items)
	}
}

func TestServeKeyConfig_PutInvalidType(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	putBody := map[string]any{
		"type":  "invalid",
		"items": []string{"esc"},
	}
	req := newRequest(t, http.MethodPut, "/api/terminal/key-config", putBody)
	w := callHandler(ServeKeyConfig, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeKeyConfig_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/terminal/key-config", nil)
	w := callHandler(ServeKeyConfig, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)

	req = newRequest(t, http.MethodPost, "/api/terminal/key-config", nil)
	w = callHandler(ServeKeyConfig, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestServeKeyConfig_GetEmptyType(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// GET with no type parameter (empty string) should return 400
	req := newRequest(t, http.MethodGet, "/api/terminal/key-config", nil)
	w := callHandler(ServeKeyConfig, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeKeyConfig_PutInvalidJSON(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// PUT with invalid JSON body should return 400
	req := httptest.NewRequest(http.MethodPut, "/api/terminal/key-config", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	w := callHandler(ServeKeyConfig, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeKeyConfig_GetDBError(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Close the DB to force an error from GetKeyConfig
	closedDB, _ := sql.Open("sqlite", ":memory:")
	closedDB.Close()
	cleanup := service.SetDBForTest(closedDB, closedDB)
	defer cleanup()

	req := newRequest(t, http.MethodGet, "/api/terminal/key-config?type=key", nil)
	w := callHandler(ServeKeyConfig, req)
	assertStatus(t, w, http.StatusInternalServerError)
}

func TestServeKeyConfig_PutDBError(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	// Close the DB to force an error from ReplaceKeyConfig
	closedDB, _ := sql.Open("sqlite", ":memory:")
	closedDB.Close()
	cleanup := service.SetDBForTest(closedDB, closedDB)
	defer cleanup()

	putBody := map[string]any{
		"type":  "key",
		"items": []string{"esc"},
	}
	req := newRequest(t, http.MethodPut, "/api/terminal/key-config", putBody)
	w := callHandler(ServeKeyConfig, req)
	assertStatus(t, w, http.StatusInternalServerError)
}
