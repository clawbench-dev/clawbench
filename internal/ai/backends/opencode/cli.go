package opencode

import (
	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

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
