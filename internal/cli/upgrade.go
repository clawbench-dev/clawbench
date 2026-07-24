package cli

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// RunUpgradeReplaceCommand handles the "clawbench upgrade-replace" subcommand.
// This is launched as a subprocess by the upgrade service.
// It:
// 1. Kills the parent process (the old server)
// 2. Waits for parent to die
// 3. Replaces the target binary with the new one
// 4. Starts the new binary with the original arguments
// 5. Cleans up temp directory
func RunUpgradeReplaceCommand(args []string) int {
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
	if runtime.GOOS == "windows" {
		killProcessForce(parentPID)
	} else {
		killProcessForce(parentPID)
	}

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

	// 3. Verify new binary exists and is executable
	if info, err := os.Stat(newBinPath); err != nil {
		slog.Error("upgrade-replace: new binary not found", "path", newBinPath, "error", err)
		return 1
	} else {
		slog.Info("upgrade-replace: new binary found", "path", newBinPath, "size", info.Size(), "mode", info.Mode())
	}

	// 4. Replace binary
	slog.Info("upgrade-replace: replacing binary", "new", newBinPath, "target", targetPath)

	if runtime.GOOS == "windows" {
		// Windows: rename old to .old first (can't replace running exe, but it's dead now)
		oldPath := targetPath + ".old"
		_ = os.Rename(targetPath, oldPath)
		if err := os.Rename(newBinPath, targetPath); err != nil {
			slog.Error("upgrade-replace: failed to rename", "error", err)
			return 1
		}
		_ = os.Remove(oldPath)
	} else {
		// Unix: mv new binary to target location
		if err := os.Rename(newBinPath, targetPath); err != nil {
			slog.Warn("upgrade-replace: rename failed, trying copy", "error", err)
			if err := copyFile(newBinPath, targetPath); err != nil {
				slog.Error("upgrade-replace: copy also failed", "error", err)
				return 1
			}
		}
		os.Chmod(targetPath, 0755)
	}

	slog.Info("upgrade-replace: binary replaced successfully")

	// 5. Clean up temp directory
	if tmpDir != "" {
		if err := os.RemoveAll(tmpDir); err != nil {
			slog.Warn("upgrade-replace: failed to clean temp dir", "path", tmpDir, "error", err)
		} else {
			slog.Info("upgrade-replace: temp dir cleaned", "path", tmpDir)
		}
	}

	// 6. Wait for port to be released
	slog.Info("upgrade-replace: waiting for port to be released...")
	time.Sleep(2 * time.Second)

	// 7. Start new binary with original server flags
	slog.Info("upgrade-replace: starting new binary", "target", targetPath, "args", serverArgs)

	cmd := exec.Command(targetPath, serverArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	setNewProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		slog.Error("upgrade-replace: failed to start new binary", "error", err)
		return 1
	}

	slog.Info("upgrade-replace: new binary started, waiting for it to initialize...", "pid", cmd.Process.Pid)

	// Wait a moment and check if the new process is still alive
	time.Sleep(3 * time.Second)
	if processAlive(cmd.Process.Pid) {
		slog.Info("upgrade-replace: new binary is running", "pid", cmd.Process.Pid)
	} else {
		slog.Error("upgrade-replace: new binary exited prematurely — check logs above for errors")
		return 1
	}

	return 0
}

// startsWithServerFlag checks if an arg is a server flag that should be passed through.
func startsWithServerFlag(arg string) bool {
	return strings.HasPrefix(arg, "--data-dir=") ||
		strings.HasPrefix(arg, "--port=") ||
		strings.HasPrefix(arg, "--host=")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := out.ReadFrom(in); err != nil {
		return err
	}

	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.Chmod(dst, info.Mode())
}
