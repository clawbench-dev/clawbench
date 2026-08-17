package ai

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// ---------------------------------------------------------------------------
// Crash diagnostics — collected after an agent process exits unexpectedly
// ---------------------------------------------------------------------------

// crashDiagWaitTimeout bounds how long collectCrashDiagnostics will block
// waiting for the agent process to exit. When the connection drops but the
// process is still alive (e.g. a respawn replaced it while a stale goroutine
// collected diagnostics), cmd.Process.Wait() would otherwise block forever and
// hang the session. Package-level so tests can shrink it.
var crashDiagWaitTimeout = 3 * time.Second

// crashDiagnostics holds crash info collected after an agent process exits unexpectedly.
type crashDiagnostics struct {
	ExitCode   int
	StderrTail string // last ~2KB of stderr
	WasAlive   bool   // was conn.Done() already closed?
	Uptime     time.Duration
	Signal     string // decoded signal name (e.g., "SIGKILL", "SIGSEGV") if killed by signal
	ParentPID  int    // PPid of the crashed process (from /proc/<pid>/status)
	VMRSSKB    int    // Resident memory at crash time (from /proc/<pid>/status)
	FDCount    int    // Open file descriptors at crash time (from /proc/<pid>/fd)
}

func (d crashDiagnostics) String() string {
	parts := make([]string, 0, 7)
	if d.ExitCode != 0 {
		exitStr := fmt.Sprintf("exit_code=%d", d.ExitCode)
		if sig := d.Signal; sig != "" {
			exitStr += " (" + sig + ")"
		} else if decoded := decodeExitCode(d.ExitCode); decoded != "" {
			exitStr += " (" + decoded + ")"
		}
		parts = append(parts, exitStr)
	}
	if d.Uptime > 0 {
		parts = append(parts, fmt.Sprintf("uptime=%s", d.Uptime.Round(time.Second)))
	}
	if d.ParentPID > 0 {
		parts = append(parts, fmt.Sprintf("ppid=%d", d.ParentPID))
	}
	if d.VMRSSKB > 0 {
		parts = append(parts, fmt.Sprintf("rss=%dMB", d.VMRSSKB/1024))
	}
	if d.FDCount > 0 {
		parts = append(parts, fmt.Sprintf("fds=%d", d.FDCount))
	}
	if d.StderrTail != "" {
		parts = append(parts, fmt.Sprintf("stderr: %s", d.StderrTail))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

// decodeExitCode maps common exit codes to human-readable descriptions.
// On Unix, exit codes > 128 indicate the process was killed by signal (128 + signal number).
func decodeExitCode(code int) string {
	switch code {
	case 1:
		return "general error"
	case 126:
		return "permission denied / not executable"
	case 127:
		return "command not found"
	case 128:
		return "invalid exit argument"
	case 129:
		return "SIGHUP"
	case 130:
		return "SIGINT (Ctrl+C)"
	case 137:
		return "SIGKILL (possible OOM killer)"
	case 139:
		return "SIGSEGV (segmentation fault)"
	case 141:
		return "SIGPIPE (broken pipe)"
	case 143:
		return "SIGTERM"
	default:
		if code > 128 {
			sigNum := code - 128
			return fmt.Sprintf("signal %d", sigNum)
		}
		return ""
	}
}

// collectCrashDiagnostics gathers exit code and stderr from the crashed agent process.
// Must be called after Prompt() returns a peer-disconnect error.
func (c *ACPConn) collectCrashDiagnostics() crashDiagnostics {
	var diag crashDiagnostics

	c.mu.Lock()
	cmd := c.cmd
	conn := c.conn
	startedAt := c.startedAt
	c.mu.Unlock()

	// Uptime
	if !startedAt.IsZero() {
		diag.Uptime = time.Since(startedAt)
	}

	// Check if the connection's Done channel is closed (confirming peer disconnect)
	if conn != nil {
		select {
		case <-conn.Done():
			diag.WasAlive = false
		default:
			diag.WasAlive = true
		}
	}

	if cmd == nil || cmd.Process == nil {
		return diag
	}

	// Snapshot /proc/<pid>/status and FD count while the process still exists.
	// This data is only available between the signal and Wait() returning,
	// so we read it before calling Wait() which reaps the process.
	pid := cmd.Process.Pid
	diag.ParentPID, diag.VMRSSKB, _ = readProcStatus(pid)
	if fds, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid)); err == nil {
		diag.FDCount = len(fds)
	}

	// Use cmdWaitOnce to safely call Wait() exactly once, caching the result.
	// This avoids a race between collectCrashDiagnostics and spawnLocked both
	// calling Wait() on the same process.
	c.waitProcessExitOnce(cmd)

	if c.cmdWaitState != nil {
		diag.ExitCode = c.cmdWaitState.ExitCode()
		// Check if the process was killed by a signal (Unix-specific)
		if ws, ok := c.cmdWaitState.Sys().(syscall.WaitStatus); ok {
			if ws.Signaled() {
				diag.Signal = ws.Signal().String()
			}
		}
	}

	// Extract stderr from the strings.Builder
	c.mu.Lock()
	if c.cmd != nil {
		if sb, ok := c.cmd.Stderr.(*strings.Builder); ok {
			stderr := sb.String()
			if len(stderr) > 2048 {
				stderr = "..." + stderr[len(stderr)-2048:]
			}
			diag.StderrTail = stderr
		}
	}
	c.mu.Unlock()

	return diag
}

// waitProcessExitOnce calls cmd.Process.Wait() exactly once (via cmdWaitOnce)
// and caches the resulting state, but bounds the wait by crashDiagWaitTimeout.
// collectCrashDiagnostics is a best-effort diagnostic helper; if the process is
// still running (e.g. it was superseded by a respawn and this is a stale
// goroutine collecting diagnostics), blocking forever would hang the caller
// (Prompt / setConfigOptionWithCrashCheck) on a healthy process. Returning
// partial diagnostics is preferable.
func (c *ACPConn) waitProcessExitOnce(cmd *exec.Cmd) {
	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		c.cmdWaitOnce.Do(func() {
			if state, err := cmd.Process.Wait(); err == nil {
				c.cmdWaitState = state
			}
		})
	}()

	select {
	case <-waitDone:
		// process exited (or Wait was already performed)
	case <-time.After(crashDiagWaitTimeout):
		// process still alive — don't block the caller
	}
}

// readProcStatus reads PPid and VmRSS from /proc/<pid>/status.
// Returns (ppid, vmRSSKB, error). Best-effort; returns zeros on failure.
func readProcStatus(pid int) (ppid int, vmRSSKB int, err error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, 0, err
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, "PPid:"); ok {
			_, _ = fmt.Sscanf(rest, "%d", &ppid)
		} else if rest, ok := strings.CutPrefix(line, "VmRSS:"); ok {
			_, _ = fmt.Sscanf(rest, "%d", &vmRSSKB)
		}
	}
	return ppid, vmRSSKB, nil
}
