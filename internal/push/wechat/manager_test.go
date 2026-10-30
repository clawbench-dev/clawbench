package wechat

import (
	"testing"

	"clawbench/internal/model"
)

func TestGetManager_NilByDefault(t *testing.T) {
	SetManager(nil)
	if GetManager() != nil {
		t.Fatal("expected nil manager after SetManager(nil)")
	}
	if IsStarted() {
		t.Fatal("IsStarted must be false without a manager")
	}
}

func TestSetManager_GetManager(t *testing.T) {
	mgr := NewManager(&model.WeChatConfig{BotToken: "tok"})
	SetManager(mgr)
	defer SetManager(nil)

	if GetManager() != mgr {
		t.Fatal("GetManager must return the manager set by SetManager")
	}
	if IsStarted() {
		t.Fatal("a manager that was never started must not report started")
	}
}

func TestReconfigure(t *testing.T) {
	base := model.WeChatConfig{BotToken: "tok", BotID: "bot1", BaseURL: "https://a", UserID: "u1"}
	mgr := NewManager(&base)

	t.Run("identical credentials do not need a restart", func(t *testing.T) {
		same := base
		same.UserID = "u2" // UserID is display-only
		if got := mgr.Reconfigure(&same); got.NeedsRestart {
			t.Error("only UserID changed; a restart is unnecessary")
		}
		if mgr.cfg.UserID != "u2" {
			t.Error("UserID must be updated in place")
		}
	})

	t.Run("token change needs a restart", func(t *testing.T) {
		next := base
		next.BotToken = "tok2"
		if got := mgr.Reconfigure(&next); !got.NeedsRestart {
			t.Error("a new bot token means a new client; a restart is required")
		}
	})

	t.Run("base url change needs a restart", func(t *testing.T) {
		next := base
		next.BaseURL = "https://b"
		if got := mgr.Reconfigure(&next); !got.NeedsRestart {
			t.Error("a new base URL means a new client; a restart is required")
		}
	})

	t.Run("bot id change needs a restart", func(t *testing.T) {
		next := base
		next.BotID = "bot2"
		if got := mgr.Reconfigure(&next); !got.NeedsRestart {
			t.Error("a different bot identity is a different session")
		}
	})
}

func TestStart_RefusesWithoutToken(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{}

	mgr := NewManager(&model.WeChatConfig{})
	if err := mgr.Start(); err != nil {
		t.Fatalf("missing credentials must not be an error, got %v", err)
	}
	if mgr.isStarted() {
		t.Fatal("a manager without a token must not report started")
	}
}

func TestStart_RefusesWithoutDB(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = nil

	mgr := NewManager(&model.WeChatConfig{BotToken: "tok"})
	if err := mgr.Start(); err != nil {
		t.Fatalf("missing DB adapter must not be an error, got %v", err)
	}
	if mgr.isStarted() {
		t.Fatal("a manager without a DB adapter must not report started")
	}
}

func TestStop_NotStartedIsNoop(t *testing.T) {
	mgr := NewManager(&model.WeChatConfig{BotToken: "tok"})
	mgr.Stop() // must not panic or block
	if mgr.isStarted() {
		t.Fatal("Stop on a stopped manager must leave it stopped")
	}
}

func TestStatus_States(t *testing.T) {
	mgr := startedManager(t)

	st := mgr.Status()
	if st.State != "running" {
		t.Errorf("expected running, got %q", st.State)
	}
	if !st.HasToken {
		t.Error("expected HasToken")
	}

	// A -14 expiry must be visible even though the manager is still "started"
	// until the monitor goroutine retires.
	mgr.statusMu.Lock()
	mgr.expired = true
	mgr.statusMu.Unlock()
	if st := mgr.Status(); st.State != "expired" {
		t.Errorf("expected expired, got %q", st.State)
	}

	mgr.Stop()
	if st := mgr.Status(); st.State != "stopped" {
		t.Errorf("expected stopped after Stop, got %q", st.State)
	}
}

// TestGenerationGuard is the guard on the generation counter: a monitor that
// belonged to a previous generation must not mutate the current manager's
// state. Without it, a monitor dying from an old token would mark a freshly
// restarted manager as stopped (and every push would be skipped).
func TestGenerationGuard(t *testing.T) {
	mgr := NewManager(&model.WeChatConfig{BotToken: "tok"})
	mgr.startMu.Lock()
	mgr.started = true
	mgr.gen = 5
	mgr.startMu.Unlock()

	// A stale monitor (gen 4) retires.
	mgr.retire(4)
	if !mgr.isStarted() {
		t.Fatal("a stale generation must not clear the current started flag")
	}

	mgr.markExpired(4)
	if st := mgr.Status(); st.State == "expired" {
		t.Fatal("a stale generation must not mark the current manager expired")
	}

	// The current generation does retire.
	mgr.retire(5)
	if mgr.isStarted() {
		t.Fatal("the current generation must clear the started flag")
	}
}
