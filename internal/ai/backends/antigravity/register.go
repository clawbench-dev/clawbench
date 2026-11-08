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
			// A session is gated on a signed-in Google account: without one,
			// session/new answers -32000 AUTH_REQUIRED after ~20s (measured
			// 1.5.2). `agy acp --login` is the CLI's own terminal sign-in entry
			// ("Log in with Google interactively, then exit"). Declaring it makes
			// the AUTH_REQUIRED error render an actionable instruction instead of
			// the agent's bare "Authentication required".
			LoginCmd:  "agy acp --login",
			SortOrder: 14,
		},
	})
}
