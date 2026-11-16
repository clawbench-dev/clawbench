package grok

import (
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	// Grok prefers ACP over stdio (grok agent stdio); a streaming-json CLI
	// fallback is wired below through CLI.NewBackend.
	backends.Register(&backends.BackendPlugin{
		ID: "grok",
		Spec: model.BackendSpec{
			ID: "grok", Backend: "grok", DefaultCmd: "grok", Name: "Grok", Specialty: "xAI 编码代理",
			ThinkingEffortLevels: []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"},
			AcpCommand:           "grok agent stdio",
			ACPLoadSession:       true,
			InstallCmd:           "curl -fsSL https://x.ai/cli/install.sh | bash",
			SortOrder:            13,
		},
		ACP: &backends.ACPPlugin{
			ToolCallIDPrefixes: GrokACPTCIDPrefixes,
			InputRemaps:        GrokACPRemaps,
		},
		CLI: &backends.CLIPlugin{NewBackend: newGrokBackend},
		ModelSource: model.NewCLISource("grok", model.CLIOptions{
			Command:  "grok",
			Args:     []string{"models"},
			Parse:    parseGrokModels,
			Fallback: GrokCatalog,
		}),
		ProviderEnv: &model.ProviderEnvSpec{
			OpenAI: model.ProtoEnv("GROK_XAI_API_BASE_URL", "XAI_API_KEY", ""),
		},
	})
}
