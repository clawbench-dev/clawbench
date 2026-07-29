package feishu

import (
	"context"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/push/common"
)

func TestIsStarted_NoManager(t *testing.T) {
	if IsStarted() {
		t.Error("IsStarted() should be false when no manager is set")
	}
}

func TestPushSessionEvent_NotStarted(t *testing.T) {
	if PushSessionEvent("test-session", "completed", "Test", "Preview", "/path", "Bash", "") {
		t.Error("expected false when not started")
	}
}

func TestPushTaskEvent_NotStarted(t *testing.T) {
	if PushTaskEvent("test-task", "completed", "Test Task", "Preview", "/path") {
		t.Error("expected false when not started")
	}
}

func setupPushMode(mode string) func() {
	orig := model.ConfigInstance.PushMode
	model.ConfigInstance.PushMode = mode
	return func() { model.ConfigInstance.PushMode = orig }
}

func TestPushSuppressed_WhenClientOnline(t *testing.T) {
	defer setupPushMode("feishu")()

	origChecker := clientChecker
	defer func() { clientChecker = origChecker }()

	RegisterClientChecker(&mockClientChecker{hasConnected: true})

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{subscribers: []common.SubscriberInfo{{UserID: "ou_user1"}}}

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	mgr.started = true
	SetManager(mgr)
	defer SetManager(nil)

	if PushSessionEvent("s1", "completed", "Test", "Preview", "/path", "Bash", "") {
		t.Error("expected push to be suppressed when client is online")
	}
	if PushTaskEvent("t1", "completed", "Test Task", "Preview", "/path") {
		t.Error("expected push to be suppressed when client is online")
	}
}

func TestPushNotSuppressed_WhenNoClientOnline(t *testing.T) {
	defer setupPushMode("feishu")()

	origChecker := clientChecker
	defer func() { clientChecker = origChecker }()

	RegisterClientChecker(&mockClientChecker{hasConnected: false})

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{subscribers: []common.SubscriberInfo{{UserID: "ou_user1"}}}

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	mgr.started = true
	SetManager(mgr)
	defer SetManager(nil)

	// Will attempt send (may fail since no real server), but should not be suppressed
	PushSessionEvent("s1", "completed", "Test", "Preview", "/path", "Bash", "")
}

func TestPushSuppressed_InNativeMode(t *testing.T) {
	defer setupPushMode("native")()

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{subscribers: []common.SubscriberInfo{{UserID: "ou_user1"}}}

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	mgr.started = true
	SetManager(mgr)
	defer SetManager(nil)

	if PushSessionEvent("s1", "completed", "Test", "Preview", "/path", "Bash", "") {
		t.Error("expected false in native mode")
	}
}

func TestPushSuppressed_InDisabledMode(t *testing.T) {
	defer setupPushMode("disabled")()

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{subscribers: []common.SubscriberInfo{{UserID: "ou_user1"}}}

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	mgr.started = true
	SetManager(mgr)
	defer SetManager(nil)

	if PushSessionEvent("s1", "completed", "Test", "Preview", "/path", "Bash", "") {
		t.Error("expected false in disabled mode")
	}
}

func TestPushSessionEvent_UnknownStatus(t *testing.T) {
	defer setupPushMode("feishu")()

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{}

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	mgr.started = true
	SetManager(mgr)
	defer SetManager(nil)

	if PushSessionEvent("s1", "unknown_status", "Title", "Preview", "/path", "Bash", "") {
		t.Error("expected false for unknown status")
	}
}

func TestPushTaskEvent_UnknownStatus(t *testing.T) {
	defer setupPushMode("feishu")()

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{}

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	mgr.started = true
	SetManager(mgr)
	defer SetManager(nil)

	if PushTaskEvent("t1", "unknown_status", "Task", "Preview", "/path") {
		t.Error("expected false for unknown status")
	}
}

func TestTruncateForFeishu(t *testing.T) {
	if got := truncateForFeishu(""); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
	input := "Hello, world!"
	if got := truncateForFeishu(input); got != input {
		t.Errorf("expected %q, got %q", input, got)
	}
}

func TestTruncateForFeishu_OverLimit(t *testing.T) {
	longText := string(make([]rune, feishuPreviewMaxRunes+100))
	got := truncateForFeishu(longText)
	if len([]rune(got)) != feishuPreviewMaxRunes+1 { // +1 for ellipsis
		t.Errorf("expected %d runes, got %d", feishuPreviewMaxRunes+1, len([]rune(got)))
	}
}

func TestFormatPermissionDetail(t *testing.T) {
	if got := formatPermissionDetail("", ""); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
	if got := formatPermissionDetail("Bash", ""); got != "**操作**: Bash\n\n" {
		t.Errorf("expected '**操作**: Bash', got %q", got)
	}
	if got := formatPermissionDetail("Bash", `{"command":"rm -rf /tmp"}`); got != "**操作**: Bash\n\n**命令**: `rm -rf /tmp`\n\n" {
		t.Errorf("unexpected output: %q", got)
	}
}

func TestGetPushMode_Default(t *testing.T) {
	orig := model.ConfigInstance.PushMode
	defer func() { model.ConfigInstance.PushMode = orig }()
	model.ConfigInstance.PushMode = ""

	if mode := GetPushMode(); mode != "native" {
		t.Errorf("expected native, got %q", mode)
	}
}

func TestGetPushMode_Feishu(t *testing.T) {
	orig := model.ConfigInstance.PushMode
	defer func() { model.ConfigInstance.PushMode = orig }()
	model.ConfigInstance.PushMode = "feishu"

	if mode := GetPushMode(); mode != "feishu" {
		t.Errorf("expected feishu, got %q", mode)
	}
}

// mockClientChecker implements common.ConnectedClientChecker for testing.
type mockClientChecker struct {
	hasConnected bool
}

func (m *mockClientChecker) HasConnectedClients() bool { return m.hasConnected }

// mockDB implements common.PushDB for testing.
type mockDB struct {
	subscribers []common.SubscriberInfo
}

func (m *mockDB) MergeConfigSubscribers(_ []string)                      {}
func (m *mockDB) GetSubscribers() ([]common.SubscriberInfo, error)       { return m.subscribers, nil }
func (m *mockDB) UpsertSubscriber(_, _, _, _ string) error               { return nil }
func (m *mockDB) DeleteSubscriber(_ string) error                        { return nil }

// testContext returns a context with a short timeout for tests.
func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}
