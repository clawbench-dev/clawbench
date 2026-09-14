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
	Progress   int          `json:"progress"`    // 0-100
	Message    string       `json:"message"`     // human-readable status
	BackupPath string       `json:"backup_path"` // populated after backing_up
	// ErrorCode is a stable machine-readable failure identifier the frontend
	// maps to a localized, actionable message. Empty for generic failures.
	//
	// Deliberately NOT omitempty: the frontend merges updates with
	// Object.assign, which does not delete keys missing from the payload. If an
	// empty code were omitted, a stale code from a previous attempt would
	// survive a retry and mislabel an unrelated failure.
	ErrorCode string `json:"error_code"`
	Error     string `json:"error,omitempty"`
	// VerificationWarning is the human-readable text shown to the user when this
	// release cannot be fully verified, listing every reason (an uncheckable
	// signature, a missing integrity hash). Presentation only.
	VerificationWarning string `json:"verification_warning"`
	// VerificationIssues is the stable identity of the same problems, as sorted
	// issue codes. This is what the client echoes back and the service compares,
	// so it must not vary with wording, registry routing or failure mode.
	VerificationIssues string `json:"verification_issues"`
}

var (
	upgradeState = &UpgradeState{}
	upgradeMu    sync.RWMutex
)

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

// Upgrade error codes surfaced to the frontend. Kept stable across releases
// so the client can render a localized, actionable message.
const (
	// UpgradeErrInstallDirNotWritable means the running user cannot create
	// files in the directory holding the binary, so backup/replace will fail.
	UpgradeErrInstallDirNotWritable = "install_dir_not_writable"

	// UpgradeErrSelfPathUnresolved means no usable path to the running binary
	// could be found. Typical cause: the package directory was replaced or
	// deleted by an external package manager (e.g. npm install/update while the
	// service was running), so neither the recorded self-path nor
	// os.Executable() resolves to a live file. Restarting the service fixes it.
	UpgradeErrSelfPathUnresolved = "self_path_unresolved"

	// UpgradeErrRestartFailed means the version short-circuit could not restart
	// the service. The binary on disk is already at the target version, but the
	// process is still running the old one, so the upgrade has no effect until
	// a restart happens. Reported instead of leaving the phase at "restarting"
	// forever, which would show an endless spinner with no way forward.
	UpgradeErrRestartFailed = "restart_failed"

	// UpgradeErrUnverifiedNotConfirmed means the registry metadata says this
	// release cannot be fully verified, but the request did not carry the
	// acknowledgment the user would have given for exactly that warning. Either
	// the caller skipped the confirmation step, or the metadata changed between
	// the check and the start. The upgrade is refused so an unverified install
	// cannot happen without a decision; the client should re-check and re-ask.
	UpgradeErrUnverifiedNotConfirmed = "unverified_not_confirmed"
)

// SetUpgradeError sets phase to failed with an error message.
func SetUpgradeError(errMsg string) {
	SetUpgradeErrorCode("", errMsg)
}

// SetUpgradeErrorCode sets phase to failed with a machine-readable code and a
// human-readable message. An empty code means a generic failure.
func SetUpgradeErrorCode(code, errMsg string) {
	upgradeMu.Lock()
	defer upgradeMu.Unlock()
	upgradeState.Phase = UpgradePhaseFailed
	upgradeState.ErrorCode = code
	upgradeState.Error = errMsg
}

// SetUpgradeVersions records current and latest version strings.
func SetUpgradeVersions(current, latest string) {
	upgradeMu.Lock()
	defer upgradeMu.Unlock()
	upgradeState.CurrentVer = current
	upgradeState.LatestVer = latest
}

// SetUpgradeVerificationWarning records that this release cannot be fully
// verified, so the UI can tell the user before they commit. The message is for
// display; the fingerprint is what an acknowledgment is later compared against.
func SetUpgradeVerificationWarning(warning, issues string) {
	upgradeMu.Lock()
	defer upgradeMu.Unlock()
	upgradeState.VerificationWarning = warning
	upgradeState.VerificationIssues = issues
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
