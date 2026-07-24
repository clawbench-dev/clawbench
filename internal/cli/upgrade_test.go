package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
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

// ---------- copyFile tests ----------

func TestCopyFile_Success(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	content := []byte("hello upgrade test")
	srcPath := filepath.Join(srcDir, "src.bin")
	dstPath := filepath.Join(dstDir, "dst.bin")

	if err := os.WriteFile(srcPath, content, 0o755); err != nil {
		t.Fatalf("setup: write src: %v", err)
	}

	if err := copyFile(srcPath, dstPath); err != nil {
		t.Fatalf("copyFile: %v", err)
	}

	got, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}

	// Verify permissions preserved
	srcInfo, _ := os.Stat(srcPath)
	dstInfo, _ := os.Stat(dstPath)
	if srcInfo.Mode() != dstInfo.Mode() {
		t.Errorf("mode mismatch: got %v, want %v", dstInfo.Mode(), srcInfo.Mode())
	}
}

func TestCopyFile_SourceNotFound(t *testing.T) {
	dstDir := t.TempDir()
	dstPath := filepath.Join(dstDir, "dst.bin")

	err := copyFile("/nonexistent/path/to/src.bin", dstPath)
	if err == nil {
		t.Error("expected error for missing source file")
	}
}

func TestCopyFile_DestDirNotWritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission-based test not reliable on Windows")
	}

	srcDir := t.TempDir()
	content := []byte("test content")
	srcPath := filepath.Join(srcDir, "src.bin")
	if err := os.WriteFile(srcPath, content, 0o644); err != nil {
		t.Fatalf("setup: write src: %v", err)
	}

	dstDir := t.TempDir()
	// Remove write permission from destination directory
	if err := os.Chmod(dstDir, 0o555); err != nil {
		t.Fatalf("setup: chmod dst dir: %v", err)
	}
	defer os.Chmod(dstDir, 0o755) // restore for cleanup

	dstPath := filepath.Join(dstDir, "dst.bin")
	err := copyFile(srcPath, dstPath)
	if err == nil {
		t.Error("expected error when destination directory is not writable")
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
