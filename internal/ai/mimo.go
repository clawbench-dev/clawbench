package ai

import "strings"

// mimoBackend is the CLIBackend instance for MiMo-Code CLI.
// MiMo-Code is a fork of OpenCode and uses the same JSON stream format,
// so we reuse OpenCode's stream parser and arg builder.
var mimoBackend = &CLIBackend{
	name:           "mimo",
	defaultCommand: "mimo",
	buildArgs:      buildMimoStreamArgs,
	newParser:      func() LineParser { return &OpenCodeStreamParser{} },
	filterLine: func(line string) (string, bool) {
		if line == "" || strings.HasPrefix(line, "[opencode-mobile]") {
			return "", false
		}
		if !strings.HasPrefix(line, "{") {
			return "", false
		}
		return line, true
	},
	preStart: nil,
}

// buildMimoStreamArgs constructs the CLI arguments for MiMo-Code streaming.
// MiMo-Code uses the same `run --format json` interface as OpenCode.
func buildMimoStreamArgs(req ChatRequest) []string {
	prompt := injectSystemPrompt(req)

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
