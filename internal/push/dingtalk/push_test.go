package dingtalk

import (
	"strings"
	"testing"

	"clawbench/internal/model"
)

func TestShortSessionID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"standard UUID", "a1b2c3d4-e5f6-7890-abcd-ef1234567890", "a1b2c3d4"},
		{"short ID", "abcdef12", "abcdef12"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shortSessionID(tt.input)
			if got != tt.expected {
				t.Errorf("shortSessionID(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

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

// setupPushMode sets push_mode for testing and returns a restore function.
func setupPushMode(mode string) func() {
	orig := model.ConfigInstance.PushMode
	model.ConfigInstance.PushMode = mode
	return func() { model.ConfigInstance.PushMode = orig }
}

func TestPushSuppressed_WhenClientOnline(t *testing.T) {
	defer setupPushMode("dingtalk")()

	origChecker := clientChecker
	defer func() { clientChecker = origChecker }()

	RegisterClientChecker(&mockClientChecker{hasConnected: true})

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{subscribers: []SubscriberInfo{{UserID: "user1"}}}

	mgr := &Manager{cfg: &model.DingTalkConfig{AppKey: "k", AppSecret: "s", AgentID: 1}}
	mgr.started = true
	SetManager(mgr)
	defer SetManager(nil)

	// When a client is connected (user is viewing UI), DingTalk push should be suppressed
	if PushSessionEvent("s1", "completed", "Test", "Preview", "/path", "Bash", "") {
		t.Error("expected push to be suppressed when client is online")
	}
	if PushTaskEvent("t1", "completed", "Test Task", "Preview", "/path") {
		t.Error("expected push to be suppressed when client is online")
	}
}

func TestPushNotSuppressed_WhenNoClientOnline(t *testing.T) {
	defer setupPushMode("dingtalk")()

	origChecker := clientChecker
	defer func() { clientChecker = origChecker }()

	RegisterClientChecker(&mockClientChecker{hasConnected: false})

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{subscribers: []SubscriberInfo{{UserID: "user1"}}}

	mgr := &Manager{cfg: &model.DingTalkConfig{AppKey: "k", AppSecret: "s", AgentID: 1}}
	mgr.started = true
	SetManager(mgr)
	defer SetManager(nil)

	// When no client is connected, push should not be suppressed (will attempt send)
	PushSessionEvent("s1", "completed", "Test", "Preview", "/path", "Bash", "")
}

func TestPushNotSuppressed_NilClientChecker(t *testing.T) {
	defer setupPushMode("dingtalk")()

	origChecker := clientChecker
	defer func() { clientChecker = origChecker }()
	clientChecker = nil // no checker registered

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{subscribers: []SubscriberInfo{{UserID: "user1"}}}

	mgr := &Manager{cfg: &model.DingTalkConfig{AppKey: "k", AppSecret: "s", AgentID: 1}}
	mgr.started = true
	SetManager(mgr)
	defer SetManager(nil)

	// When clientChecker is nil, push should not be suppressed (will attempt send)
	PushSessionEvent("s1", "completed", "Test", "Preview", "/path", "Bash", "")
}

func TestPushSuppressed_InNativeMode(t *testing.T) {
	defer setupPushMode("native")()

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{subscribers: []SubscriberInfo{{UserID: "user1"}}}

	mgr := &Manager{cfg: &model.DingTalkConfig{AppKey: "k", AppSecret: "s", AgentID: 1}}
	mgr.started = true
	SetManager(mgr)
	defer SetManager(nil)

	// In native mode, push should be suppressed (sendToAllSubscribers returns false early)
	if PushSessionEvent("s1", "completed", "Test", "Preview", "/path", "Bash", "") {
		t.Error("expected false in native mode")
	}
	if PushTaskEvent("t1", "completed", "Test", "Preview", "/path") {
		t.Error("expected false in native mode")
	}
}

func TestPushSuppressed_InDisabledMode(t *testing.T) {
	defer setupPushMode("disabled")()

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{subscribers: []SubscriberInfo{{UserID: "user1"}}}

	mgr := &Manager{cfg: &model.DingTalkConfig{AppKey: "k", AppSecret: "s", AgentID: 1}}
	mgr.started = true
	SetManager(mgr)
	defer SetManager(nil)

	if PushSessionEvent("s1", "completed", "Test", "Preview", "/path", "Bash", "") {
		t.Error("expected false in disabled mode")
	}
	if PushTaskEvent("t1", "completed", "Test", "Preview", "/path") {
		t.Error("expected false in disabled mode")
	}
}

// mockClientChecker implements ConnectedClientChecker for testing.
type mockClientChecker struct {
	hasConnected bool
}

func (m *mockClientChecker) HasConnectedClients() bool { return m.hasConnected }

// mockDB implements DingtalkDB for testing.
type mockDB struct {
	subscribers []SubscriberInfo
}

func (m *mockDB) MergeConfigSubscribers(_ []string)         {}
func (m *mockDB) GetSubscribers() ([]SubscriberInfo, error) { return m.subscribers, nil }
func (m *mockDB) UpsertSubscriber(_, _, _, _ string) error  { return nil }
func (m *mockDB) DeleteSubscriber(_ string) error           { return nil }

func TestPushSessionEvent_UnknownStatus(t *testing.T) {
	defer setupPushMode("dingtalk")()

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{}

	mgr := &Manager{cfg: &model.DingTalkConfig{AppKey: "k", AppSecret: "s"}}
	mgr.started = true
	SetManager(mgr)
	defer SetManager(nil)

	if PushSessionEvent("s1", "unknown_status", "Title", "Preview", "/path", "Bash", "") {
		t.Error("expected false for unknown status")
	}
}

func TestPushTaskEvent_UnknownStatus(t *testing.T) {
	defer setupPushMode("dingtalk")()

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{}

	mgr := &Manager{cfg: &model.DingTalkConfig{AppKey: "k", AppSecret: "s"}}
	mgr.started = true
	SetManager(mgr)
	defer SetManager(nil)

	if PushTaskEvent("t1", "unknown_status", "Task", "Preview", "/path") {
		t.Error("expected false for unknown status")
	}
}

func TestPushSessionEvent_Cancelled(t *testing.T) {
	defer setupPushMode("dingtalk")()

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{}

	origChecker := clientChecker
	defer func() { clientChecker = origChecker }()
	RegisterClientChecker(&mockClientChecker{hasConnected: false})

	mgr := &Manager{cfg: &model.DingTalkConfig{AppKey: "k", AppSecret: "s"}}
	mgr.started = true
	SetManager(mgr)
	defer SetManager(nil)

	PushSessionEvent("s1", "cancelled", "Title", "Preview", "/path", "", "")
}

func TestPushSessionEvent_PermissionPending(t *testing.T) {
	defer setupPushMode("dingtalk")()

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{}

	origChecker := clientChecker
	defer func() { clientChecker = origChecker }()
	RegisterClientChecker(&mockClientChecker{hasConnected: false})

	mgr := &Manager{cfg: &model.DingTalkConfig{AppKey: "k", AppSecret: "s"}}
	mgr.started = true
	SetManager(mgr)
	defer SetManager(nil)

	PushSessionEvent("s1", "permission_pending", "Title", "Preview", "/path", "Bash", "")
}

func TestPushTaskEvent_Failed(t *testing.T) {
	defer setupPushMode("dingtalk")()

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{}

	origChecker := clientChecker
	defer func() { clientChecker = origChecker }()
	RegisterClientChecker(&mockClientChecker{hasConnected: false})

	mgr := &Manager{cfg: &model.DingTalkConfig{AppKey: "k", AppSecret: "s"}}
	mgr.started = true
	SetManager(mgr)
	defer SetManager(nil)

	PushTaskEvent("t1", "failed", "Task", "Preview", "/path")
}

func TestPushTaskEvent_Cancelled(t *testing.T) {
	defer setupPushMode("dingtalk")()

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{}

	origChecker := clientChecker
	defer func() { clientChecker = origChecker }()
	RegisterClientChecker(&mockClientChecker{hasConnected: false})

	mgr := &Manager{cfg: &model.DingTalkConfig{AppKey: "k", AppSecret: "s"}}
	mgr.started = true
	SetManager(mgr)
	defer SetManager(nil)

	PushTaskEvent("t1", "cancelled", "Task", "Preview", "/path")
}

func TestPushTaskEvent_Started(t *testing.T) {
	defer setupPushMode("dingtalk")()

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{}

	origChecker := clientChecker
	defer func() { clientChecker = origChecker }()
	RegisterClientChecker(&mockClientChecker{hasConnected: false})

	mgr := &Manager{cfg: &model.DingTalkConfig{AppKey: "k", AppSecret: "s"}}
	mgr.started = true
	SetManager(mgr)
	defer SetManager(nil)

	PushTaskEvent("t1", "running", "Task", "", "/path")
}

func TestSendToAllSubscribers_NoSubscribers(t *testing.T) {
	defer setupPushMode("dingtalk")()

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{subscribers: []SubscriberInfo{}}

	origChecker := clientChecker
	defer func() { clientChecker = origChecker }()
	RegisterClientChecker(&mockClientChecker{hasConnected: false})

	if sendToAllSubscribers("Title", "Markdown") {
		t.Error("expected false when no subscribers")
	}
}

func TestSendToAllSubscribers_NilManager(t *testing.T) {
	defer setupPushMode("dingtalk")()

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{subscribers: []SubscriberInfo{{UserID: "user1"}}}

	origMgr := GetManager()
	SetManager(nil)
	defer SetManager(origMgr)

	origChecker := clientChecker
	defer func() { clientChecker = origChecker }()
	RegisterClientChecker(&mockClientChecker{hasConnected: false})

	if sendToAllSubscribers("Title", "Markdown") {
		t.Error("expected false with nil manager")
	}
}

func TestSendToAllSubscribers_NativeMode(t *testing.T) {
	defer setupPushMode("native")()

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{subscribers: []SubscriberInfo{{UserID: "user1"}}}

	origChecker := clientChecker
	defer func() { clientChecker = origChecker }()
	RegisterClientChecker(&mockClientChecker{hasConnected: false})

	if sendToAllSubscribers("Title", "Markdown") {
		t.Error("expected false in native mode")
	}
}

func TestSendToAllSubscribers_DisabledMode(t *testing.T) {
	defer setupPushMode("disabled")()

	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{subscribers: []SubscriberInfo{{UserID: "user1"}}}

	if sendToAllSubscribers("Title", "Markdown") {
		t.Error("expected false in disabled mode")
	}
}

func TestTruncateForDingTalk_Empty(t *testing.T) {
	if got := truncateForDingTalk(""); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestTruncateForDingTalk_Short(t *testing.T) {
	input := "Hello, world!"
	if got := truncateForDingTalk(input); got != input {
		t.Errorf("expected %q, got %q", input, got)
	}
}

func TestTruncateForDingTalk_ExactLimit(t *testing.T) {
	input := strings.Repeat("x", dingtalkPreviewMaxRunes)
	if got := truncateForDingTalk(input); got != input {
		t.Errorf("expected no truncation at exact limit")
	}
}

func TestTruncateForDingTalk_OverLimit(t *testing.T) {
	input := strings.Repeat("x", dingtalkPreviewMaxRunes+100)
	got := truncateForDingTalk(input)
	if !strings.HasSuffix(got, "…") {
		t.Error("expected ellipsis suffix")
	}
	// Should be exactly dingtalkPreviewMaxRunes + 1 (ellipsis)
	if len([]rune(got)) != dingtalkPreviewMaxRunes+1 {
		t.Errorf("expected %d runes, got %d", dingtalkPreviewMaxRunes+1, len([]rune(got)))
	}
}

func TestTruncateForDingTalk_CJK(t *testing.T) {
	// CJK characters are multi-byte in UTF-8 but single runes
	input := strings.Repeat("中", dingtalkPreviewMaxRunes+50)
	got := truncateForDingTalk(input)
	if !strings.HasSuffix(got, "…") {
		t.Error("expected ellipsis suffix for CJK")
	}
	if len([]rune(got)) != dingtalkPreviewMaxRunes+1 {
		t.Errorf("expected %d runes, got %d", dingtalkPreviewMaxRunes+1, len([]rune(got)))
	}
}

func TestFormatPermissionDetail(t *testing.T) {
	tests := []struct {
		name      string
		toolName  string
		toolInput string
		want      string
	}{
		{
			name:      "empty toolName and toolInput",
			toolName:  "",
			toolInput: "",
			want:      "",
		},
		{
			name:      "only toolName",
			toolName:  "Bash",
			toolInput: "",
			want:      "**操作**: Bash\n\n",
		},
		{
			name:      "toolName with command",
			toolName:  "Bash",
			toolInput: `{"command":"rm -rf /tmp/test"}`,
			want:      "**操作**: Bash\n\n**命令**: `rm -rf /tmp/test`\n\n",
		},
		{
			name:      "toolName with file_path",
			toolName:  "Read",
			toolInput: `{"file_path":"/home/user/main.go"}`,
			want:      "**操作**: Read\n\n**文件**: `/home/user/main.go`\n\n",
		},
		{
			name:      "toolName with path instead of file_path",
			toolName:  "Read",
			toolInput: `{"path":"/home/user/main.go"}`,
			want:      "**操作**: Read\n\n**文件**: `/home/user/main.go`\n\n",
		},
		{
			name:      "toolName with both command and file_path",
			toolName:  "Bash",
			toolInput: `{"command":"cat foo.txt","file_path":"/home/user/foo.txt"}`,
			want:      "**操作**: Bash\n\n**命令**: `cat foo.txt`\n\n**文件**: `/home/user/foo.txt`\n\n",
		},
		{
			name:      "invalid JSON toolInput",
			toolName:  "Bash",
			toolInput: "not-json",
			want:      "**操作**: Bash\n\n",
		},
		{
			name:      "toolInput with no command or file_path",
			toolName:  "Bash",
			toolInput: `{"timeout":30}`,
			want:      "**操作**: Bash\n\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatPermissionDetail(tt.toolName, tt.toolInput)
			if got != tt.want {
				t.Errorf("formatPermissionDetail(%q, %q) = %q, want %q", tt.toolName, tt.toolInput, got, tt.want)
			}
		})
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

func TestGetPushMode_Dingtalk(t *testing.T) {
	orig := model.ConfigInstance.PushMode
	defer func() { model.ConfigInstance.PushMode = orig }()
	model.ConfigInstance.PushMode = "dingtalk"

	if mode := GetPushMode(); mode != "dingtalk" {
		t.Errorf("expected dingtalk, got %q", mode)
	}
}
