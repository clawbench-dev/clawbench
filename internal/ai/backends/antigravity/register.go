package antigravity

import (
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	// Antigravity (agy) is ACP-only via the agy-acp bridge adapter: no
	// ai.RegisterBackend (CLI factory) is registered, so chat always uses the
	// ACP stdio transport (npx -y agy-acp). The agy CLI has no stream-json
	// output mode — its non-interactive --print mode returns plain text only,
	// so there is no CLI backend to register.
	backends.Register(&backends.BackendPlugin{
		ID: "antigravity",
		Spec: model.BackendSpec{
			ID: "antigravity", Backend: "antigravity", DefaultCmd: "agy", Name: "Antigravity", Specialty: "Google 编码代理",
			ThinkingEffortLevels: []string{"low", "medium", "high"},
			AcpCommand:           "npx -y agy-acp@latest",
			ACPLoadSession:       true,
			InstallCmd:           "curl -fsSL https://antigravity.google/cli/install.sh | bash",
			SortOrder:            14,
		},
	})
}
