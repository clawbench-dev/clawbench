package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/version"
)

// selfPathFileName is the record of the running binary's absolute path, kept
// under the data directory.
//
// Why a record at all: os.Executable() resolves /proc/self/exe on every call,
// so it reports where the binary is *now* — not where it was at startup. When
// an external package manager replaces the package while the service runs
// (npm retires the old package directory, then deletes it), that path becomes
// a dangling reference to a deleted inode: os.Executable() still returns it
// successfully (Go strips the " (deleted)" suffix), and the backup step then
// fails with a bare ENOENT that gives the user nothing to act on.
//
// The data directory is never touched by npm, so a path recorded there at
// startup survives the replacement and still points at a live binary.
const selfPathFileName = "self-path"

// WriteSelfPath records exe as the running binary's path.
//
// Best-effort: an unset or unwritable DataDir must not fail startup, since the
// upgrade path degrades gracefully to os.Executable() when no record exists.
func WriteSelfPath(exe string) error {
	if model.DataDir == "" {
		return nil
	}
	if err := os.MkdirAll(model.DataDir, 0o755); err != nil {
		return fmt.Errorf("failed to create data dir: %w", err)
	}
	path := filepath.Join(model.DataDir, selfPathFileName)
	// 0600: the record exposes the install layout; nothing else needs to read it.
	if err := os.WriteFile(path, []byte(exe), 0o600); err != nil {
		return fmt.Errorf("failed to write self-path: %w", err)
	}
	return nil
}

// ReadSelfPath returns the recorded binary path, or "" when no record exists
// or it cannot be read.
func ReadSelfPath() string {
	if model.DataDir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(model.DataDir, selfPathFileName))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// probeBinaryVersion runs `<path> --version` and returns the reported version.
//
// The probe is best-effort: a binary that cannot be executed (permissions,
// wrong architecture, missing file) yields an error so callers fall back to
// the normal upgrade path rather than acting on an unknown version.
func probeBinaryVersion(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("failed to probe version of %s: %w", path, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// shouldShortCircuit reports whether the binary already on disk is at (or
// ahead of) the target version, in which case downloading it again is
// redundant — the upgrade only needs to restart so the binary is loaded.
//
// A development build on disk never short-circuits: its version string carries
// no comparable release number, so the safe action is the normal download.
func shouldShortCircuit(diskVersion, targetVersion string) bool {
	if diskVersion == "" || targetVersion == "" {
		return false
	}
	if version.IsDevBuild(diskVersion) {
		return false
	}
	return version.CompareVersions(diskVersion, targetVersion) >= 0
}

// ResolveSelfBinary returns a path to the running binary that is usable for
// backup, in-place replacement and re-exec.
//
// Preference order:
//  1. The path recorded at startup (survives an external package-manager
//     replacement of the package directory).
//  2. os.Executable() — correct on a normal install, and the only option for
//     installs that predate the record.
//
// A candidate is only accepted if it still exists, so a stale record degrades
// to the next candidate rather than producing an inscrutable ENOENT later.
//
// Callers that REPLACE the binary (self-upgrade) must use this function: they
// have to write to the exact file that is currently executing. Restart callers
// must use ResolveRestartBinary instead.
func ResolveSelfBinary() (string, error) {
	recorded := ReadSelfPath()
	if recorded != "" {
		_, statErr := os.Stat(recorded)
		if statErr == nil {
			return recorded, nil
		}
		slog.Warn("upgrade: recorded self-path is not usable, falling back",
			"path", recorded, "error", statErr)
	}

	exe, err := upgradeExecutable()
	if err != nil {
		return "", fmt.Errorf("failed to resolve executable path: %w", err)
	}
	if exe == "" {
		return "", fmt.Errorf("failed to resolve executable path: empty path")
	}
	if _, statErr := os.Stat(exe); statErr != nil {
		return "", fmt.Errorf("running binary is not accessible at %s: %w", exe, statErr)
	}
	return exe, nil
}

// restartBinaryName is the command name looked up on PATH as a last resort.
const restartBinaryName = "clawbench"

// lookPath resolves a command name on PATH. A package-level var so tests can
// stub it without depending on the host's PATH.
var lookPath = exec.LookPath

// ResolveRestartBinary returns a path that can be re-exec'd to bring the
// service back after this process exits.
//
// It differs from ResolveSelfBinary in what it accepts, not in what it
// prefers:
//
//  1. ResolveSelfBinary() — the exact binary that is running. Still the best
//     choice when it exists.
//  2. The `clawbench` command on PATH. This is the escape hatch for the case
//     that broke real restarts: the process was started from an npm-managed
//     path, npm then reinstalled the package (retiring the old directory to
//     `@scope/.<name>-<random>` and deleting it), and BOTH candidates above
//     now point at deleted paths. The package manager's own shim
//     (`/usr/local/bin/clawbench`) is stable across reinstalls and resolves to
//     whatever build is installed *now* — which is also the build the user
//     just installed, so restarting through it loads the new version.
//
// Replacing the binary in place must NOT use this: an upgrade has to write the
// file that is currently mapped by the process, and re-resolving through PATH
// could target a different install.
//
// Every candidate is verified to be an executable regular file BEFORE it is
// returned. That check has to happen here rather than in the sentinel: the
// sentinel's `exec` replaces the shell, so a failed exec kills it before it
// can retry or report anything, and the user would see a restart that appeared
// to be accepted but never brought the service back. Failing here turns it
// into a 500 the UI can show.
//
// The returned error lists every candidate that was tried, because "restart
// failed" with no reason is what made the original failure invisible.
func ResolveRestartBinary() (string, error) {
	self, selfErr := ResolveSelfBinary()
	if selfErr == nil {
		execErr := checkExecutable(self)
		if execErr == nil {
			return self, nil
		}
		selfErr = execErr
	}

	fromPath, pathErr := lookPath(restartBinaryName)
	if pathErr == nil {
		// PATH entries may be relative; the sentinel runs in its own shell with
		// an inherited CWD, so pin the absolute path now.
		abs := fromPath
		if !filepath.IsAbs(abs) {
			if resolved, absErr := filepath.Abs(abs); absErr == nil {
				abs = resolved
			}
		}
		execErr := checkExecutable(abs)
		if execErr == nil {
			slog.Warn("restart: running binary is gone, falling back to the one on PATH",
				"running_binary_error", selfErr.Error(), "path", abs)
			return abs, nil
		}
		pathErr = execErr
	}

	// Both candidates failed; report both. errors.Join keeps BOTH wrapped, so
	// a caller doing errors.Is/As can inspect either one — a single %w plus a
	// %v would silently drop the second.
	return "", fmt.Errorf(
		"cannot locate an executable clawbench binary to restart: %w",
		errors.Join(selfErr, fmt.Errorf("%q on PATH: %w", restartBinaryName, pathErr)),
	)
}

// checkExecutable reports whether path is a regular file that can plausibly be
// executed. It is a cheap pre-flight, not a guarantee: it cannot detect a wrong
// architecture or a missing interpreter, but those are far rarer than the
// deleted-file case and they still surface on the sentinel's stderr.
//
// The execute-bit test is skipped on Windows: Go synthesizes 0666 for writable
// regular files there and never sets the 0o111 bits, so applying the POSIX rule
// would reject every valid binary. Windows decides executability by extension.
func checkExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s: is a directory", path)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%s: not executable (mode %v)", path, info.Mode().Perm())
	}
	return nil
}
