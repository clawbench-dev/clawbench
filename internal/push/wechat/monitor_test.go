package wechat

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

	// The cursor must become readable with the value the server handed back.
	//
	// Poll rather than sample once: the mock answers instantly, so the monitor
	// publishes the cursor many times a second, and on Windows a publish that
	// lands while the file is open for reading makes that read fail (the
	// replace is not one atomic step there). Retrying observes the same
	// guarantee without depending on where in that window the sample falls.
	require.Eventually(t, func() bool { return loadSyncBuf("") == "cursor-42" },
		5*time.Second, 10*time.Millisecond,
		"the cursor must be persisted so a restart resumes instead of replaying the backlog")
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

// TestMonitor_StopJoinsBeforeReturning pins the fix for an intermittent CI
// failure: `TestMonitor_PersistsSyncBuf` failed during t.TempDir cleanup with
// "directory not empty".
//
// saveSyncBuf writes a `.tmp` sibling and renames it. Stop used to cancel the
// context and return immediately, so the monitor goroutine could still be
// between those two steps — leaving `sync_buf.tmp` behind when the caller
// removed the directory, which is exactly what t.TempDir reported.
//
// TODO(flaky): this test cannot yet DISTINGUISH the two behaviors locally.
// Removing `monitorWG.Wait()` leaves it green, because cancelling the context
// aborts the in-flight long poll and the SDK returns from Monitor without ever
// reaching OnBufUpdate again — so on a fast local machine the write window this
// test targets never opens. It reproduced only on a loaded CI runner. The
// assertions below still hold the guarantee (no `.tmp` after Stop, and none
// appearing later), so the test is not a tautology about the WRONG thing; it
// simply has not been observed failing without the join. To make it
// discriminating, the write would have to be parked deterministically — e.g.
// an OnBufUpdate hook that blocks until released — which requires exposing a
// seam the SDK does not offer.
func TestMonitor_StopJoinsBeforeReturning(t *testing.T) {
	dir := t.TempDir()
	origDataDir := model.DataDir
	model.DataDir = dir
	defer func() { model.DataDir = origDataDir }()

	var n atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ret":             0,
			"get_updates_buf": fmt.Sprintf("cursor-%d", n.Add(1)),
			"msgs":            []any{},
		})
		// Hold the poll open so Stop lands while the goroutine is mid-cycle.
		time.Sleep(120 * time.Millisecond)
	}))
	defer srv.Close()

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{}

	mgr := NewManager(&model.WeChatConfig{BotToken: "tok", BaseURL: srv.URL})
	SetManager(mgr)
	defer SetManager(nil)

	require.NoError(t, mgr.Start())
	require.Eventually(t, func() bool { return loadSyncBuf("") != "" },
		5*time.Second, 5*time.Millisecond, "the monitor must have written a cursor before Stop")

	mgr.Stop()

	stateDir := filepath.Dir(syncBufPath(""))
	assertNoTempLeftovers(t, stateDir)
	// A goroutine that outlived Stop would finish its cycle and write again.
	time.Sleep(400 * time.Millisecond)
	assertNoTempLeftovers(t, stateDir)
}

// assertNoTempLeftovers fails if saveSyncBuf's intermediate file is present.
func assertNoTempLeftovers(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return
	}
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasSuffix(e.Name(), ".tmp"),
			"a cursor write was still in flight when Stop returned: %s", e.Name())
	}
}
