package cli

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"clawbench/internal/platform"
)

// Test hooks: each maps to one side effect of the replacement so the post-kill
// sequence can be exercised without killing a real parent process, launching a
// real server or waiting in real time. Production uses the real functions.
var (
	replaceBinary     = platform.ReplaceBinary
	startServerBinary = startDetachedServer
	serverAlive       = processAlive
	upgradeWait       = time.Sleep
)

// RunUpgradeReplaceCommand handles the "clawbench upgrade-replace" subcommand.
// This is launched as a subprocess by the upgrade service.
// It:
//  1. Kills the parent process (the old server)
//  2. Waits for parent to die
//  3. Swaps in the new binary, then starts the target with the original
//     arguments — see performReplacement for why starting is unconditional
func RunUpgradeReplaceCommand(args []string) int { //nolint:gocyclo // upgrade-replace is inherently multi-step
	var newBinPath, targetPath, tmpDir string
	var serverArgs []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--new-bin":
			if i+1 < len(args) {
				newBinPath = args[i+1]
				i++
			}
		case "--target":
			if i+1 < len(args) {
				targetPath = args[i+1]
				i++
			}
		case "--tmp-dir":
			if i+1 < len(args) {
				tmpDir = args[i+1]
				i++
			}
		case "--data-dir", "--port", "--host":
			// Pass through server flags
			serverArgs = append(serverArgs, args[i])
			if i+1 < len(args) {
				i++
				serverArgs = append(serverArgs, args[i])
			}
		default:
			// Pass through --flag=value style args
			if startsWithServerFlag(args[i]) {
				serverArgs = append(serverArgs, args[i])
			}
		}
	}

	if newBinPath == "" || targetPath == "" {
		fmt.Fprintf(os.Stderr, "upgrade-replace: --new-bin and --target are required\n")
		return 1
	}

	parentPID := os.Getppid()
	slog.Info("upgrade-replace: starting", "parent_pid", parentPID, "new_bin", newBinPath, "target", targetPath,
		"tmp_dir", tmpDir, "server_args", serverArgs)

	// 1. Kill parent process
	killProcessForce(parentPID)

	// 2. Wait for parent to die (up to 30 seconds)
	slog.Info("upgrade-replace: waiting for parent to exit", "parent_pid", parentPID)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(parentPID) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if processAlive(parentPID) {
		slog.Warn("upgrade-replace: parent still alive after timeout, forcing kill")
		killProcessForce(parentPID)
		time.Sleep(500 * time.Millisecond)
	}

	slog.Info("upgrade-replace: parent is dead, proceeding with replacement")

	return performReplacement(newBinPath, targetPath, tmpDir, serverArgs)
}

// performReplacement swaps the new binary into place and (re)starts the server.
// It runs after the old process is dead, split out of RunUpgradeReplaceCommand
// so tests can drive it without killing a parent process.
//
// The central guarantee is that targetPath holds a runnable binary no matter
// how this function ends. platform.ReplaceBinary never moves the existing
// binary aside before its replacement exists, so after a failed swap
// targetPath is still the previous version — and this function starts it. An
// upgrade that cannot complete therefore leaves the service running on the old
// version, which the user can retry, rather than leaving the install directory
// without a binary and the service permanently down.
func performReplacement(newBinPath, targetPath, tmpDir string, serverArgs []string) int {
	swapped := swapBinary(newBinPath, targetPath)

	// Clean up regardless of the swap outcome: a failed swap would otherwise
	// leave a ~90MB download behind, and the next attempt re-downloads anyway.
	removeTempDir(tmpDir)

	// Wait for the port to be released
	slog.Info("upgrade-replace: waiting for port to be released...")
	upgradeWait(2 * time.Second)

	// Bring the service back. This runs on every path — successful swap, failed
	// swap, or a download that never made it — because targetPath always holds
	// a runnable binary (the new version, or the previous one if the swap did
	// not happen). Skipping it is what turned a failed upgrade into a service
	// that never came back.
	slog.Info("upgrade-replace: starting binary", "target", targetPath, "args", serverArgs)
	pid, startErr := startServerBinary(targetPath, serverArgs)
	if startErr != nil {
		slog.Error("upgrade-replace: failed to start binary", "target", targetPath, "error", startErr)
		return 1
	}

	slog.Info("upgrade-replace: binary started, waiting for it to initialize...", "pid", pid)

	// Verify the process is still alive
	upgradeWait(3 * time.Second)
	if !serverAlive(pid) {
		slog.Error("upgrade-replace: binary exited prematurely — check logs above for errors", "pid", pid)
		return 1
	}

	if !swapped {
		// The service is back, but on the previous version. Report failure so
		// this is not mistaken for a completed upgrade.
		return 1
	}

	slog.Info("upgrade-replace: upgrade complete", "pid", pid)
	return 0
}

// swapBinary puts the new binary at targetPath, reporting whether it succeeded.
// Every failure is logged and returns false; none of them leave targetPath
// without a binary, so the caller can still start the service.
func swapBinary(newBinPath, targetPath string) bool {
	info, err := os.Stat(newBinPath)
	if err != nil {
		slog.Error("upgrade-replace: new binary not found; the existing binary will be restarted",
			"path", newBinPath, "error", err)
		return false
	}
	slog.Info("upgrade-replace: new binary found", "path", newBinPath, "size", info.Size(), "mode", info.Mode())

	slog.Info("upgrade-replace: replacing binary", "new", newBinPath, "target", targetPath)
	if err := replaceBinary(newBinPath, targetPath); err != nil {
		slog.Error("upgrade-replace: replacement failed; the existing binary will be restarted",
			"error", err, "target", targetPath)
		return false
	}
	slog.Info("upgrade-replace: binary replaced successfully")
	return true
}

// startDetachedServer launches the binary with the given args and returns its
// PID. The child is put in its own process group so it outlives this helper.
func startDetachedServer(path string, args []string) (int, error) {
	cmd := exec.Command(path, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	setNewProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		return 0, err
	}
	return cmd.Process.Pid, nil
}

// removeTempDir deletes the upgrade's working directory, logging rather than
// failing: a leftover directory is a disk-space concern, not a correctness one.
func removeTempDir(tmpDir string) {
	if tmpDir == "" {
		return
	}
	if err := os.RemoveAll(tmpDir); err != nil {
		slog.Warn("upgrade-replace: failed to clean temp dir", "path", tmpDir, "error", err)
		return
	}
	slog.Info("upgrade-replace: temp dir cleaned", "path", tmpDir)
}

// startsWithServerFlag checks if an arg is a server flag that should be passed through.
func startsWithServerFlag(arg string) bool {
	return strings.HasPrefix(arg, "--data-dir=") ||
		strings.HasPrefix(arg, "--port=") ||
		strings.HasPrefix(arg, "--host=")
}
