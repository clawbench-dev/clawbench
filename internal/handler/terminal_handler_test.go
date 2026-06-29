package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/terminal"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decodeRespJSON is already defined in testutil_test.go but we need json for QuickCommand
var _ = json.Marshal // ensure json is used

func TestTerminalConfigRouteRequiresAuth(t *testing.T) {
	origToken := model.SessionToken
	origMgr := GetTerminalManager()
	t.Cleanup(func() {
		model.SessionToken = origToken
		SetTerminalManager(origMgr)
	})

	model.SessionToken = "test-token"
	SetTerminalManager(nil)

	mux := http.NewServeMux()
	RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/terminal/config", http.NoBody)
	req.RemoteAddr = "203.0.113.10:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected terminal config to require auth, got status %d body %s", w.Code, w.Body.String())
	}
}

func TestTerminalWebSocketRejectsInvalidCwdBeforeUpgrade(t *testing.T) {
	origMgr := GetTerminalManager()
	t.Cleanup(func() {
		curMgr := GetTerminalManager()
		if curMgr != nil && curMgr != origMgr {
			curMgr.Close()
		}
		SetTerminalManager(origMgr)
	})

	projectDir := t.TempDir()
	SetTerminalManager(terminal.NewManager(model.TerminalConfig{
		Enabled:      true,
		IdleTimeout:  "1m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}, 20000))

	req := httptest.NewRequest(http.MethodGet, "/api/terminal/ws?cwd=../../etc", http.NoBody)
	withProjectCookie(req, projectDir)
	w := callHandler(TerminalWebSocket, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected invalid cwd to be rejected before websocket upgrade, got status %d body %s", w.Code, w.Body.String())
	}
}

// ---------- TerminalConfigHandler ----------

func TestTerminalConfig_NilManager(t *testing.T) {
	origMgr := GetTerminalManager()
	t.Cleanup(func() { SetTerminalManager(origMgr) })
	SetTerminalManager(nil)

	req := newRequest(t, http.MethodGet, "/api/terminal/config", nil)
	w := callHandler(TerminalConfigHandler, req)
	assertOK(t, w)

	var result map[string]any
	decodeRespJSON(t, w.Body, &result)
	assert.Equal(t, false, result["enabled"])
}

func TestTerminalConfig_EnabledManager(t *testing.T) {
	origMgr := GetTerminalManager()
	t.Cleanup(func() {
		curMgr := GetTerminalManager()
		if curMgr != nil && curMgr != origMgr {
			curMgr.Close()
		}
		SetTerminalManager(origMgr)
	})

	SetTerminalManager(terminal.NewManager(model.TerminalConfig{
		Enabled:      true,
		IdleTimeout:  "1m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}, 20000))

	req := newRequest(t, http.MethodGet, "/api/terminal/config", nil)
	w := callHandler(TerminalConfigHandler, req)
	assertOK(t, w)

	var result map[string]any
	decodeRespJSON(t, w.Body, &result)
	assert.Equal(t, true, result["enabled"])
}

// ---------- TerminalStatus ----------

func TestTerminalStatus_NilManager(t *testing.T) {
	origMgr := GetTerminalManager()
	t.Cleanup(func() { SetTerminalManager(origMgr) })
	SetTerminalManager(nil)

	req := newRequest(t, http.MethodGet, "/api/terminal/status", nil)
	w := callHandler(TerminalStatus, req)
	assertOK(t, w)

	var result map[string]any
	decodeRespJSON(t, w.Body, &result)
	assert.Equal(t, false, result["enabled"])
}

func TestTerminalStatus_AllSessions(t *testing.T) {
	origMgr := GetTerminalManager()
	t.Cleanup(func() {
		curMgr := GetTerminalManager()
		if curMgr != nil && curMgr != origMgr {
			curMgr.Close()
		}
		SetTerminalManager(origMgr)
	})

	SetTerminalManager(terminal.NewManager(model.TerminalConfig{
		Enabled:      true,
		IdleTimeout:  "1m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}, 20000))

	req := newRequest(t, http.MethodGet, "/api/terminal/status", nil)
	w := callHandler(TerminalStatus, req)
	assertOK(t, w)

	var result map[string]any
	decodeRespJSON(t, w.Body, &result)
	assert.Equal(t, true, result["enabled"])
	// No active sessions — AllSessionStatus returns nil slice which marshals to null
	_, ok := result["sessions"]
	assert.True(t, ok, "sessions field should be present")
	// session_count should be present in the response
	_, ok = result["session_count"]
	assert.True(t, ok, "session_count field should be present")
}

// ---------- TerminalClose ----------

func TestTerminalClose_NilManager(t *testing.T) {
	origMgr := GetTerminalManager()
	t.Cleanup(func() { SetTerminalManager(origMgr) })
	SetTerminalManager(nil)

	req := newRequest(t, http.MethodPost, "/api/terminal/close", nil)
	w := callHandler(TerminalClose, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestTerminalClose_DisabledManager(t *testing.T) {
	origMgr := GetTerminalManager()
	t.Cleanup(func() {
		curMgr := GetTerminalManager()
		if curMgr != nil && curMgr != origMgr {
			curMgr.Close()
		}
		SetTerminalManager(origMgr)
	})

	SetTerminalManager(terminal.NewManager(model.TerminalConfig{
		Enabled: false,
	}, 20000))

	req := newRequest(t, http.MethodPost, "/api/terminal/close", nil)
	w := callHandler(TerminalClose, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestTerminalClose_AllSessions(t *testing.T) {
	origMgr := GetTerminalManager()
	t.Cleanup(func() {
		curMgr := GetTerminalManager()
		if curMgr != nil && curMgr != origMgr {
			curMgr.Close()
		}
		SetTerminalManager(origMgr)
	})

	SetTerminalManager(terminal.NewManager(model.TerminalConfig{
		Enabled:      true,
		IdleTimeout:  "1m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}, 20000))

	req := newRequest(t, http.MethodPost, "/api/terminal/close", nil)
	w := callHandler(TerminalClose, req)
	assertOK(t, w)

	var result map[string]any
	decodeRespJSON(t, w.Body, &result)
	assert.Equal(t, true, result["success"])
}

// ---------- ServeQuickCommands ----------

func TestServeQuickCommands_ListEmpty(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/terminal/quick-commands", nil)
	w := callHandler(ServeQuickCommands, req)
	assertOK(t, w)

	var items []service.QuickCommand
	decodeRespJSON(t, w.Body, &items)
	assert.NotNil(t, items, "should return empty array, not nil")
	assert.Empty(t, items)
}

func TestServeQuickCommands_Create(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	body := map[string]any{
		"label":        "Build",
		"command":      "go build ./...",
		"hidden":       false,
		"auto_execute": false,
	}
	req := newRequest(t, http.MethodPost, "/api/terminal/quick-commands", body)
	w := callHandler(ServeQuickCommands, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	var result map[string]any
	decodeRespJSON(t, w.Body, &result)
	assert.Equal(t, "Build", result["label"])
	assert.Equal(t, "go build ./...", result["command"])
}

func TestServeQuickCommands_CreateValidation(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	tests := []struct {
		name string
		body map[string]any
	}{
		{"empty label", map[string]any{"label": "", "command": "cmd"}},
		{"empty command", map[string]any{"label": "Test", "command": ""}},
		{"label too long", map[string]any{"label": string(make([]byte, 101)), "command": "cmd"}},
		{"command too long", map[string]any{"label": "Test", "command": string(make([]byte, 4097))}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := newRequest(t, http.MethodPost, "/api/terminal/quick-commands", tt.body)
			w := callHandler(ServeQuickCommands, req)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}

func TestServeQuickCommands_ListWithItems(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	_, err := service.AddQuickCommand("Build", "go build ./...", false, false)
	require.NoError(t, err)
	_, err = service.AddQuickCommand("Test", "go test ./...", false, true)
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/terminal/quick-commands", nil)
	w := callHandler(ServeQuickCommands, req)
	assertOK(t, w)

	var items []service.QuickCommand
	decodeRespJSON(t, w.Body, &items)
	assert.Len(t, items, 2)
}

func TestServeQuickCommands_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/terminal/quick-commands", nil)
	w := callHandler(ServeQuickCommands, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// ---------- ServeQuickCommandByID ----------

func TestServeQuickCommandByID_Update(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	id, err := service.AddQuickCommand("Old", "old cmd", false, false)
	require.NoError(t, err)

	body := map[string]any{
		"label":        "New",
		"command":      "new cmd",
		"hidden":       true,
		"auto_execute": true,
	}
	req := newRequest(t, http.MethodPut, "/api/terminal/quick-commands/"+fmt.Sprint(id), body)
	w := callHandler(ServeQuickCommandByID, req)
	assertOK(t, w)
}

func TestServeQuickCommandByID_Delete(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	id, err := service.AddQuickCommand("Delete", "rm -rf /", false, false)
	require.NoError(t, err)

	req := newRequest(t, http.MethodDelete, "/api/terminal/quick-commands/"+fmt.Sprint(id), nil)
	w := callHandler(ServeQuickCommandByID, req)
	assertOK(t, w)
}

func TestServeQuickCommandByID_InvalidID(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPut, "/api/terminal/quick-commands/notanumber", map[string]any{"label": "X", "command": "Y"})
	w := callHandler(ServeQuickCommandByID, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeQuickCommandByID_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/terminal/quick-commands/1", nil)
	w := callHandler(ServeQuickCommandByID, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// ---------- GetTerminalManager / SetTerminalManager concurrent access ----------

func TestGetTerminalManager_ConcurrentAccess(t *testing.T) {
	origMgr := GetTerminalManager()
	t.Cleanup(func() { SetTerminalManager(origMgr) })

	// Create a test manager
	mgr := terminal.NewManager(model.TerminalConfig{
		Enabled:      true,
		IdleTimeout:  "1m",
		BufferLines:  100,
		MaxLineBytes: 65536,
		MaxBufferMB:  4,
	}, 20000)
	t.Cleanup(func() { mgr.Close() })

	done := make(chan struct{})

	// Concurrent setter goroutine
	go func() {
		defer func() { done <- struct{}{} }()
		for range 100 {
			SetTerminalManager(mgr)
		}
	}()

	// Concurrent reader goroutine
	go func() {
		defer func() { done <- struct{}{} }()
		for range 100 {
			_ = GetTerminalManager()
		}
	}()

	// Wait for both goroutines to finish (no data race = test passes under -race)
	<-done
	<-done

	// Verify we can still get a valid manager after concurrent access
	result := GetTerminalManager()
	assert.NotNil(t, result)
}
