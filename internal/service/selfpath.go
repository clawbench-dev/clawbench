package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
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
func ResolveSelfBinary() (string, error) {
	recorded := ReadSelfPath()
	if recorded != "" {
		if _, err := os.Stat(recorded); err == nil {
			return recorded, nil
		} else {
			slog.Warn("upgrade: recorded self-path is not usable, falling back",
				"path", recorded, "error", err)
		}
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
