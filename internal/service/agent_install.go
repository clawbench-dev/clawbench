package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"clawbench/internal/platform"
)

// One-click agent install: run a backend's registry install command on the
// server and report what happened.
//
// The command is ALWAYS the BackendSpec's own InstallCmd (the handler resolves
// it from the registry), never something the client sent. That is what keeps
// this from becoming an arbitrary remote-execution endpoint: the only thing a
// caller chooses is which registered backend to install.
//
// Everything here runs on the machine serving ClawBench — a remote server or a
// container installs there, not on the phone or desktop looking at the UI.

const (
	// DefaultAgentInstallTimeout bounds one install. npm downloads a full
	// package tree over a possibly slow link, and `curl | bash` installers fetch
	// more; 10 minutes is generous without letting a stalled download pin the
	// slot forever.
	DefaultAgentInstallTimeout = 10 * time.Minute

	// agentInstallOutputCap bounds the captured output. Only the TAIL is kept:
	// installers print progress first and the reason for a failure last, and the
	// head is exactly the part a user does not need.
	agentInstallOutputCap = 16 * 1024
)

var (
	// ErrAgentInstallBusy is returned while another install is running. Two
	// concurrent `npm install -g` runs write the same global prefix and can
	// corrupt each other's package trees, so this is a hard refusal rather than
	// a queue.
	ErrAgentInstallBusy = errors.New("another agent install is already running")
	// ErrAgentInstallEmptyCommand means the backend has no built-in installer
	// (BackendSpec.InstallCmd is empty); the caller must fall back to manual
	// instructions.
	ErrAgentInstallEmptyCommand = errors.New("this backend has no built-in install command")
)

// AgentInstallNodeMissingError reports that the command needs npm/npx and it is
// not on PATH. Named so the handler can turn it into actionable text (install
// Node.js) instead of echoing "exec: npm: executable file not found".
type AgentInstallNodeMissingError struct {
	// Tool is the missing executable, e.g. "npm" or "npx".
	Tool string
}

func (e *AgentInstallNodeMissingError) Error() string {
	return fmt.Sprintf("%s was not found on PATH", e.Tool)
}

// AgentInstallCommandError wraps a command that ran and failed (non-zero exit,
// timeout, or failure to start) together with its output tail, which is the
// only thing that tells the user WHY (EACCES, 404 on the package, network
// timeout, a registry that requires auth).
type AgentInstallCommandError struct {
	Output string
	Err    error
}

func (e *AgentInstallCommandError) Error() string {
	if e.Output == "" {
		return e.Err.Error()
	}
	return e.Err.Error() + ": " + e.Output
}

func (e *AgentInstallCommandError) Unwrap() error { return e.Err }

// AgentInstallResult is what a successful install reports.
type AgentInstallResult struct {
	Backend    string
	Command    string
	Output     string
	DurationMs int64
}

var (
	// agentInstallRunning is the single-flight gate. A package-level bool rather
	// than a per-backend map: the conflict is on the shared global npm prefix,
	// so even two DIFFERENT backends must not install at once.
	agentInstallMu      sync.Mutex
	agentInstallRunning bool

	// agentInstallLookPath and agentInstallRun are variables so tests can drive
	// the missing-tool and failure branches without a real package manager.
	agentInstallLookPath = exec.LookPath
	agentInstallRun      = runAgentInstallCommand

	// agentInstallTimeout is a variable so a test can shorten it.
	agentInstallTimeout = DefaultAgentInstallTimeout
)

// SetAgentInstallRunForTest replaces the command runner and returns a restore
// function. Production code must not call this.
func SetAgentInstallRunForTest(fn func(ctx context.Context, command string) (string, error)) func() {
	prev := agentInstallRun
	agentInstallRun = fn
	return func() { agentInstallRun = prev }
}

// SetAgentInstallLookPathForTest replaces the PATH probe and returns a restore
// function. Production code must not call this.
func SetAgentInstallLookPathForTest(fn func(string) (string, error)) func() {
	prev := agentInstallLookPath
	agentInstallLookPath = fn
	return func() { agentInstallLookPath = prev }
}

// InstallAgent runs one backend's install command and returns its output tail.
//
// The command runs detached from any request context on purpose: a client that
// closes the drawer mid-install must not abort a half-written `npm install -g`
// (that leaves a broken package behind), so the only bound is the install
// timeout.
func InstallAgent(backendID, command string) (*AgentInstallResult, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, ErrAgentInstallEmptyCommand
	}

	agentInstallMu.Lock()
	if agentInstallRunning {
		agentInstallMu.Unlock()
		return nil, ErrAgentInstallBusy
	}
	agentInstallRunning = true
	agentInstallMu.Unlock()
	defer func() {
		agentInstallMu.Lock()
		agentInstallRunning = false
		agentInstallMu.Unlock()
	}()

	// npm/npx are the one dependency worth naming up front. Without this the
	// user gets "npm: command not found" buried in a wall of shell output and no
	// hint that the fix is installing Node.js.
	if tool := nodeToolForCommand(command); tool != "" {
		if _, err := agentInstallLookPath(tool); err != nil {
			return nil, &AgentInstallNodeMissingError{Tool: tool}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), agentInstallTimeout)
	defer cancel()

	start := time.Now()
	output, err := agentInstallRun(ctx, command)
	elapsed := time.Since(start)

	if err != nil {
		slog.Warn("agent install failed",
			"backend", backendID,
			"elapsed", elapsed,
			"error", err,
			"output_tail", output)
		return nil, &AgentInstallCommandError{Output: output, Err: err}
	}

	slog.Info("agent install finished",
		"backend", backendID,
		"elapsed", elapsed,
		"output_tail", output)
	return &AgentInstallResult{
		Backend:    backendID,
		Command:    command,
		Output:     output,
		DurationMs: elapsed.Milliseconds(),
	}, nil
}

// nodeToolForCommand reports the Node.js tool a command needs, or "".
//
// Only the FIRST token is inspected: these commands are simple prefixes from the
// registry (`npm install -g …`, `npx -y …`), and a `curl … | bash` installer has
// no Node dependency to check.
func nodeToolForCommand(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	switch fields[0] {
	case "npm", "npx":
		return fields[0]
	}
	return ""
}

// runAgentInstallCommand executes command through the user's login shell.
//
// The shell (not a direct exec) is required because several registry commands
// are pipelines (`curl -fsSL … | bash`), and splitting those by hand would be a
// second, divergent implementation of the command syntax.
func runAgentInstallCommand(ctx context.Context, command string) (string, error) {
	var cmd *exec.Cmd
	if shell := platform.ResolveLoginShell(); shell != "" {
		cmd = exec.CommandContext(ctx, shell, "-c", command)
	} else {
		// Windows without $SHELL: cmd.exe is the only guaranteed interpreter.
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	}
	cmd.Env = os.Environ()
	// No stdin: an installer that decides to prompt (npm asking for a login)
	// must fail fast instead of blocking until the timeout.
	cmd.Stdin = nil

	var output tailBuffer
	output.cap = agentInstallOutputCap
	// stdout and stderr share one buffer: npm writes progress and errors to
	// stderr, the shell writes command-not-found to stderr, and an installer's
	// own output to stdout. Keeping them in one stream preserves the order the
	// user would have seen in a terminal.
	cmd.Stdout = &output
	cmd.Stderr = &output

	// Own process group, so a cancelled install reaps the whole tree. Without
	// it, killing only the shell leaves the package manager running and holding
	// the pipes (cmd.Wait would block past the timeout).
	setCmdProcessGroup(cmd)
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			killTaskScriptProcessGroup(cmd.Process)
		}
		return nil
	}
	cmd.WaitDelay = taskScriptWaitDelay

	err := cmd.Run()
	// os/exec only substitutes ErrWaitDelay when the process itself exited
	// successfully (see exec.go awaitGoroutines): an installer that leaves a
	// background process holding the inherited pipes has still succeeded.
	if errors.Is(err, exec.ErrWaitDelay) {
		err = nil
	}
	return output.String(), err
}

// tailBuffer keeps the LAST cap bytes written to it.
//
// The counterpart of cappedBuffer (which keeps the head, because a gating
// script's stdout is content). Install output is diagnostics: the failure reason
// is at the end.
type tailBuffer struct {
	buf       []byte
	cap       int
	truncated bool
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if n == 0 || t.cap <= 0 {
		return n, nil
	}
	if n >= t.cap {
		t.buf = append(t.buf[:0], p[n-t.cap:]...)
		t.truncated = true
		return n, nil
	}
	if overflow := len(t.buf) + n - t.cap; overflow > 0 {
		t.buf = t.buf[overflow:]
		t.truncated = true
	}
	t.buf = append(t.buf, p...)
	return n, nil
}

// String returns the retained tail, trimmed. A truncated buffer is marked so the
// UI can say the output was cut rather than implying that is all it printed.
func (t *tailBuffer) String() string {
	s := strings.TrimSpace(string(t.buf))
	if t.truncated && s != "" {
		return "…\n" + s
	}
	return s
}
