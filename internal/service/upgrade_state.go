package service

import "sync"

// UpgradePhase represents the current phase of the upgrade process.
type UpgradePhase string

const (
	UpgradePhaseIdle        UpgradePhase = ""
	UpgradePhaseChecking    UpgradePhase = "checking"
	UpgradePhaseDownloading UpgradePhase = "downloading"
	UpgradePhaseExtracting  UpgradePhase = "extracting"
	UpgradePhaseBackingUp   UpgradePhase = "backing_up"
	UpgradePhaseReplacing   UpgradePhase = "replacing"
	UpgradePhaseRestarting  UpgradePhase = "restarting"
	UpgradePhaseCompleted   UpgradePhase = "completed"
	UpgradePhaseFailed      UpgradePhase = "failed"
)

// UpgradeState holds the current state of the upgrade process.
type UpgradeState struct {
	Phase      UpgradePhase `json:"phase"`
	CurrentVer string       `json:"current_version"`
	LatestVer  string       `json:"latest_version"`
	Progress   int          `json:"progress"`       // 0-100
	Message    string       `json:"message"`        // human-readable status
	BackupPath string       `json:"backup_path"`    // populated after backing_up
	Error      string       `json:"error,omitempty"`
}

var upgradeState = &UpgradeState{}
var upgradeMu sync.RWMutex

// GetUpgradeState returns a copy of the current upgrade state.
func GetUpgradeState() UpgradeState {
	upgradeMu.RLock()
	defer upgradeMu.RUnlock()
	return *upgradeState
}

// SetUpgradePhase sets only the phase.
func SetUpgradePhase(phase UpgradePhase) {
	upgradeMu.Lock()
	defer upgradeMu.Unlock()
	upgradeState.Phase = phase
}

// SetUpgradeState sets phase, progress, and message.
func SetUpgradeState(phase UpgradePhase, progress int, message string) {
	upgradeMu.Lock()
	defer upgradeMu.Unlock()
	upgradeState.Phase = phase
	upgradeState.Progress = progress
	upgradeState.Message = message
}

// SetUpgradeError sets phase to failed with an error message.
func SetUpgradeError(errMsg string) {
	upgradeMu.Lock()
	defer upgradeMu.Unlock()
	upgradeState.Phase = UpgradePhaseFailed
	upgradeState.Error = errMsg
}

// SetUpgradeVersions records current and latest version strings.
func SetUpgradeVersions(current, latest string) {
	upgradeMu.Lock()
	defer upgradeMu.Unlock()
	upgradeState.CurrentVer = current
	upgradeState.LatestVer = latest
}

// SetUpgradeBackupPath records the backup file path.
func SetUpgradeBackupPath(path string) {
	upgradeMu.Lock()
	defer upgradeMu.Unlock()
	upgradeState.BackupPath = path
}

// ResetUpgradeState clears all upgrade state.
func ResetUpgradeState() {
	upgradeMu.Lock()
	defer upgradeMu.Unlock()
	*upgradeState = UpgradeState{}
}

// IsUpgradeInProgress returns true if an upgrade is currently active.
func IsUpgradeInProgress() bool {
	upgradeMu.RLock()
	defer upgradeMu.RUnlock()
	p := upgradeState.Phase
	return p != "" && p != UpgradePhaseCompleted && p != UpgradePhaseFailed
}
