package deepseek

import (
	"log/slog"
	"strings"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

// DeepSeekInputRemaps maps CodeWhale (formerly DeepSeek TUI) CLI input field names to canonical names.
// Injected into DeepSeekStreamParser at construction time.
var DeepSeekInputRemaps = map[string]string{
	"path": "file_path", "search": "old_string", "replace": "new_string",
	"filePaths": "file_paths", "dirPath": "path",
}

func init() {
	ai.RegisterBackend("deepseek", newDeepSeekBackend, true)
	backends.Register(&backends.BackendPlugin{
		ID: "deepseek",
		Spec: model.BackendSpec{
			ID: "deepseek", Backend: "deepseek", DefaultCmd: "codewhale", AltCmd: "deepseek", Name: "CodeWhale", Icon: "🐋", Specialty: "AI 推理与编码",
			AcpCommand: "codewhale serve --acp",
			InstallCmd: "npm install -g codewhale",
			SortOrder:  7,
		},
		ACP: &backends.ACPPlugin{
			ToolCallIDPrefixes: CodeWhaleACPToolCallIDPrefixes,
			InputRemaps:        CodeWhaleACPInputRemaps,
		},
	})
}

// newDeepSeekBackend returns a CLIBackend instance configured for CodeWhale CLI.
// CodeWhale was formerly known as DeepSeek TUI; the backend ID remains "deepseek"
// for backward compatibility with existing session data.
func newDeepSeekBackend() ai.AIBackend {
	return &ai.CLIBackend{
		BackendName: "deepseek",
		Cmd:         "codewhale", // primary; legacy "deepseek" shim handled via req.Command
		BuildArgsFn: buildDeepSeekStreamArgs,
		NewParserFn: func() ai.LineParser {
			return &ai.DeepSeekStreamParser{InputRemaps: DeepSeekInputRemaps}
		},
		FilterLineFn: nil, // skip empty lines only (default)
		PreStartFn:   nil, // prompt is passed as positional argument
	}
}

// buildDeepSeekStreamArgs constructs the CLI arguments for CodeWhale streaming.
//
// Command: codewhale exec --auto --output-format stream-json [flags] "prompt"
//
// Supported flags:
//
//	--resume <session_id>      Resume a previous session
//	--continue                 Continue the most recent session
//	--system-prompt <text>     Inject custom system prompt
//	--system-prompt-file <path> Read system prompt from file
//	--model <model>            Override model (e.g. deepseek-v4-flash, deepseek-v4-pro)
func buildDeepSeekStreamArgs(req ai.ChatRequest) []string {
	args := []string{
		"exec",
		"--auto",
		"--output-format", "stream-json",
	}

	// Resume previous session
	if req.Resume && req.SessionID != "" {
		args = append(args, "--resume", req.SessionID)
		slog.Info("cli: --resume (codewhale)",
			slog.String("session_id", req.SessionID))
	} else if req.Resume {
		// Session capture event was missed — fall back to --continue
		// which resumes the most recent session without needing an ID.
		args = append(args, "--continue")
		slog.Warn("cli: --continue fallback (codewhale, session_id missing)",
			slog.String("backend", "deepseek"))
	}

	// System prompt — CodeWhale supports --system-prompt natively
	if req.SystemPrompt != "" {
		args = append(args, "--system-prompt", req.SystemPrompt)
	}

	// Model override — CodeWhale expects a plain model ID (e.g. "deepseek-v4-pro"),
	// but ClawBench stores model IDs as "provider/model" (e.g. "deepseek/deepseek-v4-pro").
	// Strip the provider prefix before passing to the CLI.
	if req.Model != "" {
		if idx := strings.LastIndex(req.Model, "/"); idx >= 0 {
			args = append(args, "--model", req.Model[idx+1:])
		} else {
			args = append(args, "--model", req.Model)
		}
	}

	// Prompt is the last positional argument
	args = append(args, req.Prompt)

	return args
}
