package zcode

import (
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	// ZCode (Zhipu GLM) is ACP-only via the zcode-acp-server bridge adapter:
	// no ai.RegisterBackend (CLI factory) is registered, so chat always uses
	// the ACP stdio transport (npx -y zcode-acp-server). The bridge spawns the
	// real ZCode headless engine (`zcode app-server --stdio`), resolving the
	// zcode CLI from ZCODE_BIN → PATH → the desktop-app bundle
	// (/Applications/ZCode.app/Contents/Resources/glm/zcode.cjs on macOS), so
	// no CLI on PATH is required. Auth is the GLM API key / Coding Plan
	// credential in ~/.zcode/v2/config.json — no editor-side credentials.
	backends.Register(&backends.BackendPlugin{
		ID: "zcode",
		Spec: model.BackendSpec{
			ID: "zcode", Backend: "zcode", DefaultCmd: "zcode-acp-server", Name: "ZCode", Specialty: "智谱 GLM 编码代理",
			ThinkingEffortLevels: []string{"low", "high", "max"},
			AcpCommand:           "npx -y zcode-acp-server",
			ACPLoadSession:       true,
			InstallCmd:           "npm install -g zcode-acp-server",
			SortOrder:            15,
		},
	})
}
