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
