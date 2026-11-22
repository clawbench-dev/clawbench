package deepseek

import (
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	backends.Register(&backends.BackendPlugin{
		ID: "deepseek",
		Spec: model.BackendSpec{
			ID: "deepseek", Backend: "deepseek", DefaultCmd: "codewhale", AltCmd: "deepseek", Name: "CodeWhale", Specialty: "AI 推理与编码",
			AcpCommand: "codewhale serve --acp",
			InstallCmd: "npm install -g codewhale",
			SortOrder:  7,
			// CodeWhale loads its own skills (verified in the shipped binary:
			// ".codewhale/skills", the legacy ".deepseek/skills", and the
			// shared ".agents/skills"), so ClawBench must not inject them again.
			NativeSkillsDirs:      []string{".codewhale/skills", ".agents/skills"},
			AutoLoadsNativeSkills: true,
			// Measured 2026-10-19: session/new accepts `mcpServers`, returns no
			// error, and the tool never appears.
			MCPUnsupported: true,
		},
		ACP: &backends.ACPPlugin{
			ToolCallIDPrefixes: CodeWhaleACPToolCallIDPrefixes,
			InputRemaps:        CodeWhaleACPInputRemaps,
		},
		ProviderEnv: &model.ProviderEnvSpec{
			OpenAI: model.ProtoEnv("OPENAI_BASE_URL", "OPENAI_API_KEY", ""),
		},
	})
}
