package tools

import (
	"context"
	"encoding/json"
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

func (BashTool) Name() string { return "bash" }

func (BashTool) Snippet() string { return "Run a shell command (git, builds, tests, find)" }

func (BashTool) Guidelines() []string {
	return []string{"Use bash for commands, not for reading or searching files: read, glob and grep are more precise and their output is easier to act on."}
}

func (BashTool) Spec() Spec {
	return Spec{
		Name: "bash",
		Description: "Run a shell command in the working directory. Use it for git, builds, tests and file " +
			"inspection (find, grep, ls). Output is truncated to the first and last part when large.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "Shell command to run. Runs with /bin/sh -c.",
				},
				"timeout": map[string]any{
					"type":        "integer",
					"description": "Timeout in seconds. Omit for the configured default.",
				},
			},
			"required": []string{"command"},
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
	// Keep stdout and stderr separate so the model can tell "the command
	// failed" from "the command printed to stderr but worked". Both capture
	// writers enforce the size cap as bytes arrive: a command that prints
	// gigabytes must not grow the server's memory before it is truncated.
	stdout := newBoundedOutput(MaxBashOutputBytes)
	stderr := newBoundedOutput(MaxBashOutputBytes)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	runErr := cmd.Run()
	timedOut := ctx.Err() == context.DeadlineExceeded

	var b strings.Builder
	if stdout.Len() > 0 {
		b.WriteString(stdout.String())
	}
	if stderr.Len() > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("[stderr]\n")
		b.WriteString(stderr.String())
	}
	if b.Len() == 0 {
		b.WriteString("(no output)")
	}

	if timedOut {
		fmt.Fprintf(&b, "\n[command timed out after %d seconds and was killed]", timeout)
		return Result{Text: b.String(), IsError: true, Display: firstLine(args.Command)}
	}
	if runErr != nil {
		fmt.Fprintf(&b, "\n[exit status: %v]", runErr)
		return Result{Text: b.String(), IsError: true, Display: firstLine(args.Command)}
	}
	return Result{Text: b.String(), Display: firstLine(args.Command)}
}

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
