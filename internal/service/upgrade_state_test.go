package service

import "testing"

func TestUpgradeStateTransitions(t *testing.T) {
	ResetUpgradeState()

	// Initial state
	s := GetUpgradeState()
	if s.Phase != UpgradePhaseIdle {
		t.Errorf("initial phase = %q, want empty", s.Phase)
	}

	// Set versions
	SetUpgradeVersions("v1.0.0", "1.1.0")
	s = GetUpgradeState()
	if s.CurrentVer != "v1.0.0" || s.LatestVer != "1.1.0" {
		t.Errorf("versions = (%q, %q), want (v1.0.0, 1.1.0)", s.CurrentVer, s.LatestVer)
	}

	// Set state
	SetUpgradeState(UpgradePhaseDownloading, 50, "Downloading...")
	s = GetUpgradeState()
	if s.Phase != UpgradePhaseDownloading || s.Progress != 50 || s.Message != "Downloading..." {
		t.Errorf("state = (%q, %d, %q), want (downloading, 50, Downloading...)", s.Phase, s.Progress, s.Message)
	}

	// In progress check
	if !IsUpgradeInProgress() {
		t.Error("IsUpgradeInProgress() = false, want true")
	}

	// Set error
	SetUpgradeError("network timeout")
	s = GetUpgradeState()
	if s.Phase != UpgradePhaseFailed || s.Error != "network timeout" {
		t.Errorf("failed state = (%q, %q), want (failed, network timeout)", s.Phase, s.Error)
	}
	if IsUpgradeInProgress() {
		t.Error("IsUpgradeInProgress() = true after failure, want false")
	}

	// Reset
	ResetUpgradeState()
	s = GetUpgradeState()
	if s.Phase != UpgradePhaseIdle {
		t.Errorf("after reset phase = %q, want empty", s.Phase)
	}
}

func TestUpgradeBackupPath(t *testing.T) {
	ResetUpgradeState()

	SetUpgradeBackupPath("/path/to/clawbench.bak")
	s := GetUpgradeState()
	if s.BackupPath != "/path/to/clawbench.bak" {
		t.Errorf("backup path = %q, want /path/to/clawbench.bak", s.BackupPath)
	}

	ResetUpgradeState()
}

func TestUpgradeCompletedNotInProgress(t *testing.T) {
	ResetUpgradeState()

	SetUpgradeState(UpgradePhaseCompleted, 100, "Done")
	if IsUpgradeInProgress() {
		t.Error("completed should not be in progress")
	}

	SetUpgradeState(UpgradePhaseRestarting, 95, "Restarting...")
	if !IsUpgradeInProgress() {
		t.Error("restarting should be in progress")
	}

	ResetUpgradeState()
}

// ---------- SetUpgradePhase tests ----------

func TestSetUpgradePhase(t *testing.T) {
	ResetUpgradeState()
	defer ResetUpgradeState()

	SetUpgradePhase(UpgradePhaseChecking)
	s := GetUpgradeState()
	if s.Phase != UpgradePhaseChecking {
		t.Errorf("phase = %q, want %q", s.Phase, UpgradePhaseChecking)
	}

	SetUpgradePhase(UpgradePhaseDownloading)
	s = GetUpgradeState()
	if s.Phase != UpgradePhaseDownloading {
		t.Errorf("phase = %q, want %q", s.Phase, UpgradePhaseDownloading)
	}
}

// ---------- SetUpgradeShutdownFunc tests ----------

func TestSetUpgradeShutdownFunc(t *testing.T) {
	orig := upgradeShutdownFunc
	defer func() { upgradeShutdownFunc = orig }()

	called := false
	SetUpgradeShutdownFunc(func() {
		called = true
	})

	if upgradeShutdownFunc == nil {
		t.Error("upgradeShutdownFunc should not be nil after SetUpgradeShutdownFunc")
	}

	upgradeShutdownFunc()
	if !called {
		t.Error("upgradeShutdownFunc should have been called")
	}
}

func TestSetUpgradeShutdownFunc_Nil(t *testing.T) {
	orig := upgradeShutdownFunc
	defer func() { upgradeShutdownFunc = orig }()

	SetUpgradeShutdownFunc(nil)
	if upgradeShutdownFunc != nil {
		t.Error("upgradeShutdownFunc should be nil after setting nil")
	}
}

// ---------- SetUpgradeIsSupervised tests ----------

func TestSetUpgradeIsSupervised(t *testing.T) {
	orig := upgradeIsSupervised
	defer func() { upgradeIsSupervised = orig }()

	SetUpgradeIsSupervised(func() bool {
		return true
	})

	if upgradeIsSupervised == nil {
		t.Error("upgradeIsSupervised should not be nil after SetUpgradeIsSupervised")
	}

	result := upgradeIsSupervised()
	if !result {
		t.Error("upgradeIsSupervised should return true")
	}
}

func TestSetUpgradeIsSupervised_Nil(t *testing.T) {
	orig := upgradeIsSupervised
	defer func() { upgradeIsSupervised = orig }()

	SetUpgradeIsSupervised(nil)
	if upgradeIsSupervised != nil {
		t.Error("upgradeIsSupervised should be nil after setting nil")
	}
}

// ---------- IsUpgradeInProgress edge cases ----------

func TestIsUpgradeInProgress_AllPhases(t *testing.T) {
	ResetUpgradeState()
	defer ResetUpgradeState()

	tests := []struct {
		phase    UpgradePhase
		expected bool
	}{
		{UpgradePhaseIdle, false},
		{UpgradePhaseChecking, true},
		{UpgradePhaseDownloading, true},
		{UpgradePhaseExtracting, true},
		{UpgradePhaseBackingUp, true},
		{UpgradePhaseReplacing, true},
		{UpgradePhaseRestarting, true},
		{UpgradePhaseCompleted, false},
		{UpgradePhaseFailed, false},
	}

	for _, tt := range tests {
		ResetUpgradeState()
		SetUpgradePhase(tt.phase)
		result := IsUpgradeInProgress()
		if result != tt.expected {
			t.Errorf("IsUpgradeInProgress() for phase %q = %v, want %v", tt.phase, result, tt.expected)
		}
	}
}
