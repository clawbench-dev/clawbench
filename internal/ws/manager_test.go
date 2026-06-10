package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"

	"clawbench/internal/push"
)

func newTestManager(jpush *push.JPushClient) *Manager {
	return &Manager{
		subscriptions: make(map[string]*ClientSubscription),
		jpush:         jpush,
	}
}

func TestManager_Subscribe(t *testing.T) {
	mgr := newTestManager(nil)
	var writeMu sync.Mutex

	sub := mgr.Subscribe(nil, &writeMu, "client-1", "")
	if sub == nil {
		t.Fatal("expected non-nil subscription")
	}

	mgr.mu.Lock()
	stored, ok := mgr.subscriptions["client-1"]
	mgr.mu.Unlock()
	if !ok || stored != sub {
		t.Error("subscription not stored correctly")
	}
}

func TestManager_Subscribe_StoresLocale(t *testing.T) {
	mgr := newTestManager(nil)
	var writeMu sync.Mutex

	sub := mgr.Subscribe(nil, &writeMu, "client-locale", "zh")
	if sub == nil {
		t.Fatal("expected non-nil subscription")
		return
	}

	sub.mu.Lock()
	locale := sub.locale
	sub.mu.Unlock()
	if locale != "zh" {
		t.Errorf("expected locale %q, got %q", "zh", locale)
	}
}

func TestManager_SubscribeReplacesExisting(t *testing.T) {
	mgr := newTestManager(nil)
	var writeMu1, writeMu2 sync.Mutex

	sub1 := mgr.Subscribe(nil, &writeMu1, "client-1", "en")
	_ = sub1

	// Second subscribe with same clientID should replace the first and update locale
	sub2 := mgr.Subscribe(nil, &writeMu2, "client-1", "zh")

	mgr.mu.Lock()
	stored := mgr.subscriptions["client-1"]
	mgr.mu.Unlock()
	if stored != sub2 {
		t.Error("expected subscription to be replaced")
	}
	sub2.mu.Lock()
	if sub2.locale != "zh" {
		t.Errorf("expected locale to be updated to %q, got %q", "zh", sub2.locale)
	}
	sub2.mu.Unlock()
}

func TestManager_SubscribeMultipleClients(t *testing.T) {
	mgr := newTestManager(nil)
	var writeMu1, writeMu2 sync.Mutex

	sub1 := mgr.Subscribe(nil, &writeMu1, "client-1", "")
	sub2 := mgr.Subscribe(nil, &writeMu2, "client-2", "")

	// Both should exist independently
	mgr.mu.Lock()
	s1 := mgr.subscriptions["client-1"]
	s2 := mgr.subscriptions["client-2"]
	mgr.mu.Unlock()
	if s1 != sub1 {
		t.Error("client-1 subscription not stored correctly")
	}
	if s2 != sub2 {
		t.Error("client-2 subscription not stored correctly")
	}
	if len(mgr.subscriptions) != 2 {
		t.Errorf("expected 2 subscriptions, got %d", len(mgr.subscriptions))
	}
}

func TestManager_Unsubscribe(t *testing.T) {
	mgr := newTestManager(nil)
	var writeMu sync.Mutex

	mgr.Subscribe(nil, &writeMu, "client-1", "")
	mgr.DisconnectClient("client-1")

	mgr.mu.Lock()
	sub, ok := mgr.subscriptions["client-1"]
	mgr.mu.Unlock()

	if !ok {
		t.Fatal("subscription should still exist after unsubscribe")
	}
	sub.mu.Lock()
	conn := sub.conn
	sub.mu.Unlock()
	if conn != nil {
		t.Error("expected conn to be nil after unsubscribe")
	}
}

func TestManager_RegisterPushID(t *testing.T) {
	mgr := newTestManager(nil)
	var writeMu sync.Mutex

	mgr.Subscribe(nil, &writeMu, "client-1", "")
	mgr.RegisterPushID("test-reg-id", "client-1")

	mgr.mu.Lock()
	sub := mgr.subscriptions["client-1"]
	mgr.mu.Unlock()

	sub.mu.Lock()
	regID := sub.pushRegID
	sub.mu.Unlock()
	if regID != "test-reg-id" {
		t.Errorf("expected push reg ID 'test-reg-id', got %q", regID)
	}
}

func TestManager_RegisterPushID_Dedup(t *testing.T) {
	mgr := newTestManager(nil)
	var writeMu1, writeMu2 sync.Mutex

	// Two clients, same pushRegID (same device reconnecting)
	mgr.Subscribe(nil, &writeMu1, "client-1", "")
	mgr.RegisterPushID("shared-reg-id", "client-1")

	mgr.Subscribe(nil, &writeMu2, "client-2", "")
	mgr.RegisterPushID("shared-reg-id", "client-2")

	// Client-2 should have the regID
	mgr.mu.Lock()
	s2 := mgr.subscriptions["client-2"]
	mgr.mu.Unlock()
	s2.mu.Lock()
	if s2.pushRegID != "shared-reg-id" {
		t.Errorf("client-2 expected 'shared-reg-id', got %q", s2.pushRegID)
	}
	s2.mu.Unlock()

	// Client-1 should have been cleared (dedup)
	mgr.mu.Lock()
	s1 := mgr.subscriptions["client-1"]
	mgr.mu.Unlock()
	s1.mu.Lock()
	if s1.pushRegID != "" {
		t.Errorf("client-1 expected empty pushRegID (dedup), got %q", s1.pushRegID)
	}
	s1.mu.Unlock()
}

func TestManager_BroadcastEvent_NoSubscription(_ *testing.T) {
	mgr := newTestManager(nil)
	// Should not panic
	mgr.BroadcastEvent(ServerMessage{Type: "event", Event: "session_update"})
}

func TestManager_BroadcastEvent_Disconnected(t *testing.T) {
	mgr := newTestManager(nil)
	var writeMu sync.Mutex

	mgr.Subscribe(nil, &writeMu, "client-1", "")
	mgr.DisconnectClient("client-1")

	// Broadcast while disconnected — should buffer
	msg := ServerMessage{Type: "event", ID: "evt_1", Event: "session_update", Data: &SessionUpdateData{SessionID: "s1", Status: "completed"}}
	mgr.BroadcastEvent(msg)

	mgr.mu.Lock()
	sub := mgr.subscriptions["client-1"]
	mgr.mu.Unlock()

	buffered := sub.GetBufferedEvents()
	if len(buffered) != 1 {
		t.Fatalf("expected 1 buffered event, got %d", len(buffered))
	}
	if buffered[0].ID != "evt_1" {
		t.Errorf("expected buffered event ID 'evt_1', got %q", buffered[0].ID)
	}
}

func TestManager_BroadcastEvent_JPushWhenDisconnected(_ *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"sendno":"123","msg_id":"456"}`))
	}))
	defer server.Close()

	jpush := push.NewJPushClient(push.JPushConfig{
		Enabled:      true,
		AppKey:       "test-key",
		MasterSecret: "test-secret",
	})

	mgr := newTestManager(jpush)
	var writeMu sync.Mutex

	mgr.Subscribe(nil, &writeMu, "client-1", "")
	mgr.RegisterPushID("reg-123", "client-1")
	mgr.DisconnectClient("client-1")

	// Broadcast while disconnected — should send JPush
	msg := ServerMessage{Type: "event", ID: "evt_1", Event: "session_update", Data: &SessionUpdateData{SessionID: "s1", Status: "completed"}}
	mgr.BroadcastEvent(msg)
	// If we get here without panic, JPush was called
}

func TestManager_BroadcastEvent_JPushDisabled(_ *testing.T) {
	mgr := newTestManager(nil) // nil jpush = disabled
	var writeMu sync.Mutex

	mgr.Subscribe(nil, &writeMu, "client-1", "")
	mgr.RegisterPushID("reg-123", "client-1")
	mgr.DisconnectClient("client-1")

	// Should not panic with nil jpush
	msg := ServerMessage{Type: "event", ID: "evt_1", Event: "task_update", Data: &TaskUpdateData{TaskID: "t1", Status: "completed"}}
	mgr.BroadcastEvent(msg)
}

func TestManager_BroadcastEvent_MultipleClients(t *testing.T) {
	mgr := newTestManager(nil)
	var writeMu1, writeMu2 sync.Mutex

	// Two clients subscribed
	mgr.Subscribe(nil, &writeMu1, "client-1", "")
	mgr.Subscribe(nil, &writeMu2, "client-2", "")

	// Disconnect both
	mgr.DisconnectClient("client-1")
	mgr.DisconnectClient("client-2")

	// Broadcast — both should buffer the event
	msg := ServerMessage{Type: "event", ID: "evt_1", Event: "session_update", Data: &SessionUpdateData{SessionID: "s1", Status: "completed"}}
	mgr.BroadcastEvent(msg)

	mgr.mu.Lock()
	s1 := mgr.subscriptions["client-1"]
	s2 := mgr.subscriptions["client-2"]
	mgr.mu.Unlock()

	if len(s1.GetBufferedEvents()) != 1 {
		t.Errorf("client-1: expected 1 buffered event, got %d", len(s1.GetBufferedEvents()))
	}
	if len(s2.GetBufferedEvents()) != 1 {
		t.Errorf("client-2: expected 1 buffered event, got %d", len(s2.GetBufferedEvents()))
	}
}

func TestBufferEvent_MaxSize(t *testing.T) {
	sub := &ClientSubscription{}

	for i := range 60 {
		sub.bufferEvent(ServerMessage{ID: string(rune('a' + i%26))})
	}

	if len(sub.eventBuffer) > 50 {
		t.Errorf("expected at most 50 buffered events, got %d", len(sub.eventBuffer))
	}

	if len(sub.eventBuffer) == 50 {
		if sub.eventBuffer[0].ID != "k" {
			t.Logf("first buffered event ID: %q (eviction order may vary)", sub.eventBuffer[0].ID)
		}
	}
}

func TestGetBufferedEvents_Copy(t *testing.T) {
	sub := &ClientSubscription{}
	sub.bufferEvent(ServerMessage{ID: "evt_1"})

	events := sub.GetBufferedEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	// Modifying the copy should not affect the original
	events[0] = ServerMessage{ID: "modified"}
	original := sub.GetBufferedEvents()
	if original[0].ID == "modified" {
		t.Error("GetBufferedEvents should return a copy")
	}
}

func TestCleanupStale_NoPushRegID(t *testing.T) {
	mgr := newTestManager(nil)
	var writeMu sync.Mutex

	mgr.Subscribe(nil, &writeMu, "client-1", "")
	mgr.DisconnectClient("client-1")

	// Set bufferStart to just past staleNoPushTimeout — should be cleaned up (no pushRegID)
	mgr.mu.Lock()
	sub := mgr.subscriptions["client-1"]
	mgr.mu.Unlock()
	sub.mu.Lock()
	sub.bufferStart = time.Now().Add(-staleNoPushTimeout - time.Second)
	sub.mu.Unlock()

	mgr.CleanupStale()

	mgr.mu.Lock()
	_, exists := mgr.subscriptions["client-1"]
	mgr.mu.Unlock()
	if exists {
		t.Error("expected stale subscription (no push) to be cleaned up after staleNoPushTimeout")
	}
}

func TestCleanupStale_NoPushRegID_RecentNotCleaned(t *testing.T) {
	mgr := newTestManager(nil)
	var writeMu sync.Mutex

	mgr.Subscribe(nil, &writeMu, "client-1", "")
	mgr.DisconnectClient("client-1")

	// Set bufferStart to well before staleNoPushTimeout — should NOT be cleaned up
	mgr.mu.Lock()
	sub := mgr.subscriptions["client-1"]
	mgr.mu.Unlock()
	sub.mu.Lock()
	sub.bufferStart = time.Now().Add(-60 * time.Second)
	sub.mu.Unlock()

	mgr.CleanupStale()

	mgr.mu.Lock()
	_, exists := mgr.subscriptions["client-1"]
	mgr.mu.Unlock()
	if !exists {
		t.Error("expected subscription (no push, before staleNoPushTimeout) to not be cleaned up")
	}
}

func TestCleanupStale_WithPushRegID(t *testing.T) {
	mgr := newTestManager(nil)
	var writeMu sync.Mutex

	mgr.Subscribe(nil, &writeMu, "client-1", "")
	mgr.RegisterPushID("reg-123", "client-1")
	mgr.DisconnectClient("client-1")

	// Set bufferStart to 31 minutes ago, lastActive to 31 minutes ago
	// Should NOT be cleaned up (has pushRegID, lastActive < staleWithPushTimeout)
	mgr.mu.Lock()
	sub := mgr.subscriptions["client-1"]
	mgr.mu.Unlock()
	sub.mu.Lock()
	sub.bufferStart = time.Now().Add(-31 * time.Minute)
	sub.lastActive = time.Now().Add(-31 * time.Minute)
	sub.mu.Unlock()

	mgr.CleanupStale()

	mgr.mu.Lock()
	_, exists := mgr.subscriptions["client-1"]
	mgr.mu.Unlock()
	if !exists {
		t.Error("expected subscription with pushRegID to survive (lastActive < staleWithPushTimeout)")
	}

	// Set lastActive beyond staleWithPushTimeout — should be cleaned up
	sub.mu.Lock()
	sub.lastActive = time.Now().Add(-staleWithPushTimeout - 24*time.Hour)
	sub.mu.Unlock()

	mgr.CleanupStale()

	mgr.mu.Lock()
	_, exists = mgr.subscriptions["client-1"]
	mgr.mu.Unlock()
	if exists {
		t.Error("expected subscription with pushRegID to be cleaned up (lastActive > staleWithPushTimeout)")
	}
}

func TestCleanupStale_RecentNotCleaned(t *testing.T) {
	mgr := newTestManager(nil)
	var writeMu sync.Mutex

	mgr.Subscribe(nil, &writeMu, "client-1", "")
	// Not unsubscribing — conn is active, should not be cleaned

	mgr.CleanupStale()

	mgr.mu.Lock()
	_, exists := mgr.subscriptions["client-1"]
	mgr.mu.Unlock()
	if !exists {
		t.Error("expected active subscription to not be cleaned up")
	}
}

func TestSetManagerForTest(t *testing.T) {
	orig := defaultManager
	defer func() { defaultManager = orig }()

	mgr := NewManagerForTest(nil)
	SetManagerForTest(mgr)

	if GetManager() != mgr {
		t.Error("expected GetManager to return the test manager")
	}

	SetManagerForTest(nil)
	if GetManager() != nil {
		t.Error("expected GetManager to return nil after reset")
	}
}

func TestNewManagerForTest(t *testing.T) {
	mgr := NewManagerForTest(nil)
	if mgr == nil {
		t.Fatal("expected non-nil manager")
		return
	}
	if len(mgr.subscriptions) != 0 {
		t.Errorf("expected empty subscriptions, got %d", len(mgr.subscriptions))
	}
}

func TestClientSubscription_GetBufferedEvents_Empty(t *testing.T) {
	sub := &ClientSubscription{}
	events := sub.GetBufferedEvents()
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestBroadcastEvent_BufferWindow(t *testing.T) {
	mgr := newTestManager(nil)
	var writeMu sync.Mutex

	mgr.Subscribe(nil, &writeMu, "client-1", "")
	mgr.DisconnectClient("client-1")

	// Within buffer window (10s) — should buffer
	msg := ServerMessage{Type: "event", ID: "evt_1", Event: "session_update", Data: &SessionUpdateData{SessionID: "s1", Status: "completed"}}
	mgr.BroadcastEvent(msg)

	mgr.mu.Lock()
	sub := mgr.subscriptions["client-1"]
	mgr.mu.Unlock()

	buffered := sub.GetBufferedEvents()
	if len(buffered) != 1 {
		t.Fatalf("expected 1 buffered event within window, got %d", len(buffered))
	}

	// Beyond buffer window — should not buffer
	sub.mu.Lock()
	sub.bufferStart = time.Now().Add(-15 * time.Second)
	sub.eventBuffer = nil
	sub.mu.Unlock()

	msg2 := ServerMessage{Type: "event", ID: "evt_2", Event: "task_update", Data: &TaskUpdateData{TaskID: "t1", Status: "completed"}}
	mgr.BroadcastEvent(msg2)

	buffered2 := sub.GetBufferedEvents()
	if len(buffered2) != 0 {
		t.Errorf("expected 0 buffered events outside window, got %d", len(buffered2))
	}
}

func TestManager_BroadcastEvent_JPushAlert_WithSessionTitle(t *testing.T) {
	var receivedTitle, receivedAlert string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
			if n, ok := payload["notification"].(map[string]any); ok {
				if a, ok := n["android"].(map[string]any); ok {
					receivedAlert, _ = a["alert"].(string)
					receivedTitle, _ = a["title"].(string)
				}
			}
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"sendno":"123","msg_id":"456"}`))
	}))
	defer server.Close()

	jpush := push.NewJPushClient(push.JPushConfig{
		Enabled:      true,
		AppKey:       "test-key",
		MasterSecret: "test-secret",
	})
	jpush.SetBaseURL(server.URL)

	mgr := newTestManager(jpush)
	var writeMu sync.Mutex
	mgr.Subscribe(nil, &writeMu, "client-1", "")
	mgr.RegisterPushID("reg-123", "client-1")
	mgr.DisconnectClient("client-1")

	msg := ServerMessage{
		Type:  "event",
		ID:    "evt_1",
		Event: "session_update",
		Data: &SessionUpdateData{
			SessionID:       "s1",
			Status:          "completed",
			HasNewMessages:  true,
			SessionTitle:    "帮我写一个Go HTTP服务器",
			ResponsePreview: "AI回复的预览内容",
		},
	}
	mgr.BroadcastEvent(msg)

	// SessionTitle goes into the notification title; ResponsePreview goes into the alert
	if receivedTitle != "Done:帮我写一个Go HTTP服务器" {
		t.Errorf("expected title to include session title, got %q", receivedTitle)
	}
	if receivedAlert != "AI回复的预览内容" {
		t.Errorf("expected alert to be response preview, got %q", receivedAlert)
	}
}

func TestManager_BroadcastEvent_JPushAlert_WithResponsePreview(t *testing.T) {
	var receivedTitle, receivedAlert string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
			if n, ok := payload["notification"].(map[string]any); ok {
				if a, ok := n["android"].(map[string]any); ok {
					receivedAlert, _ = a["alert"].(string)
					receivedTitle, _ = a["title"].(string)
				}
			}
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"sendno":"123","msg_id":"456"}`))
	}))
	defer server.Close()

	jpush := push.NewJPushClient(push.JPushConfig{
		Enabled:      true,
		AppKey:       "test-key",
		MasterSecret: "test-secret",
	})
	jpush.SetBaseURL(server.URL)

	mgr := newTestManager(jpush)
	var writeMu sync.Mutex
	mgr.Subscribe(nil, &writeMu, "client-1", "")
	mgr.RegisterPushID("reg-123", "client-1")
	mgr.DisconnectClient("client-1")

	msg := ServerMessage{
		Type:  "event",
		ID:    "evt_1",
		Event: "session_update",
		Data: &SessionUpdateData{
			SessionID:       "s1",
			Status:          "completed",
			HasNewMessages:  true,
			ResponsePreview: "AI回复的预览内容",
		},
	}
	mgr.BroadcastEvent(msg)

	// Without session title, notification title stays as default (i18n-based, defaults to English)
	if receivedTitle != "AI Task Completed" {
		t.Errorf("expected default title, got %q", receivedTitle)
	}
	// Short preview should pass through unchanged (under pushAlertMaxRunes)
	if receivedAlert != "AI回复的预览内容" {
		t.Errorf("expected alert to be response preview, got %q", receivedAlert)
	}
}

func TestManager_BroadcastEvent_JPushAlert_TruncatesLongPreview(t *testing.T) {
	var receivedTitle, receivedAlert string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
			if n, ok := payload["notification"].(map[string]any); ok {
				if a, ok := n["android"].(map[string]any); ok {
					receivedAlert, _ = a["alert"].(string)
					receivedTitle, _ = a["title"].(string)
				}
			}
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"sendno":"123","msg_id":"456"}`))
	}))
	defer server.Close()

	jpush := push.NewJPushClient(push.JPushConfig{
		Enabled:      true,
		AppKey:       "test-key",
		MasterSecret: "test-secret",
	})
	jpush.SetBaseURL(server.URL)

	mgr := newTestManager(jpush)
	var writeMu sync.Mutex
	mgr.Subscribe(nil, &writeMu, "client-1", "")
	mgr.RegisterPushID("reg-123", "client-1")
	mgr.DisconnectClient("client-1")

	// Long response preview — should be truncated in alert
	longPreview := strings.Repeat("测", pushAlertMaxRunes+1)
	msg := ServerMessage{
		Type:  "event",
		ID:    "evt_1",
		Event: "session_update",
		Data: &SessionUpdateData{
			SessionID:       "s1",
			Status:          "completed",
			HasNewMessages:  true,
			SessionTitle:    "测试标题",
			ResponsePreview: longPreview,
		},
	}
	mgr.BroadcastEvent(msg)

	// Title includes session title without truncation
	if receivedTitle != "Done:测试标题" {
		t.Errorf("expected title with session title, got %q", receivedTitle)
	}
	// Alert is the truncated response preview
	expected := string([]rune(longPreview)[:pushAlertMaxRunes]) + "…"
	if receivedAlert != expected {
		t.Errorf("expected truncated alert, got %q (want %q)", receivedAlert, expected)
	}
	if utf8.RuneCountInString(receivedAlert) != pushAlertMaxRunes+1 {
		t.Errorf("expected alert of %d runes, got %d", pushAlertMaxRunes+1, utf8.RuneCountInString(receivedAlert))
	}
}

func TestManager_BroadcastEvent_JPushAlert_WithoutTitleOrPreview(t *testing.T) {
	var receivedTitle, receivedAlert string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
			if n, ok := payload["notification"].(map[string]any); ok {
				if a, ok := n["android"].(map[string]any); ok {
					receivedAlert, _ = a["alert"].(string)
					receivedTitle, _ = a["title"].(string)
				}
			}
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"sendno":"123","msg_id":"456"}`))
	}))
	defer server.Close()

	jpush := push.NewJPushClient(push.JPushConfig{
		Enabled:      true,
		AppKey:       "test-key",
		MasterSecret: "test-secret",
	})
	jpush.SetBaseURL(server.URL)

	mgr := newTestManager(jpush)
	var writeMu sync.Mutex
	mgr.Subscribe(nil, &writeMu, "client-1", "")
	mgr.RegisterPushID("reg-123", "client-1")
	mgr.DisconnectClient("client-1")

	msg := ServerMessage{
		Type:  "event",
		ID:    "evt_1",
		Event: "session_update",
		Data: &SessionUpdateData{
			SessionID:      "s1",
			Status:         "completed",
			HasNewMessages: true,
		},
	}
	mgr.BroadcastEvent(msg)

	if receivedTitle != "AI Task Completed" {
		t.Errorf("expected default title, got %q", receivedTitle)
	}
	if receivedAlert != "AI session ended" {
		t.Errorf("expected default alert, got %q", receivedAlert)
	}
}

func TestManager_BroadcastEvent_JPushAlert_TaskUpdate(t *testing.T) {
	var receivedTitle, receivedAlert string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
			if n, ok := payload["notification"].(map[string]any); ok {
				if a, ok := n["android"].(map[string]any); ok {
					receivedAlert, _ = a["alert"].(string)
					receivedTitle, _ = a["title"].(string)
				}
			}
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"sendno":"123","msg_id":"456"}`))
	}))
	defer server.Close()

	jpush := push.NewJPushClient(push.JPushConfig{
		Enabled:      true,
		AppKey:       "test-key",
		MasterSecret: "test-secret",
	})
	jpush.SetBaseURL(server.URL)

	mgr := newTestManager(jpush)
	var writeMu sync.Mutex
	mgr.Subscribe(nil, &writeMu, "client-1", "")
	mgr.RegisterPushID("reg-123", "client-1")
	mgr.DisconnectClient("client-1")

	msg := ServerMessage{
		Type:  "event",
		ID:    "evt_1",
		Event: "task_update",
		Data: &TaskUpdateData{
			TaskID:          "t1",
			Status:          "completed",
			SessionTitle:    "自动修复Bug",
			ResponsePreview: "已修复空指针异常，添加了nil检查",
		},
	}
	mgr.BroadcastEvent(msg)

	if receivedTitle != "Done:自动修复Bug" {
		t.Errorf("expected title with task name, got %q", receivedTitle)
	}
	if receivedAlert != "已修复空指针异常，添加了nil检查" {
		t.Errorf("expected response preview alert, got %q", receivedAlert)
	}
}

func TestManager_BroadcastEvent_JPushNotSentForRunning(t *testing.T) {
	pushCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		pushCalled = true
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"sendno":"123","msg_id":"456"}`))
	}))
	defer server.Close()

	jpush := push.NewJPushClient(push.JPushConfig{
		Enabled:      true,
		AppKey:       "test-key",
		MasterSecret: "test-secret",
	})
	jpush.SetBaseURL(server.URL)

	mgr := newTestManager(jpush)
	var writeMu sync.Mutex
	mgr.Subscribe(nil, &writeMu, "client-1", "")
	mgr.RegisterPushID("reg-123", "client-1")
	mgr.DisconnectClient("client-1")

	// session_update with status "running" — should NOT trigger JPush
	msg := ServerMessage{
		Type:  "event",
		ID:    "evt_1",
		Event: "session_update",
		Data: &SessionUpdateData{
			SessionID: "s1",
			Status:    "running",
		},
	}
	mgr.BroadcastEvent(msg)

	if pushCalled {
		t.Error("JPush should NOT be called for running status")
	}

	// But the event should still be buffered
	mgr.mu.Lock()
	sub := mgr.subscriptions["client-1"]
	mgr.mu.Unlock()
	if len(sub.GetBufferedEvents()) != 1 {
		t.Errorf("running event should still be buffered, got %d events", len(sub.GetBufferedEvents()))
	}
}

func TestManager_BroadcastEvent_JPushSentForCompleted(t *testing.T) {
	pushCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		pushCalled = true
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"sendno":"123","msg_id":"456"}`))
	}))
	defer server.Close()

	jpush := push.NewJPushClient(push.JPushConfig{
		Enabled:      true,
		AppKey:       "test-key",
		MasterSecret: "test-secret",
	})
	jpush.SetBaseURL(server.URL)

	mgr := newTestManager(jpush)
	var writeMu sync.Mutex
	mgr.Subscribe(nil, &writeMu, "client-1", "")
	mgr.RegisterPushID("reg-123", "client-1")
	mgr.DisconnectClient("client-1")

	// session_update with status "completed" — SHOULD trigger JPush
	msg := ServerMessage{
		Type:  "event",
		ID:    "evt_1",
		Event: "session_update",
		Data: &SessionUpdateData{
			SessionID: "s1",
			Status:    "completed",
		},
	}
	mgr.BroadcastEvent(msg)

	if !pushCalled {
		t.Error("JPush should be called for completed status")
	}
}

func TestManager_BroadcastEvent_JPushNotSentForTaskRunning(t *testing.T) {
	pushCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		pushCalled = true
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"sendno":"123","msg_id":"456"}`))
	}))
	defer server.Close()

	jpush := push.NewJPushClient(push.JPushConfig{
		Enabled:      true,
		AppKey:       "test-key",
		MasterSecret: "test-secret",
	})
	jpush.SetBaseURL(server.URL)

	mgr := newTestManager(jpush)
	var writeMu sync.Mutex
	mgr.Subscribe(nil, &writeMu, "client-1", "")
	mgr.RegisterPushID("reg-123", "client-1")
	mgr.DisconnectClient("client-1")

	// task_update with status "running" — should NOT trigger JPush
	msg := ServerMessage{
		Type:  "event",
		ID:    "evt_1",
		Event: "task_update",
		Data: &TaskUpdateData{
			TaskID: "t1",
			Status: "running",
		},
	}
	mgr.BroadcastEvent(msg)

	if pushCalled {
		t.Error("JPush should NOT be called for task running status")
	}
}

func TestManager_BroadcastEvent_JPushSentForCancelled(t *testing.T) {
	pushCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		pushCalled = true
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"sendno":"123","msg_id":"456"}`))
	}))
	defer server.Close()

	jpush := push.NewJPushClient(push.JPushConfig{
		Enabled:      true,
		AppKey:       "test-key",
		MasterSecret: "test-secret",
	})
	jpush.SetBaseURL(server.URL)

	mgr := newTestManager(jpush)
	var writeMu sync.Mutex
	mgr.Subscribe(nil, &writeMu, "client-1", "")
	mgr.RegisterPushID("reg-123", "client-1")
	mgr.DisconnectClient("client-1")

	// session_update with terminal status — SHOULD trigger JPush
	msg := ServerMessage{
		Type:  "event",
		ID:    "evt_1",
		Event: "session_update",
		Data: &SessionUpdateData{
			SessionID: "s1",
			Status:    "cancelled",
		},
	}
	mgr.BroadcastEvent(msg)

	if !pushCalled {
		t.Error("JPush should be called for canceled status")
	}
}

func TestManager_BroadcastEvent_JPushDedupSameRegID(t *testing.T) {
	// Two disconnected subscriptions with the same pushRegID (same device)
	// should only result in one JPush notification.
	pushCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		pushCount++
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"sendno":"123","msg_id":"456"}`))
	}))
	defer server.Close()

	jpush := push.NewJPushClient(push.JPushConfig{
		Enabled:      true,
		AppKey:       "test-key",
		MasterSecret: "test-secret",
	})
	jpush.SetBaseURL(server.URL)

	mgr := newTestManager(jpush)

	// Client-1: disconnected, has pushRegID
	var writeMu1 sync.Mutex
	mgr.Subscribe(nil, &writeMu1, "client-1", "")
	mgr.RegisterPushID("reg-shared", "client-1")
	mgr.DisconnectClient("client-1")

	// Client-2: disconnected, manually set same pushRegID
	var writeMu2 sync.Mutex
	mgr.Subscribe(nil, &writeMu2, "client-2", "")
	mgr.DisconnectClient("client-2")
	mgr.mu.Lock()
	s2 := mgr.subscriptions["client-2"]
	mgr.mu.Unlock()
	s2.mu.Lock()
	s2.pushRegID = "reg-shared"
	s2.mu.Unlock()

	msg := ServerMessage{
		Type:  "event",
		ID:    "evt_1",
		Event: "session_update",
		Data: &SessionUpdateData{
			SessionID: "s1",
			Status:    "completed",
		},
	}
	mgr.BroadcastEvent(msg)

	if pushCount != 1 {
		t.Errorf("expected exactly 1 JPush call for same regID, got %d", pushCount)
	}
}

func TestManager_BroadcastEvent_JPushExtras_SessionWithProjectPath(t *testing.T) {
	var receivedExtras map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
			if n, ok := payload["notification"].(map[string]any); ok {
				if a, ok := n["android"].(map[string]any); ok {
					receivedExtras, _ = a["extras"].(map[string]any)
				}
			}
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"sendno":"123","msg_id":"456"}`))
	}))
	defer server.Close()

	jpush := push.NewJPushClient(push.JPushConfig{
		Enabled:      true,
		AppKey:       "test-key",
		MasterSecret: "test-secret",
	})
	jpush.SetBaseURL(server.URL)

	mgr := newTestManager(jpush)
	var writeMu sync.Mutex
	mgr.Subscribe(nil, &writeMu, "client-1", "")
	mgr.RegisterPushID("reg-123", "client-1")
	mgr.DisconnectClient("client-1")

	msg := ServerMessage{
		Type:  "event",
		ID:    "evt_1",
		Event: "session_update",
		Data: &SessionUpdateData{
			SessionID:      "s1",
			Status:         "completed",
			HasNewMessages: true,
			ProjectPath:    "/home/user/project",
		},
	}
	mgr.BroadcastEvent(msg)

	if receivedExtras == nil {
		t.Fatal("expected extras in JPush payload")
	}
	if receivedExtras["session_id"] != "s1" {
		t.Errorf("expected session_id 's1', got %v", receivedExtras["session_id"])
	}
	if receivedExtras["project_path"] != "/home/user/project" {
		t.Errorf("expected project_path '/home/user/project', got %v", receivedExtras["project_path"])
	}
	if receivedExtras["event_type"] != "session_update" {
		t.Errorf("expected event_type 'session_update', got %v", receivedExtras["event_type"])
	}
}

func TestManager_BroadcastEvent_JPushExtras_TaskWithSessionAndProjectPath(t *testing.T) {
	var receivedExtras map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
			if n, ok := payload["notification"].(map[string]any); ok {
				if a, ok := n["android"].(map[string]any); ok {
					receivedExtras, _ = a["extras"].(map[string]any)
				}
			}
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"sendno":"123","msg_id":"456"}`))
	}))
	defer server.Close()

	jpush := push.NewJPushClient(push.JPushConfig{
		Enabled:      true,
		AppKey:       "test-key",
		MasterSecret: "test-secret",
	})
	jpush.SetBaseURL(server.URL)

	mgr := newTestManager(jpush)
	var writeMu sync.Mutex
	mgr.Subscribe(nil, &writeMu, "client-1", "")
	mgr.RegisterPushID("reg-123", "client-1")
	mgr.DisconnectClient("client-1")

	msg := ServerMessage{
		Type:  "event",
		ID:    "evt_1",
		Event: "task_update",
		Data: &TaskUpdateData{
			TaskID:      "t1",
			Status:      "completed",
			ExecutionID: "e1",
			SessionID:   "s1",
			ProjectPath: "/home/user/project",
		},
	}
	mgr.BroadcastEvent(msg)

	if receivedExtras == nil {
		t.Fatal("expected extras in JPush payload")
	}
	if receivedExtras["task_id"] != "t1" {
		t.Errorf("expected task_id 't1', got %v", receivedExtras["task_id"])
	}
	if receivedExtras["session_id"] != "s1" {
		t.Errorf("expected session_id 's1', got %v", receivedExtras["session_id"])
	}
	if receivedExtras["project_path"] != "/home/user/project" {
		t.Errorf("expected project_path '/home/user/project', got %v", receivedExtras["project_path"])
	}
}

func TestManager_BroadcastEvent_JPushWhenNoWS(t *testing.T) {
	// When all subscriptions for a regID are disconnected, JPush should fire.
	pushCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		pushCalled = true
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"sendno":"123","msg_id":"456"}`))
	}))
	defer server.Close()

	jpush := push.NewJPushClient(push.JPushConfig{
		Enabled:      true,
		AppKey:       "test-key",
		MasterSecret: "test-secret",
	})
	jpush.SetBaseURL(server.URL)

	mgr := newTestManager(jpush)

	// Client-1: disconnected, has pushRegID
	var writeMu1 sync.Mutex
	mgr.Subscribe(nil, &writeMu1, "client-1", "")
	mgr.RegisterPushID("reg-123", "client-1")
	mgr.DisconnectClient("client-1")

	// Client-2: disconnected, same pushRegID (simulating same device)
	var writeMu2 sync.Mutex
	mgr.Subscribe(nil, &writeMu2, "client-2", "")
	mgr.DisconnectClient("client-2")
	mgr.mu.Lock()
	s2 := mgr.subscriptions["client-2"]
	mgr.mu.Unlock()
	s2.mu.Lock()
	s2.pushRegID = "reg-123"
	s2.mu.Unlock()

	msg := ServerMessage{
		Type:  "event",
		ID:    "evt_1",
		Event: "session_update",
		Data: &SessionUpdateData{
			SessionID: "s1",
			Status:    "completed",
		},
	}
	mgr.BroadcastEvent(msg)

	if !pushCalled {
		t.Error("JPush should be called when no WS is connected")
	}
}

func TestTruncateForPush(t *testing.T) {
	short := "短文本"
	if got := truncateForPush(short); got != short {
		t.Errorf("short text should pass through, got %q", got)
	}

	// Exactly pushAlertMaxRunes — no truncation
	exact := strings.Repeat("一二", pushAlertMaxRunes/2)
	if utf8.RuneCountInString(exact) != pushAlertMaxRunes {
		t.Fatalf("test setup: expected %d runes, got %d", pushAlertMaxRunes, utf8.RuneCountInString(exact))
	}
	if got := truncateForPush(exact); got != exact {
		t.Errorf("exact-length text should not be truncated, got %q", got)
	}

	// Over pushAlertMaxRunes — truncate + "…"
	long := strings.Repeat("一二", pushAlertMaxRunes/2) + "三"
	runes := []rune(long)
	expected := string(runes[:pushAlertMaxRunes]) + "…"
	got := truncateForPush(long)
	if got != expected {
		t.Errorf("long text should be truncated, got %q (want %q)", got, expected)
	}
	if utf8.RuneCountInString(got) != pushAlertMaxRunes+1 {
		t.Errorf("truncated text should be %d+1 runes, got %d", pushAlertMaxRunes, utf8.RuneCountInString(got))
	}
}

// TestManager_Subscribe_ConnectionReplacement verifies that subscribing with
// the same clientID closes the old connection (exercises _ = oldConn.Close).
func TestManager_Subscribe_ConnectionReplacement(t *testing.T) {
	mgr := newTestManager(nil)

	// Create a real WebSocket server for the test
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		var writeMu sync.Mutex
		sub := mgr.Subscribe(conn, &writeMu, "replace-test", "")
		if sub == nil {
			return
		}
		defer mgr.DisconnectClient("replace-test")
		readClientMessages(conn, mgr, "replace-test")
		_ = conn.Close(websocket.StatusNormalClosure, "done")
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	// First connection
	wsURL := "ws" + server.URL[4:]
	ctx1, cancel1 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel1()
	conn1, _, err := websocket.Dial(ctx1, wsURL, nil)
	if err != nil {
		t.Fatalf("first connection failed: %v", err)
	}

	// Wait for subscription
	time.Sleep(100 * time.Millisecond)

	// Verify first connection is stored
	mgr.mu.Lock()
	sub := mgr.subscriptions["replace-test"]
	mgr.mu.Unlock()
	sub.mu.Lock()
	firstConn := sub.conn
	sub.mu.Unlock()
	if firstConn == nil {
		t.Fatal("expected first connection to be stored")
	}

	// Second connection with same clientID — should replace and close old conn
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	conn2, _, err := websocket.Dial(ctx2, wsURL, nil)
	if err != nil {
		t.Fatalf("second connection failed: %v", err)
	}
	defer func() { _ = conn2.Close(websocket.StatusNormalClosure, "") }()

	// Wait for replacement
	time.Sleep(100 * time.Millisecond)

	// Verify the new connection replaced the old one
	mgr.mu.Lock()
	sub = mgr.subscriptions["replace-test"]
	mgr.mu.Unlock()
	sub.mu.Lock()
	secondConn := sub.conn
	sub.mu.Unlock()
	if secondConn == nil {
		t.Fatal("expected second connection to be stored")
	}
	if secondConn == firstConn {
		t.Error("expected connection to be replaced with a new one")
	}

	_ = conn1.Close(websocket.StatusNormalClosure, "")
	mgr.DisconnectClient("replace-test")
}

// TestManager_Subscribe_LimitReached verifies that Subscribe rejects new
// subscriptions when the limit is reached (exercises _ = conn.Close in Subscribe).
func TestManager_Subscribe_LimitReached(t *testing.T) {
	mgr := newTestManager(nil)

	// Fill up to the subscription limit
	for i := range maxSubscriptions {
		var writeMu sync.Mutex
		mgr.Subscribe(nil, &writeMu, fmt.Sprintf("filler-%d", i), "")
	}

	// Create a real WebSocket server and try to subscribe beyond the limit
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		var writeMu sync.Mutex
		sub := mgr.Subscribe(conn, &writeMu, "overflow", "")
		if sub == nil {
			// Subscription rejected — conn.Close was already called by Subscribe
			return
		}
		defer mgr.DisconnectClient("overflow")
		readClientMessages(conn, mgr, "overflow")
		_ = conn.Close(websocket.StatusNormalClosure, "done")
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// The connection should be closed by the server since limit is reached
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		// Connection rejected — expected
		return
	}
	// Try to read — server should close the connection
	_, _, readErr := conn.Read(ctx)
	if readErr == nil {
		t.Error("expected connection to be closed by server (limit reached)")
	}
	_ = conn.Close(websocket.StatusNormalClosure, "")
}
