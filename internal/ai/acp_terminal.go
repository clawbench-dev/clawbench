package ai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/platform"
)

// ---------------------------------------------------------------------------
// Terminal session management — ACP terminal/* method implementations
// ---------------------------------------------------------------------------

// terminalSession tracks an executing terminal command.
type terminalSession struct {
	id        string
	cmd       *exec.Cmd
	cancel    context.CancelFunc // cancel the command context
	output    bytes.Buffer
	truncated bool
	done      chan struct{} // closed when command exits
	exitCode  *int
	signal    *string
	mu        sync.Mutex
}

// CreateTerminal creates a terminal session by executing the command via os/exec.
func (c *ClawBenchACPClient) CreateTerminal(ctx context.Context, req acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	// Use Background context — the command must outlive the JSON-RPC request context.
	// Apply a generous timeout so commands don't run forever.
	cmdCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)

	var cmd *exec.Cmd
	if shell := platform.ResolveLoginShell(); shell != "" {
		cmd = exec.CommandContext(cmdCtx, shell, "-c", req.Command)
	} else {
		// Windows fallback: ResolveLoginShell() returns empty on Windows
		// when $SHELL is not set — use cmd.exe instead.
		cmd = exec.CommandContext(cmdCtx, "cmd", "/C", req.Command)
	}
	// Resolve working directory for the command:
	// 1. Use req.Cwd if the ACP agent provided it (some agents pass cwd per-command).
	// 2. Fallback to connRef.cwd — the project working directory locked on first
	//    ensureAliveWithSession call. This is necessary because CodeBuddy's ACP
	//    implementation omits Cwd in terminal/create requests, so without this
	//    fallback, commands would inherit the ClawBench server's CWD instead of
	//    the project directory.
	if req.Cwd != nil && *req.Cwd != "" {
		cmd.Dir = *req.Cwd
	} else if c.connRef != nil {
		if connCwd := c.connRef.Cwd(); connCwd != "" {
			cmd.Dir = connCwd
			slog.Info("acp terminal: req.Cwd is nil/empty, using connRef.cwd as fallback",
				slog.String("command", req.Command),
				slog.String("fallback_cwd", connCwd))
		}
	}
	if cmd.Dir == "" {
		slog.Warn("acp terminal: no Cwd available — command will inherit server CWD",
			slog.String("command", req.Command))
	}
	if len(req.Env) > 0 {
		env := os.Environ()
		for _, e := range req.Env {
			env = append(env, e.Name+"="+e.Value)
		}
		cmd.Env = env
	}

	termID := fmt.Sprintf("term-%d", c.termSeq.Add(1))
	ts := &terminalSession{
		id:     termID,
		cmd:    cmd,
		cancel: cancel,
		done:   make(chan struct{}),
	}

	// Capture combined stdout+stderr
	cmd.Stdout = &ts.output
	cmd.Stderr = &ts.output

	// Detach the terminal command from the server's controlling terminal.
	// Without Setsid:true, a command like `sudo` would prompt for a password
	// on the ClawBench server console via /dev/tty and block the whole
	// session. A new session has no controlling terminal, so sudo fails fast
	// with "no tty present" instead of hanging. This also enables
	// killProcessGroup to reach the entire process tree on cancellation.
	setProcessGroup(cmd)

	// Replace the default leader-only kill with a process-group kill: when
	// a spawned child inherits the pipes, killing only the leader leaves the
	// child holding the pipes open and cmd.Wait() blocks indefinitely.
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			killProcessGroup(cmd.Process)
		}
		return nil
	}
	// Bound how long pipes may stay open after the process exits when a
	// spawned child inherited them. Wait() forcibly closes the parent's pipe
	// ends after the delay, unblocking the output buffer read.
	cmd.WaitDelay = streamWaitDelay

	if err := cmd.Start(); err != nil {
		cancel() // release the context timeout
		return acp.CreateTerminalResponse{}, fmt.Errorf("start command: %w", err)
	}

	c.termMu.Lock()
	c.terminals[termID] = ts
	c.termMu.Unlock()

	// Background goroutine: wait for exit and apply output byte limit
	go func() {
		defer close(ts.done)
		waitErr := cmd.Wait()

		ts.mu.Lock()
		defer ts.mu.Unlock()

		if waitErr != nil {
			var exitErr *exec.ExitError
			if errors.As(waitErr, &exitErr) {
				code := exitErr.ExitCode()
				ts.exitCode = &code
			} else {
				// Killed by signal or other error
				msg := waitErr.Error()
				ts.signal = &msg
			}
		} else {
			code := 0
			ts.exitCode = &code
		}

		// Apply output byte limit
		if req.OutputByteLimit != nil && *req.OutputByteLimit > 0 {
			limit := *req.OutputByteLimit
			if ts.output.Len() > limit {
				overflow := ts.output.Len() - limit
				ts.output.Next(overflow)
				ts.truncated = true
			}
		}
	}()

	slog.Debug("acp: terminal created", "terminal_id", termID, "command", req.Command)
	return acp.CreateTerminalResponse{TerminalId: termID}, nil
}

// KillTerminal kills a terminal session.
func (c *ClawBenchACPClient) KillTerminal(_ context.Context, req acp.KillTerminalRequest) (acp.KillTerminalResponse, error) {
	c.termMu.Lock()
	ts, ok := c.terminals[req.TerminalId]
	c.termMu.Unlock()
	if !ok {
		return acp.KillTerminalResponse{}, nil
	}
	ts.cancel() // cancel the command context → Process.Kill via CommandContext
	return acp.KillTerminalResponse{}, nil
}

// TerminalOutput returns terminal output and exit status.
func (c *ClawBenchACPClient) TerminalOutput(_ context.Context, req acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	c.termMu.Lock()
	ts, ok := c.terminals[req.TerminalId]
	c.termMu.Unlock()
	if !ok {
		return acp.TerminalOutputResponse{}, fmt.Errorf("terminal %s not found", req.TerminalId)
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()

	resp := acp.TerminalOutputResponse{
		Output:    ts.output.String(),
		Truncated: ts.truncated,
	}

	// Include exit status only if command has finished
	select {
	case <-ts.done:
		if ts.exitCode != nil || ts.signal != nil {
			resp.ExitStatus = &acp.TerminalExitStatus{
				ExitCode: ts.exitCode,
				Signal:   ts.signal,
			}
		}
	default:
		// Still running — no exit status
	}

	return resp, nil
}

// ReleaseTerminal releases terminal resources.
func (c *ClawBenchACPClient) ReleaseTerminal(_ context.Context, req acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	c.termMu.Lock()
	ts, ok := c.terminals[req.TerminalId]
	if ok {
		ts.cancel() // ensure command context is cancelled
		delete(c.terminals, req.TerminalId)
	}
	c.termMu.Unlock()
	return acp.ReleaseTerminalResponse{}, nil
}

// WaitForTerminalExit waits for terminal command to exit.
func (c *ClawBenchACPClient) WaitForTerminalExit(_ context.Context, req acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	c.termMu.Lock()
	ts, ok := c.terminals[req.TerminalId]
	c.termMu.Unlock()
	if !ok {
		return acp.WaitForTerminalExitResponse{}, fmt.Errorf("terminal %s not found", req.TerminalId)
	}

	// Wait for command to finish (command context has its own 120s timeout)
	<-ts.done

	ts.mu.Lock()
	defer ts.mu.Unlock()

	return acp.WaitForTerminalExitResponse{
		ExitCode: ts.exitCode,
		Signal:   ts.signal,
	}, nil
}
