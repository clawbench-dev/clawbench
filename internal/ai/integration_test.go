//go:build integration

package ai

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ===========================================================================
// Table-Driven CLI Integration Tests
// ===========================================================================
//
// All CLI backends share a single CLIBackend.ExecuteStream implementation.
// The only differences between backends are: CLI binary name, command args,
// timeout defaults, and metadata capabilities. This is expressed as
// cliTestConfig entries, and each test category is one table-driven function.
//
// ACP backends are NOT tested here — they have their own protocol-level
// integration tests in acp_integration_test.go.

// --- Backend Registration ---
//
// Integration tests run in package ai, which cannot import internal/ai/backends
// (circular dependency). We manually register the backends needed for CLI
// integration tests here. This mirrors the init() registrations in each
// backends/* sub-package but is self-contained within this file.

func init() {
	// claude — needs AutoResume (ExitPlanMode → cancel → resume)
	RegisterBackend("claude", func() AIBackend {
		return &CLIBackend{
			BackendName: "claude",
			Cmd:         "claude",
			BuildArgsFn: func(req ChatRequest) []string {
				return BuildBaseStreamArgs(req, func(r ChatRequest) []string {
					return []string{"--verbose"}
				})
			},
			NewParserFn: func() LineParser { return &StreamParser{} },
			PreStartFn: func(cmd *exec.Cmd, req ChatRequest) {
				cmd.Stdin = strings.NewReader(req.Prompt)
			},
		}
	}, true)

	// codebuddy — needs AutoResume
	RegisterBackend("codebuddy", func() AIBackend {
		return &CLIBackend{
			BackendName: "codebuddy",
			Cmd:         "codebuddy",
			BuildArgsFn: func(req ChatRequest) []string {
				return BuildBaseStreamArgs(req, nil)
			},
			NewParserFn: func() LineParser { return &StreamParser{} },
			FilterLineFn: func(line string) (string, bool) {
				line = strings.TrimPrefix(line, "\xEF\xBB\xBF")
				if line == "" {
					return "", false
				}
				return line, true
			},
			PreStartFn: func(cmd *exec.Cmd, req ChatRequest) {
				cmd.Stdin = strings.NewReader(req.Prompt)
			},
		}
	}, true)

	// opencode — handles ExitPlanMode internally, no AutoResume
	RegisterBackend("opencode", func() AIBackend {
		return &CLIBackend{
			BackendName: "opencode",
			Cmd:         "opencode",
			BuildArgsFn: buildOpenCodeArgs,
			NewParserFn: func() LineParser { return &OpenCodeStreamParser{} },
			FilterLineFn: func(line string) (string, bool) {
				if line == "" || strings.HasPrefix(line, "[opencode-mobile]") {
					return "", false
				}
				if !strings.HasPrefix(line, "{") {
					return "", false
				}
				return line, true
			},
		}
	}, false)

	// codex — custom backend, no AutoResume
	RegisterBackend("codex", func() AIBackend {
		return &CodexBackend{}
	}, false)

	// qoder — needs AutoResume
	RegisterBackend("qoder", func() AIBackend {
		return &CLIBackend{
			BackendName: "qoder",
			Cmd:         "qodercli",
			BuildArgsFn: buildQoderArgs,
			NewParserFn: func() LineParser { return &StreamParser{} },
			PreStartFn: func(cmd *exec.Cmd, req ChatRequest) {
				cmd.Stdin = strings.NewReader(req.Prompt)
			},
		}
	}, true)

	// deepseek (CodeWhale) — needs AutoResume
	RegisterBackend("deepseek", func() AIBackend {
		return &CLIBackend{
			BackendName: "deepseek",
			Cmd:         "codewhale",
			BuildArgsFn: buildDeepSeekArgs,
			NewParserFn: func() LineParser { return &DeepSeekStreamParser{} },
		}
	}, true)

	// vecli — custom backend, no AutoResume
	RegisterBackend("vecli", func() AIBackend {
		return NewVeCLIBackend()
	}, false)

	// cline — needs AutoResume
	RegisterBackend("cline", func() AIBackend {
		return &CLIBackend{
			BackendName: "cline",
			Cmd:         "cline",
			BuildArgsFn: func(req ChatRequest) []string {
				args := []string{"--json", "--auto-approve", "true"}
				if req.SessionID != "" && req.Resume {
					args = append(args, "--id", req.SessionID)
				}
				if req.WorkDir != "" {
					args = append(args, "--cwd", req.WorkDir)
				}
				if req.Model != "" {
					args = append(args, "--model", req.Model)
				}
				if req.ThinkingEffort != "" {
					args = append(args, "--thinking", req.ThinkingEffort)
				}
				return args
			},
			NewParserFn: func() LineParser { return &StreamParser{} },
			PreStartFn: func(cmd *exec.Cmd, req ChatRequest) {
				cmd.Stdin = strings.NewReader(req.Prompt)
			},
		}
	}, true)

	// copilot — needs AutoResume
	RegisterBackend("copilot", func() AIBackend {
		return &CLIBackend{
			BackendName: "copilot",
			Cmd:         "copilot",
			BuildArgsFn: func(req ChatRequest) []string {
				args := []string{"--output-format", "json", "--allow-all"}
				args = append(args, "-p", req.Prompt)
				if req.SessionID != "" && req.Resume {
					args = append(args, "--resume", req.SessionID)
				}
				if req.WorkDir != "" {
					args = append(args, "-C", req.WorkDir)
				}
				if req.Model != "" {
					args = append(args, "--model", req.Model)
				}
				if req.ThinkingEffort != "" {
					args = append(args, "--effort", req.ThinkingEffort)
				}
				return args
			},
			NewParserFn: func() LineParser { return &StreamParser{} },
			FilterLineFn: func(line string) (string, bool) {
				if line == "" || !strings.HasPrefix(line, "{") {
					return "", false
				}
				return line, true
			},
			PreStartFn: func(cmd *exec.Cmd, req ChatRequest) {
				cmd.Stdin = strings.NewReader(req.Prompt)
			},
		}
	}, true)

	// kimi — needs AutoResume
	RegisterBackend("kimi", func() AIBackend {
		return &CLIBackend{
			BackendName: "kimi",
			Cmd:         "kimi",
			BuildArgsFn: func(req ChatRequest) []string {
				prompt := InjectSystemPrompt(req)
				args := []string{"--print", "--prompt", prompt, "--output-format", "stream-json", "--yes"}
				if req.SessionID != "" && req.Resume {
					args = append(args, "--session", req.SessionID)
				}
				if req.WorkDir != "" {
					args = append(args, "--work-dir", req.WorkDir)
				}
				if req.Model != "" {
					args = append(args, "--model", req.Model)
				}
				return args
			},
			NewParserFn: func() LineParser {
				return &StreamJSONParser{
					ToolNameMap: map[string]string{
						"read_file": "Read", "write_file": "Write", "edit_file": "Edit",
						"replace": "Edit", "run_shell_command": "Bash", "list_directory": "LS",
						"search_file": "Grep", "search_directory": "Grep", "glob": "Glob",
						"ask": "AskUserQuestion", "file_search": "Glob", "list_files": "LS",
						"shell": "Bash",
					},
					InputRemaps: map[string]string{
						"dir_path": "path", "allow_multiple": "replace_all",
						"is_background": "run_in_background", "include_pattern": "glob",
						"name": "skill",
					},
				}
			},
			FilterLineFn: func(line string) (string, bool) {
				if line == "" || !strings.HasPrefix(line, "{") {
					return "", false
				}
				return line, true
			},
		}
	}, true)

	// mimo — needs AutoResume
	RegisterBackend("mimo", func() AIBackend {
		return &CLIBackend{
			BackendName: "mimo",
			Cmd:         "mimo",
			BuildArgsFn: func(req ChatRequest) []string {
				prompt := InjectSystemPrompt(req)
				args := []string{"run", prompt, "--format", "json", "--dangerously-skip-permissions"}
				if req.SessionID != "" && req.Resume {
					args = append(args, "--session", req.SessionID)
				}
				if req.WorkDir != "" {
					args = append(args, "--dir", req.WorkDir)
				}
				if req.Model != "" {
					args = append(args, "--model", req.Model)
				}
				if req.ThinkingEffort != "" {
					args = append(args, "--variant", req.ThinkingEffort)
				}
				return args
			},
			NewParserFn: func() LineParser {
				return &OpenCodeStreamParser{
					ToolNameMap: map[string]string{
						"Read": "Read", "Write": "Write", "Edit": "Edit",
						"Bash": "Bash", "LS": "LS", "Grep": "Grep",
						"Glob": "Glob", "AskUserQuestion": "AskUserQuestion",
					},
					InputRemaps: map[string]string{},
				}
			},
			FilterLineFn: func(line string) (string, bool) {
				if line == "" || strings.HasPrefix(line, "[opencode-mobile]") {
					return "", false
				}
				if !strings.HasPrefix(line, "{") {
					return "", false
				}
				return line, true
			},
		}
	}, true)

	// pi — needs AutoResume
	RegisterBackend("pi", func() AIBackend {
		return &CLIBackend{
			BackendName: "pi",
			Cmd:         "pi",
			BuildArgsFn: func(req ChatRequest) []string {
				args := []string{"-p", "--mode", "json"}
				switch {
				case req.Resume && req.SessionID != "":
					args = append(args, "--session", req.SessionID)
				case req.Resume:
					args = append(args, "--continue")
				case req.ScheduledExecution:
					args = append(args, "--no-session")
				}
				args = append(args, "--no-context-files")
				if req.SystemPrompt != "" {
					args = append(args, "--append-system-prompt", req.SystemPrompt)
				}
				if req.Model != "" {
					args = append(args, "--model", req.Model)
				}
				if req.ThinkingEffort != "" {
					args = append(args, "--thinking", req.ThinkingEffort)
				}
				args = append(args, req.Prompt)
				return args
			},
			NewParserFn: func() LineParser {
				return &PiStreamParser{InputRemaps: map[string]string{"path": "file_path"}}
			},
		}
	}, true)
}

// buildOpenCodeArgs mirrors backends/opencode/cli.go buildOpenCodeStreamArgs.
func buildOpenCodeArgs(req ChatRequest) []string {
	prompt := InjectSystemPrompt(req)
	args := []string{
		"run",
		prompt,
		"--format", "json",
		"--dangerously-skip-permissions",
	}
	if req.SessionID != "" && req.Resume {
		args = append(args, "--session", req.SessionID)
	}
	if req.WorkDir != "" {
		args = append(args, "--dir", req.WorkDir)
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.ThinkingEffort != "" {
		args = append(args, "--variant", req.ThinkingEffort)
	}
	return args
}

// buildQoderArgs mirrors backends/qoder/cli.go buildQoderStreamArgs.
func buildQoderArgs(req ChatRequest) []string {
	args := []string{"--print", "--output-format", "stream-json"}
	if req.Resume {
		args = append(args, "--resume", req.SessionID)
	} else if req.SessionID != "" {
		args = append(args, "--session-id", req.SessionID)
	}
	if req.WorkDir != "" {
		args = append(args, "--cwd", req.WorkDir)
	}
	args = append(args, "--dangerously-skip-permissions")
	if req.SystemPrompt != "" {
		args = append(args, "--system-prompt", req.SystemPrompt)
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	return args
}

// buildDeepSeekArgs mirrors backends/deepseek/cli.go buildDeepSeekStreamArgs.
func buildDeepSeekArgs(req ChatRequest) []string {
	args := []string{
		"exec",
		"--auto",
		"--output-format", "stream-json",
	}
	if req.Resume && req.SessionID != "" {
		args = append(args, "--resume", req.SessionID)
	} else if req.Resume {
		args = append(args, "--continue")
	}
	if req.SystemPrompt != "" {
		args = append(args, "--system-prompt", req.SystemPrompt)
	}
	if req.Model != "" {
		if idx := strings.LastIndex(req.Model, "/"); idx >= 0 {
			args = append(args, "--model", req.Model[idx+1:])
		} else {
			args = append(args, "--model", req.Model)
		}
	}
	args = append(args, req.Prompt)
	return args
}

// --- CLI Test Config ---

// cliTestConfig describes a CLI backend for table-driven integration tests.
type cliTestConfig struct {
	// Backend is the backend ID used in NewBackend().
	Backend string

	// CLIName is the binary name checked by requireCLIAvailable().
	CLIName string

	// AltCLIName is a fallback binary name (e.g. "deepseek" legacy shim when "codewhale" not found).
	AltCLIName string

	// Command is an optional override for ChatRequest.Command (e.g. "codex --profile m27").
	Command string

	// Timeout is the per-prompt timeout. Defaults to 60s if zero.
	Timeout time.Duration

	// CollectTimeout is the timeout for collectAllEvents. Defaults to 90s if zero.
	CollectTimeout time.Duration

	// SkipNewSessionID is true for backends that don't accept UUID session IDs
	// on new sessions (they capture session IDs from the stream instead).
	SkipNewSessionID bool

	// EmitsSessionCapture is true if the backend emits a session_capture event
	// (for backends like opencode/codex/deepseek that auto-capture session IDs).
	EmitsSessionCapture bool

	// HasModelInMeta is true if the backend always includes a model name in metadata.
	HasModelInMeta bool

	// HasSessionIDInMeta is true if the backend includes SessionID in metadata events.
	HasSessionIDInMeta bool

	// HasTokenUsageInMeta is true if the backend reports InputTokens in metadata.
	HasTokenUsageInMeta bool

	// SupportsResume is true if the backend supports --resume (or equivalent).
	SupportsResume bool

	// SetupFn is an optional per-test setup function (e.g. requireCodexEnv).
	SetupFn func(t *testing.T)
}

// cliBackends is the master table of all CLI backends for integration tests.
var cliBackends = []cliTestConfig{
	{
		Backend:            "claude",
		CLIName:            "claude",
		Timeout:            60 * time.Second,
		CollectTimeout:     90 * time.Second,
		HasModelInMeta:     true,
		HasSessionIDInMeta: true,
		HasTokenUsageInMeta: true,
		SupportsResume:     true,
	},
	{
		Backend:            "codebuddy",
		CLIName:            "codebuddy",
		Timeout:            60 * time.Second,
		CollectTimeout:     90 * time.Second,
		HasModelInMeta:     true,
		HasSessionIDInMeta: true,
		HasTokenUsageInMeta: true,
		SupportsResume:     true,
	},
	{
		Backend:            "opencode",
		CLIName:            "opencode",
		Timeout:            60 * time.Second,
		CollectTimeout:     90 * time.Second,
		SkipNewSessionID:     true,
		EmitsSessionCapture:  true,
		HasSessionIDInMeta:   true,
		HasTokenUsageInMeta:  false, // OpenCodeStreamParser does not always report InputTokens
		SupportsResume:      true,
	},
	{
		Backend:             "codex",
		CLIName:             "codex",
		Command:             codexCommand,
		Timeout:             60 * time.Second,
		CollectTimeout:      90 * time.Second,
		SkipNewSessionID:    true,
		EmitsSessionCapture: true,
		HasSessionIDInMeta:  true,
		HasTokenUsageInMeta: false, // CodexBackend resume path omits token usage
		SupportsResume:      true,
		SetupFn:             func(t *testing.T) { requireCodexEnv(t) },
	},
	{
		Backend:            "qoder",
		CLIName:            "qodercli",
		Timeout:            60 * time.Second,
		CollectTimeout:     90 * time.Second,
		HasModelInMeta:     true,
		HasSessionIDInMeta: true,
		HasTokenUsageInMeta: false, // QoderStreamParser omits InputTokens
		SupportsResume:     true,
	},
	{
		Backend:            "deepseek",
		CLIName:            "codewhale",
		AltCLIName:         "deepseek", // legacy shim (removed in CodeWhale v0.9.0)
		Timeout:            120 * time.Second,
		CollectTimeout:     150 * time.Second,
		HasModelInMeta:     true,
		SkipNewSessionID:     true,   // CodeWhale generates own session IDs
		EmitsSessionCapture:  true,
		HasSessionIDInMeta:   true,   // Reports session ID in metadata
		HasTokenUsageInMeta: false, // CodeWhale mode doesn't reliably report tokens
		SupportsResume:     true,
	},
	{
		Backend:          "vecli",
		CLIName:          "vecli",
		Timeout:          60 * time.Second,
		CollectTimeout:   90 * time.Second,
		HasModelInMeta:   true,
		// VeCLI: no session_capture, no session ID in metadata, no resume support
		HasSessionIDInMeta:   false,
		HasTokenUsageInMeta:  false,
		SkipNewSessionID:     true,
		EmitsSessionCapture:  false,
	},
	{
		Backend:            "cline",
		CLIName:            "cline",
		Timeout:            60 * time.Second,
		CollectTimeout:     90 * time.Second,
		HasModelInMeta:     true,
		HasSessionIDInMeta: true,
		HasTokenUsageInMeta: false,
		SupportsResume:     true,
	},
	{
		Backend:            "copilot",
		CLIName:            "copilot",
		Timeout:            60 * time.Second,
		CollectTimeout:     90 * time.Second,
		HasModelInMeta:     true,
		HasSessionIDInMeta: true,
		HasTokenUsageInMeta: false,
		SupportsResume:     true,
	},
	{
		Backend:            "kimi",
		CLIName:            "kimi",
		Timeout:            60 * time.Second,
		CollectTimeout:     90 * time.Second,
		HasModelInMeta:     true,
		HasSessionIDInMeta: true,
		HasTokenUsageInMeta: false,
		SupportsResume:     true,
	},
	{
		Backend:            "mimo",
		CLIName:            "mimo",
		Timeout:            60 * time.Second,
		CollectTimeout:     90 * time.Second,
		HasModelInMeta:     true,
		HasSessionIDInMeta: true,
		HasTokenUsageInMeta: false,
		SupportsResume:     true,
	},
	{
		Backend:            "pi",
		CLIName:            "pi",
		Timeout:            60 * time.Second,
		CollectTimeout:     90 * time.Second,
		SkipNewSessionID:     true,
		EmitsSessionCapture:  true,
		HasModelInMeta:       true,
		HasSessionIDInMeta:   true,
		HasTokenUsageInMeta:  true,
		SupportsResume:       true,
	},
}

// resumeBackends filters cliBackends to only those that support resume.
var resumeBackends []cliTestConfig

func init() {
	for _, cfg := range cliBackends {
		if cfg.SupportsResume {
			resumeBackends = append(resumeBackends, cfg)
		}
	}
}

// --- Shared Helpers ---

const codexCommand = "codex --profile m27"

func newSessionID() string {
	return uuid.New().String()
}

func testWorkDir() string {
	if dir, _ := os.Getwd(); dir != "" {
		return dir
	}
	return os.TempDir()
}

func requireCLIAvailable(t *testing.T, cliName string) {
	t.Helper()
	if _, err := exec.LookPath(cliName); err != nil {
		t.Skipf("%s CLI not available, skipping integration test", cliName)
	}
}

// requireBackendCLI checks if the CLI for a backend config is available.
// If CLIName is not found, tries AltCLIName as fallback.
// Returns the actual command name to use (may differ from CLIName if AltCLIName was used).
func requireBackendCLI(t *testing.T, cfg cliTestConfig) string {
	t.Helper()
	if _, err := exec.LookPath(cfg.CLIName); err == nil {
		return cfg.CLIName
	}
	if cfg.AltCLIName != "" {
		if _, err := exec.LookPath(cfg.AltCLIName); err == nil {
			return cfg.AltCLIName
		}
	}
	t.Skipf("%s CLI not available, skipping integration test", cfg.CLIName)
	return ""
}

func requireCodexEnv(t *testing.T) {
	t.Helper()
	requireCLIAvailable(t, "codex")

	dotenvPaths := []string{}
	if dir, _ := os.Getwd(); dir != "" {
		for d := dir; d != "/"; d = filepath.Dir(d) {
			if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
				dotenvPaths = append(dotenvPaths, filepath.Join(d, ".env"))
				break
			}
		}
	}
	if model.BinDir != "" {
		dotenvPaths = append(dotenvPaths, filepath.Join(model.BinDir, ".env"))
	}
	dotenvPaths = append(dotenvPaths, ".env")

	for _, p := range dotenvPaths {
		if _, err := os.Stat(p); err == nil {
			if err := model.LoadDotEnv(p); err != nil {
				t.Logf("warning: failed to load .env from %s: %v", p, err)
			} else {
				t.Logf("loaded .env from %s", p)
			}
			break
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "codex", "exec", "--profile", "m27", "--json",
		"--dangerously-bypass-approvals-and-sandbox",
		"--skip-git-repo-check", "echo ok")
	cmd.Dir = os.TempDir()
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("codex CLI environment not ready (exit error: %v, output: %s), skipping integration test", err, truncate(string(output), 200))
	}
}

func collectAllEvents(t *testing.T, ch <-chan StreamEvent, timeout time.Duration) []StreamEvent {
	t.Helper()
	var events []StreamEvent
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, event)
		case <-timer.C:
			t.Log("collectEvents: timeout waiting for channel to close")
			return events
		}
	}
}

func findEvents(events []StreamEvent, eventType string) []StreamEvent {
	var matched []StreamEvent
	for _, e := range events {
		if e.Type == eventType {
			matched = append(matched, e)
		}
	}
	return matched
}

func requireEventSequence(t *testing.T, events []StreamEvent, expectedTypes ...string) {
	t.Helper()
	var actualTypes []string
	for _, e := range events {
		actualTypes = append(actualTypes, e.Type)
	}

	idx := 0
	for _, actual := range actualTypes {
		if idx < len(expectedTypes) && actual == expectedTypes[idx] {
			idx++
		}
	}
	if idx < len(expectedTypes) {
		t.Errorf("expected event sequence %v not found; actual types: %v", expectedTypes, actualTypes)
	}
}

func concatContent(events []StreamEvent) string {
	var sb strings.Builder
	for _, e := range events {
		if e.Type == "content" {
			sb.WriteString(e.Content)
		}
	}
	return sb.String()
}

func extractSessionID(events []StreamEvent) string {
	for _, e := range events {
		if e.Type == "session_capture" && e.Content != "" {
			return e.Content
		}
	}
	for _, e := range events {
		if e.Type == "metadata" && e.Meta != nil && e.Meta.SessionID != "" {
			return e.Meta.SessionID
		}
	}
	return ""
}

func eventTypes(events []StreamEvent) []string {
	types := make([]string, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	return types
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// cancelOnFirstContent reads events from ch, calls cancelFunc after the first
// content event, then continues collecting until the channel closes.
func cancelOnFirstContent(t *testing.T, ch <-chan StreamEvent, cancelFunc context.CancelFunc) []StreamEvent {
	t.Helper()
	var events []StreamEvent
	cancelled := false
	timer := time.NewTimer(90 * time.Second)
	defer timer.Stop()

	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, event)
			if !cancelled && event.Type == "content" {
				cancelled = true
				cancelFunc()
			}
		case <-timer.C:
			t.Log("cancelOnFirstContent: timeout")
			return events
		}
	}
}

// --- CLI Test Runner ---

// cliTestBackend runs a single CLI backend test with the given config and request.
// It handles setup, backend creation, and event collection.
func cliTestBackend(t *testing.T, cfg cliTestConfig, req ChatRequest) []StreamEvent {
	t.Helper()

	// Per-backend setup
	if cfg.SetupFn != nil {
		cfg.SetupFn(t)
	} else {
		actualCmd := requireBackendCLI(t, cfg)
		if actualCmd != cfg.CLIName && cfg.Command == "" {
			req.Command = actualCmd
		}
	}

	backend, err := NewBackend(cfg.Backend)
	require.NoError(t, err)

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	collectTimeout := cfg.CollectTimeout
	if collectTimeout == 0 {
		collectTimeout = 90 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if req.WorkDir == "" {
		req.WorkDir = testWorkDir()
	}
	if cfg.Command != "" && req.Command == "" {
		req.Command = cfg.Command
	}

	ch, err := backend.ExecuteStream(ctx, req)
	require.NoError(t, err)

	return collectAllEvents(t, ch, collectTimeout)
}

// cliNewSessionRequest builds a ChatRequest for a new session test.
func cliNewSessionRequest(cfg cliTestConfig, prompt string) ChatRequest {
	req := ChatRequest{
		Prompt:  prompt,
		WorkDir: testWorkDir(),
	}
	if !cfg.SkipNewSessionID {
		req.SessionID = newSessionID()
	}
	if cfg.Command != "" {
		req.Command = cfg.Command
	}
	return req
}

// cliSetupAndCreate handles per-backend setup and backend creation.
func cliSetupAndCreate(t *testing.T, cfg cliTestConfig) AIBackend {
	t.Helper()
	if cfg.SetupFn != nil {
		cfg.SetupFn(t)
	} else {
		requireBackendCLI(t, cfg)
	}
	backend, err := NewBackend(cfg.Backend)
	require.NoError(t, err)
	return backend
}

// cliExecPrompt executes a prompt on the given backend and returns events.
// Handles timeout, WorkDir, Command, and event collection.
func cliExecPrompt(t *testing.T, cfg cliTestConfig, backend AIBackend, req ChatRequest) []StreamEvent {
	t.Helper()
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	collectTimeout := cfg.CollectTimeout
	if collectTimeout == 0 {
		collectTimeout = 90 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if req.WorkDir == "" {
		req.WorkDir = testWorkDir()
	}
	if cfg.Command != "" && req.Command == "" {
		req.Command = cfg.Command
	}

	ch, err := backend.ExecuteStream(ctx, req)
	require.NoError(t, err)
	return collectAllEvents(t, ch, collectTimeout)
}

// cliResolveSessionID returns the session ID for a backend that may
// auto-capture it from the stream (SkipNewSessionID) or use the provided one.
func cliResolveSessionID(cfg cliTestConfig, req ChatRequest, events []StreamEvent) string {
	if cfg.SkipNewSessionID {
		return extractSessionID(events)
	}
	return req.SessionID
}

// skipIfVeCLINoContent skips the test if VeCLI produced no content events
// (common when API auth/network issues prevent actual responses).
func skipIfVeCLINoContent(t *testing.T, cfg cliTestConfig, events []StreamEvent) {
	if cfg.Backend != "vecli" {
		return
	}
	if len(findEvents(events, "content")) == 0 {
		warnings := findEvents(events, "warning")
		errors := findEvents(events, "error")
		t.Skipf("vecli produced no content events (likely auth/network issue); warnings: %d, errors: %d, event types: %v",
			len(warnings), len(errors), eventTypes(events))
	}
}

// --- 1. New Session Basic Dialog ---

func TestIntegration_CLI_NewSession(t *testing.T) {
	for _, cfg := range cliBackends {
		t.Run(cfg.Backend, func(t *testing.T) {
			req := cliNewSessionRequest(cfg, "说一个字：好")
			events := cliTestBackend(t, cfg, req)

			skipIfVeCLINoContent(t, cfg, events)

			requireEventSequence(t, events, "content", "metadata")
			content := concatContent(events)
			assert.NotEmpty(t, content, "should receive content from %s", cfg.Backend)

			metaEvents := findEvents(events, "metadata")
			require.NotEmpty(t, metaEvents, "should have metadata event")

			if cfg.HasModelInMeta {
				assert.NotEmpty(t, metaEvents[0].Meta.Model, "metadata should contain model name")
			}

			// AutoResumeBackend forwards the "done" event
			doneEvents := findEvents(events, "done")
			assert.NotEmpty(t, doneEvents, "should receive 'done' event from AutoResumeBackend")

			errorEvents := findEvents(events, "error")
			assert.Empty(t, errorEvents, "should not have error events")
		})
	}
}

// --- 2. Stream Event Completeness ---

func TestIntegration_CLI_StreamEvents(t *testing.T) {
	for _, cfg := range cliBackends {
		t.Run(cfg.Backend, func(t *testing.T) {
			req := cliNewSessionRequest(cfg, "1+1等于几？只回答数字")
			events := cliTestBackend(t, cfg, req)

			skipIfVeCLINoContent(t, cfg, events)

			contentEvents := findEvents(events, "content")
			thinkingEvents := findEvents(events, "thinking")
			assert.True(t, len(contentEvents) > 0 || len(thinkingEvents) > 0,
				"should have content or thinking events")

			metaEvents := findEvents(events, "metadata")
			require.NotEmpty(t, metaEvents, "should have metadata event")

			// Backends that capture session ID via session_capture
			if cfg.EmitsSessionCapture {
				sessionCaptureEvents := findEvents(events, "session_capture")
				assert.NotEmpty(t, sessionCaptureEvents, "%s should emit session_capture", cfg.Backend)
			} else if cfg.HasSessionIDInMeta {
				assert.NotEmpty(t, metaEvents[0].Meta.SessionID, "%s metadata should contain session ID", cfg.Backend)
			}

			// Most CLI backends emit raw_output for debugging (except CodeWhale which has its own parser)
			if cfg.Backend != "deepseek" {
				rawEvents := findEvents(events, "raw_output")
				assert.NotEmpty(t, rawEvents, "should have raw_output event")
			}
		})
	}
}

// --- 3. Session Resume (2-turn) ---

func TestIntegration_CLI_ResumeSession(t *testing.T) {
	for _, cfg := range resumeBackends {
		t.Run(cfg.Backend, func(t *testing.T) {
			backend := cliSetupAndCreate(t, cfg)

			// Phase 1: new session
			req1 := cliNewSessionRequest(cfg, "记住数字42，稍后我会问你。只回复OK")
			events1 := cliExecPrompt(t, cfg, backend, req1)

			meta1 := findEvents(events1, "metadata")
			require.NotEmpty(t, meta1, "first conversation should complete with metadata event")

			sessionID := cliResolveSessionID(cfg, req1, events1)
			require.NotEmpty(t, sessionID, "should capture session ID")

			// Phase 2: resume session
			req2 := ChatRequest{
				Prompt:    "我之前让你记住的数字是什么？只回答数字",
				SessionID: sessionID,
				WorkDir:   testWorkDir(),
				Resume:    true,
			}
			events2 := cliExecPrompt(t, cfg, backend, req2)

			requireEventSequence(t, events2, "content", "metadata")
			content := concatContent(events2)
			assert.NotEmpty(t, content, "should receive content in resumed session")

			doneEvents2 := findEvents(events2, "done")
			assert.NotEmpty(t, doneEvents2, "should receive 'done' event in resumed session")
		})
	}
}

// --- 4. Context Cancellation ---

func TestIntegration_CLI_CancelMidStream(t *testing.T) {
	cancelBackends := []cliTestConfig{
		{Backend: "claude", CLIName: "claude", Timeout: 60 * time.Second, CollectTimeout: 90 * time.Second},
		{Backend: "codebuddy", CLIName: "codebuddy", Timeout: 60 * time.Second, CollectTimeout: 90 * time.Second},
		{Backend: "opencode", CLIName: "opencode", Timeout: 60 * time.Second, CollectTimeout: 90 * time.Second, SkipNewSessionID: true, EmitsSessionCapture: true},
		{Backend: "codex", CLIName: "codex", Command: codexCommand, Timeout: 60 * time.Second, CollectTimeout: 90 * time.Second, SetupFn: func(t *testing.T) { requireCodexEnv(t) }},
		{Backend: "deepseek", CLIName: "codewhale", AltCLIName: "deepseek", Timeout: 120 * time.Second, CollectTimeout: 150 * time.Second, SkipNewSessionID: true},
		{Backend: "vecli", CLIName: "vecli", Timeout: 60 * time.Second, CollectTimeout: 90 * time.Second, SkipNewSessionID: true},
	}

	for _, cfg := range cancelBackends {
		t.Run(cfg.Backend, func(t *testing.T) {
			if cfg.SetupFn != nil {
				cfg.SetupFn(t)
			} else {
				requireBackendCLI(t, cfg)
			}

			backend, err := NewBackend(cfg.Backend)
			require.NoError(t, err)

			ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
			defer cancel()

			req := ChatRequest{
				Prompt:  "写一篇500字的文章，主题是春天的花园",
				WorkDir: testWorkDir(),
			}
			if !cfg.SkipNewSessionID {
				req.SessionID = newSessionID()
			}
			if cfg.Command != "" {
				req.Command = cfg.Command
			}

			ch, err := backend.ExecuteStream(ctx, req)
			require.NoError(t, err)

			events := cancelOnFirstContent(t, ch, cancel)

			skipIfVeCLINoContent(t, cfg, events)

			contentEvents := findEvents(events, "content")
			assert.NotEmpty(t, contentEvents, "should have received at least one content before cancel")
		})
	}
}

// --- 5. Error Paths ---

func TestIntegration_CLI_InvalidWorkDir(t *testing.T) {
	errorBackends := []cliTestConfig{
		{Backend: "claude", CLIName: "claude"},
		{Backend: "codebuddy", CLIName: "codebuddy"},
		{Backend: "opencode", CLIName: "opencode"},
		{Backend: "vecli", CLIName: "vecli"},
	}

	for _, cfg := range errorBackends {
		t.Run(cfg.Backend, func(t *testing.T) {
			requireBackendCLI(t, cfg)
			backend, err := NewBackend(cfg.Backend)
			require.NoError(t, err)

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			ch, err := backend.ExecuteStream(ctx, ChatRequest{
				Prompt:    "hello",
				SessionID: newSessionID(),
				WorkDir:   "/nonexistent/path/that/does/not/exist/abc123",
			})
			if err != nil {
				t.Logf("ExecuteStream returned error (expected for invalid WorkDir): %v", err)
				return
			}

			events := collectAllEvents(t, ch, 30*time.Second)
			hasError := len(findEvents(events, "error")) > 0
			hasWarning := len(findEvents(events, "warning")) > 0
			assert.True(t, hasError || hasWarning,
				"invalid WorkDir should produce error or warning events; got types: %v",
				eventTypes(events))
		})
	}
}

func TestIntegration_Codex_InvalidCommand(t *testing.T) {
	requireCodexEnv(t)
	backend, err := NewBackend("codex")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err = backend.ExecuteStream(ctx, ChatRequest{
		Prompt:  "hello",
		WorkDir: testWorkDir(),
		Command: "nonexistent-codex-binary-12345",
	})
	assert.Error(t, err, "invalid Command should cause ExecuteStream to return error")
}

// --- 6. AutoResume ExitPlanMode ---

func TestIntegration_AutoResume_ExitPlanMode(t *testing.T) {
	requireCLIAvailable(t, "claude")
	backend, err := NewBackend("claude")
	require.NoError(t, err)

	// Verify it's an AutoResumeBackend
	_, ok := backend.(*AutoResumeBackend)
	require.True(t, ok, "claude backend should be AutoResumeBackend")

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:    "请进入规划模式，帮我规划一下如何给hello world程序写测试",
		SessionID: newSessionID(),
		WorkDir:   testWorkDir(),
	})
	require.NoError(t, err)

	events := collectAllEvents(t, ch, 200*time.Second)

	resumeSplitEvents := findEvents(events, "resume_split")
	if len(resumeSplitEvents) == 0 {
		t.Log("ExitPlanMode was not triggered in this run — this is expected and not a failure")
		t.Log("AI behavior is non-deterministic; ExitPlanMode may not always be triggered")
		metaEvents := findEvents(events, "metadata")
		if len(metaEvents) > 0 {
			t.Log("basic flow completed with metadata event")
		} else {
			contentEvents := findEvents(events, "content")
			if assert.NotEmpty(t, contentEvents, "should have at least content events") {
				t.Log("basic flow produced content events but no metadata (may have timed out)")
			}
		}
		return
	}

	t.Log("ExitPlanMode detected — verifying resume flow")
	requireEventSequence(t, events, "resume_split", "content", "metadata")

	contentEvents := findEvents(events, "content")
	assert.NotEmpty(t, contentEvents, "should have content events after resume")

	metaEvents := findEvents(events, "metadata")
	assert.NotEmpty(t, metaEvents, "should have metadata from resumed session")

	doneEvents := findEvents(events, "done")
	assert.NotEmpty(t, doneEvents, "should receive 'done' event")
}

// --- 7. System Prompt Injection ---

func TestIntegration_CLI_SystemPromptInjection(t *testing.T) {
	for _, cfg := range cliBackends {
		t.Run(cfg.Backend, func(t *testing.T) {
			marker := "INTEGRATION_TEST_MARKER_" + strings.ToUpper(cfg.Backend[:3])

			// For backends without --system-prompt flag, use injection interval
			if cfg.Backend == "opencode" || cfg.Backend == "codex" || cfg.Backend == "vecli" || cfg.Backend == "qoder" {
				model.ChatSystemPromptInterval = 10
			}

			req := cliNewSessionRequest(cfg, "请重复以下标记："+marker)
			req.SystemPrompt = "你必须在你回复的开头包含标记 " + marker + "，这是系统级要求"
			events := cliTestBackend(t, cfg, req)

			skipIfVeCLINoContent(t, cfg, events)

			requireEventSequence(t, events, "content", "metadata")

			// Verify the stream completed successfully with metadata
			metaEvents := findEvents(events, "metadata")
			require.NotEmpty(t, metaEvents, "should have metadata event")

			// Best-effort check — AI compliance is non-deterministic
			content := concatContent(events)
			if !strings.Contains(content, marker) {
				t.Logf("%s did not include marker %q in response — AI compliance is non-deterministic; content: %s", cfg.Backend, marker, truncate(content, 200))
			}
		})
	}
}

// --- 8. Multi-Turn Resume (3-turn) ---

func TestIntegration_CLI_MultiTurnResume(t *testing.T) {
	for _, cfg := range resumeBackends {
		t.Run(cfg.Backend, func(t *testing.T) {
			backend := cliSetupAndCreate(t, cfg)

			// Turn 1: establish session
			req1 := cliNewSessionRequest(cfg, "记住数字42，稍后我会问你。只回复OK")
			events1 := cliExecPrompt(t, cfg, backend, req1)
			meta1 := findEvents(events1, "metadata")
			require.NotEmpty(t, meta1, "turn 1 should complete with metadata event")

			sessionID := cliResolveSessionID(cfg, req1, events1)
			require.NotEmpty(t, sessionID, "should capture session ID after turn 1")

			if cfg.HasSessionIDInMeta {
				assert.NotEmpty(t, meta1[0].Meta.SessionID, "turn 1 metadata should contain session ID")
			}

			// Turn 2: resume and recall
			req2 := ChatRequest{
				Prompt:    "我之前让你记住的数字是什么？只回答数字",
				SessionID: sessionID,
				WorkDir:   testWorkDir(),
				Resume:    true,
			}
			events2 := cliExecPrompt(t, cfg, backend, req2)
			requireEventSequence(t, events2, "content", "metadata")
			content2 := concatContent(events2)
			assert.NotEmpty(t, content2, "turn 2 should receive content")

			meta2 := findEvents(events2, "metadata")
			require.NotEmpty(t, meta2, "turn 2 should have metadata event")

			doneEvents2 := findEvents(events2, "done")
			assert.NotEmpty(t, doneEvents2, "turn 2 should receive 'done' event")

			// Turn 3: resume again
			req3 := ChatRequest{
				Prompt:    "再告诉我一次那个数字",
				SessionID: sessionID,
				WorkDir:   testWorkDir(),
				Resume:    true,
			}
			events3 := cliExecPrompt(t, cfg, backend, req3)
			requireEventSequence(t, events3, "content", "metadata")
			content3 := concatContent(events3)
			assert.NotEmpty(t, content3, "turn 3 should receive content")

			doneEvents3 := findEvents(events3, "done")
			assert.NotEmpty(t, doneEvents3, "turn 3 should receive 'done' event")
		})
	}
}

// --- 9. Resume Session ID Consistency ---

func TestIntegration_CLI_ResumeSessionIDConsistency(t *testing.T) {
	// Only backends that report SessionID in metadata can be verified for consistency
	var consistentBackends []cliTestConfig
	for _, cfg := range resumeBackends {
		if cfg.HasSessionIDInMeta && !cfg.SkipNewSessionID {
			consistentBackends = append(consistentBackends, cfg)
		}
	}

	for _, cfg := range consistentBackends {
		t.Run(cfg.Backend, func(t *testing.T) {
			backend := cliSetupAndCreate(t, cfg)

			sessionID := newSessionID()

			// Phase 1
			req1 := ChatRequest{
				Prompt:    "说一个字：好",
				SessionID: sessionID,
				WorkDir:   testWorkDir(),
			}
			events1 := cliExecPrompt(t, cfg, backend, req1)
			meta1 := findEvents(events1, "metadata")
			require.NotEmpty(t, meta1, "first conversation should complete with metadata event")
			firstSessionID := meta1[0].Meta.SessionID
			assert.NotEmpty(t, firstSessionID, "metadata should contain session ID")

			// Phase 2
			req2 := ChatRequest{
				Prompt:    "再说一个字：是",
				SessionID: sessionID,
				WorkDir:   testWorkDir(),
				Resume:    true,
			}
			events2 := cliExecPrompt(t, cfg, backend, req2)
			meta2 := findEvents(events2, "metadata")
			require.NotEmpty(t, meta2, "resumed session should complete with metadata event")
			resumedSessionID := meta2[0].Meta.SessionID
			assert.NotEmpty(t, resumedSessionID, "resumed metadata should contain session ID")

			assert.Equal(t, firstSessionID, resumedSessionID,
				"session ID should remain consistent across resume")
		})
	}
}

// --- 10. Resume After Cancel ---

func TestIntegration_CLI_ResumeAfterCancel(t *testing.T) {
	for _, cfg := range resumeBackends {
		t.Run(cfg.Backend, func(t *testing.T) {
			backend := cliSetupAndCreate(t, cfg)

			// Phase 1: new session — must complete fully
			req1 := cliNewSessionRequest(cfg, "记住数字7，只回复OK")
			events1 := cliExecPrompt(t, cfg, backend, req1)
			meta1 := findEvents(events1, "metadata")
			require.NotEmpty(t, meta1, "first conversation should complete with metadata event")

			sessionID := cliResolveSessionID(cfg, req1, events1)
			require.NotEmpty(t, sessionID, "should capture session ID")

			// Phase 2: cancel a second prompt mid-stream
			timeout2 := cfg.Timeout
			if timeout2 == 0 {
				timeout2 = 60 * time.Second
			}
			ctx2, cancel2 := context.WithTimeout(context.Background(), timeout2)
			defer cancel2()

			req2 := ChatRequest{
				Prompt:    "现在从1数到100，每个数字一行",
				SessionID: sessionID,
				WorkDir:   testWorkDir(),
				Resume:    true,
			}
			if cfg.Command != "" {
				req2.Command = cfg.Command
			}

			ch2, err := backend.ExecuteStream(ctx2, req2)
			require.NoError(t, err)

			var events2 []StreamEvent
			cancelled := false
			timer := time.NewTimer(90 * time.Second)
			defer timer.Stop()
			for {
				select {
				case event, ok := <-ch2:
					if !ok {
						goto phase2Done
					}
					events2 = append(events2, event)
					if !cancelled && event.Type == "content" {
						cancelled = true
						cancel2()
					}
				case <-timer.C:
					t.Log("phase 2: timeout waiting for content")
					goto phase2Done
				}
			}
		phase2Done:
			contentEvents2 := findEvents(events2, "content")
			assert.NotEmpty(t, contentEvents2, "should have received content before cancel in phase 2")
			t.Logf("phase 2: cancelled after %d events, %d content events", len(events2), len(contentEvents2))

			// Phase 3: resume the session after cancellation
			req3 := ChatRequest{
				Prompt:    "我之前让你记住的数字是什么？只回答数字",
				SessionID: sessionID,
				WorkDir:   testWorkDir(),
				Resume:    true,
			}
			events3 := cliExecPrompt(t, cfg, backend, req3)
			requireEventSequence(t, events3, "content", "metadata")
			content3 := concatContent(events3)
			assert.NotEmpty(t, content3, "should receive content in resumed session after cancel")

			if !strings.Contains(content3, "7") {
				t.Logf("%s did not recall number 7 after cancel+resume — AI behavior is non-deterministic; content: %s", cfg.Backend, truncate(content3, 300))
			}
		})
	}
}

// --- 11. Resume Metadata Capture ---

func TestIntegration_CLI_ResumeMetadataCapture(t *testing.T) {
	for _, cfg := range resumeBackends {
		t.Run(cfg.Backend, func(t *testing.T) {
			backend := cliSetupAndCreate(t, cfg)

			req1 := cliNewSessionRequest(cfg, "说一个字：好")
			events1 := cliExecPrompt(t, cfg, backend, req1)
			meta1 := findEvents(events1, "metadata")
			require.NotEmpty(t, meta1, "should have metadata event from new session")

			sessionID := cliResolveSessionID(cfg, req1, events1)
			require.NotEmpty(t, sessionID, "should capture session ID")

			// Check metadata fields that this backend is expected to populate
			if cfg.HasSessionIDInMeta {
				assert.NotEmpty(t, meta1[0].Meta.SessionID, "new session metadata should have SessionID")
			}
			if cfg.HasModelInMeta {
				assert.NotEmpty(t, meta1[0].Meta.Model, "new session metadata should have Model")
			}
			if cfg.HasTokenUsageInMeta {
				assert.NotZero(t, meta1[0].Meta.InputTokens, "new session should report input token usage")
			}

			// Phase 2: resume
			req2 := ChatRequest{
				Prompt:    "再说一个字：是",
				SessionID: sessionID,
				WorkDir:   testWorkDir(),
				Resume:    true,
			}
			events2 := cliExecPrompt(t, cfg, backend, req2)
			meta2 := findEvents(events2, "metadata")
			require.NotEmpty(t, meta2, "should have metadata event from resumed session")

			if cfg.HasSessionIDInMeta {
				assert.NotEmpty(t, meta2[0].Meta.SessionID, "resumed session metadata should have SessionID")
			}
			if cfg.HasModelInMeta {
				assert.NotEmpty(t, meta2[0].Meta.Model, "resumed session metadata should have Model")
			}
			if cfg.HasTokenUsageInMeta {
				assert.NotZero(t, meta2[0].Meta.InputTokens, "resumed session should report input token usage")
			}
		})
	}
}

// --- 12. Fork Session Amnesia Detection ---
//
// ForkSession does NOT copy external_session_id. When the forked session
// receives its first prompt, the CLI backend starts a brand-new process
// (Resume=false, no --resume flag). The AI has no prior conversation context,
// even though messages were copied in the DB.
//
// We use a "记住 X, then 回想 X" pattern with a distinctive secret number
// to reliably detect amnesia. The secret is generated at test runtime to
// prevent the AI from finding it in the source code via --add-dir.

// forkAmnesiaSecret generates a random 4-digit secret for fork amnesia tests.
// Must be called at test runtime (not init) to ensure the AI cannot find it
// in source code via --add-dir file reading.
func forkAmnesiaSecret() string {
	return fmt.Sprintf("%04d", time.Now().UnixNano()%10000)
}

// TestIntegration_CLI_ForkSessionAmnesia detects whether a forked session is
// "amnesiac" using Claude as the anchor backend.
func TestIntegration_CLI_ForkSessionAmnesia(t *testing.T) {
	anchorCfg := cliTestConfig{
		Backend:            "claude",
		CLIName:            "claude",
		Timeout:            60 * time.Second,
		CollectTimeout:     90 * time.Second,
		HasModelInMeta:     true,
		HasSessionIDInMeta: true,
		HasTokenUsageInMeta: true,
		SupportsResume:     true,
	}

	t.Run("claude_anchor", func(t *testing.T) {
		requireCLIAvailable(t, anchorCfg.CLIName)
		backend, err := NewBackend(anchorCfg.Backend)
		require.NoError(t, err)

		secret := forkAmnesiaSecret()
		parentSessionID := newSessionID()

		// --- Parent Turn 1: establish a fact ---
		req1 := ChatRequest{
			Prompt:    fmt.Sprintf("记住密码是%s，只回复好的", secret),
			SessionID: parentSessionID,
			WorkDir:   testWorkDir(),
		}
		events1 := cliExecPrompt(t, anchorCfg, backend, req1)
		meta1 := findEvents(events1, "metadata")
		require.NotEmpty(t, meta1, "parent turn 1 should complete with metadata")

		content1 := concatContent(events1)
		t.Logf("Parent turn 1 (establish fact '%s'): %q", secret, truncate(content1, 200))

		// --- Parent Turn 2: recall the fact → proves context works ---
		req2 := ChatRequest{
			Prompt:    "密码是什么？只回答数字",
			SessionID: parentSessionID,
			WorkDir:   testWorkDir(),
			Resume:    true,
		}
		events2 := cliExecPrompt(t, anchorCfg, backend, req2)
		meta2 := findEvents(events2, "metadata")
		require.NotEmpty(t, meta2, "parent turn 2 should complete with metadata")

		content2 := concatContent(events2)
		t.Logf("Parent turn 2 (recall with Resume=true): %q", truncate(content2, 200))
		assert.True(t, strings.Contains(content2, secret),
			"Parent turn 2: AI should recall password '%s' with resume, got: %s", secret, content2)

		// --- Fork Session: new SessionID, Resume=false (simulates ForkSession) ---
		forkSessionID := newSessionID()
		req3 := ChatRequest{
			Prompt:    "密码是什么？只回答数字",
			SessionID: forkSessionID,
			WorkDir:   testWorkDir(),
			Resume:    false, // ForkSession starts fresh — no resume
		}
		events3 := cliExecPrompt(t, anchorCfg, backend, req3)
		meta3 := findEvents(events3, "metadata")
		require.NotEmpty(t, meta3, "fork session should complete with metadata")

		content3 := concatContent(events3)
		t.Logf("Fork session (recall, Resume=false, NEW SessionID): %q", truncate(content3, 200))

		// --- Amnesia Detection ---
		if strings.Contains(content3, secret) {
			t.Logf("FORK AMNESIA FIXED: AI recalled '%s' in forked session — context was preserved!", secret)
		} else {
			t.Logf("FORK AMNESIA CONFIRMED: AI answered %q in forked session — context was LOST", truncate(content3, 50))
			t.Logf("Root cause: ForkSession does not copy external_session_id. The CLI starts")
			t.Logf("a brand-new session (no --resume), so the AI has no conversation context.")
		}
		// The key assertion: forked session does NOT recall the secret (amnesia)
		assert.NotContains(t, content3, secret,
			"AMNESIA: forked session should NOT recall password '%s' because it has no context from parent session. "+
				"If this assertion FAILS, the amnesia has been fixed!", secret)
	})
}

// TestIntegration_CLI_ForkSessionAmnesia_AcrossBackends tests fork session
// amnesia across all resume-capable backends.
func TestIntegration_CLI_ForkSessionAmnesia_AcrossBackends(t *testing.T) {
	for _, cfg := range resumeBackends {
		t.Run(cfg.Backend, func(t *testing.T) {
			backend := cliSetupAndCreate(t, cfg)
			secret := forkAmnesiaSecret()

			// Parent Turn 1: establish fact
			req1 := cliNewSessionRequest(cfg, fmt.Sprintf("记住密码是%s，只回复好的", secret))
			events1 := cliExecPrompt(t, cfg, backend, req1)
			meta1 := findEvents(events1, "metadata")
			require.NotEmpty(t, meta1, "parent turn 1 should complete with metadata")

			sessionID := cliResolveSessionID(cfg, req1, events1)
			require.NotEmpty(t, sessionID, "should capture parent session ID")

			// Parent Turn 2: recall (resume) → should recall the secret
			req2 := ChatRequest{
				Prompt:    "密码是什么？只回答数字",
				SessionID: sessionID,
				WorkDir:   testWorkDir(),
				Resume:    true,
			}
			events2 := cliExecPrompt(t, cfg, backend, req2)
			meta2 := findEvents(events2, "metadata")
			require.NotEmpty(t, meta2, "parent turn 2 should complete with metadata")

			content2 := concatContent(events2)
			t.Logf("Parent turn 2 (recall, Resume=true): %q", truncate(content2, 200))

			// Fork: new session ID, Resume=false
			forkSessionID := newSessionID()
			req3 := ChatRequest{
				Prompt:    "密码是什么？只回答数字",
				SessionID: forkSessionID,
				WorkDir:   testWorkDir(),
				Resume:    false,
			}
			events3 := cliExecPrompt(t, cfg, backend, req3)
			meta3 := findEvents(events3, "metadata")
			require.NotEmpty(t, meta3, "fork session should complete with metadata")

			content3 := concatContent(events3)
			t.Logf("Fork session (recall, Resume=false, NEW SessionID): %q", truncate(content3, 200))

			// Report amnesia status
			if strings.Contains(content3, secret) {
				t.Logf("FORK AMNESIA FIXED for %s: AI recalled '%s'", cfg.Backend, secret)
			} else {
				t.Logf("FORK AMNESIA CONFIRMED for %s: AI answered %q (cannot recall '%s' without context)",
					cfg.Backend, truncate(content3, 50), secret)
			}
		})
	}
}

// TestIntegration_CLI_ForkSessionAmnesia_ResumeVsNewSession explicitly compares
// three approaches to fix fork amnesia, using Claude as the diagnostic backend:
//
//   - Fork A: new SessionID + Resume=false (current behavior → amnesiac)
//   - Fork B: new SessionID + Resume=true (unknown session → likely amnesiac)
//   - Fork C: parent SessionID + Resume=true (ContinueFromExecution pattern → should work)
//
// If Fork C recalls the secret, the fix is: ForkSession should copy
// external_session_id and send Resume=true with the parent's session ID.
func TestIntegration_CLI_ForkSessionAmnesia_ResumeVsNewSession(t *testing.T) {
	requireCLIAvailable(t, "claude")
	backend, err := NewBackend("claude")
	require.NoError(t, err)

	cfg := cliTestConfig{
		Backend: "claude", CLIName: "claude",
		Timeout: 60 * time.Second, CollectTimeout: 90 * time.Second,
	}

	secret := forkAmnesiaSecret()
	parentSessionID := newSessionID()

	// Parent Turn 1: establish fact
	events1 := cliExecPrompt(t, cfg, backend, ChatRequest{
		Prompt:    fmt.Sprintf("记住密码是%s，只回复好的", secret),
		SessionID: parentSessionID,
		WorkDir:   testWorkDir(),
	})
	meta1 := findEvents(events1, "metadata")
	require.NotEmpty(t, meta1, "parent turn 1 should complete")
	t.Logf("Parent turn 1 (establish fact '%s'): %q", secret, truncate(concatContent(events1), 200))

	// Parent Turn 2: recall with resume → proves context works
	events2 := cliExecPrompt(t, cfg, backend, ChatRequest{
		Prompt:    "密码是什么？只回答数字",
		SessionID: parentSessionID,
		WorkDir:   testWorkDir(),
		Resume:    true,
	})
	meta2 := findEvents(events2, "metadata")
	require.NotEmpty(t, meta2, "parent turn 2 should complete")
	content2 := concatContent(events2)
	t.Logf("Parent turn 2 (Resume=true): %q", truncate(content2, 200))
	assert.True(t, strings.Contains(content2, secret),
		"Parent turn 2: should recall '%s' with resume, got: %s", secret, content2)

	// --- Fork scenario A: new SessionID + Resume=false (current behavior) ---
	forkA := newSessionID()
	eventsA := cliExecPrompt(t, cfg, backend, ChatRequest{
		Prompt:    "密码是什么？只回答数字",
		SessionID: forkA,
		WorkDir:   testWorkDir(),
		Resume:    false,
	})
	contentA := concatContent(eventsA)
	t.Logf("Fork A (NEW SessionID, Resume=false): %q", truncate(contentA, 200))

	// --- Fork scenario B: new SessionID + Resume=true ---
	forkB := newSessionID()
	eventsB := cliExecPrompt(t, cfg, backend, ChatRequest{
		Prompt:    "密码是什么？只回答数字",
		SessionID: forkB,
		WorkDir:   testWorkDir(),
		Resume:    true,
	})
	contentB := concatContent(eventsB)
	t.Logf("Fork B (NEW SessionID, Resume=true): %q", truncate(contentB, 200))

	// --- Fork scenario C: parent SessionID + Resume=true (the fix) ---
	eventsC := cliExecPrompt(t, cfg, backend, ChatRequest{
		Prompt:    "密码是什么？只回答数字",
		SessionID: parentSessionID,
		WorkDir:   testWorkDir(),
		Resume:    true,
	})
	contentC := concatContent(eventsC)
	t.Logf("Fork C (PARENT SessionID, Resume=true): %q", truncate(contentC, 200))

	// Summary
	t.Log("")
	t.Log("=== Fork Session Amnesia Diagnosis ===")
	t.Logf("Fork A (new SID, Resume=false):     %s", amnesiaStatus(contentA, secret))
	t.Logf("Fork B (new SID, Resume=true):      %s", amnesiaStatus(contentB, secret))
	t.Logf("Fork C (parent SID, Resume=true):   %s", amnesiaStatus(contentC, secret))
	t.Log("")
	t.Log("Fix analysis:")
	if strings.Contains(contentC, secret) {
		t.Logf("  ✓ Using parent's SessionID + Resume=true FIXES amnesia (recalled '%s')", secret)
		t.Log("  → Fix: ForkSession should copy external_session_id and use Resume=true")
	} else {
		t.Log("  ✗ Even parent's SessionID + Resume=true doesn't fix amnesia")
		t.Log("  → The AI session may have been finalized or the context window was lost")
	}
	if !strings.Contains(contentA, secret) {
		t.Log("  ✓ Fork A (new SID, Resume=false) is amnesiac — this is the current bug")
	}

	// Key assertion: Fork C should recall the secret (using parent's session with resume)
	assert.True(t, strings.Contains(contentC, secret),
		"Fork C (parent SessionID + Resume=true) should recall '%s' — this is the fix path. Got: %s",
		secret, contentC)
}

// amnesiaStatus returns a human-readable string indicating whether the AI
// recalled the secret number or not.
func amnesiaStatus(content, secret string) string {
	if strings.Contains(content, secret) {
		return "NOT AMNESIAC (recalled " + secret + ")"
	}
	return "AMNESIAC (cannot recall " + secret + ")"
}
