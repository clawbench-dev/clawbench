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
	// ErrNpmAuthRequired means a registry rejected the probe with 401/403. The
	// caller must stop probing and use that source, so the install shows the
	// real authentication error instead of silently bypassing a private
	// registry.
	ErrNpmAuthRequired = errors.New("npm registry requires authentication")
)

// npmProbeTimeout bounds one `npm view` probe. It is deliberately short: the
// probe only decides which source to use, and a slow source must not delay the
// install dialog for long.
const npmProbeTimeout = 3 * time.Second

// AgentInstallNodeMissingError reports that the command needs npm/npx and it is
// not on PATH, even after ClawBench tried to install a private Node.js. Named so
// the handler can turn it into actionable text instead of echoing
// "exec: npm: executable file not found".
type AgentInstallNodeMissingError struct {
	// Tool is the missing executable, e.g. "npm" or "npx".
	Tool string
	// Cause is why the automatic Node.js install did not produce it: the
	// download failed, the unpacked binary does not run, or it still is not on
	// PATH. Shown to the user — "npm is missing" alone leaves them guessing
	// whether the network or their machine is at fault.
	Cause error
}

func (e *AgentInstallNodeMissingError) Error() string {
	if e.Cause == nil {
		return fmt.Sprintf("%s was not found on PATH", e.Tool)
	}
	return fmt.Sprintf("%s was not found on PATH: %v", e.Tool, e.Cause)
}

func (e *AgentInstallNodeMissingError) Unwrap() error { return e.Cause }

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

	// agentInstallNodeRuntime installs a private Node.js when the machine has
	// none. A variable so tests do not download 30MB from a real mirror.
	agentInstallNodeRuntime = platform.EnsureNodeRuntime

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

// SetAgentInstallNodeRuntimeForTest replaces the Node.js installer and returns a
// restore function. Production code must not call this.
func SetAgentInstallNodeRuntimeForTest(fn func() (string, error)) func() {
	prev := agentInstallNodeRuntime
	agentInstallNodeRuntime = fn
	return func() { agentInstallNodeRuntime = prev }
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

	// npm/npx are the one dependency worth handling up front. Without this the
	// user gets "npm: command not found" buried in a wall of shell output, and a
	// machine that has never had Node.js — a fresh container, a Windows desktop,
	// a phone-only user's server — cannot recover from that message by itself.
	// So a missing runtime is installed rather than reported.
	var runtimeNote string
	if tool := nodeToolForCommand(command); tool != "" {
		if _, err := agentInstallLookPath(tool); err != nil {
			note, installErr := agentInstallNodeRuntime()
			if installErr != nil {
				return nil, &AgentInstallNodeMissingError{Tool: tool, Cause: installErr}
			}
			runtimeNote = note
			// The installer only reports success after putting the private
			// Node.js on PATH, so this can only fail if that PATH edit did not
			// take effect — worth naming instead of letting the command fail
			// with "command not found" again.
			if _, err := agentInstallLookPath(tool); err != nil {
				return nil, &AgentInstallNodeMissingError{
					Tool:  tool,
					Cause: fmt.Errorf("installed Node.js but %s is still not on PATH", tool),
				}
			}
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
		Output:     joinInstallOutput(runtimeNote, output),
		DurationMs: elapsed.Milliseconds(),
	}, nil
}

// joinInstallOutput puts the runtime note above the installer's own output, so
// the user can see that Node.js came along with the agent.
func joinInstallOutput(note, output string) string {
	output = strings.TrimSpace(output)
	switch {
	case note == "":
		return output
	case output == "":
		return note
	default:
		return note + "\n" + output
	}
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

// agentProbeRun runs one probe command. A variable so tests can drive the
// success/auth/failure branches without a real npm.
var agentProbeRun = runAgentInstallCommand

// SetAgentProbeRunForTest replaces the probe runner and returns a restore
// function. Production code must not call this.
func SetAgentProbeRunForTest(fn func(context.Context, string) (string, error)) func() {
	prev := agentProbeRun
	agentProbeRun = fn
	return func() { agentProbeRun = prev }
}

// ProbeNpmPackage reports whether a registry has a package, using
// `npm view <pkg> version --registry=<base>`.
//
// It returns ErrNpmAuthRequired when the registry rejects the request with
// 401/403: the caller must stop probing and use that source, because the user
// explicitly configured it and silently falling back to a public mirror would
// bypass that choice. Any other failure (404, timeout, network) means "try the
// next source".
func ProbeNpmPackage(ctx context.Context, pkg, registryBase string) error {
	if !IsSafeNpmProbeSpec(pkg) {
		return fmt.Errorf("unsupported package spec")
	}
	if !isSafeProbeBase(registryBase) {
		return fmt.Errorf("unsupported registry base")
	}

	probeCtx, cancel := context.WithTimeout(ctx, npmProbeTimeout)
	defer cancel()

	out, err := agentProbeRun(probeCtx, "npm view "+pkg+" version --registry="+registryBase)
	if err == nil {
		return nil
	}
	if isNpmAuthOutput(out) {
		return fmt.Errorf("%w: %s", ErrNpmAuthRequired, strings.TrimSpace(out))
	}
	return fmt.Errorf("npm view failed: %w: %s", err, strings.TrimSpace(out))
}

func isNpmAuthOutput(out string) bool {
	lower := strings.ToLower(out)
	for _, marker := range []string{"e401", "e403", "401 unauthorized", "403 forbidden", "unauthorized", "forbidden"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// IsSafeNpmProbeSpec accepts a single registry package spec: a name, an optional
// scope, and an optional exact version. Paths, tarballs, git URLs and range
// syntax are rejected rather than passed to a shell.
func IsSafeNpmProbeSpec(s string) bool {
	if s == "" || len(s) > 200 {
		return false
	}
	if strings.HasPrefix(s, ".") || strings.HasPrefix(s, "/") || strings.HasPrefix(s, "~") {
		return false
	}
	if strings.Contains(s, "://") || strings.Contains(s, "\\") || strings.HasSuffix(s, ".tgz") {
		return false
	}
	for _, c := range s {
		if !isNpmSpecChar(c) {
			return false
		}
	}
	return true
}

// isNpmSpecChar reports whether c is allowed in a package spec passed to npm:
// letters, digits, and the separators used by scoped names and versions.
func isNpmSpecChar(c rune) bool {
	if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
		return true
	}
	return c == '@' || c == '.' || c == '_' || c == '-' || c == '/'
}

// isSafeProbeBase rejects anything that could be interpreted by a shell. The
// source package already normalizes registries; this is defense in depth before
// the value is concatenated into a command.
func isSafeProbeBase(s string) bool {
	if strings.ContainsAny(s, " \t\r\n\"'`$&|;<>()\\") {
		return false
	}
	return strings.HasPrefix(s, "https://") ||
		strings.HasPrefix(s, "http://localhost") ||
		strings.HasPrefix(s, "http://127.0.0.1") ||
		strings.HasPrefix(s, "http://[::1]")
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
