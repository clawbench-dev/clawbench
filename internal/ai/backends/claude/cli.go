package claude

import (
	"os/exec"
	"strings"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	backends.Register(&backends.BackendPlugin{
		ID: "claude",
		Spec: model.BackendSpec{
			ID: "claude", Backend: "claude", DefaultCmd: "claude", Name: "Claude", Specialty: "代码编写与推理",
			ThinkingEffortLevels: []string{"low", "medium", "high", "xhigh", "max"},
			AcpCommand:           "npx -y @agentclientprotocol/claude-agent-acp@latest",
			ACPLoadSession:       true,
			InstallCmd:           "npm install -g @anthropic-ai/claude-code",
			SortOrder:            1,
			// Claude Code loads .claude/skills into context itself (verified in
			// the shipped claude.exe: it references ".claude/skills" and
			// "SKILL.md"), so ClawBench must not inject them again — they would
			// appear twice. They still shadow same-named skills from other
			// sources. Claude does NOT read the shared .agents/skills.
			NativeSkillsDirs:      []string{".claude/skills"},
			AutoLoadsNativeSkills: true,
		},
		// Claude can inject a message into the running turn via the ACP
		// steering extension (_session/steering). It emits no echo boundary, so
		// the injection MERGES into the running reply (no split).
		CLI: &backends.CLIPlugin{NewBackend: newClaudeBackend},
		// This is the definition of the Claude-family ACP hook set. qoder /
		// qodercn are Claude Code forks and re-export these same function
		// values rather than copying the parsers; codex shares ExtractMeta too.
		// Every hook Claude's bridge protocol needs must be listed here — a hook
		// missing from this struct silently degrades to the generic fallback for
		// claude AND every fork that re-exports it (which is exactly how
		// ExtractMeta was lost once), so keep the set complete and let
		// TestClaudeFamilyBehaviorsAreShared catch a fork that stops sharing.
		Behavior: &ai.ACPBehavior{
			ParseToolCall:       ParseACPToolCall,
			ParseToolCallUpdate: ParseACPToolCallUpdate,
			ExtractMeta:         ExtractMeta,
		},
		ModelSource: model.PluginSource("claude", discoverClaudeModels),
		MidTurn:     &midTurnInjector{},
		ProviderEnv: &model.ProviderEnvSpec{
			Anthropic: model.ProtoEnv("ANTHROPIC_BASE_URL", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"),
			// CLAUDE_CONFIG_DIR relocates the whole ~/.claude tree, and the ACP
			// adapter (claude-agent-acp) merges settings.json's `env` block into
			// the session. Verified end-to-end: with a private directory holding
			// ONLY settings.json (no login state at all), a turn against a
			// third-party Anthropic-compatible endpoint returned its reply.
			//
			// This is the ONE backend where the config file is not merely an
			// alternative to the environment variables: ANTHROPIC_BASE_URL alone
			// created the session but the turn never produced a reply (the
			// adapter reads its endpoint from the merged settings, not from the
			// raw environment). So without this the Anthropic-protocol binding
			// was effectively unusable for claude.
			ConfigDirEnv:          "CLAUDE_CONFIG_DIR",
			ConfigDirFile:         "settings.json",
			ConfigDirProtocols:    []string{model.ProviderProtocolAnthropic},
			ConfigDirProviderID:   "clawbench",
			ConfigDirSecretInFile: true,
		},
	})
}

// newClaudeBackend returns a CLIBackend instance configured for Claude CLI.
func newClaudeBackend() ai.AIBackend {
	return &ai.CLIBackend{
		BackendName:  "claude",
		Cmd:          "claude",
		BuildArgsFn:  buildClaudeStreamArgs,
		NewParserFn:  func() ai.LineParser { return NewStreamParser() },
		FilterLineFn: nil, // skip empty lines only (default)
		PreStartFn: func(cmd *exec.Cmd, req ai.ChatRequest) {
			// Claude CLI in --print mode with stdout piped (non-TTY) requires prompt
			// via stdin — positional prompt argument is not recognized.
			// Both new sessions and resume sessions use stdin for prompt.
			cmd.Stdin = strings.NewReader(req.Prompt)
		},
	}
}

// buildClaudeStreamArgs constructs the CLI arguments for Claude streaming.
func buildClaudeStreamArgs(req ai.ChatRequest) []string {
	return ai.BuildBaseStreamArgs(req, func(r ai.ChatRequest) []string {
		return []string{"--verbose"}
	})
}
