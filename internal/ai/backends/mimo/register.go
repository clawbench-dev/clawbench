package mimo

import (
	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/ai/backends/opencode"
	"clawbench/internal/model"
)

func init() {
	backends.Register(&backends.BackendPlugin{
		ID: "mimo",
		Spec: model.BackendSpec{
			ID: "mimo", Backend: "mimo", DefaultCmd: "mimo", Name: "MiMo-Code", Specialty: "小米 MiMo 编码助手",
			ThinkingEffortLevels: []string{"minimal", "high", "max"},
			// MiMo-Code is an OpenCode fork and inherits its skills subsystem
			// (verified in the shipped binary: ".mimocode/skills",
			// ".opencode/skill", the shared ".agents/skills"), so ClawBench must
			// not inject them again.
			NativeSkillsDirs:      []string{".mimocode/skills", ".agents/skills"},
			AutoLoadsNativeSkills: true,
			AcpCommand:            "mimo acp",
			ACPLoadSession:        true,
			InstallCmd:            "npm install -g @mimo-ai/cli",
			SortOrder:             12,
		},
		// No InputRemaps: MiMo inherits OpenCode's field names, which are the
		// shared generic set (ai.GenericACPRemaps) — declaring a copy here would
		// only create a second place to keep in sync.
		// MiMo-Code is an OpenCode fork: same ACP wire shapes, shared parsers.
		Behavior: &ai.ACPBehavior{
			ParseToolCall:       opencode.ParseACPToolCall,
			ParseToolCallUpdate: opencode.ParseACPToolCallUpdate,
			OnToolCallUpdate: func(conn *ai.ACPConn, ch chan<- ai.StreamEvent, tcu acp.SessionToolCallUpdate) {
				opencode.BridgePlanFromToolUpdate(conn, ch, tcu)
			},
		},
		ModelSource: model.StaticSource("mimo", "mimo", MimoCatalog),
		ProviderEnv: &model.ProviderEnvSpec{
			Anthropic: model.ProtoEnv("ANTHROPIC_BASE_URL", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"),
			OpenAI:    model.ProtoEnv("OPENAI_BASE_URL", "OPENAI_API_KEY", ""),
		},
	})
}
