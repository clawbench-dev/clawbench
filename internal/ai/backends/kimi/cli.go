package kimi

import (
	"strings"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

// KimiInputRemaps maps Kimi CLI input field names to canonical names.
// Injected into StreamJSONParser at construction time.
var KimiInputRemaps = map[string]string{
	"dirPath":         "path",              // camelCase fallback
	"dir_path":        "path",              // Kimi CLI outputs snake_case dir_path → canonical path (for Grep/Glob/LS)
	"allow_multiple":  "replace_all",       // Edit allow_multiple → replace_all
	"is_background":   "run_in_background", // Bash is_background → run_in_background
	"include_pattern": "glob",              // Grep include_pattern → canonical glob
	"name":            "skill",             // activate_skill name → canonical skill
}

// KimiToolNameMap maps Kimi CLI tool names to canonical names.
// Injected into StreamJSONParser at construction time.
var KimiToolNameMap = map[string]string{
	"read_file":         "Read",
	"write_file":        "Write",
	"edit_file":         "Edit",
	"replace":           "Edit",
	"run_shell_command": "Bash",
	"list_directory":    "LS",
	"search_file":       "Grep",
	"search_directory":  "Grep",
	"glob":              "Glob",
	"ask":               "AskUserQuestion",
	"file_search":       "Glob",
	"list_files":        "LS",
	"shell":             "Bash",
}

func init() {
	ai.RegisterBackend("kimi", newKimiBackend, true)
	backends.Register(&backends.BackendPlugin{
		ID: "kimi",
		Spec: model.BackendSpec{
			ID: "kimi", Backend: "kimi", DefaultCmd: "kimi", Name: "Kimi", Icon: "🌙", Specialty: "Kimi AI 编码助手",
			ThinkingEffortLevels: []string{"off", "on"},
			AcpCommand:           "kimi acp",
			SortOrder:            10,
		},
		ACP: &backends.ACPPlugin{
			ToolCallIDPrefixes: KimiACPTCIDPrefixes,
			InputRemaps:        map[string]string{},
		},
	})
}

// newKimiBackend returns a CLIBackend instance for Kimi CLI.
// Kimi uses stream-json output format (--print --output-format stream-json).
func newKimiBackend() ai.AIBackend {
	return &ai.CLIBackend{
		BackendName: "kimi",
		Cmd:         "kimi",
		BuildArgsFn: buildKimiStreamArgs,
		NewParserFn: func() ai.LineParser {
			return &ai.StreamJSONParser{
				ToolNameMap: KimiToolNameMap,
				InputRemaps: KimiInputRemaps,
			}
		},
		FilterLineFn: func(line string) (string, bool) {
			if line == "" || !strings.HasPrefix(line, "{") {
				return "", false
			}
			return line, true
		},
		PreStartFn: nil,
	}
}

// buildKimiStreamArgs constructs the CLI arguments for Kimi streaming.
// Kimi uses --print for non-interactive mode and --output-format stream-json
// for streaming output (Kimi CLI is forked from Gemini CLI and uses the same stream-json format).
func buildKimiStreamArgs(req ai.ChatRequest) []string {
	// Kimi CLI has no --system-prompt flag, so inject into the user prompt.
	prompt := ai.InjectSystemPrompt(req)

	args := []string{
		"--print",
		"--prompt", prompt,
		"--output-format", "stream-json",
		"--yes",
	}

	// Resume previous session
	if req.SessionID != "" && req.Resume {
		args = append(args, "--session", req.SessionID)
	}

	// Working directory
	if req.WorkDir != "" {
		args = append(args, "--work-dir", req.WorkDir)
	}

	// Model override
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}

	// Thinking mode
	if req.ThinkingEffort != "" {
		if req.ThinkingEffort == "off" {
			args = append(args, "--no-thinking")
		} else {
			args = append(args, "--thinking")
		}
	}

	return args
}
