package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// ---------- startsWithServerFlag tests ----------

func TestStartsWithServerFlag_DataDir(t *testing.T) {
	if !startsWithServerFlag("--data-dir=/home/user/.clawbench") {
		t.Error("expected --data-dir= to match")
	}
}

func TestStartsWithServerFlag_Port(t *testing.T) {
	if !startsWithServerFlag("--port=8080") {
		t.Error("expected --port= to match")
	}
}

func TestStartsWithServerFlag_Host(t *testing.T) {
	if !startsWithServerFlag("--host=0.0.0.0") {
		t.Error("expected --host= to match")
	}
}

func TestStartsWithServerFlag_NonMatchingArg(t *testing.T) {
	if startsWithServerFlag("--new-bin=/tmp/clawbench") {
		t.Error("expected --new-bin= not to match")
	}
	if startsWithServerFlag("--target=/usr/local/bin/clawbench") {
		t.Error("expected --target= not to match")
	}
	if startsWithServerFlag("--tmp-dir=/tmp/upgrade") {
		t.Error("expected --tmp-dir= not to match")
	}
	if startsWithServerFlag("positional-arg") {
		t.Error("expected positional arg not to match")
	}
	if startsWithServerFlag("--verbose") {
		t.Error("expected --verbose not to match")
	}
}

// ---------- processAlive tests ----------

func TestProcessAlive_CurrentProcess(t *testing.T) {
	// The current test process should be alive
	if !processAlive(os.Getpid()) {
		t.Error("current process should be alive")
	}
}

func TestProcessAlive_NonExistentPID(t *testing.T) {
	// Use a very high PID that almost certainly doesn't exist
	if processAlive(999999999) {
		t.Error("non-existent PID should not be alive")
	}
}

// ---------- setNewProcessGroup tests ----------

func TestSetNewProcessGroup_NoPanic(t *testing.T) {
	cmd := exec.Command("true") // no-op command
	setNewProcessGroup(cmd)
	// Just verify it doesn't panic and sets the attribute
	if runtime.GOOS != "windows" && cmd.SysProcAttr == nil {
		t.Error("expected SysProcAttr to be set on non-Windows")
	}
}

// ---------- RunUpgradeReplaceCommand tests ----------
//
// IMPORTANT: RunUpgradeReplaceCommand calls killProcessForce(os.Getppid()) after
// the required-args check, so we can only safely test cases that fail BEFORE
// that point (i.e., missing --new-bin or missing --target). Tests that provide
// both required args would kill the test runner's parent process.
//
// Everything after the kill lives in performReplacement, which is driven
// directly by the tests below with the process/exec/time side effects stubbed.

func TestRunUpgradeReplaceCommand_MissingNewBin(t *testing.T) {
	args := []string{"--target", "/usr/local/bin/clawbench"}
	code := RunUpgradeReplaceCommand(args)
	if code != 1 {
		t.Errorf("expected exit code 1 for missing --new-bin, got %d", code)
	}
}

func TestRunUpgradeReplaceCommand_MissingTarget(t *testing.T) {
	args := []string{"--new-bin", "/tmp/clawbench-new"}
	code := RunUpgradeReplaceCommand(args)
	if code != 1 {
		t.Errorf("expected exit code 1 for missing --target, got %d", code)
	}
}

func TestRunUpgradeReplaceCommand_ServerFlagPassthrough_EqualsForm(t *testing.T) {
	// --flag=value style server flags are parsed but we still exit 1
	// because required --new-bin and --target are missing
	args := []string{
		"--data-dir=/opt/data",
		"--port=9090",
		"--host=0.0.0.0",
	}
	code := RunUpgradeReplaceCommand(args)
	if code != 1 {
		t.Errorf("expected exit code 1 (missing required args), got %d", code)
	}
}

func TestRunUpgradeReplaceCommand_ServerFlagPassthrough_SpaceForm(t *testing.T) {
	// --flag value style passthrough for server flags
	args := []string{
		"--data-dir", "/opt/data",
		"--port", "9090",
		"--host", "0.0.0.0",
	}
	code := RunUpgradeReplaceCommand(args)
	if code != 1 {
		t.Errorf("expected exit code 1 (missing required args), got %d", code)
	}
}

func TestRunUpgradeReplaceCommand_NonServerFlagsIgnored(t *testing.T) {
	// Flags that are not server flags and not upgrade flags should be ignored
	args := []string{
		"--verbose",
		"--unknown-flag", "value",
		"--new-bin", "/tmp/clawbench-new",
	}
	code := RunUpgradeReplaceCommand(args)
	if code != 1 {
		t.Errorf("expected exit code 1 (missing --target), got %d", code)
	}
}

func TestRunUpgradeReplaceCommand_BothRequiredMissing(t *testing.T) {
	// Neither --new-bin nor --target provided
	code := RunUpgradeReplaceCommand([]string{})
	if code != 1 {
		t.Errorf("expected exit code 1 (no required args), got %d", code)
	}
}

// ---------- killProcessForce tests ----------

func TestKillProcessForce_DeadPID(t *testing.T) {
	// Killing a non-existent PID should not panic or return an error
	// (the function ignores the error from syscall.Kill)
	killProcessForce(999999999)
}

func TestKillProcessForce_CurrentProcess(t *testing.T) {
	// Sending SIGKILL to ourselves would be fatal, so just verify
	// the function exists and is callable. We test with a dead PID only.
	killProcessForce(999999998)
}

// ---------- RunUpgradeReplaceCommand: --tmp-dir parsing ----------

func TestRunUpgradeReplaceCommand_TmpDirParsedButStillMissingRequired(t *testing.T) {
	args := []string{
		"--tmp-dir", "/tmp/upgrade-work",
		"--target", "/usr/local/bin/clawbench",
	}
	code := RunUpgradeReplaceCommand(args)
	if code != 1 {
		t.Errorf("expected exit code 1 for missing --new-bin, got %d", code)
	}
}

func TestRunUpgradeReplaceCommand_AllFlagsButMissingTarget(t *testing.T) {
	args := []string{
		"--new-bin", "/tmp/clawbench-new",
		"--tmp-dir", "/tmp/upgrade-work",
	}
	code := RunUpgradeReplaceCommand(args)
	if code != 1 {
		t.Errorf("expected exit code 1 for missing --target, got %d", code)
	}
}

func TestRunUpgradeReplaceCommand_FlagWithoutValue(t *testing.T) {
	// --new-bin at the end with no value should result in empty newBinPath → exit 1
	args := []string{"--new-bin"}
	code := RunUpgradeReplaceCommand(args)
	if code != 1 {
		t.Errorf("expected exit code 1 when --new-bin has no value, got %d", code)
	}
}

func TestRunUpgradeReplaceCommand_TargetWithoutValue(t *testing.T) {
	args := []string{"--new-bin", "/tmp/new", "--target"}
	code := RunUpgradeReplaceCommand(args)
	if code != 1 {
		t.Errorf("expected exit code 1 when --target has no value, got %d", code)
	}
}

func TestRunUpgradeReplaceCommand_TmpDirWithoutValue(t *testing.T) {
	// --tmp-dir at the end with no value — still missing required args → exit 1
	args := []string{"--tmp-dir"}
	code := RunUpgradeReplaceCommand(args)
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestRunUpgradeReplaceCommand_DataDirFlagPassthrough(t *testing.T) {
	// --data-dir with space form should be parsed and passed through
	args := []string{
		"--data-dir", "/opt/data",
		"--new-bin", "/tmp/clawbench-new",
	}
	code := RunUpgradeReplaceCommand(args)
	if code != 1 {
		t.Errorf("expected exit code 1 (missing --target), got %d", code)
	}
}

// ---------- performReplacement tests ----------
//
// These drive the post-kill sequence with the process/exec/time side effects
// stubbed, so they can assert on the install directory's contents — the thing
// that actually decides whether the service can come back.

// replacementHarness stubs the side effects of performReplacement and records
// which binary was launched.
type replacementHarness struct {
	started   []string // paths passed to startServerBinary, in order
	startErr  error
	alive     bool
	sleepArgs []time.Duration
}

// install stubs the hooks for the duration of the test.
func (h *replacementHarness) install(t *testing.T) {
	t.Helper()

	origReplace := replaceBinary
	origStart := startServerBinary
	origAlive := serverAlive
	origWait := upgradeWait
	t.Cleanup(func() {
		replaceBinary = origReplace
		startServerBinary = origStart
		serverAlive = origAlive
		upgradeWait = origWait
	})

	startServerBinary = func(path string, _ []string) (int, error) {
		h.started = append(h.started, path)
		if h.startErr != nil {
			return 0, h.startErr
		}
		return 4242, nil
	}
	serverAlive = func(int) bool { return h.alive }
	upgradeWait = func(d time.Duration) { h.sleepArgs = append(h.sleepArgs, d) }
}

// TestPerformReplacement_Success verifies the happy path swaps the binary in
// and launches the target (now the new version).
func TestPerformReplacement_Success(t *testing.T) {
	dir := t.TempDir()
	newBin := filepath.Join(dir, "clawbench-new")
	target := filepath.Join(dir, "clawbench")
	if err := os.WriteFile(newBin, []byte("new-binary"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(target, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	h := &replacementHarness{alive: true}
	h.install(t)

	if code := performReplacement(newBin, target, "", nil); code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != "new-binary" {
		t.Errorf("target content = %q, want %q", got, "new-binary")
	}
	if len(h.started) != 1 || h.started[0] != target {
		t.Errorf("started = %v, want [%s]", h.started, target)
	}
}

// TestPerformReplacement_SwapFails_KeepsServiceAvailable is the regression test
// for the Windows cross-drive upgrade failure (issue #463).
//
// The old implementation renamed the target aside, then attempted a
// cross-device rename that failed, and returned without starting anything. The
// install directory was left with no binary at all and the service never came
// back. The invariant now is: a failed swap leaves the previous binary in place
// and this function still starts it.
func TestPerformReplacement_SwapFails_KeepsServiceAvailable(t *testing.T) {
	dir := t.TempDir()
	newBin := filepath.Join(dir, "clawbench-new")
	target := filepath.Join(dir, "clawbench")
	if err := os.WriteFile(newBin, []byte("new-binary"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(target, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	h := &replacementHarness{alive: true}
	h.install(t)
	replaceBinary = func(string, string) error {
		return errors.New("the system cannot move the file to a different disk drive")
	}

	code := performReplacement(newBin, target, "", nil)
	if code != 1 {
		t.Errorf("expected exit code 1 so the failed upgrade is not reported as success, got %d", code)
	}

	// The whole point: a binary still exists at the target path...
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("target must still exist after a failed swap: %v", err)
	}
	// ...and it is the previous version, so the service can run.
	if string(got) != "old-binary" {
		t.Errorf("target content = %q, want the previous binary %q", got, "old-binary")
	}

	// ...and it was actually started, so the service comes back.
	if len(h.started) != 1 || h.started[0] != target {
		t.Errorf("previous binary must be restarted after a failed swap, started = %v", h.started)
	}
}

// TestPerformReplacement_SwapFails_NoBinaryLeftBehind pins the invariant that
// the failure mode can never be "install directory has no binary". The old
// implementation renamed the target to .old and left it there, so the directory
// held only .old/.bak.
func TestPerformReplacement_SwapFails_NoBinaryLeftBehind(t *testing.T) {
	dir := t.TempDir()
	newBin := filepath.Join(dir, "clawbench-new")
	target := filepath.Join(dir, "clawbench")
	if err := os.WriteFile(newBin, []byte("new-binary"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(target, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	h := &replacementHarness{alive: true}
	h.install(t)
	replaceBinary = func(string, string) error { return errors.New("cross-device") }

	performReplacement(newBin, target, "", nil)

	if _, err := os.Stat(target); err != nil {
		t.Fatalf("install directory lost its binary: %v", err)
	}
}

// TestPerformReplacement_StartFailure verifies a binary that cannot be launched
// is reported as a failure.
func TestPerformReplacement_StartFailure(t *testing.T) {
	dir := t.TempDir()
	newBin := filepath.Join(dir, "clawbench-new")
	target := filepath.Join(dir, "clawbench")
	if err := os.WriteFile(newBin, []byte("new-binary"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(target, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	h := &replacementHarness{alive: true, startErr: errors.New("permission denied")}
	h.install(t)

	if code := performReplacement(newBin, target, "", nil); code != 1 {
		t.Errorf("expected exit code 1 when the binary cannot be started, got %d", code)
	}
}

// TestPerformReplacement_NewProcessDied verifies a binary that starts and then
// immediately exits is reported as a failure rather than a completed upgrade.
func TestPerformReplacement_NewProcessDied(t *testing.T) {
	dir := t.TempDir()
	newBin := filepath.Join(dir, "clawbench-new")
	target := filepath.Join(dir, "clawbench")
	if err := os.WriteFile(newBin, []byte("new-binary"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(target, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	h := &replacementHarness{alive: false}
	h.install(t)

	if code := performReplacement(newBin, target, "", nil); code != 1 {
		t.Errorf("expected exit code 1 when the started binary dies, got %d", code)
	}
}

// TestPerformReplacement_MissingNewBinary verifies a missing download is
// reported as a failure while still bringing the service back on the existing
// binary. The old code returned before the start step, so a failed download
// left the service down — the same "install directory has no running process"
// outcome as the cross-drive bug.
func TestPerformReplacement_MissingNewBinary(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "clawbench")
	if err := os.WriteFile(target, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	h := &replacementHarness{alive: true}
	h.install(t)

	code := performReplacement(filepath.Join(dir, "does-not-exist"), target, "", nil)
	if code != 1 {
		t.Errorf("expected exit code 1 for a missing new binary, got %d", code)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "old-binary" {
		t.Errorf("target must be untouched, content = %q err = %v", got, err)
	}
	if len(h.started) != 1 || h.started[0] != target {
		t.Errorf("existing binary must be restarted, started = %v", h.started)
	}
}

// TestPerformReplacement_CleansTempDirOnFailure verifies the download's working
// directory is removed even when the swap fails. The old code returned before
// the cleanup, leaking a ~90MB temp directory on every failed upgrade.
func TestPerformReplacement_CleansTempDirOnFailure(t *testing.T) {
	dir := t.TempDir()
	newBin := filepath.Join(dir, "clawbench-new")
	target := filepath.Join(dir, "clawbench")
	tmpDir := filepath.Join(dir, "clawbench-upgrade-work")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(newBin, []byte("new-binary"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(target, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	h := &replacementHarness{alive: true}
	h.install(t)
	replaceBinary = func(string, string) error { return errors.New("cross-device") }

	performReplacement(newBin, target, tmpDir, nil)

	if _, err := os.Stat(tmpDir); !os.IsNotExist(err) {
		t.Errorf("temp dir should be removed even when the swap fails, stat err = %v", err)
	}
}

// TestPerformReplacement_CleansTempDirOnSuccess verifies the same on the happy
// path.
func TestPerformReplacement_CleansTempDirOnSuccess(t *testing.T) {
	dir := t.TempDir()
	newBin := filepath.Join(dir, "clawbench-new")
	target := filepath.Join(dir, "clawbench")
	tmpDir := filepath.Join(dir, "clawbench-upgrade-work")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(newBin, []byte("new-binary"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(target, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	h := &replacementHarness{alive: true}
	h.install(t)

	if code := performReplacement(newBin, target, tmpDir, nil); code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if _, err := os.Stat(tmpDir); !os.IsNotExist(err) {
		t.Errorf("temp dir should be removed, stat err = %v", err)
	}
}
