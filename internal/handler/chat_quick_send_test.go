package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// ---------- GET /api/chat/quick-send ----------

func TestServeChatQuickSend_ListEmpty(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/chat/quick-send", nil)
	w := callHandler(ServeChatQuickSend, req)

	assertOK(t, w)

	var items []service.ChatQuickSendItem
	decodeRespJSON(t, w.Body, &items)
	if items == nil {
		t.Error("expected empty array, got nil")
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

func TestServeChatQuickSend_ListWithItems(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	_, _ = service.AddChatQuickSend("继续", "继续", "")
	_, _ = service.AddChatQuickSend("提交", "提交", "")

	req := newRequest(t, http.MethodGet, "/api/chat/quick-send", nil)
	w := callHandler(ServeChatQuickSend, req)

	assertOK(t, w)

	var items []service.ChatQuickSendItem
	decodeRespJSON(t, w.Body, &items)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].Label != "继续" {
		t.Errorf("expected first item label '继续', got %q", items[0].Label)
	}
}

// ---------- POST /api/chat/quick-send ----------

func TestServeChatQuickSend_Create(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	body := map[string]any{"label": "继续", "command": "继续"}
	req := newRequest(t, http.MethodPost, "/api/chat/quick-send", body)
	w := callHandler(ServeChatQuickSend, req)

	assertStatus(t, w, http.StatusCreated)

	var result map[string]any
	decodeRespJSON(t, w.Body, &result)
	if result["id"] == nil {
		t.Error("expected id in response")
	}
	if result["label"] != "继续" {
		t.Errorf("expected label '继续', got %v", result["label"])
	}
}

func TestServeChatQuickSend_CreateEmptyLabel(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	body := map[string]any{"label": "", "command": "继续"}
	req := newRequest(t, http.MethodPost, "/api/chat/quick-send", body)
	w := callHandler(ServeChatQuickSend, req)

	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeChatQuickSend_CreateEmptyCommand(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	body := map[string]any{"label": "继续", "command": "   "}
	req := newRequest(t, http.MethodPost, "/api/chat/quick-send", body)
	w := callHandler(ServeChatQuickSend, req)

	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeChatQuickSend_CreateLabelTooLong(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	longLabel := ""
	for range 101 {
		longLabel += "x"
	}
	body := map[string]any{"label": longLabel, "command": "test"}
	req := newRequest(t, http.MethodPost, "/api/chat/quick-send", body)
	w := callHandler(ServeChatQuickSend, req)

	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeChatQuickSend_CreateCommandTooLong(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	longCommand := ""
	for range 4097 {
		longCommand += "x"
	}
	body := map[string]any{"label": "test", "command": longCommand}
	req := newRequest(t, http.MethodPost, "/api/chat/quick-send", body)
	w := callHandler(ServeChatQuickSend, req)

	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeChatQuickSend_CreateWhitespaceTrimmed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	body := map[string]any{"label": "  继续  ", "command": "  继续  "}
	req := newRequest(t, http.MethodPost, "/api/chat/quick-send", body)
	w := callHandler(ServeChatQuickSend, req)

	assertStatus(t, w, http.StatusCreated)

	var result map[string]any
	decodeRespJSON(t, w.Body, &result)
	if result["label"] != "继续" {
		t.Errorf("expected trimmed label '继续', got %v", result["label"])
	}
	if result["command"] != "继续" {
		t.Errorf("expected trimmed command '继续', got %v", result["command"])
	}
}

// ---------- PUT /api/chat/quick-send/reorder ----------

func TestServeChatQuickSend_Reorder(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	_, _ = service.AddChatQuickSend("A", "a", "") // id=1, sort=0
	_, _ = service.AddChatQuickSend("B", "b", "") // id=2, sort=1

	body := map[string]any{"ids": []int64{2, 1}}
	req := newRequest(t, http.MethodPut, "/api/chat/quick-send/reorder", body)
	w := callHandler(ServeChatQuickSend, req)

	assertOK(t, w)

	var result map[string]any
	decodeRespJSON(t, w.Body, &result)
	if result["success"] != true {
		t.Error("expected success:true")
	}

	items, _ := service.GetChatQuickSend("")
	if len(items) != 2 || items[0].Label != "B" {
		t.Errorf("expected B first after reorder, got %v", items[0].Label)
	}
}

func TestServeChatQuickSend_ReorderEmpty(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	body := map[string]any{"ids": []int64{}}
	req := newRequest(t, http.MethodPut, "/api/chat/quick-send/reorder", body)
	w := callHandler(ServeChatQuickSend, req)

	assertOK(t, w)
}

// ---------- PUT /api/chat/quick-send/{id} ----------

func TestServeChatQuickSendByID_Update(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	_, _ = service.AddChatQuickSend("继续", "继续", "")

	body := map[string]any{"label": "▶️ 继续", "command": "请继续"}
	req := newRequest(t, http.MethodPut, "/api/chat/quick-send/1", body)
	w := callHandler(ServeChatQuickSendByID, req)

	assertOK(t, w)

	items, _ := service.GetChatQuickSend("")
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Label != "▶️ 继续" {
		t.Errorf("expected updated label, got %q", items[0].Label)
	}
	if items[0].Command != "请继续" {
		t.Errorf("expected updated command, got %q", items[0].Command)
	}
}

func TestServeChatQuickSendByID_UpdateEmptyLabel(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	_, _ = service.AddChatQuickSend("继续", "继续", "")

	body := map[string]any{"label": "", "command": "test"}
	req := newRequest(t, http.MethodPut, "/api/chat/quick-send/1", body)
	w := callHandler(ServeChatQuickSendByID, req)

	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeChatQuickSendByID_UpdateInvalidID(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	body := map[string]any{"label": "test", "command": "test"}
	req := newRequest(t, http.MethodPut, "/api/chat/quick-send/abc", body)
	w := callHandler(ServeChatQuickSendByID, req)

	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeChatQuickSendByID_UpdateProjectOnlyWithoutCookie(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	_, _ = service.AddChatQuickSend("继续", "继续", "")

	body := map[string]any{"label": "项目", "command": "cmd", "project_only": true}
	req := newRequest(t, http.MethodPut, "/api/chat/quick-send/1", body)
	w := callHandler(ServeChatQuickSendByID, req)

	assertStatus(t, w, http.StatusForbidden)
}

func TestServeChatQuickSendByID_UpdateProjectOnlyWithCookie(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	id, _ := service.AddChatQuickSend("继续", "继续", "")

	body := map[string]any{"label": "项目", "command": "cmd", "project_only": true}
	req := withProjectCookie(newRequest(t, http.MethodPut, "/api/chat/quick-send/"+fmt.Sprint(id), body), "/proj/z")
	w := callHandler(ServeChatQuickSendByID, req)

	assertStatus(t, w, http.StatusOK)

	// Item moved out of global scope into /proj/z.
	global, _ := service.GetChatQuickSend("")
	if len(global) != 0 {
		t.Errorf("expected item to leave global scope, got %+v", global)
	}
	proj, _ := service.GetChatQuickSend("/proj/z")
	if len(proj) != 1 || !proj[0].ProjectOnly {
		t.Errorf("expected one project-scoped item, got %+v", proj)
	}
}

// ---------- DELETE /api/chat/quick-send/{id} ----------

func TestServeChatQuickSendByID_Delete(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	_, _ = service.AddChatQuickSend("继续", "继续", "")
	_, _ = service.AddChatQuickSend("提交", "提交", "")

	req := newRequest(t, http.MethodDelete, "/api/chat/quick-send/1", nil)
	w := callHandler(ServeChatQuickSendByID, req)

	assertOK(t, w)

	items, _ := service.GetChatQuickSend("")
	if len(items) != 1 {
		t.Fatalf("expected 1 item after delete, got %d", len(items))
	}
	if items[0].Label != "提交" {
		t.Errorf("expected remaining item '提交', got %q", items[0].Label)
	}
}

func TestServeChatQuickSendByID_DeleteInvalidID(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/chat/quick-send/notanumber", nil)
	w := callHandler(ServeChatQuickSendByID, req)

	assertStatus(t, w, http.StatusBadRequest)
}

// ---------- Method not allowed ----------

func TestServeChatQuickSend_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPatch, "/api/chat/quick-send", nil)
	w := callHandler(ServeChatQuickSend, req)

	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestServeChatQuickSendByID_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/chat/quick-send/1", nil)
	w := callHandler(ServeChatQuickSendByID, req)

	assertStatus(t, w, http.StatusMethodNotAllowed)
}

// ---------- Auth required ----------

func TestChatQuickSendRouteRequiresAuth(t *testing.T) {
	origToken := model.SessionToken
	t.Cleanup(func() { model.SessionToken = origToken })

	model.SessionToken = "test-token"

	mux := http.NewServeMux()
	RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/chat/quick-send", http.NoBody)
	req.RemoteAddr = "203.0.113.10:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected chat quick-send to require auth, got status %d body %s", w.Code, w.Body.String())
	}
}

// ---------- Reorder sub-path routing ----------

func TestServeChatQuickSendByID_ReorderForwardedToServeChatQuickSend(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	_, _ = service.AddChatQuickSend("A", "a", "")
	_, _ = service.AddChatQuickSend("B", "b", "")

	body := map[string]any{"ids": []int64{2, 1}}
	req := newRequest(t, http.MethodPut, "/api/chat/quick-send/reorder", body)
	w := callHandler(ServeChatQuickSendByID, req)

	assertOK(t, w)

	var result map[string]any
	decodeRespJSON(t, w.Body, &result)
	if result["success"] != true {
		t.Error("expected reorder to succeed via forwarded handler")
	}
}

// ---------- JSON null handling ----------

func TestServeChatQuickSend_ListNullToEmptyArray(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/chat/quick-send", nil)
	w := callHandler(ServeChatQuickSend, req)

	assertOK(t, w)

	var result []any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON array, got parse error: %v; body: %s", err, w.Body.String())
	}
}

// ---------- Project scoping ----------

func TestServeChatQuickSend_ListFiltersByProjectCookie(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	_, _ = service.AddChatQuickSend("全局", "g", "")
	_, _ = service.AddChatQuickSend("项目A", "a", "/proj/a")
	_, _ = service.AddChatQuickSend("项目B", "b", "/proj/b")

	// No project cookie → only global items
	req := newRequest(t, http.MethodGet, "/api/chat/quick-send", nil)
	w := callHandler(ServeChatQuickSend, req)
	assertOK(t, w)
	var globalItems []service.ChatQuickSendItem
	decodeRespJSON(t, w.Body, &globalItems)
	if len(globalItems) != 1 || globalItems[0].Label != "全局" {
		t.Errorf("expected only global item, got %+v", globalItems)
	}

	// Project A cookie → global + A items
	req = withProjectCookie(newRequest(t, http.MethodGet, "/api/chat/quick-send", nil), "/proj/a")
	w = callHandler(ServeChatQuickSend, req)
	assertOK(t, w)
	var projAItems []service.ChatQuickSendItem
	decodeRespJSON(t, w.Body, &projAItems)
	if len(projAItems) != 2 {
		t.Errorf("expected 2 items for project A, got %d", len(projAItems))
	}
}

func TestServeChatQuickSend_CreateProjectOnlyWithoutCookie(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	body := map[string]any{"label": "项目", "command": "cmd", "project_only": true}
	req := newRequest(t, http.MethodPost, "/api/chat/quick-send", body)
	w := callHandler(ServeChatQuickSend, req)

	assertStatus(t, w, http.StatusForbidden)
}

func TestServeChatQuickSend_CreateProjectOnlyWithCookie(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	body := map[string]any{"label": "项目", "command": "cmd", "project_only": true}
	req := withProjectCookie(newRequest(t, http.MethodPost, "/api/chat/quick-send", body), "/proj/x")
	w := callHandler(ServeChatQuickSend, req)

	assertStatus(t, w, http.StatusCreated)

	// Global list must not contain it; project list must.
	items, _ := service.GetChatQuickSend("")
	if len(items) != 0 {
		t.Errorf("expected no global items, got %+v", items)
	}
	projItems, _ := service.GetChatQuickSend("/proj/x")
	if len(projItems) != 1 || !projItems[0].ProjectOnly {
		t.Errorf("expected one project-scoped item, got %+v", projItems)
	}
}

func TestServeChatQuickSend_CreateGlobalByDefault(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	body := map[string]any{"label": "全局", "command": "cmd"}
	req := withProjectCookie(newRequest(t, http.MethodPost, "/api/chat/quick-send", body), "/proj/x")
	w := callHandler(ServeChatQuickSend, req)

	assertStatus(t, w, http.StatusCreated)

	items, _ := service.GetChatQuickSend("")
	if len(items) != 1 || items[0].ProjectOnly {
		t.Errorf("expected one global item, got %+v", items)
	}
}
