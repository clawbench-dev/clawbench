package ai

import (
	"log/slog"
	"os"
	"runtime"
	"syscall"
)

// OrphanChildEnvVar is the environment variable injected into every AI
// subprocess spawned by ClawBench. On server startup, CleanupOrphans
// scans running processes for this marker and kills any orphans left
// behind by a previous server crash.
//
// This is simpler than PID-file tracking because:
//   - No Register/Unregister lifecycle — just set the env var on spawn
//   - No file I/O on every process create/destroy
//   - No cleanup needed on graceful shutdown
//   - No stale PID files to manage
//
// The env var is inert: no CLI tool reads or acts on it. It exists solely
// as a marker for orphan detection.
const OrphanChildEnvVar = "CLAWBENCH_CHILD=1"

// CleanupOrphans kills any AI subprocess left running after a previous
// server crash. Called once at startup, before any new subprocesses spawn.
//
// On Linux: scans /proc/<pid>/environ for CLAWBENCH_CHILD=1
// On macOS/Windows: falls back to checking /proc (macOS has no /proc,
// so the function is a no-op there — orphaned processes will exit when
// their stdin pipe closes after the parent dies).
func CleanupOrphans() {
	if runtime.GOOS != "linux" {
		// On non-Linux, rely on stdin-pipe-break to kill orphans.
		// macOS doesn't have /proc; Windows uses a different process model.
		return
	}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		slog.Debug("orphan_cleanup: cannot read /proc, skipping", "error", err)
		return
	}

	var killed int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pid := 0
		if _, err := parsePID(entry.Name(), &pid); err != nil {
			continue
		}
		if pid <= 1 {
			continue // skip kernel/init
		}

		// Read the process environment to check for our marker
		environPath := "/proc/" + entry.Name() + "/environ"
		data, err := os.ReadFile(environPath)
		if err != nil {
			continue // permission denied or process exited
		}

		if !hasClawBenchChildMarker(data) {
			continue
		}

		// Found an orphan — kill it
		proc, err := os.FindProcess(pid)
		if err != nil {
			continue
		}

		// Verify process is still alive before killing
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			continue
		}

		slog.Info("orphan_cleanup: killing orphan AI process", "pid", pid)
		if err := proc.Kill(); err != nil {
			slog.Warn("orphan_cleanup: failed to kill orphan", "pid", pid, "error", err)
		} else {
			// Reap the process to avoid zombies
			_, _ = proc.Wait()
			killed++
		}
	}

	if killed > 0 {
		slog.Info("orphan_cleanup: complete", "killed", killed)
	}
}

// hasClawBenchChildMarker checks if the /proc/<pid>/environ data
// contains the CLAWBENCH_CHILD=1 marker. The environ file uses
// null bytes (\0) as delimiters between entries.
func hasClawBenchChildMarker(environData []byte) bool {
	return bytesContainsSep(environData, []byte(OrphanChildEnvVar), 0)
}

// bytesContainsSep checks if data contains target as a segment
// delimited by sep byte. This avoids false positives from substring
// matches (e.g., "FOO_CLAWBENCH_CHILD=1" shouldn't match).
func bytesContainsSep(data, target []byte, sep byte) bool {
	targetLen := len(target)
	if targetLen == 0 {
		return true
	}

	for i := 0; i <= len(data)-targetLen; i++ {
		if data[i] != target[0] {
			continue
		}
		match := true
		for j := 1; j < targetLen; j++ {
			if data[i+j] != target[j] {
				match = false
				break
			}
		}
		if match {
			// Verify it's a complete segment (bounded by sep or at start/end)
			before := i == 0 || data[i-1] == sep
			after := i+targetLen == len(data) || data[i+targetLen] == sep
			if before && after {
				return true
			}
		}
	}
	return false
}

// parsePID parses a string as a positive integer PID.
func parsePID(s string, pid *int) (bool, error) { //nolint:unparam // error return kept for API consistency
	for _, c := range s {
		if c < '0' || c > '9' {
			return false, nil
		}
	}
	for _, c := range s {
		*pid = *pid*10 + int(c-'0')
	}
	return true, nil
}
