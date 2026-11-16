package tools

import (
	"bytes"
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
	// failed" from "the command printed to stderr but worked".
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	timedOut := ctx.Err() == context.DeadlineExceeded

	var b strings.Builder
	if stdout.Len() > 0 {
		b.WriteString(truncateOutput(stdout.String()))
	}
	if stderr.Len() > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("[stderr]\n")
		b.WriteString(truncateOutput(stderr.String()))
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

// truncateOutput keeps the head and the tail of a large output.
//
// Both ends matter: the head says what the command did, the tail says how it
// ended (an error, a summary line, a final test result). Keeping only the head
// hides the failure, which is the part the model needs.
func truncateOutput(s string) string {
	if len(s) <= MaxBashOutputBytes {
		return s
	}
	half := MaxBashOutputBytes / 2
	omitted := len(s) - 2*half
	return s[:half] +
		fmt.Sprintf("\n[... %d bytes omitted ...]\n", omitted) +
		s[len(s)-half:]
}

// firstLine renders a command's first line for the tool card.
func firstLine(cmd string) string {
	if i := strings.IndexByte(cmd, '\n'); i >= 0 {
		return cmd[:i]
	}
	return cmd
}
