package dingtalk

import (
	"testing"

	"clawbench/internal/model"
)

func TestGetManager_Nil(t *testing.T) {
	// Reset global state
	SetManager(nil)
	if mgr := GetManager(); mgr != nil {
		t.Error("expected nil manager")
	}
}

func TestManager_Reconfigure_InPlace(t *testing.T) {
	origCfg := &model.DingTalkConfig{
		Enabled:   true,
		AppKey:    "key1",
		AppSecret: "secret1",
		AgentID:   100,
		Users:     []string{"user1"},
	}
	mgr := NewManager(origCfg)

	// Change only agent_id — should be in-place
	newCfg := &model.DingTalkConfig{
		Enabled:   true,
		AppKey:    "key1",
		AppSecret: "secret1",
		AgentID:   200,
		Users:     []string{"user1", "user2"},
	}
	result := mgr.Reconfigure(newCfg)
	if result.NeedsRestart {
		t.Error("expected NeedsRestart=false for in-place update")
	}
	if mgr.cfg.AgentID != 200 {
		t.Errorf("expected agent_id=200, got %d", mgr.cfg.AgentID)
	}
}

func TestManager_Reconfigure_CredentialChange(t *testing.T) {
	origCfg := &model.DingTalkConfig{
		Enabled:   true,
		AppKey:    "key1",
		AppSecret: "secret1",
		AgentID:   100,
	}
	mgr := NewManager(origCfg)

	// Change app_key — should require restart
	newCfg := &model.DingTalkConfig{
		Enabled:   true,
		AppKey:    "key2",
		AppSecret: "secret1",
		AgentID:   100,
	}
	result := mgr.Reconfigure(newCfg)
	if !result.NeedsRestart {
		t.Error("expected NeedsRestart=true for app_key change")
	}

	// Change app_secret — should require restart
	newCfg2 := &model.DingTalkConfig{
		Enabled:   true,
		AppKey:    "key1",
		AppSecret: "secret2",
		AgentID:   100,
	}
	result2 := mgr.Reconfigure(newCfg2)
	if !result2.NeedsRestart {
		t.Error("expected NeedsRestart=true for app_secret change")
	}
}

func TestManager_Reconfigure_EnabledChange(t *testing.T) {
	origCfg := &model.DingTalkConfig{
		Enabled:   true,
		AppKey:    "key1",
		AppSecret: "secret1",
		AgentID:   100,
	}
	mgr := NewManager(origCfg)

	// Change enabled — should require restart
	newCfg := &model.DingTalkConfig{
		Enabled:   false,
		AppKey:    "key1",
		AppSecret: "secret1",
		AgentID:   100,
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

	mgr := NewManager(&model.DingTalkConfig{Enabled: true, AppKey: "", AppSecret: ""})
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

	mgr := NewManager(&model.DingTalkConfig{Enabled: true, AppKey: "key", AppSecret: "secret"})
	err := mgr.Start()
	if err != nil {
		t.Fatalf("expected nil error for nil DB, got %v", err)
	}
	if mgr.started {
		t.Error("should not start with nil DB")
	}
}

func TestManager_Stop_NotStarted(t *testing.T) {
	mgr := NewManager(&model.DingTalkConfig{})
	mgr.Stop() // should not panic
}

func TestManager_Stop_Started(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{}

	mgr := NewManager(&model.DingTalkConfig{AppKey: "key", AppSecret: "secret"})
	mgr.started = true
	mgr.Stop()

	if mgr.started {
		t.Error("should not be started after stop")
	}
}

func TestManager_Reconfigure_WithDB(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()

	mergeCalled := false
	db = &mockDBWithCallback{
		mergeFn: func(users []string) {
			mergeCalled = true
			if len(users) != 1 || users[0] != "new_user" {
				t.Errorf("expected merge with [new_user], got %v", users)
			}
		},
	}

	mgr := NewManager(&model.DingTalkConfig{Enabled: true, AppKey: "key", AppSecret: "secret", Users: []string{"old"}})
	result := mgr.Reconfigure(&model.DingTalkConfig{Enabled: true, AppKey: "key", AppSecret: "secret", Users: []string{"new_user"}})

	if result.NeedsRestart {
		t.Error("should not need restart for in-place update")
	}
	if !mergeCalled {
		t.Error("expected MergeConfigSubscribers to be called")
	}
}

func TestManager_StartedManager_IsStartedTrue(t *testing.T) {
	origMgr := GetManager()
	defer SetManager(origMgr)

	mgr := NewManager(&model.DingTalkConfig{})
	mgr.started = true
	SetManager(mgr)

	if !IsStarted() {
		t.Error("should be true with started manager")
	}
}

func TestManager_StoppedManager_IsStartedFalse(t *testing.T) {
	origMgr := GetManager()
	defer SetManager(origMgr)

	mgr := NewManager(&model.DingTalkConfig{})
	mgr.started = false
	SetManager(mgr)

	if IsStarted() {
		t.Error("should be false with stopped manager")
	}
}

func TestRegisterClientChecker_SetsChecker(t *testing.T) {
	origChecker := clientChecker
	defer func() { clientChecker = origChecker }()

	m := &mockClientChecker{hasConnected: true}
	RegisterClientChecker(m)

	if clientChecker != m {
		t.Error("expected client checker to be set")
	}
}

func TestManager_Start_AlreadyStarted(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{}

	mgr := NewManager(&model.DingTalkConfig{Enabled: true, AppKey: "key", AppSecret: "secret"})
	mgr.started = true
	err := mgr.Start()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestManager_Start_StreamFailure(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{}

	mgr := NewManager(&model.DingTalkConfig{Enabled: true, AppKey: "bad_key", AppSecret: "bad_secret"})
	err := mgr.Start()
	// Stream connection will fail with invalid credentials, but Start() doesn't return fatal error
	if err != nil {
		t.Logf("Start returned: %v", err)
	}
}

func TestManager_Reconfigure_NilDB(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = nil

	mgr := NewManager(&model.DingTalkConfig{Enabled: true, AppKey: "key", AppSecret: "secret", Users: []string{"old"}})
	result := mgr.Reconfigure(&model.DingTalkConfig{Enabled: true, AppKey: "key", AppSecret: "secret", Users: []string{"new_user"}})

	if result.NeedsRestart {
		t.Error("should not need restart for in-place update")
	}
	// Should not panic when db is nil — MergeConfigSubscribers is skipped
}
