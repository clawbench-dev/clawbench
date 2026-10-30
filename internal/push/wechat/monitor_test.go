package wechat

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/push/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMonitor_ExpiredStopsPromptly is the guard on the -14 handling.
//
// The iLink SDK's Monitor sleeps for a full hour after a -14 before retrying,
// which would leave a silently dead bot with no visible cause. The manager
// cancels its own context instead, so the loop exits immediately, the expired
// state becomes visible, and Push* stops attempting sends with a dead token.
func TestMonitor_ExpiredStopsPromptly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ret": 0, "errcode": -14, "errmsg": "session expired"})
	}))
	defer srv.Close()

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{}

	mgr := NewManager(&model.WeChatConfig{BotToken: "tok", BaseURL: srv.URL})
	SetManager(mgr)
	defer SetManager(nil)

	start := time.Now()
	require.NoError(t, mgr.Start())
	require.True(t, mgr.isStarted(), "Start must mark the manager running")

	// The whole point: this must finish in seconds, not after the SDK's 1-hour
	// sleep. A 30-second budget is generous and still fails loudly if the
	// cancellation is ever removed.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if !mgr.isStarted() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	elapsed := time.Since(start)
	require.False(t, mgr.isStarted(), "the monitor must retire after -14 (took %s)", elapsed)
	assert.Less(t, elapsed, 10*time.Second, "the -14 path must not wait for the SDK's 1-hour sleep")

	st := mgr.Status()
	assert.Equal(t, "expired", st.State, "the UI must be able to tell the user to re-scan")
	assert.Contains(t, st.LastError, "-14")
}

// TestMonitor_PersistsSyncBuf verifies the cursor is written when the server
// hands one back, so a restart resumes instead of replaying the backlog.
func TestMonitor_PersistsSyncBuf(t *testing.T) {
	dir := t.TempDir()
	origDataDir := model.DataDir
	model.DataDir = dir
	defer func() { model.DataDir = origDataDir }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ret":             0,
			"get_updates_buf": "cursor-42",
			"msgs":            []any{},
		})
	}))
	defer srv.Close()

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{}

	mgr := NewManager(&model.WeChatConfig{BotToken: "tok", BaseURL: srv.URL})
	SetManager(mgr)
	defer SetManager(nil)

	require.NoError(t, mgr.Start())

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if loadSyncBuf("") == "cursor-42" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	assert.Equal(t, "cursor-42", loadSyncBuf(""))
	mgr.Stop()
}

// TestMonitor_DispatchesInbound proves the handler is wired to the loop.
func TestMonitor_DispatchesInbound(t *testing.T) {
	dir := t.TempDir()
	origDataDir := model.DataDir
	model.DataDir = dir
	defer func() { model.DataDir = origDataDir }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ret": 0,
			"msgs": []any{map[string]any{
				"message_type":  1,
				"from_user_id":  "u1",
				"context_token": "ctx-1",
				"item_list": []any{map[string]any{
					"type":      4, // file: unsupported path, so no session is needed
					"file_item": map[string]any{},
				}},
			}},
		})
	}))
	defer srv.Close()

	origDB := db
	defer func() { db = origDB }()
	mock := &mockDB{}
	db = mock

	mgr := NewManager(&model.WeChatConfig{BotToken: "tok", BaseURL: srv.URL})
	SetManager(mgr)
	defer SetManager(nil)

	require.NoError(t, mgr.Start())

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if mock.tokenFor("u1") == "ctx-1" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	assert.Equal(t, "ctx-1", mock.tokenFor("u1"), "the inbound handler must run and persist the token")
	mgr.Stop()
}

var _ common.ConnectedClientChecker = (*mockClientChecker)(nil)
