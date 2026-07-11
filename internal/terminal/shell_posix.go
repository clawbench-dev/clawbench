//go:build !windows

package terminal

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// startPTY starts a new shell process with a POSIX PTY.
// Returns the PTY file (output + input, bidirectional), the command,
// the same PTY file as inputWrite (POSIX PTY is bidirectional), a resize
// function wrapping pty.Setsize, a close function wrapping ptmx.Close,
// and any error.
func startPTY(cwd string, cols, rows uint16) (outputFile *os.File, cmd *exec.Cmd, inputWrite *os.File, resizeFn func(uint16, uint16) error, closeFn func(), err error) {
	shell := resolveShell()
	slog.Info(
		"terminal: starting PTY",
		slog.String("shell", shell),
		slog.String("cwd", cwd),
	)

	if _, err := exec.LookPath(shell); err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("shell not found: %w", err)
	}

	cmd = exec.Command(shell)
	cmd.Dir = cwd
	cmd.Env = append(
		os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
	)

	if cols == 0 || rows == 0 {
		cols, rows = 80, 24
	}
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("failed to start PTY: %w", err)
	}

	// On POSIX, PTY is bidirectional — same file for input and output
	resizeFn = func(c, r uint16) error {
		return pty.Setsize(ptmx, &pty.Winsize{Cols: c, Rows: r})
	}

	closeFn = func() {
		ptmx.Close()
		// Wait for the process to finish with a timeout,
		// then force-kill if still running.
		done := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			killProcessGroupSig(cmd, syscall.SIGKILL)
		}
	}

	return ptmx, cmd, ptmx, resizeFn, closeFn, nil
}

// killProcessGroupSig sends a signal to the process group of the given command.
// pty.Start creates the shell with Setsid=true, which starts a new session
// and process group — so Getpgid works to find and kill the whole group.
func killProcessGroupSig(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		_ = cmd.Process.Signal(sig)
		return
	}

	if err := syscall.Kill(-pgid, sig); err != nil {
		_ = cmd.Process.Signal(sig)
	}
}
