package opencode

import (
	"strings"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

// OpenCodeInputRemaps maps OpenCode CLI input field names to canonical names.
// Injected into OpenCodeStreamParser at construction time.
var OpenCodeInputRemaps = map[string]string{
	"oldString":  "old_string",
	"newString":  "new_string",
	"replaceAll": "replace_all", // Edit replaceAll → replace_all
	"include":    "glob",        // Grep include → canonical glob
	"name":       "skill",       // Skill name → skill
}

// OpenCodeToolNameMap maps OpenCode CLI tool names to canonical names.
// Injected into OpenCodeStreamParser at construction time.
var OpenCodeToolNameMap = map[string]string{
	"read_file":  "Read",
	"write_file": "Write",
	"edit_file":  "Edit",
	"replace":    "Edit",
	"bash":       "Bash",
	"list_files": "LS",
	"grep":       "Grep",
	"glob":       "Glob",
	"web_fetch":  "WebFetch",
	"agent":      "Agent",
	"skill":      "Skill",
}

func init() {
	ai.RegisterBackend("opencode", newOpenCodeBackend, false)
	backends.Register(&backends.BackendPlugin{
		ID: "opencode",
		Spec: model.BackendSpec{
			ID: "opencode", Backend: "opencode", DefaultCmd: "opencode", Name: "OpenCode", Icon: "📟", Specialty: "终端编码工具",
			ThinkingEffortLevels: []string{"minimal", "high", "max"},
			AcpCommand:           "opencode acp",
			EmbeddedSubDir:       "opencode",
			EmbeddedVersionFile:  "VERSION",
			EmbeddedGitHubRepo:   "anomalyco/opencode",
			EmbeddedArchMapping:  "amd64=x64",
			SortOrder:            3,
		},
		ACP: &backends.ACPPlugin{
			InputRemaps: OpenCodeACPInputRemaps,
		},
	})
}

// newOpenCodeBackend returns a CLIBackend instance configured for OpenCode CLI.
func newOpenCodeBackend() ai.AIBackend {
	return &ai.CLIBackend{
		BackendName: "opencode",
		Cmd:         "opencode",
		BuildArgsFn: buildOpenCodeStreamArgs,
		NewParserFn: func() ai.LineParser {
			return &ai.OpenCodeStreamParser{
				ToolNameMap: OpenCodeToolNameMap,
				InputRemaps: OpenCodeInputRemaps,
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
		PreStartFn: nil,
	}
}

// buildOpenCodeStreamArgs constructs the CLI arguments for OpenCode streaming.
func buildOpenCodeStreamArgs(req ai.ChatRequest) []string {
	// OpenCode CLI has no --system-prompt flag — inject into user prompt.
	prompt := ai.InjectSystemPrompt(req)

	args := []string{
		"run",
		prompt,
		"--format", "json",
		"--dangerously-skip-permissions",
	}

	// Pass OpenCode session ID for continuing conversations.
	// Only pass --session when resuming an existing OpenCode session
	// (indicated by Resume=true and a ses_ prefixed session ID).
	// On first message, SessionID contains ClawBench's UUID which OpenCode
	// doesn't recognize — let OpenCode create its own session.
	if req.SessionID != "" && req.Resume {
		args = append(args, "--session", req.SessionID)
	}

	// Working directory
	if req.WorkDir != "" {
		args = append(args, "--dir", req.WorkDir)
	}

	// Model override (format: provider/model, e.g., "minimax-cn-coding-plan/MiniMax-M2.7")
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}

	// Thinking effort level (e.g., --variant high)
	if req.ThinkingEffort != "" {
		args = append(args, "--variant", req.ThinkingEffort)
	}

	return args
}
