package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"clawbench/internal/platform"
)

// DefaultScriptTimeout is the timeout applied by RunTaskScript when the caller
// passes a non-positive timeout.
const DefaultScriptTimeout = 300 * time.Second

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
}

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
	// be empty independently, so they must not be merged.
	var stdout, stderr bytes.Buffer
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
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: -1,
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
	)

	return res
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
