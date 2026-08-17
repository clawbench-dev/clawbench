package ai

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// streamWaitDelay bounds how long the process's exec-managed pipes (stderr)
// may be held open by child processes that inherit them after the command
// exits. Without this, a spawned background command (e.g. `npm run dev` started
// by a Bash tool) keeps the pipe open forever and Wait blocks indefinitely.
const streamWaitDelay = 15 * time.Second

// streamDrainGrace is how long the scanner may keep draining stdout after the
// process has exited before we force-close the pipe. The pipe only stays open
// after exit when a spawned child inherited it; 2s is far more than enough to
// drain any legitimate trailing output.
const streamDrainGrace = 2 * time.Second

// defaultStreamIdleTimeout is the default no-progress watchdog window for CLI
// backends. A stream that produces no new output for this long is treated as
// stalled and terminated (process group killed), so a hung CLI can never block
// a session forever.
const defaultStreamIdleTimeout = 30 * time.Minute

// CLIBackend is a generic AI backend that shells out to a CLI tool and streams
// JSON output. It implements the AIBackend interface via callbacks for
// backend-specific behavior.
type CLIBackend struct {
	BackendName  string // exported for sub-package construction; Name() method returns this
	Cmd          string // default CLI command
	BuildArgsFn  func(req ChatRequest) []string
	NewParserFn  func() LineParser
	FilterLineFn func(line string) (string, bool)     // nil = skip empty lines only
	PreStartFn   func(cmd *exec.Cmd, req ChatRequest) // optional, e.g. Claude stdin

	// NoProgressTimeout is the stall-watchdog window for the stream: if the CLI
	// produces no new output for this long without exiting, the stream is
	// terminated (process group killed) so the session can never block
	// indefinitely. Zero uses the package default; negative disables it.
	NoProgressTimeout time.Duration
}

// streamIdleTimeout returns the effective no-progress watchdog window.
// 0 → defaultStreamIdleTimeout, negative → disabled (0).
func (b *CLIBackend) streamIdleTimeout() time.Duration {
	switch {
	case b.NoProgressTimeout == 0:
		return defaultStreamIdleTimeout
	case b.NoProgressTimeout < 0:
		return 0
	default:
		return b.NoProgressTimeout
	}
}

// truncatePrompt returns a truncated version of the prompt for logging.
// When fork context is prepended, the prompt can be very long.
func truncatePrompt(req ChatRequest) string {
	const maxPromptLog = 200
	p := req.Prompt
	if len(p) > maxPromptLog {
		return p[:maxPromptLog] + "..."
	}
	return p
}

// Name returns the backend identifier (implements AIBackend).
func (b *CLIBackend) Name() string {
	return b.BackendName
}

// ExecuteStream runs the CLI backend in streaming mode and returns a channel of events.
//
// The stream is guaranteed to terminate: the process is reaped as soon as it
// exits (Wait runs in its own goroutine), the whole process group is killed on
// context cancellation, and a no-progress watchdog terminates a hung CLI. The
// returned channel is always closed.
func (b *CLIBackend) ExecuteStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	// Prepend fork context to prompt if present (fork session first message).
	// This must happen before BuildArgsFn and PreStartFn so the AI receives
	// the full context via stdin.
	if req.ForkContext != "" {
		req.Prompt = req.ForkContext + req.Prompt
	}

	args := b.BuildArgsFn(req)

	cmdName := req.Command
	if cmdName == "" {
		cmdName = b.Cmd
	}
	cmd := exec.CommandContext(ctx, cmdName, args...)
	cmd.Dir = req.WorkDir

	// Detach the backend process from the server's controlling terminal so
	// agent-spawned commands (e.g. `sudo`) cannot prompt on the ClawBench
	// server console via /dev/tty and block the session. The process runs in
	// its own group so cancel can reap the whole tree.
	setProcessGroup(cmd)

	// Replace the default leader-only kill with a process-group kill: when a
	// spawned child inherits the pipes, killing only the leader leaves the
	// child holding the pipes open and the read loop blocks forever.
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			killProcessGroup(cmd.Process)
		}
		return nil
	}
	// Bound how long the process's pipes may stay open after it exits when a
	// spawned child inherited them: Wait() forcibly closes the parent's pipe
	// ends after the delay, unblocking the read loop (see os/exec.WaitDelay).
	cmd.WaitDelay = streamWaitDelay

	if req.WorkDir == "" {
		slog.Warn("cli backend: WorkDir is EMPTY, process will inherit server CWD",
			slog.String("backend", b.BackendName),
			slog.String("session_id", req.SessionID),
			slog.String("agent_id", req.AgentID))
	}

	// Initialize env vars from current process environment
	cmd.Env = os.Environ()

	// Mark as ClawBench child process for orphan cleanup on server crash.
	// On restart, CleanupOrphans scans /proc for this marker and kills
	// any processes left behind by a crashed server instance.
	cmd.Env = append(cmd.Env, OrphanChildEnvVar)

	// Inject CLAWBENCH_SCHEDULED=1 for anti-recursion: prevents AI from
	// creating new scheduled tasks during a scheduled execution.
	if req.ScheduledExecution {
		cmd.Env = append(cmd.Env, "CLAWBENCH_SCHEDULED=1")
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if b.PreStartFn != nil {
		b.PreStartFn(cmd, req)
	}

	slog.Info(
		"executing ai stream command",
		slog.String("backend", b.BackendName),
		slog.String("work_dir", req.WorkDir),
		slog.String("session_id", req.SessionID),
		slog.String("prompt", truncatePrompt(req)),
		slog.Bool("has_fork_context", req.ForkContext != ""),
		slog.Any("args", args),
	)

	// Create a manual stdout pipe so we own the read end. Unlike StdoutPipe,
	// Wait() never closes it, so the scanner can drain the full output before
	// we decide to close it — this avoids losing the tail of a large response
	// when Wait runs concurrently with the read loop.
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("%s stream: failed to create stdout pipe: %w", b.BackendName, err)
	}
	cmd.Stdout = pw

	if err := cmd.Start(); err != nil {
		_ = pr.Close()
		_ = pw.Close()
		return nil, fmt.Errorf("%s stream: failed to start command: %w", b.BackendName, err)
	}
	// The child has its own copy of the write end (fd 1); the parent must close
	// its copy or the read end never sees EOF after the process exits.
	_ = pw.Close()

	ch := make(chan StreamEvent, streamChanSize)
	go b.runStream(ctx, cmd, pr, &stderrBuf, ch, req)

	return ch, nil
}

// runStream drives the CLI process to completion: reaps it promptly, enforces
// the no-progress watchdog, and streams parsed events on ch. It guarantees the
// channel is always closed, even when the process or a spawned child hangs.
//
//nolint:gocognit,gocyclo // complex stream lifecycle orchestration
func (b *CLIBackend) runStream(
	ctx context.Context,
	cmd *exec.Cmd,
	pr *os.File,
	stderrBuf *bytes.Buffer,
	ch chan<- StreamEvent,
	req ChatRequest,
) {
	defer close(ch)
	defer func() { _ = pr.Close() }()

	// Reap the process as soon as it exits, independent of the read loop.
	// Previously Wait() ran only after the scanner reached EOF — but EOF can be
	// delayed indefinitely when a spawned child inherits the stdout pipe,
	// leaving a zombie process and a permanently blocked read loop.
	var waitErr error
	procDone := make(chan struct{})
	go func() {
		waitErr = cmd.Wait()
		close(procDone)
	}()

	// Shared termination path: closes the read end so the scanner can never
	// block forever, and kills the whole process group. Closing the read end
	// first makes the scanner unblock deterministically (the kill's fd teardown
	// is async and could otherwise race to a clean EOF); it also kills any
	// daemon child that tries to write to the pipe. `stall` distinguishes an
	// idle-timeout termination (reported to the user) from a cancellation or
	// pipe-hold cleanup (which end the stream quietly).
	var stalled atomic.Bool
	var terminateOnce sync.Once
	terminate := func(stall bool) {
		terminateOnce.Do(func() {
			stalled.Store(stall)
			_ = pr.Close()
			if cmd.Process != nil {
				killProcessGroup(cmd.Process)
			}
		})
	}

	// Watchdog goroutine: terminates the stream on context cancellation or when
	// no new output arrives within the idle window. Stopped once the process
	// exits or the scan loop finishes.
	progress := make(chan struct{}, 1)
	idleTimeout := b.streamIdleTimeout()
	watchDone := make(chan struct{})
	defer close(watchDone)
	go b.watchStream(ctx, procDone, watchDone, progress, idleTimeout, terminate)

	// Coordinator: if the process exits but the scanner is still blocked, a
	// spawned child probably inherited the pipe. Give it a grace period to drain
	// legitimate trailing output, then force-close the read end so the stream
	// terminates promptly instead of waiting for the idle watchdog.
	scanDone := make(chan struct{})
	defer close(scanDone)
	go func() {
		select {
		case <-scanDone:
		case <-procDone:
			select {
			case <-scanDone:
			case <-time.After(streamDrainGrace):
				terminate(false)
				<-scanDone
			}
		}
	}()

	scanner := bufio.NewScanner(pr)
	buf := make([]byte, scannerInitial)
	scanner.Buffer(buf, scannerMax)

	var rawLines strings.Builder
	var lastCapturedSessionID string
	parser := b.NewParserFn()

	for scanner.Scan() {
		line := scanner.Text()

		// Filter lines based on backend-specific logic
		if b.FilterLineFn != nil {
			filtered, ok := b.FilterLineFn(line)
			if !ok {
				continue
			}
			line = filtered
		} else if line == "" {
			continue
		}

		// Any line counts as progress for the stall watchdog.
		select {
		case progress <- struct{}{}:
		default:
		}

		// Collect raw line for debugging
		if rawLines.Len() > 0 {
			rawLines.WriteByte('\n')
		}
		rawLines.WriteString(line)

		// Check if this is the final "result" line — send raw_output
		// before parsing so the handler receives it before the "done" event.
		if strings.HasPrefix(line, `{"type":"result"`) {
			sendEvent(ctx, ch, StreamEvent{Type: "raw_output", RawOutput: rawLines.String()})
		}

		slog.Debug(b.BackendName+" stream: raw line", "session_id", req.SessionID, "line", line)
		parser.ParseLine(line, ch)

		// Early capture of external session ID (OpenCode ses_xxx, Codex thread_xxx).
		// This allows the handler to persist the ID immediately, even if the stream
		// is cancelled before step_finish/turn.completed emits the metadata event.
		if capturedID := parser.GetCapturedSessionID(); capturedID != "" && capturedID != lastCapturedSessionID {
			lastCapturedSessionID = capturedID
			sendEvent(ctx, ch, StreamEvent{Type: "session_capture", Content: capturedID})
		}

		// Check context after parsing
		select {
		case <-ctx.Done():
			slog.Warn(
				b.BackendName+" stream: context cancelled",
				slog.String("session_id", req.SessionID),
			)
			if rawLines.Len() > 0 {
				sendEvent(ctx, ch, StreamEvent{Type: "raw_output", RawOutput: rawLines.String()})
			}
			return
		default:
		}
	}

	// Wait briefly for Wait() so we can report exit diagnostics, but never let
	// a delayed Wait (e.g. a child holding stderr until WaitDelay) block the
	// stream end. waitErr and stderrBuf are only safe to read once Wait has
	// returned (procDone closed): stderrBuf is written by exec's copy goroutine,
	// which finishes only when Wait returns.
	var procDoneClosed bool
	select {
	case <-procDone:
		procDoneClosed = true
	case <-time.After(2 * time.Second):
	}

	// Scanner-level failure — but only report it when the stream was NOT
	// cancelled (which produces its own terminal signal) and the error is a
	// genuine parsing problem rather than the pipe being closed by us.
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		if stalled.Load() {
			sendEvent(ctx, ch, StreamEvent{
				Type:    "warning",
				Content: fmt.Sprintf("AI stream stalled: no output for %s, terminated", idleTimeout),
				Reason:  ReasonStreamStall,
			})
		} else if !isStreamEndError(err) {
			sendEvent(ctx, ch, StreamEvent{
				Type:    "warning",
				Content: fmt.Sprintf("AI output parse error: %v", err),
				Reason:  ReasonParseError,
			})
		}
	}

	// Completion diagnostics (abnormal exit / stderr output) unless the stream
	// was cancelled or stalled — those already signaled their own terminal state.
	// Also skipped when Wait hasn't returned yet (pathological pipe-hold case):
	// reading waitErr/stderrBuf then would race with exec's copy goroutine.
	if ctx.Err() == nil && !stalled.Load() && procDoneClosed {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) || (waitErr != nil && !errors.Is(waitErr, exec.ErrWaitDelay)) {
			stderr := stderrBuf.String()
			slog.Error(
				b.BackendName+" stream: command exited abnormally",
				slog.String("session_id", req.SessionID),
				slog.String("exit_error", waitErr.Error()),
				slog.String("stderr", stderr),
			)
			warnMsg := "AI backend exited abnormally"
			if stderr != "" {
				warnMsg = fmt.Sprintf("AI backend exited abnormally\n%s", stderr)
			}
			sendEvent(ctx, ch, StreamEvent{Type: "warning", Content: warnMsg, Reason: ReasonBackendExit})
		} else if stderrBuf.Len() > 0 {
			stderr := stderrBuf.String()
			slog.Warn(
				b.BackendName+" stream: command succeeded with stderr output",
				slog.String("session_id", req.SessionID),
				slog.String("stderr", stderr),
			)
			sendEvent(ctx, ch, StreamEvent{Type: "warning", Content: stderr})
		}
	}

	// Send raw output event after all other events
	if rawLines.Len() > 0 {
		sendEvent(ctx, ch, StreamEvent{Type: "raw_output", RawOutput: rawLines.String()})
	}
}

// watchStream enforces the stream's termination conditions until the process
// exits or the scan loop finishes: it terminates on context cancellation and
// on the no-progress watchdog timer expiring.
func (b *CLIBackend) watchStream(
	ctx context.Context,
	procDone <-chan struct{},
	watchDone <-chan struct{},
	progress <-chan struct{},
	idleTimeout time.Duration,
	onTerminate func(stall bool),
) {
	if idleTimeout <= 0 {
		// Watchdog disabled: only react to cancellation.
		select {
		case <-ctx.Done():
			onTerminate(false)
		case <-procDone:
		case <-watchDone:
		}
		return
	}
	timer := time.NewTimer(idleTimeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			onTerminate(false)
			return
		case <-procDone:
			return
		case <-watchDone:
			return
		case <-progress:
			// Reset the idle window on every line of output.
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(idleTimeout)
		case <-timer.C:
			onTerminate(true)
			return
		}
	}
}

// isStreamEndError reports whether a scanner error merely reflects the read end
// being closed (closed pipe, closed file) rather than a genuine parsing failure
// such as a token exceeding the scanner buffer. io.EOF is never reported as a
// scanner error by bufio.Scanner, so it is not checked here.
func isStreamEndError(err error) bool {
	return errors.Is(err, io.ErrClosedPipe) ||
		errors.Is(err, os.ErrClosed) ||
		errors.Is(err, syscall.EBADF)
}

// sendEvent sends ev on ch unless the context is already done. This prevents
// the producer from blocking forever on a full channel once the consumer has
// stopped reading (e.g. after cancellation or the terminal event).
//
// NOTE: this only guards runStream's own emits. The parsers' sends
// (ParseLine) remain plain blocking sends and are safe only because the
// consumer drains the channel until it closes (SessionExecutor.Finalize) — so
// callers must NOT stop reading the channel without ensuring the producer can
// still close it (see the producer contract on AIBackend.ExecuteStream).
func sendEvent(ctx context.Context, ch chan<- StreamEvent, ev StreamEvent) {
	select {
	case ch <- ev:
	case <-ctx.Done():
	}
}

// filterSkipNonJSON returns a line filter that discards lines
// that don't start with '{' (non-JSON lines from CLI stderr).
func filterSkipNonJSON() func(string) (string, bool) {
	return func(line string) (string, bool) {
		if line == "" || !strings.HasPrefix(line, "{") {
			return "", false
		}
		return line, true
	}
}
