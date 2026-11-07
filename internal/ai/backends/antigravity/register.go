package antigravity

import (
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	// Antigravity (agy) is ACP-only via native `agy acp` stdio transport:
	// no ai.RegisterBackend (CLI factory) is registered, so chat always uses
	// the ACP stdio transport (agy acp).
	backends.Register(&backends.BackendPlugin{
		ID: "antigravity",
		Spec: model.BackendSpec{
			ID: "antigravity", Backend: "antigravity", DefaultCmd: "agy", Name: "Antigravity", Specialty: "Google 编码代理",
			ThinkingEffortLevels: []string{"low", "medium", "high"},
			AcpCommand:           "agy acp",
			ACPLoadSession:       true,
			InstallCmd:           "curl -fsSL https://antigravity.google/cli/install.sh | bash",
			SortOrder:            14,
		},
	})
}
