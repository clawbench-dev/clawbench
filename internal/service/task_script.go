package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"math"
	"os/exec"
	"strings"
	"time"

	"clawbench/internal/platform"
)

// DefaultScriptTimeout is the timeout applied by RunTaskScript when the caller
// passes a non-positive timeout.
const DefaultScriptTimeout = 300 * time.Second

// maxScriptTimeoutSeconds is the largest whole-second timeout that still fits
// in a time.Duration without wrapping negative (roughly 292 years).
const maxScriptTimeoutSeconds = math.MaxInt64 / int64(time.Second)

// scriptTimeoutDuration converts a per-task timeout in seconds to a Duration.
//
// A value large enough to overflow the nanosecond Duration wraps negative,
// which the `timeout <= 0` default check in RunTaskScript would then read as
// "unset" and silently replace with 300s — the opposite of what the caller
// asked for. Saturating at the Duration maximum keeps an enormous timeout
// effectively unbounded instead of collapsing it to the default.
func scriptTimeoutDuration(seconds int) time.Duration {
	if seconds <= 0 {
		return DefaultScriptTimeout
	}
	if int64(seconds) > maxScriptTimeoutSeconds {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(seconds) * time.Second
}

// taskScriptWaitDelay bounds how long the process's pipes may stay open after
// the process itself has exited. A spawned grandchild that inherited the pipes
// would otherwise block Wait() forever; after this delay Wait() forcibly closes
// the parent's pipe ends and unblocks.
const taskScriptWaitDelay = 5 * time.Second

// ScriptOutcome classifies the result of a scheduled task's pre-AI script.
type ScriptOutcome int

const (
	// ScriptSkipped means the script exited 0 with no output at all (stdout and
	// stderr both empty). The caller must skip the AI call and send no notification.
	ScriptSkipped ScriptOutcome = iota
	// ScriptProduced means the script exited 0 and produced output.
	ScriptProduced
	// ScriptFailed means a non-zero exit status, or the process could not be started.
	ScriptFailed
	// ScriptTimedOut means the timeout elapsed.
	ScriptTimedOut
	// ScriptCanceled means the caller's context was canceled (e.g. user pressed cancel).
	ScriptCanceled
)

// ScriptResult is the outcome of RunTaskScript.
type ScriptResult struct {
	Outcome  ScriptOutcome
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error // non-nil for ScriptFailed/ScriptTimedOut/ScriptCanceled when there is a cause
	// StdoutTruncated/StderrTruncated record that the capture hit
	// scriptOutputCap and dropped the tail. They come from cappedBuffer rather
	// than being re-derived from len(out) == cap, which cannot tell a stream
	// that was cut from one that happened to end exactly at the cap.
	StdoutTruncated bool
	StderrTruncated bool
}

// cappedBuffer is an io.Writer that retains at most cap bytes and discards the
// rest, recording that it had to.
//
// The cap is enforced WHILE the process writes, not after cmd.Run() returns:
// the downstream truncation (truncateScriptOutput) only runs once the whole
// output is already in memory, so a runaway script (`yes`, `find /`) would grow
// the server heap without bound and can OOM the process long before the 300s
// timeout fires. Bounding the buffer itself is what makes the capture safe.
//
// Write always reports the full len(p) even when it discards bytes. A short
// count would surface to the script as a broken pipe and change its exit
// status — and with it the outcome classification. os/exec writes each stream
// from a single goroutine, and stdout/stderr use separate writers, so no lock
// is needed.
type cappedBuffer struct {
	buf       bytes.Buffer
	cap       int
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if n == 0 {
		return 0, nil
	}
	if remaining := c.cap - c.buf.Len(); remaining > 0 {
		if n > remaining {
			c.buf.Write(p[:remaining])
			c.truncated = true
		} else {
			c.buf.Write(p)
		}
	} else {
		c.truncated = true
	}
	return n, nil
}

func (c *cappedBuffer) String() string { return c.buf.String() }

// RunTaskScript executes script in workDir through the platform shell.
// timeout <= 0 means DefaultScriptTimeout (300 * time.Second).
// ctx cancellation and timeout must both terminate the whole process group.
//
// stdout and stderr are captured into distinct buffers so the caller can decide
// whether the script produced any output. The emptiness test used for
// ScriptSkipped compares the *trimmed* output: a script that emits only
// whitespace (e.g. a bare trailing newline) counts as no output and is skipped.
func RunTaskScript(ctx context.Context, script string, workDir string, timeout time.Duration) ScriptResult {
	if timeout <= 0 {
		timeout = DefaultScriptTimeout
	}

	scriptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	if shell := platform.ResolveLoginShell(); shell != "" {
		// Pass the script as the -c argument; never interpolate it into a
		// larger shell string.
		cmd = exec.CommandContext(scriptCtx, shell, "-c", script)
	} else {
		// Windows fallback: ResolveLoginShell() returns empty on Windows
		// when $SHELL is not set — use cmd.exe instead.
		cmd = exec.CommandContext(scriptCtx, "cmd", "/C", script)
	}
	if workDir != "" {
		cmd.Dir = workDir
	}

	// Capture stdout and stderr separately: the skip decision requires both to
	// be empty independently, so they must not be merged. Both are capped at
	// scriptOutputCap bytes while the script writes (see cappedBuffer) so a
	// runaway producer cannot exhaust the heap before the timeout fires. The
	// same constant bounds the downstream truncation in truncateScriptOutput,
	// so the two caps cannot drift.
	var stdout, stderr cappedBuffer
	stdout.cap = scriptOutputCap
	stderr.cap = scriptOutputCap
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run the script in its own process group so cancel can reap the whole tree.
	setCmdProcessGroup(cmd)

	// Replace the default leader-only kill with a process-group kill: when a
	// spawned child inherits the pipes, killing only the leader leaves the
	// child holding the pipes open and cmd.Wait() blocks indefinitely.
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			killTaskScriptProcessGroup(cmd.Process)
		}
		return nil
	}
	cmd.WaitDelay = taskScriptWaitDelay

	runErr := cmd.Run()

	res := ScriptResult{
		Stdout:          stdout.String(),
		Stderr:          stderr.String(),
		ExitCode:        -1,
		StdoutTruncated: stdout.truncated,
		StderrTruncated: stderr.truncated,
	}

	// A script that leaves a backgrounded child holding the inherited pipes
	// makes cmd.Wait() report exec.ErrWaitDelay once WaitDelay closes them.
	// That is a pipe-plumbing artifact, not a script failure: the script
	// itself already exited successfully. Classify from the process's own exit
	// status in that case — otherwise a perfectly successful `foo &` script
	// would be reported as failed (and a silent one would stop being skipped),
	// inverting the feature's contract for any script that daemonizes.
	//
	// os/exec only substitutes ErrWaitDelay when the process's own wait error
	// was nil (see exec.go awaitGoroutines), so a non-zero exit still surfaces
	// as an *ExitError and never reaches this branch.
	waitDelayExpired, runErr := dropWaitDelayError(runErr)
	if waitDelayExpired && cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}

	switch {
	case ctx.Err() == context.Canceled:
		res.Outcome = ScriptCanceled
		res.Err = ctx.Err()
	case scriptCtx.Err() == context.DeadlineExceeded:
		res.Outcome = ScriptTimedOut
		res.Err = scriptCtx.Err()
	case runErr != nil:
		res.Outcome = ScriptFailed
		res.Err = runErr
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
		}
	default:
		res.ExitCode = 0
		if strings.TrimSpace(res.Stdout) == "" && strings.TrimSpace(res.Stderr) == "" {
			res.Outcome = ScriptSkipped
		} else {
			res.Outcome = ScriptProduced
		}
	}

	// Never log the script body at Info level — it may contain secrets.
	slog.Info("task script finished",
		slog.String("outcome", scriptOutcomeName(res.Outcome)),
		slog.Int("exit_code", res.ExitCode),
		slog.Bool("wait_delay_expired", waitDelayExpired),
	)

	return res
}

// dropWaitDelayError discards the exec.ErrWaitDelay that cmd.Wait() returns
// when WaitDelay force-closes a pipe an abandoned grandchild still held, so
// the caller classifies from the script's own exit status instead. Any other
// error is returned unchanged. The second result reports whether the WaitDelay
// branch was taken, for logging.
func dropWaitDelayError(runErr error) (bool, error) {
	if runErr != nil && errors.Is(runErr, exec.ErrWaitDelay) {
		return true, nil
	}
	return false, runErr
}

// scriptOutcomeName renders an outcome for logging.
func scriptOutcomeName(o ScriptOutcome) string {
	switch o {
	case ScriptSkipped:
		return "skipped"
	case ScriptProduced:
		return "produced"
	case ScriptFailed:
		return "failed"
	case ScriptTimedOut:
		return "timed_out"
	case ScriptCanceled:
		return "canceled"
	default:
		return "unknown"
	}
}
