package grok

import (
	"strings"

	"clawbench/internal/ai"
)

// init registers the Grok streaming-json CLI backend as a fallback transport.
// ACP (grok agent stdio) is preferred; when unavailable, chat falls back here.
func init() {
	ai.RegisterBackend("grok", newGrokBackend)
}

// newGrokBackend returns a CLIBackend for Grok headless streaming-json mode.
func newGrokBackend() ai.AIBackend {
	return &ai.CLIBackend{
		BackendName: "grok",
		Cmd:         "grok",
		BuildArgsFn: buildGrokStreamArgs,
		NewParserFn: func() ai.LineParser {
			return &ai.GrokStreamParser{}
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

// buildGrokStreamArgs constructs CLI args for Grok headless streaming.
//
//	grok -p <prompt> --output-format streaming-json --yolo [flags]
//
// Flags:
//
//	--resume <id>            Resume a previous session
//	--cwd <dir>              Working directory
//	--model <id>             Model override
//	--reasoning-effort <lvl> Thinking effort
//	--rules <text>           Extra rules appended to system prompt
func buildGrokStreamArgs(req ai.ChatRequest) []string {
	args := []string{
		"-p", req.Prompt,
		"--output-format", "streaming-json",
		"--yolo", // auto-approve tools for unattended ClawBench execution
	}

	// Resume previous session by Grok session ID (captured from end.sessionId).
	if req.SessionID != "" && req.Resume {
		args = append(args, "--resume", req.SessionID)
	}

	if req.WorkDir != "" {
		args = append(args, "--cwd", req.WorkDir)
	}

	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}

	if req.ThinkingEffort != "" {
		args = append(args, "--reasoning-effort", req.ThinkingEffort)
	}

	// Append ClawBench rules without overriding Grok's built-in system prompt.
	if req.SystemPrompt != "" {
		args = append(args, "--rules", req.SystemPrompt)
	}

	return args
}
