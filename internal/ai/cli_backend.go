package ai

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"clawbench/internal/model"
)

// CLIBackend is a generic AI backend that shells out to a CLI tool and streams
// JSON output. It implements the AIBackend interface via callbacks for
// backend-specific behavior.
type CLIBackend struct {
	BackendName   string // exported for sub-package construction; Name() method returns this
	Cmd           string // default CLI command
	BuildArgsFn   func(req ChatRequest) []string
	NewParserFn   func() LineParser
	FilterLineFn  func(line string) (string, bool)     // nil = skip empty lines only
	PreStartFn    func(cmd *exec.Cmd, req ChatRequest) // optional, e.g. Claude stdin
	PreExecHookFn func(cmd *exec.Cmd, req ChatRequest) // optional, e.g. Pi API key injection
}

// Name returns the backend identifier (implements AIBackend).
func (b *CLIBackend) Name() string {
	return b.BackendName
}

// ExecuteStream runs the CLI backend in streaming mode and returns a channel of events.
//
//nolint:gocognit,gocyclo // complex stream parsing logic
func (b *CLIBackend) ExecuteStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	args := b.BuildArgsFn(req)

	cmdName := req.Command
	if cmdName == "" {
		cmdName = b.Cmd
	}
	cmd := exec.CommandContext(ctx, cmdName, args...)
	cmd.Dir = req.WorkDir

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

	// Inject API key from agent_api_keys table if available.
	// This enables Pi CLI to authenticate without relying on global auth.json.
	// Env vars are per-process, so concurrent sessions with different providers work correctly.
	if req.AgentID != "" {
		injectAgentAPIKey(cmd, req)
	}
	// Call backend-specific pre-exec hook (e.g. Pi API key injection).
	// When all backends are migrated, injectAgentAPIKey will be replaced by preExecHook.
	if b.PreExecHookFn != nil {
		b.PreExecHookFn(cmd, req)
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
		slog.String("prompt", req.Prompt),
		slog.Any("args", args),
	)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("%s stream: failed to create stdout pipe: %w", b.BackendName, err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%s stream: failed to start command: %w", b.BackendName, err)
	}

	ch := make(chan StreamEvent, streamChanSize)

	// Collect raw stdout lines for debugging/analysis
	var rawLines strings.Builder
	// Track the last emitted captured session ID to avoid duplicate session_capture events
	var lastCapturedSessionID string

	go func() {
		defer close(ch)
		// Ensure cmd.Wait() is always called to reap the child process.
		// If the goroutine returns early (e.g. context cancellation), we must
		// still call Wait() to prevent zombie processes. See ISS-232.
		var waitCalled bool
		defer func() {
			if !waitCalled {
				// exec.CommandContext already sends SIGKILL on cancel,
				// so the process should exit quickly. Best-effort Wait
				// with timeout to reap the process and avoid zombies.
				go func() {
					timer := time.NewTimer(30 * time.Second)
					defer timer.Stop()
					waitCh := make(chan struct{})
					go func() {
						_ = cmd.Wait()
						close(waitCh)
					}()
					select {
					case <-waitCh:
						// Process reaped successfully
					case <-timer.C:
						slog.Warn(b.BackendName+" stream: cmd.Wait() timed out after context cancellation, releasing process",
							slog.String("session_id", req.SessionID))
						_ = cmd.Process.Release()
					}
				}()
			}
		}()

		scanner := bufio.NewScanner(stdoutPipe)
		buf := make([]byte, scannerInitial)
		scanner.Buffer(buf, scannerMax)

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

			// Collect raw line for debugging
			if rawLines.Len() > 0 {
				rawLines.WriteByte('\n')
			}
			rawLines.WriteString(line)

			// Check if this is the final "result" line — send raw_output
			// before parsing so the handler receives it before the "done" event.
			if strings.HasPrefix(line, `{"type":"result"`) {
				select {
				case ch <- StreamEvent{Type: "raw_output", RawOutput: rawLines.String()}:
				default:
				}
			}

			slog.Debug(b.BackendName+" stream: raw line", "session_id", req.SessionID, "line", line)
			parser.ParseLine(line, ch)

			// Early capture of external session ID (OpenCode ses_xxx, Codex thread_xxx).
			// This allows the handler to persist the ID immediately, even if the stream
			// is cancelled before step_finish/turn.completed emits the metadata event.
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
					b.BackendName+" stream: context cancelled",
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

		waitCalled = true
		if err := cmd.Wait(); err != nil {
			if ctx.Err() != nil {
				slog.Warn(
					b.BackendName+" stream: command cancelled",
					slog.String("session_id", req.SessionID),
					slog.String("ctx_err", ctx.Err().Error()),
					slog.String("stderr", stderrBuf.String()),
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
			stderr := stderrBuf.String()
			slog.Error(
				b.BackendName+" stream: command exited abnormally",
				slog.String("session_id", req.SessionID),
				slog.String("exit_error", err.Error()),
				slog.String("stderr", stderr),
			)
			warnMsg := "AI backend exited abnormally"
			if stderr != "" {
				warnMsg = fmt.Sprintf("AI backend exited abnormally\n%s", stderr)
			}
			select {
			case ch <- StreamEvent{Type: "warning", Content: warnMsg, Reason: ReasonBackendExit}:
			case <-ctx.Done():
			}
		} else if stderrBuf.Len() > 0 {
			stderr := stderrBuf.String()
			slog.Warn(
				b.BackendName+" stream: command succeeded with stderr output",
				slog.String("session_id", req.SessionID),
				slog.String("stderr", stderr),
			)
			select {
			case ch <- StreamEvent{Type: "warning", Content: stderr}:
			case <-ctx.Done():
			}
		}

		// Send raw output event after all other events
		if rawLines.Len() > 0 {
			select {
			case ch <- StreamEvent{Type: "raw_output", RawOutput: rawLines.String()}:
			default:
			}
		}
	}()

	return ch, nil
}

// AgentAPIKeyLoader loads an API key for an agent+provider combination.
// AgentAPIKeyLoader loads the API key for a Pi agent.
// Returns (provider, customURL, apiKey, true) on success, or ("", "", "", false) if not found.
// This is injected from the handler/service layer to avoid import cycles.
type AgentAPIKeyLoader func(agentID string) (provider, customURL, apiKey string, found bool)

// agentAPIKeyLoader is the global function for loading agent API keys.
// Set by the application startup via SetAgentAPIKeyLoader.
var agentAPIKeyLoader AgentAPIKeyLoader

// SetAgentAPIKeyLoader sets the function used to load encrypted API keys
// for agents. Must be called once during application startup, after
// service.InitDB(). This avoids import cycles between internal/ai and
// internal/service packages.
func SetAgentAPIKeyLoader(loader AgentAPIKeyLoader) {
	agentAPIKeyLoader = loader
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

// injectAgentAPIKey loads the encrypted API key for the agent from the database
// and injects it as an environment variable on the CLI command. For Pi agents,
// also adds the --provider flag so Pi knows which provider config to use.
// If the agent has no stored API key, this is a no-op (Pi falls back to auth.json).
//
// For custom URL agents (customURL != ""): the provider stored in agent_api_keys
// is the agent ID itself (e.g., "custom-agent"), and Pi uses models.json to find
// the endpoint. We inject --provider {agentID} --api-key {key} directly.
// For built-in providers: we inject the env var (e.g., OPENAI_API_KEY=sk-...)
// and --provider {provider}.
func injectAgentAPIKey(cmd *exec.Cmd, req ChatRequest) {
	if agentAPIKeyLoader == nil {
		return
	}

	agent, ok := model.Agents[req.AgentID]
	if !ok {
		return
	}

	// Only inject for Pi backend (setup-wizard-created agents)
	if agent.Backend != "pi" {
		return
	}

	// Find the provider and API key for this agent — single DB query
	provider, customURL, apiKey, found := agentAPIKeyLoader(req.AgentID)
	if !found || apiKey == "" {
		return // No stored API key — Pi will fall back to auth.json
	}

	// Custom URL mode: provider is the agent ID (set by setup complete).
	// Use --provider {agentID} + --api-key so Pi reads models.json for the endpoint.
	if customURL != "" {
		cmd.Args = append(cmd.Args[:len(cmd.Args)-1], "--provider", provider, "--api-key", apiKey, cmd.Args[len(cmd.Args)-1])
		slog.Debug("injected custom URL API key for agent", "agent_id", req.AgentID, "provider", provider)
		return
	}

	// Built-in provider mode: inject env var + --provider flag
	spec := model.FindProviderSpec(provider)
	if spec != nil && spec.EnvVar != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", spec.EnvVar, apiKey))
		// Add --provider flag to Pi CLI args
		cmd.Args = append(cmd.Args[:len(cmd.Args)-1], "--provider", provider, cmd.Args[len(cmd.Args)-1])
	}

	slog.Debug("injected API key for agent", "agent_id", req.AgentID, "provider", provider)
}
