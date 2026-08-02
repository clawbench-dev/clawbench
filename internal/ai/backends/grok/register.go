package grok

import (
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	// Grok Build is ACP-only: no ai.RegisterBackend (CLI factory) is registered,
	// so chat always uses the ACP stdio transport (grok agent stdio).
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
			InputRemaps: GrokACPRemaps,
		},
	})
}
