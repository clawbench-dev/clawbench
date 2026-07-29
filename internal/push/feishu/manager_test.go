package feishu

import (
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/push/common"
)

func TestGetManager_Nil(t *testing.T) {
	SetManager(nil)
	if mgr := GetManager(); mgr != nil {
		t.Error("expected nil manager")
	}
}

func TestManager_Reconfigure_InPlace(t *testing.T) {
	origCfg := &model.FeishuConfig{
		Enabled:   true,
		AppID:     "cli_test",
		AppSecret: "secret1",
		Users:     []string{"ou_user1"},
	}
	mgr := NewManager(origCfg)

	// Change only users — should be in-place
	newCfg := &model.FeishuConfig{
		Enabled:   true,
		AppID:     "cli_test",
		AppSecret: "secret1",
		Users:     []string{"ou_user1", "ou_user2"},
	}
	result := mgr.Reconfigure(newCfg)
	if result.NeedsRestart {
		t.Error("expected NeedsRestart=false for in-place update")
	}
}

func TestManager_Reconfigure_CredentialChange(t *testing.T) {
	origCfg := &model.FeishuConfig{
		Enabled:   true,
		AppID:     "cli_test",
		AppSecret: "secret1",
	}
	mgr := NewManager(origCfg)

	// Change AppID — should require restart
	newCfg := &model.FeishuConfig{
		Enabled:   true,
		AppID:     "cli_other",
		AppSecret: "secret1",
	}
	result := mgr.Reconfigure(newCfg)
	if !result.NeedsRestart {
		t.Error("expected NeedsRestart=true for app_id change")
	}

	// Change AppSecret — should require restart
	newCfg2 := &model.FeishuConfig{
		Enabled:   true,
		AppID:     "cli_test",
		AppSecret: "secret2",
	}
	result2 := mgr.Reconfigure(newCfg2)
	if !result2.NeedsRestart {
		t.Error("expected NeedsRestart=true for app_secret change")
	}
}

func TestManager_Reconfigure_EnabledChange(t *testing.T) {
	origCfg := &model.FeishuConfig{
		Enabled:   true,
		AppID:     "cli_test",
		AppSecret: "secret1",
	}
	mgr := NewManager(origCfg)

	newCfg := &model.FeishuConfig{
		Enabled:   false,
		AppID:     "cli_test",
		AppSecret: "secret1",
	}
	result := mgr.Reconfigure(newCfg)
	if !result.NeedsRestart {
		t.Error("expected NeedsRestart=true for enabled change")
	}
}

func TestRegisterDBAdapter(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()

	m := &mockDB{}
	RegisterDBAdapter(m)

	if db != m {
		t.Error("expected db to be set to mock")
	}
}

func TestManager_Start_MissingCredentials(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{}

	mgr := NewManager(&model.FeishuConfig{Enabled: true, AppID: "", AppSecret: ""})
	err := mgr.Start()
	if err != nil {
		t.Fatalf("expected nil error for missing credentials, got %v", err)
	}
	if mgr.started {
		t.Error("should not start with missing credentials")
	}
}

func TestManager_Start_NilDB(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = nil

	mgr := NewManager(&model.FeishuConfig{Enabled: true, AppID: "cli_test", AppSecret: "secret"})
	err := mgr.Start()
	if err != nil {
		t.Fatalf("expected nil error for nil DB, got %v", err)
	}
	if mgr.started {
		t.Error("should not start with nil DB")
	}
}

func TestManager_Stop_NotStarted(t *testing.T) {
	mgr := NewManager(&model.FeishuConfig{})
	mgr.Stop() // should not panic
}

func TestManager_Reconfigure_WithDB(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()

	mergeCalled := false
	db = &mockDBWithCallback{
		mergeFn: func(users []string) {
			mergeCalled = true
			if len(users) != 1 || users[0] != "ou_new_user" {
				t.Errorf("expected merge with [ou_new_user], got %v", users)
			}
		},
	}

	mgr := NewManager(&model.FeishuConfig{Enabled: true, AppID: "cli_test", AppSecret: "secret", Users: []string{"ou_old"}})
	result := mgr.Reconfigure(&model.FeishuConfig{Enabled: true, AppID: "cli_test", AppSecret: "secret", Users: []string{"ou_new_user"}})

	if result.NeedsRestart {
		t.Error("should not need restart for in-place update")
	}
	if !mergeCalled {
		t.Error("expected MergeConfigSubscribers to be called")
	}
}

func TestManager_Reconfigure_NilDB(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = nil

	mgr := NewManager(&model.FeishuConfig{Enabled: true, AppID: "cli_test", AppSecret: "secret", Users: []string{"ou_old"}})
	result := mgr.Reconfigure(&model.FeishuConfig{Enabled: true, AppID: "cli_test", AppSecret: "secret", Users: []string{"ou_new_user"}})

	if result.NeedsRestart {
		t.Error("should not need restart for in-place update")
	}
	// Should not panic when db is nil
}

func TestManager_SetStartedForTest(t *testing.T) {
	mgr := NewManager(&model.FeishuConfig{Enabled: true, AppID: "test", AppSecret: "test"})

	if mgr.started {
		t.Error("expected started=false initially")
	}

	mgr.SetStartedForTest(true)
	if !mgr.started {
		t.Error("expected started=true after SetStartedForTest(true)")
	}

	mgr.SetStartedForTest(false)
	if mgr.started {
		t.Error("expected started=false after SetStartedForTest(false)")
	}
}

// mockDBWithCallback is a mock common.PushDB with optional callback functions.
type mockDBWithCallback struct {
	mergeFn  func(users []string)
	getFn    func() ([]common.SubscriberInfo, error)
	upsertFn func(userID, conversationID, userName, source string) error
	deleteFn func(userID string) error
}

func (m *mockDBWithCallback) MergeConfigSubscribers(users []string) {
	if m.mergeFn != nil {
		m.mergeFn(users)
	}
}

func (m *mockDBWithCallback) GetSubscribers() ([]common.SubscriberInfo, error) {
	if m.getFn != nil {
		return m.getFn()
	}
	return nil, nil
}

func (m *mockDBWithCallback) UpsertSubscriber(userID, conversationID, userName, source string) error {
	if m.upsertFn != nil {
		return m.upsertFn(userID, conversationID, userName, source)
	}
	return nil
}

func (m *mockDBWithCallback) DeleteSubscriber(userID string) error {
	if m.deleteFn != nil {
		return m.deleteFn(userID)
	}
	return nil
}

// ============================================================================
// GetManager / SetManager tests
// ============================================================================

func TestSetManagerAndGetManager(t *testing.T) {
	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	SetManager(mgr)
	defer SetManager(nil)

	if got := GetManager(); got != mgr {
		t.Error("expected GetManager to return the same manager instance")
	}
}

func TestIsStarted_WithManager(t *testing.T) {
	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	mgr.started = true
	SetManager(mgr)
	defer SetManager(nil)

	if !IsStarted() {
		t.Error("expected IsStarted=true when manager is started")
	}
}

func TestIsStarted_ManagerNotStarted(t *testing.T) {
	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	mgr.started = false
	SetManager(mgr)
	defer SetManager(nil)

	if IsStarted() {
		t.Error("expected IsStarted=false when manager is not started")
	}
}

// ============================================================================
// Start lifecycle tests
// ============================================================================

func TestManager_Start_AlreadyStarted(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{}

	mgr := NewManager(&model.FeishuConfig{Enabled: true, AppID: "cli_test", AppSecret: "secret"})
	mgr.started = true

	err := mgr.Start()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should remain started without re-initializing
}

func TestManager_Start_Success(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()

	var mergedUsers []string
	db = &mockDBWithCallback{
		mergeFn: func(users []string) {
			mergedUsers = users
		},
	}

	mgr := NewManager(&model.FeishuConfig{
		Enabled:   true,
		AppID:     "cli_test",
		AppSecret: "secret",
		Users:     []string{"ou_user1"},
	})

	err := mgr.Start()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mgr.started {
		t.Error("expected manager to be started")
	}
	if len(mergedUsers) != 1 || mergedUsers[0] != "ou_user1" {
		t.Errorf("expected merge with [ou_user1], got %v", mergedUsers)
	}

	// Clean up without using Stop() to avoid lark SDK goroutine race.
	// Cancel context first, then close ws client, then clear state.
	if mgr.cancel != nil {
		mgr.cancel()
	}
	time.Sleep(50 * time.Millisecond)
	if mgr.wsClient != nil {
		mgr.wsClient.Close()
	}
	mgr.started = false
}

// ============================================================================
// Stop lifecycle tests
// ============================================================================

func TestManager_Stop_Started(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{}

	mgr := NewManager(&model.FeishuConfig{Enabled: true, AppID: "cli_test", AppSecret: "secret"})
	_ = mgr.Start()

	// Pre-cache a token to verify it's cleared on stop
	mgr.cachedToken = "t-to-clear"
	mgr.cachedExp = time.Now().Add(1 * time.Hour)

	// Cancel context first to stop the goroutine before calling Stop
	if mgr.cancel != nil {
		mgr.cancel()
	}
	time.Sleep(50 * time.Millisecond)

	mgr.Stop()

	if mgr.started {
		t.Error("expected started=false after Stop")
	}
	if mgr.cachedToken != "" {
		t.Error("expected cached token to be cleared on Stop")
	}
}

// ============================================================================
// RegisterClientChecker / RegisterSessionMessenger tests
// ============================================================================

func TestRegisterClientChecker(t *testing.T) {
	orig := clientChecker
	defer func() { clientChecker = orig }()

	checker := &mockClientChecker{hasConnected: true}
	RegisterClientChecker(checker)

	if clientChecker == nil {
		t.Error("expected clientChecker to be set")
	}
}

func TestRegisterSessionMessenger(t *testing.T) {
	orig := sessionMessenger
	defer func() { sessionMessenger = orig }()

	messenger := &mockSessionMessenger{}
	RegisterSessionMessenger(messenger)

	if sessionMessenger == nil {
		t.Error("expected sessionMessenger to be set")
	}
}

// ============================================================================
// NewManager tests
// ============================================================================

func TestNewManager_HTTPClient(t *testing.T) {
	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	if mgr.httpClient == nil {
		t.Error("expected httpClient to be initialized")
	}
	if mgr.cfg == nil {
		t.Error("expected cfg to be set")
	}
}
