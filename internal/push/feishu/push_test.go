package feishu

import (
	"context"
	"fmt"
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

func TestFormatPermissionDetail_FilePath(t *testing.T) {
	// file_path field
	got := formatPermissionDetail("Edit", `{"file_path":"/tmp/test.go"}`)
	if got != "**操作**: Edit\n\n**文件**: `/tmp/test.go`\n\n" {
		t.Errorf("unexpected output: %q", got)
	}
}

func TestFormatPermissionDetail_PathFallback(t *testing.T) {
	// "path" field as fallback when file_path is empty
	got := formatPermissionDetail("Read", `{"path":"/tmp/other.go"}`)
	if got != "**操作**: Read\n\n**文件**: `/tmp/other.go`\n\n" {
		t.Errorf("unexpected output: %q", got)
	}
}

func TestFormatPermissionDetail_FilePathPreferredOverPath(t *testing.T) {
	// Both file_path and path present — file_path takes precedence
	got := formatPermissionDetail("Write", `{"file_path":"/tmp/primary.go","path":"/tmp/secondary.go"}`)
	if got != "**操作**: Write\n\n**文件**: `/tmp/primary.go`\n\n" {
		t.Errorf("unexpected output: %q", got)
	}
}

func TestFormatPermissionDetail_CommandAndFile(t *testing.T) {
	got := formatPermissionDetail("Bash", `{"command":"cat file.go","file_path":"/tmp/file.go"}`)
	if got != "**操作**: Bash\n\n**命令**: `cat file.go`\n\n**文件**: `/tmp/file.go`\n\n" {
		t.Errorf("unexpected output: %q", got)
	}
}

func TestFormatPermissionDetail_InvalidJSON(t *testing.T) {
	// Invalid JSON input — toolInput is not parsed
	got := formatPermissionDetail("Bash", "not json")
	if got != "**操作**: Bash\n\n" {
		t.Errorf("expected only toolName for invalid JSON, got %q", got)
	}
}

func TestFormatPermissionDetail_EmptyCommandAndPath(t *testing.T) {
	// Valid JSON but command and file_path/path are empty
	got := formatPermissionDetail("Bash", `{"command":"","file_path":"","path":""}`)
	if got != "**操作**: Bash\n\n" {
		t.Errorf("expected only toolName when all fields empty, got %q", got)
	}
}

func TestFormatPermissionDetail_OnlyToolInput(t *testing.T) {
	// Only toolInput provided, no toolName
	got := formatPermissionDetail("", `{"command":"ls"}`)
	if got != "**命令**: `ls`\n\n" {
		t.Errorf("expected only command, got %q", got)
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

func (m *mockDB) MergeConfigSubscribers(_ []string)                {}
func (m *mockDB) GetSubscribers() ([]common.SubscriberInfo, error) { return m.subscribers, nil }
func (m *mockDB) UpsertSubscriber(_, _, _, _ string) error         { return nil }
func (m *mockDB) DeleteSubscriber(_ string) error                  { return nil }

// testContext returns a context with a short timeout for tests.
func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// ============================================================================
// PushSessionEvent status-specific tests
// ============================================================================

func TestPushSessionEvent_Cancelled(t *testing.T) {
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

	// Cancelled status should attempt to push (will fail since no real server)
	PushSessionEvent("s1", "cancelled", "Test Session", "Preview", "/path", "", "")
}

func TestPushSessionEvent_PermissionPending(t *testing.T) {
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

	PushSessionEvent("s1", "permission_pending", "Test Session", "", "/path", "Bash", `{"command":"rm -rf /"}`)
}

// ============================================================================
// PushTaskEvent status-specific tests
// ============================================================================

func TestPushTaskEvent_Running(t *testing.T) {
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

	PushTaskEvent("t1", "running", "Test Task", "", "/path")
}

func TestPushTaskEvent_Failed(t *testing.T) {
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

	PushTaskEvent("t1", "failed", "Test Task", "Error preview", "/path")
}

func TestPushTaskEvent_Cancelled(t *testing.T) {
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

	PushTaskEvent("t1", "cancelled", "Test Task", "", "/path")
}

// ============================================================================
// sendToAllSubscribers edge cases
// ============================================================================

func TestSendToAllSubscribers_NoSubscribers(t *testing.T) {
	defer setupPushMode("feishu")()

	origChecker := clientChecker
	defer func() { clientChecker = origChecker }()
	RegisterClientChecker(&mockClientChecker{hasConnected: false})

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{subscribers: []common.SubscriberInfo{}} // empty

	if sendToAllSubscribers("Title", "Content") {
		t.Error("expected false when no subscribers")
	}
}

func TestSendToAllSubscribers_GetSubscribersError(t *testing.T) {
	defer setupPushMode("feishu")()

	origChecker := clientChecker
	defer func() { clientChecker = origChecker }()
	RegisterClientChecker(&mockClientChecker{hasConnected: false})

	origDB := db
	defer func() { db = origDB }()
	db = &mockDBWithCallback{
		getFn: func() ([]common.SubscriberInfo, error) {
			return nil, fmt.Errorf("db error")
		},
	}

	if sendToAllSubscribers("Title", "Content") {
		t.Error("expected false when GetSubscribers fails")
	}
}

func TestSendToAllSubscribers_NilManager(t *testing.T) {
	defer setupPushMode("feishu")()

	origChecker := clientChecker
	defer func() { clientChecker = origChecker }()
	RegisterClientChecker(&mockClientChecker{hasConnected: false})

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{subscribers: []common.SubscriberInfo{{UserID: "ou_user1"}}}

	SetManager(nil)

	if sendToAllSubscribers("Title", "Content") {
		t.Error("expected false when no manager")
	}
}

func TestSendToAllSubscribers_DingtalkMode(t *testing.T) {
	defer setupPushMode("dingtalk")()

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{subscribers: []common.SubscriberInfo{{UserID: "ou_user1"}}}

	if sendToAllSubscribers("Title", "Content") {
		t.Error("expected false in dingtalk mode")
	}
}

func TestSendToAllSubscribers_ClientConnected(t *testing.T) {
	defer setupPushMode("feishu")()

	origChecker := clientChecker
	defer func() { clientChecker = origChecker }()
	RegisterClientChecker(&mockClientChecker{hasConnected: true})

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{subscribers: []common.SubscriberInfo{{UserID: "ou_user1"}}}

	if sendToAllSubscribers("Title", "Content") {
		t.Error("expected false when client is connected")
	}
}

// ============================================================================
// truncateForFeishu additional edge cases
// ============================================================================

func TestTruncateForFeishu_ExactlyAtLimit(t *testing.T) {
	text := string(make([]rune, feishuPreviewMaxRunes))
	got := truncateForFeishu(text)
	if got != text {
		t.Error("text at exact limit should not be truncated")
	}
}

func TestTruncateForFeishu_OneOverLimit(t *testing.T) {
	text := string(make([]rune, feishuPreviewMaxRunes+1))
	got := truncateForFeishu(text)
	runes := []rune(got)
	if len(runes) != feishuPreviewMaxRunes+1 {
		t.Errorf("expected %d runes (truncated + ellipsis), got %d", feishuPreviewMaxRunes+1, len(runes))
	}
	if runes[len(runes)-1] != '…' {
		t.Error("expected ellipsis as last rune")
	}
}

// ============================================================================
// PushSessionEvent with nil DB
// ============================================================================

func TestPushSessionEvent_NilDB(t *testing.T) {
	defer setupPushMode("feishu")()

	origDB := db
	defer func() { db = origDB }()
	db = nil

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	mgr.started = true
	SetManager(mgr)
	defer SetManager(nil)

	if PushSessionEvent("s1", "completed", "Title", "Preview", "/path", "", "") {
		t.Error("expected false when db is nil")
	}
}

func TestPushTaskEvent_NilDB(t *testing.T) {
	defer setupPushMode("feishu")()

	origDB := db
	defer func() { db = origDB }()
	db = nil

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	mgr.started = true
	SetManager(mgr)
	defer SetManager(nil)

	if PushTaskEvent("t1", "completed", "Task", "Preview", "/path") {
		t.Error("expected false when db is nil")
	}
}
