package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

// CodexStreamMessage represents a single JSON line from `codex exec --json`
type CodexStreamMessage struct {
	Type     string      `json:"type"`
	ThreadID string      `json:"thread_id,omitempty"`
	Message  string      `json:"message,omitempty"` // error message
	Error    *CodexError `json:"error,omitempty"`   // turn.failed error
	Item     *CodexItem  `json:"item,omitempty"`
	Usage    *CodexUsage `json:"usage,omitempty"`
}

// CodexError represents an error in Codex stream output.
type CodexError struct {
	Message string `json:"message"`
}

// CodexItem represents an item in Codex stream output
type CodexItem struct {
	ID               string `json:"id"`
	Type             string `json:"type"`                        // "agent_message" or "command_execution"
	Text             string `json:"text,omitempty"`              // agent_message
	Command          string `json:"command,omitempty"`           // command_execution
	AggregatedOutput string `json:"aggregated_output,omitempty"` // command_execution
	ExitCode         *int   `json:"exit_code,omitempty"`         // command_execution
	Status           string `json:"status,omitempty"`            // "in_progress" or "completed"
}

// CodexUsage represents token usage in a turn.completed event
type CodexUsage struct {
	InputTokens       int `json:"input_tokens"`
	CachedInputTokens int `json:"cached_input_tokens"`
	OutputTokens      int `json:"output_tokens"`
}

// CodexStreamParser parses JSON Lines output from `codex exec --json`
type CodexStreamParser struct {
	threadID string // captured from thread.started event
}

// GetCapturedSessionID returns the Codex thread ID captured from thread.started.
// Available as soon as the first event is parsed.
func (p *CodexStreamParser) GetCapturedSessionID() string { return p.threadID }

// ParseLine parses a single JSON line from Codex's --json output and sends
// StreamEvent(s) to the provided channel.
func (p *CodexStreamParser) ParseLine(line string, ch chan<- StreamEvent) {
	var msg CodexStreamMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		slog.Debug("codex stream: skipping unparseable line", "line", line, "error", err)
		return
	}

	switch msg.Type {
	case "thread.started":
		if msg.ThreadID != "" {
			p.threadID = msg.ThreadID
		}
	case "item.completed":
		p.handleItemCompleted(&msg, ch)
	case "item.started":
		p.handleItemStarted(&msg, ch)
	case "turn.completed":
		p.handleTurnCompleted(&msg, ch)
	case "turn.started":
		// Structural event — no content
	case "error":
		if msg.Message != "" {
			ch <- StreamEvent{Type: "warning", Content: msg.Message, Reason: ReasonRequestFailed}
		}
	case "turn.failed":
		p.handleTurnFailed(&msg, ch)
	default:
		slog.Debug("codex stream: skipping unknown message type", "type", msg.Type)
	}
}

// handleItemCompleted processes item.completed events.
func (p *CodexStreamParser) handleItemCompleted(msg *CodexStreamMessage, ch chan<- StreamEvent) {
	if msg.Item == nil {
		return
	}
	switch msg.Item.Type {
	case "agent_message":
		text := msg.Item.Text
		if text == "" {
			return
		}
		thinking, content := codexSplitThinking(text)
		if thinking != "" {
			ch <- StreamEvent{Type: "thinking", Content: thinking}
		}
		if content != "" {
			ch <- StreamEvent{Type: "content", Content: content}
		}
	case "command_execution":
		if tc := parseCodexToolComplete(msg); tc != nil {
			ch <- StreamEvent{Type: "tool_use", Tool: tc}
		}
	}
}

// handleItemStarted processes item.started events.
func (p *CodexStreamParser) handleItemStarted(msg *CodexStreamMessage, ch chan<- StreamEvent) {
	if msg.Item == nil {
		return
	}
	if msg.Item.Type == "command_execution" {
		if tc := parseCodexToolStart(msg); tc != nil {
			ch <- StreamEvent{Type: "tool_use", Tool: tc}
		}
	}
}

// handleTurnCompleted processes turn.completed events.
func (p *CodexStreamParser) handleTurnCompleted(msg *CodexStreamMessage, ch chan<- StreamEvent) {
	meta := &Metadata{
		SessionID: p.threadID,
	}
	if msg.Usage != nil {
		meta.InputTokens = msg.Usage.InputTokens
		meta.OutputTokens = msg.Usage.OutputTokens
	}
	ch <- StreamEvent{Type: "metadata", Meta: meta}
	ch <- StreamEvent{Type: "done"}
}

// handleTurnFailed processes turn.failed events.
func (p *CodexStreamParser) handleTurnFailed(msg *CodexStreamMessage, ch chan<- StreamEvent) {
	errMsg := "AI request failed"
	if msg.Error != nil && msg.Error.Message != "" {
		errMsg = msg.Error.Message
	}
	ch <- StreamEvent{Type: "error", Error: errMsg, Reason: ReasonRequestFailed}
	ch <- StreamEvent{Type: "done"}
}

// buildCodexStreamArgs constructs the CLI arguments for Codex streaming.
// Note: returns args WITHOUT the "exec" prefix (it's added by ExecuteStream).
func buildCodexStreamArgs(req ChatRequest) []string {
	var args []string

	// New session: --json ...
	args = append(args, "--json", "--dangerously-bypass-approvals-and-sandbox")

	// Working directory
	if req.WorkDir != "" {
		args = append(args, "-C", req.WorkDir)
	}

	// Model override
	if req.Model != "" {
		args = append(args, "-m", req.Model)
	}

	// Thinking effort level (e.g., -c model_reasoning_effort=high)
	if req.ThinkingEffort != "" {
		args = append(args, "-c", "model_reasoning_effort="+req.ThinkingEffort)
	}

	// Skip git repo check (allows running in non-git dirs)
	args = append(args, "--skip-git-repo-check")

	// Codex CLI has no --system-prompt flag — inject into user prompt.
	prompt := InjectSystemPrompt(req)

	// Prompt is the last argument for new sessions
	args = append(args, prompt)

	return args
}

// parseCodexResumeOutput parses plain text output from "codex exec resume".
// Output format:
//
//	OpenAI Codex v0.57.0 (research preview)
//	--------
//	workdir: ...
//	model: ...
//	--------
//	user
//	<prompt>
//	codex
//	<thinking block>
//	<response content>
//	exec
//	<command> in <dir> [succeeded|failed] in <time>:
//	<output>
//	codex
//	<thinking block>
//	<response content>
//
//nolint:gocognit,gocyclo // complex stream parsing logic
func parseCodexResumeOutput(scanner *bufio.Scanner, ch chan<- StreamEvent, sessionID string, rawLines *strings.Builder) {
	role := "" // current role: "codex" or "exec"
	inThinking := false
	var thinkingBuf strings.Builder
	var execOutput strings.Builder
	var execCommand string
	var execID string
	var execCounter int

	for scanner.Scan() {
		line := scanner.Text()

		// Collect raw line for debugging (all non-empty lines, same as CLIBackend)
		if line != "" {
			if rawLines.Len() > 0 {
				rawLines.WriteByte('\n')
			}
			rawLines.WriteString(line)
		}

		// Handle ERROR lines from codex resume output
		if errMsg, ok := strings.CutPrefix(line, "ERROR:"); ok && errMsg != "" {
			ch <- StreamEvent{Type: "error", Error: errMsg}
			// ISS-079: must emit done so the stream consumer can finalize;
			// without this, the SSE stream hangs indefinitely.
			ch <- StreamEvent{Type: "done"}
			return
		}

		// Detect role markers
		if line == "codex" || line == "user" {
			// Flush any pending exec block
			if role == "exec" && execCommand != "" {
				input := execCommandJSON(execCommand)
				output := truncateToolOutput(execOutput.String())
				emitBashToolCall(ch, execID, input, output, true, nil)
				execCommand = ""
				execOutput.Reset()
			}
			role = line
			continue
		}

		if line == "exec" {
			// Flush any pending exec block
			if role == "exec" && execCommand != "" {
				input := execCommandJSON(execCommand)
				output := truncateToolOutput(execOutput.String())
				emitBashToolCall(ch, execID, input, output, true, nil)
				execCommand = ""
				execOutput.Reset()
			}
			role = "exec"
			continue
		}

		// Skip everything before first codex marker (header + user section)
		if role == "" || role == "user" {
			continue
		}

		// Handle codex role: thinking blocks and content
		if role == "codex" {
			// Thinking blocks use  antici( antici) tags. Both tags may appear
			// on the same line:  anticicontent... antici. We must handle:
			// 1.  antici + content +  antici all on one line
			// 2.  antici on its own line, content on next lines,  antici on its own line
			// 3.  antici at line start with content,  antici at end of a thinking line
			if strings.HasPrefix(line, codexThinkOpen) {
				inThinking = true
				thinkingBuf.Reset()
				rest := strings.TrimPrefix(line, codexThinkOpen)
				// Check if closing tag is on the same line
				if before, afterClose, ok := strings.Cut(rest, codexThinkClose); ok {
					if before != "" {
						ch <- StreamEvent{Type: "thinking", Content: before}
					}
					inThinking = false
					afterClose = strings.TrimSpace(afterClose)
					if afterClose != "" {
						ch <- StreamEvent{Type: "content", Content: afterClose + "\n"}
					}
				} else if rest != "" {
					thinkingBuf.WriteString(rest)
				}
				continue
			}
			if strings.HasPrefix(line, codexThinkClose) {
				if inThinking {
					inThinking = false
					if thinking := thinkingBuf.String(); thinking != "" {
						ch <- StreamEvent{Type: "thinking", Content: thinking}
					}
					thinkingBuf.Reset()
				}
				// Check for content after closing tag on the same line
				afterClose := strings.TrimPrefix(line, codexThinkClose)
				afterClose = strings.TrimSpace(afterClose)
				if afterClose != "" {
					ch <- StreamEvent{Type: "content", Content: afterClose + "\n"}
				}
				continue
			}
			if inThinking {
				// Check for inline closing tag within a thinking line
				if before, afterClose, ok := strings.Cut(line, codexThinkClose); ok {
					if before != "" {
						if thinkingBuf.Len() > 0 {
							thinkingBuf.WriteByte('\n')
						}
						thinkingBuf.WriteString(before)
					}
					inThinking = false
					if thinking := thinkingBuf.String(); thinking != "" {
						ch <- StreamEvent{Type: "thinking", Content: thinking}
					}
					thinkingBuf.Reset()
					afterClose = strings.TrimSpace(afterClose)
					if afterClose != "" {
						ch <- StreamEvent{Type: "content", Content: afterClose + "\n"}
					}
				} else {
					if thinkingBuf.Len() > 0 {
						thinkingBuf.WriteByte('\n')
					}
					thinkingBuf.WriteString(line)
				}
				continue
			}
			if line != "" {
				ch <- StreamEvent{Type: "content", Content: line + "\n"}
			}
			continue
		}

		// Handle exec role: command line and output
		if role == "exec" {
			if execCommand == "" {
				// First line is the command summary, e.g.:
				// "bash -c 'ls -1 /tmp | wc -l' in /tmp succeeded in 14ms:"
				execCommand = strings.TrimSuffix(line, ":")
				execID = fmt.Sprintf("exec-%d", execCounter)
				execCounter++
				emitBashToolCall(ch, execID, execCommandJSON(execCommand), "", false, nil)
			} else {
				if execOutput.Len() > 0 {
					execOutput.WriteByte('\n')
				}
				execOutput.WriteString(line)
			}
			continue
		}
	}

	// Flush any pending exec block at EOF
	if role == "exec" && execCommand != "" {
		input := execCommandJSON(execCommand)
		output := truncateToolOutput(execOutput.String())
		emitBashToolCall(ch, execID, input, output, true, nil)
	}
	// Also flush exec blocks when role changes (codex/exec marker)
	// These are handled inline above.

	// Flush remaining thinking
	if inThinking && thinkingBuf.Len() > 0 {
		ch <- StreamEvent{Type: "thinking", Content: thinkingBuf.String()}
	}

	// ISS-080: check scanner.Err() after the loop — truncated output due to
	// a read error must not be silently accepted as complete.
	if err := scanner.Err(); err != nil {
		ch <- StreamEvent{Type: "warning", Content: fmt.Sprintf("resume output read error: %v", err), Reason: ReasonParseError}
		// Fall through to emit done so the consumer can finalize.
	}

	// Send metadata and done events
	ch <- StreamEvent{Type: "metadata", Meta: &Metadata{SessionID: sessionID}}
	ch <- StreamEvent{Type: "done"}
}

// emitBashToolCall has been moved to codex_tool.go

// buildCodexResumeArgs constructs the CLI arguments for resuming a Codex session.
// "codex exec resume" does not support --json, so output is plain text.
// We pass -c config overrides to restore model/provider from the original session.
// Note: returns args WITHOUT the "exec" prefix (it's added by ExecuteStream).
func buildCodexResumeArgs(req ChatRequest, threadID string) []string {
	var args []string

	args = append(args, "resume") //nolint:gocritic // separated for readability with comment block
	// use -c sandbox_permissions override instead.
	// Must match the new-session sandbox level (full access), otherwise
	// commands like Edit/Write will be silently blocked, causing the AI
	// to exit without producing a text response.
	args = append(args, "-c", "sandbox_permissions=[\"disk-full-read-access\",\"disk-full-write-access\"]")

	// Restore model via -c override (resume doesn't support -m/--profile).
	// Do NOT set model_provider — let Codex's own config resolve the provider
	// from the model name. Hardcoding "minimax" breaks non-MiniMax models (ISS-049).
	if req.Model != "" {
		args = append(args, "-c", fmt.Sprintf("model=%q", req.Model))
	}

	// Thinking effort level (e.g., -c model_reasoning_effort=high)
	if req.ThinkingEffort != "" {
		args = append(args, "-c", "model_reasoning_effort="+req.ThinkingEffort)
	}

	// Thread ID for resuming
	args = append(args, threadID)

	// Codex CLI has no --system-prompt flag — inject into user prompt.
	prompt := InjectSystemPrompt(req)

	// Prompt for the resumed session
	args = append(args, prompt)

	return args
}

// ExecuteStream runs the Codex CLI in streaming mode and returns a channel of events
//
//nolint:gocognit,gocyclo // complex stream parsing logic
func (c *CodexBackend) ExecuteStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	// Prepend fork context to prompt if present (fork session first message).
	if req.ForkContext != "" {
		req.Prompt = req.ForkContext + req.Prompt
	}

	// Parse command field: "codex --profile m27" -> binary="codex", baseArgs=["--profile","m27"]
	cmdBinary := "codex"
	var baseArgs []string
	if req.Command != "" {
		parts := strings.Fields(req.Command)
		if len(parts) > 0 {
			cmdBinary = parts[0]
			if len(parts) > 1 {
				baseArgs = parts[1:]
			}
		}
	}

	// Determine if we're resuming a session
	var codexArgs []string
	if req.Resume && req.SessionID != "" {
		// For resume, SessionID contains the Codex thread_id (stored as external_session_id)
		codexArgs = buildCodexResumeArgs(req, req.SessionID)
		slog.Info("cli: resume (codex)",
			slog.String("session_id", req.SessionID))
	} else {
		codexArgs = buildCodexStreamArgs(req)
		if req.Resume {
			slog.Warn("cli: resume requested but no session_id, starting fresh (codex, context amnesia)")
		}
	}

	// Combine: "exec" + baseArgs (e.g. --profile m27) + codexArgs
	// --profile must come after "exec" (codex CLI quirk).
	fullArgs := make([]string, 0, 1+len(baseArgs)+len(codexArgs))
	fullArgs = append(fullArgs, "exec")
	fullArgs = append(fullArgs, baseArgs...)
	fullArgs = append(fullArgs, codexArgs...)

	cmd := exec.CommandContext(ctx, cmdBinary, fullArgs...)
	cmd.Dir = req.WorkDir
	// Inject CLAWBENCH_SCHEDULED=1 for anti-recursion (ISS-085):
	// prevents AI from creating new scheduled tasks during a scheduled execution.
	if req.ScheduledExecution {
		cmd.Env = append(os.Environ(), "CLAWBENCH_SCHEDULED=1")
	} else {
		cmd.Env = os.Environ() // inherit current environment (includes API keys from .env)
	}

	isResume := req.Resume && req.SessionID != ""

	// For resume mode, codex outputs the formatted transcript (with role markers
	// like "codex", "user", "exec") to stderr, and only the bare content to stdout.
	// We need the role markers for parsing, so we pipe stderr in resume mode.
	var stderrBuf bytes.Buffer
	var stderrPipe io.ReadCloser
	if isResume {
		var err error
		stderrPipe, err = cmd.StderrPipe()
		if err != nil {
			return nil, fmt.Errorf("codex stream: failed to create stderr pipe: %w", err)
		}
	} else {
		cmd.Stderr = &stderrBuf
	}

	slog.Info(
		"executing ai stream command",
		slog.String("backend", "codex"),
		slog.String("work_dir", req.WorkDir),
		slog.String("session_id", req.SessionID),
		slog.String("prompt", req.Prompt),
		slog.Bool("resume", req.Resume),
		slog.Any("args", fullArgs),
	)

	// ISS-050: only create stdoutPipe for non-resume mode.
	// In resume mode, Codex writes the formatted transcript to stderr;
	// stdout is unused. Creating but not consuming stdoutPipe causes an
	// FD leak and potential deadlock if Codex writes to stdout.
	var stdoutPipe io.ReadCloser
	if isResume {
		cmd.Stdout = io.Discard // drain stdout to prevent deadlock
	} else {
		var err error
		stdoutPipe, err = cmd.StdoutPipe()
		if err != nil {
			return nil, fmt.Errorf("codex stream: failed to create stdout pipe: %w", err)
		}
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("codex stream: failed to start command: %w", err)
	}

	ch := make(chan StreamEvent, streamChanSize)

	// Collect raw stdout/stderr lines for debugging/analysis (same as CLIBackend)
	var rawLines strings.Builder

	go func() {
		defer close(ch)

		if isResume {
			// Resume mode: "codex exec resume" outputs the formatted transcript
			// (with role markers like "codex", "user", "exec") to stderr.
			// We parse stderr to extract content, thinking, and tool_use events.
			scanner := bufio.NewScanner(stderrPipe)
			buf := make([]byte, scannerInitial)
			scanner.Buffer(buf, scannerMax)
			parseCodexResumeOutput(scanner, ch, req.SessionID, &rawLines)
		} else {
			// New session with --json: parse JSONL from stdout
			scanner := bufio.NewScanner(stdoutPipe)
			buf := make([]byte, scannerInitial)
			scanner.Buffer(buf, scannerMax)

			parser := &CodexStreamParser{}
			var lastCapturedSessionID string
			for scanner.Scan() {
				line := scanner.Text()

				// Skip empty lines and non-JSON lines (e.g., ANSI codes, progress bars)
				if line == "" || !strings.HasPrefix(line, "{") {
					slog.Debug("codex stream: skipping non-JSON line", "line", line)
					continue
				}

				// Collect raw line for debugging
				if rawLines.Len() > 0 {
					rawLines.WriteByte('\n')
				}
				rawLines.WriteString(line)

				slog.Debug("codex stream: raw line", "session_id", req.SessionID, "line", line)
				parser.ParseLine(line, ch)

				// Early capture of external session ID (Codex thread_xxx).
				// This allows the handler to persist the ID immediately, even if the stream
				// is cancelled before turn.completed emits the metadata event.
				if capturedID := parser.GetCapturedSessionID(); capturedID != "" && capturedID != lastCapturedSessionID {
					lastCapturedSessionID = capturedID
					select {
					case ch <- StreamEvent{Type: "session_capture", Content: capturedID}:
					default:
					}
				}

				// Check context after parsing
				select {
				case <-ctx.Done():
					slog.Warn(
						"codex stream: context cancelled",
						slog.String("session_id", req.SessionID),
					)
					// Send raw output before returning so it's available for debugging
					if rawLines.Len() > 0 {
						select {
						case ch <- StreamEvent{Type: "raw_output", RawOutput: rawLines.String()}:
						default:
						}
					}
					return
				default:
				}
			}

			if err := scanner.Err(); err != nil {
				select {
				case ch <- StreamEvent{Type: "warning", Content: fmt.Sprintf("AI output parse error: %v", err), Reason: ReasonParseError}:
				case <-ctx.Done():
				}
			}
		}

		if err := cmd.Wait(); err != nil {
			if ctx.Err() != nil {
				slog.Warn(
					"codex stream: command cancelled",
					slog.String("session_id", req.SessionID),
					slog.String("ctx_err", ctx.Err().Error()),
				)
				// Send raw output before returning
				if rawLines.Len() > 0 {
					select {
					case ch <- StreamEvent{Type: "raw_output", RawOutput: rawLines.String()}:
					default:
					}
				}
				return
			}
			slog.Error(
				"codex stream: command exited abnormally",
				slog.String("session_id", req.SessionID),
				slog.String("exit_error", err.Error()),
			)
			select {
			case ch <- StreamEvent{Type: "warning", Content: "AI backend exited abnormally", Reason: ReasonBackendExit}:
			case <-ctx.Done():
			}
		}

		// Send raw output event after all other events (same as CLIBackend)
		if rawLines.Len() > 0 {
			select {
			case ch <- StreamEvent{Type: "raw_output", RawOutput: rawLines.String()}:
			default:
			}
		}
	}()

	return ch, nil
}
