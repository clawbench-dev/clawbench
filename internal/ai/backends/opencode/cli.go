package opencode

import (
	acp "github.com/coder/acp-go-sdk"
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
	// OpenCode 2.x calls its delegation tool `subagent` (v1 used `task`).
	"subagent": "Agent",
	"skill":    "Skill",
}

func init() {
	backends.Register(&backends.BackendPlugin{
		ID: "opencode",
		Spec: model.BackendSpec{
			ID: "opencode", Backend: "opencode", DefaultCmd: "opencode", Name: "OpenCode", Specialty: "终端编码工具",
			ThinkingEffortLevels: []string{"minimal", "high", "max"},
			// OpenCode loads its own skills (verified in the shipped binary:
			// ".config/opencode/skill", ".opencode/skills", the shared
			// ".agents/skills", and an "<available_skills>" prompt section), so
			// ClawBench must not inject them again.
			NativeSkillsDirs:      []string{".config/opencode/skill", ".opencode/skills", ".agents/skills"},
			AutoLoadsNativeSkills: true,
			AcpCommand:            "opencode acp",
			ACPLoadSession:        true,
			InstallCmd:            "npm install -g opencode-ai",
			SortOrder:             3,
		},
		ACP: &backends.ACPPlugin{
			InputRemaps: OpenCodeACPInputRemaps,
		},
		CLI: &backends.CLIPlugin{NewBackend: newOpenCodeBackend},
		Behavior: &ai.ACPBehavior{
			ParseToolCall:       ParseACPToolCall,
			ParseToolCallUpdate: ParseACPToolCallUpdate,
			// OpenCode exposes its checklist through the `todowrite` tool rather
			// than ACP plan notifications.
			OnToolCallUpdate: func(conn *ai.ACPConn, ch chan<- ai.StreamEvent, tcu acp.SessionToolCallUpdate) {
				BridgePlanFromToolUpdate(conn, ch, tcu)
			},
			IsChildSessionUpdate: IsChildSessionUpdate,
		},
		ModelSource: model.NewCLISource("opencode", model.CLIOptions{
			Command: "opencode",
			Args:    []string{"models"},
			Parse:   model.ParseProviderModel,
		}),
		ProviderEnv: &model.ProviderEnvSpec{
			Anthropic: model.ProtoEnv("ANTHROPIC_BASE_URL", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"),
			OpenAI:    model.ProtoEnv("OPENAI_BASE_URL", "OPENAI_API_KEY", ""),
			// opencode resolves a model id against its own provider catalog
			// before it sends anything, and a provider redirect does not extend
			// that catalog: OPENAI_BASE_URL/OPENAI_API_KEY alone leave "glm-…"
			// unresolvable ("model not found"), which is issue #651. Declaring
			// the provider's models through the config content is the only lever
			// that makes them resolvable. Content (not a file path) so nothing is
			// written to the user's home.
			//
			// The ids are ours, not opencode's built-in ones: it addresses a
			// declared model as "<provider>/<key>", so reusing "openai" would
			// turn a curated id that already says "openai/gpt-4o" into
			// "openai/openai/…" (verified against 2.2.6). With a dedicated id
			// the rule is simply "<our id>/<curated id verbatim>".
			InlineConfigEnv: "OPENCODE_CONFIG_CONTENT",
			InlineConfigProviderIDs: map[string]string{
				model.ProviderProtocolOpenAI:    "clawbench-openai",
				model.ProviderProtocolAnthropic: "clawbench-anthropic",
			},
		},
	})
}

// newOpenCodeBackend returns a CLIBackend instance configured for OpenCode CLI.
func newOpenCodeBackend() ai.AIBackend {
	return &ai.CLIBackend{
		BackendName: "opencode",
		Cmd:         "opencode",
		BuildArgsFn: BuildOpenCodeStreamArgs,
		NewParserFn: func() ai.LineParser {
			return &OpenCodeStreamParser{
				ToolNameMap: OpenCodeToolNameMap,
				InputRemaps: OpenCodeInputRemaps,
			}
		},
		FilterLineFn: OpenCodeFilterLine,
		PreStartFn:   nil,
	}
}

// OpenCodeFilterLine filters raw CLI output lines for OpenCode and its forks.
// Exported for reuse by MiMo-Code and any other OpenCode-fork backends.
func OpenCodeFilterLine(line string) (string, bool) {
	if line == "" || strings.HasPrefix(line, "[opencode-mobile]") {
		return "", false
	}
	if !strings.HasPrefix(line, "{") {
		return "", false
	}
	return line, true
}

// BuildOpenCodeStreamArgs constructs the CLI arguments for OpenCode streaming.
// Exported for reuse by MiMo-Code and any other OpenCode-fork backends.
func BuildOpenCodeStreamArgs(req ai.ChatRequest) []string {
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
