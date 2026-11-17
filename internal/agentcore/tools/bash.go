package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// BashTool runs a shell command in the working directory.
//
// This is the widest capability in the set: it can read, write, delete and
// reach the network, and no command allowlist changes that (sed -i, tee,
// redirects, find -exec, and any interpreter all write files). Callers must
// therefore treat "exec granted" as "full access granted" — see
// model.ToolPolicy.Normalized, which closes fs.write in when exec is granted.
type BashTool struct{}

func (BashTool) Name() string { return toolBashName }

func (BashTool) Snippet() string { return "Run a shell command (git, builds, tests, find)" }

func (BashTool) Guidelines() []string {
	return []string{
		"Use bash for commands, not for reading or searching files: read, glob and grep are more precise and their output is easier to act on.",
		// The tool card shows this line, not the command: without it a run of
		// shell calls is a wall of pipes in the chat.
		"Always write `description` as one short sentence saying what the command does — it is what the user sees in the chat.",
	}
}

func (BashTool) Spec() Spec {
	return Spec{
		Name: toolBashName,
		Description: "Run a shell command in the working directory. Use it for git, builds, tests and file " +
			"inspection (find, grep, ls). Output is truncated to the first and last part when large.",
		Parameters: map[string]any{
			schemaType: schemaObject,
			schemaProperties: map[string]any{
				argCommand: map[string]any{
					schemaType:        schemaString,
					schemaDescription: "Shell command to run. Runs with /bin/sh -c.",
				},
				argDescription: map[string]any{
					schemaType: schemaString,
					schemaDescription: "One short sentence describing what this command does, in the user's " +
						"language. Shown in the tool card instead of the raw command.",
				},
				"timeout": map[string]any{
					schemaType:        schemaInteger,
					schemaDescription: "Timeout in seconds. Omit for the configured default.",
				},
			},
			schemaRequired: []string{argCommand, argDescription},
		},
	}
}

type bashArgs struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

func (BashTool) Run(env Env, raw json.RawMessage) Result {
	var args bashArgs
	if err := decodeArgs(raw, &args); err != nil {
		return Result{Text: err.Error(), IsError: true}
	}
	if strings.TrimSpace(args.Command) == "" {
		return Result{Text: "command is empty", IsError: true}
	}

	timeout := env.CommandTimeoutSec
	if timeout <= 0 {
		timeout = 120
	}
	if args.Timeout > 0 {
		timeout = args.Timeout
	}

	// Derive the deadline from the TURN's context so the user's cancel button
	// actually stops the command. A background context here would leave an
	// orphan process running after the reply was cancelled.
	base := env.Ctx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithTimeout(base, time.Duration(timeout)*time.Second)
	defer cancel()

	// Kill the whole process group on cancel: a shell script spawns children
	// that inherit the pipes, and killing only the leader leaves them running.
	shell, flag := shellCommand()
	cmd := exec.CommandContext(ctx, shell, flag, args.Command)
	cmd.Dir = env.Root
	setProcessGroup(cmd)
	// WaitDelay bounds how long Wait keeps waiting for the output pipes after
	// the shell has exited.
	//
	// Without it, a command that leaves a BACKGROUND child holding stdout
	// (`sleep 5 &` , `npm run dev &`) makes Wait block on the pipe copy long
	// after the shell is gone: waitpid has returned, so the context watcher has
	// already finished and Cancel is never called — the tool call then waits for
	// the child (or, for a daemon, forever) while the timeout has no effect.
	// Go closes its pipes after this delay, Wait returns exec.ErrWaitDelay, and
	// the output captured so far is kept.
	cmd.WaitDelay = bashWaitDelay(timeout)
	// Keep stdout and stderr separate so the model can tell "the command
	// failed" from "the command printed to stderr but worked". Both capture
	// writers enforce the size cap as bytes arrive: a command that prints
	// gigabytes must not grow the server's memory before it is truncated.
	//
	// These are writers, not os.Pipe ends, and that is what makes WaitDelay
	// work: exec then owns the pipe and the copying goroutine, and closes them
	// when the delay expires. With StdoutPipe the pipe is the CALLER's, and Go
	// cannot close it — the child holds the write end, so Wait keeps waiting for
	// as long as the background process lives, which is the bug this bounds.
	outW := newBoundedOutput(MaxBashOutputBytes)
	errW := newBoundedOutput(MaxBashOutputBytes)
	cmd.Stdout = outW
	cmd.Stderr = errW

	runErr := cmd.Run()

	// What actually happened, now that Wait has returned. The three outcomes are
	// distinct and were previously conflated:
	//   - timedOut: our deadline fired and the process group was killed.
	//   - leftBackground: the shell itself exited, but a child it started kept
	//     the output pipes open until WaitDelay closed them (ErrWaitDelay).
	//   - cancelled: the user stopped the turn.
	timedOut := ctx.Err() == context.DeadlineExceeded
	leftBackground := errors.Is(runErr, exec.ErrWaitDelay) && !timedOut

	var b strings.Builder
	if outW.Len() > 0 {
		b.WriteString(outW.String())
	}
	if errW.Len() > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("[stderr]\n")
		b.WriteString(errW.String())
	}
	if b.Len() == 0 {
		b.WriteString("(no output)")
	}

	if timedOut {
		fmt.Fprintf(&b, "\n[command timed out after %d seconds and was killed]", timeout)
		return Result{Text: b.String(), IsError: true, Display: firstLine(args.Command)}
	}
	if leftBackground {
		// The shell itself finished; a child it started kept the output pipes
		// open. Saying "timed out and was killed" here was the lie that made this
		// state undiagnosable — nothing was killed, and nothing timed out.
		fmt.Fprintf(&b, "\n[the command exited%s but left a background process holding its output; "+
			"the output above was captured up to that point and may be truncated]", exitSuffix(runErr))
		return Result{Text: b.String(), IsError: false, Display: firstLine(args.Command)}
	}
	if runErr != nil {
		fmt.Fprintf(&b, "\n[exit status: %v]", runErr)
		return Result{Text: b.String(), IsError: true, Display: firstLine(args.Command)}
	}
	return Result{Text: b.String(), Display: firstLine(args.Command)}
}

// exitSuffix renders the exit status part of a "left a background process"
// note, or "" when the shell exited cleanly.
func exitSuffix(err error) string {
	if err == nil || errors.Is(err, exec.ErrWaitDelay) {
		return ""
	}
	return fmt.Sprintf(" with %v", err)
}

// bashWaitDelay is how long Wait waits for the output pipes after the shell
// exited.
//
// It is capped by the command's own timeout so a very long budget does not allow
// the wait to outlive the turn, and floored so a very short budget still drains
// the tail of ordinary output.
//
// It is deliberately shorter than the command's budget: the case this exists for
// is "the shell exited, a background child still holds the pipe". When the whole
// budget has already gone to the command, waiting the same amount again would
// double the wall time of a turn the user is waiting on — and, for `nohup ... &`
// style daemons, would keep doubling it.
func bashWaitDelay(timeoutSec int) time.Duration {
	if timeoutSec <= 0 {
		return minBashWaitDelay
	}
	d := time.Duration(timeoutSec) * time.Second / 2
	if d > maxBashWaitDelay {
		d = maxBashWaitDelay
	}
	if d < minBashWaitDelay {
		d = minBashWaitDelay
	}
	return d
}

const (
	// minBashWaitDelay is the floor: enough for the tail of a normal command's
	// output to be read after the shell exits.
	minBashWaitDelay = 500 * time.Millisecond
	// maxBashWaitDelay is the ceiling: a daemon started by the command must not
	// hold a tool call open longer than this once the shell is gone.
	maxBashWaitDelay = 2 * time.Second
)

// boundedOutput captures a command's output under a fixed memory budget.
//
// The previous implementation buffered the whole stream and truncated
// afterwards, so `cat` on a multi-gigabyte file (or a runaway `yes`) could
// exhaust the process's memory before the cap was ever applied — and this
// process also serves every other chat, terminal and HTTP request. Here the
// cap is enforced while writing: the first half is kept, the last half is
// kept, and the middle is counted and dropped.
//
// Both ends matter: the head says what the command did, the tail says how it
// ended (an error, a summary line, a final test result). Keeping only the head
// hides the failure, which is the part the model needs.
type boundedOutput struct {
	limit int
	head  []byte
	tail  []byte
	total int
}

func newBoundedOutput(limit int) *boundedOutput {
	return &boundedOutput{limit: limit}
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	written := len(p)
	half := b.limit / 2
	if half <= 0 {
		// A non-positive budget keeps nothing, but the command still runs:
		// returning an error here would make exec kill it.
		b.total += written
		return written, nil
	}
	for len(p) > 0 {
		if len(b.head) < half {
			n := half - len(b.head)
			if n > len(p) {
				n = len(p)
			}
			b.head = append(b.head, p[:n]...)
			p = p[n:]
			continue
		}
		// Bounded chunking keeps a single huge Write from allocating a
		// transient copy as large as itself.
		n := len(p)
		if n > half {
			n = half
		}
		b.tail = append(b.tail, p[:n]...)
		if len(b.tail) > half {
			b.tail = append(b.tail[:0], b.tail[len(b.tail)-half:]...)
		}
		p = p[n:]
	}
	b.total += written
	return written, nil
}

// Len reports how many bytes the command produced, not how many were kept.
func (b *boundedOutput) Len() int { return b.total }

func (b *boundedOutput) String() string {
	if b.total <= b.limit {
		return string(b.head) + string(b.tail)
	}
	omitted := b.total - len(b.head) - len(b.tail)
	return string(b.head) +
		fmt.Sprintf("\n[... %d bytes omitted ...]\n", omitted) +
		string(b.tail)
}

// firstLine renders a command's first line for the tool card.
func firstLine(cmd string) string {
	if i := strings.IndexByte(cmd, '\n'); i >= 0 {
		return cmd[:i]
	}
	return cmd
}
