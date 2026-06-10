package ai

import (
	"log/slog"
	"strings"
)

// deepseekBackend is the CLIBackend instance for DeepSeek TUI CLI.
var deepseekBackend = &CLIBackend{
	name:           "deepseek",
	defaultCommand: "deepseek",
	buildArgs:      buildDeepSeekStreamArgs,
	newParser:      func() LineParser { return &DeepSeekStreamParser{} },
	filterLine:     nil, // skip empty lines only (default)
	preStart:       nil, // prompt is passed as positional argument
}

// buildDeepSeekStreamArgs constructs the CLI arguments for DeepSeek TUI streaming.
//
// Command: deepseek exec --auto --output-format stream-json [flags] "prompt"
//
// Supported flags:
//
//	--resume <session_id>      Resume a previous session
//	--continue                 Continue the most recent session
//	--system-prompt <text>     Inject custom system prompt
//	--system-prompt-file <path> Read system prompt from file
//	--model <model>            Override model (e.g. deepseek-v4-flash, deepseek-v4-pro)
func buildDeepSeekStreamArgs(req ChatRequest) []string {
	args := []string{
		"exec",
		"--auto",
		"--output-format", "stream-json",
	}

	// Resume previous session
	if req.Resume && req.SessionID != "" {
		args = append(args, "--resume", req.SessionID)
		slog.Info("cli: --resume (deepseek)",
			slog.String("session_id", req.SessionID))
	} else if req.Resume {
		// Session capture event was missed — fall back to --continue
		// which resumes the most recent session without needing an ID.
		args = append(args, "--continue")
		slog.Warn("cli: --continue fallback (deepseek, session_id missing)",
			slog.String("backend", "deepseek"))
	}

	// System prompt — DeepSeek TUI supports --system-prompt natively
	if req.SystemPrompt != "" {
		args = append(args, "--system-prompt", req.SystemPrompt)
	}

	// Model override — DeepSeek CLI expects a plain model ID (e.g. "deepseek-v4-pro"),
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
