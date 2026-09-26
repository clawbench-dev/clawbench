package dsh

import (
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	// DeepSeek Harness is ACP-only: `dsh` ships a built-in `acp` profile that
	// starts a standard Agent Client Protocol server over stdio. There is no
	// CLI transport, so no ai.RegisterBackend (CLI factory) is registered and
	// chat always goes through the ACP stdio transport.
	//
	// No ACPPlugin is needed either: dsh reports lowercase tool titles
	// ("read"/"write"/"bash"/"grep") which the shared lowercase alias table
	// already maps to the canonical names, and its rawInput fields are already
	// canonical (file_path / command / pattern).
	//
	// Credentials are NOT negotiated over ACP (dsh advertises no authMethods),
	// so DEEPSEEK_API_KEY must already be present in the launching environment
	// or stored through dsh's own credential store.
	backends.Register(&backends.BackendPlugin{
		ID: "dsh",
		Spec: model.BackendSpec{
			ID: "dsh", Backend: "dsh", DefaultCmd: "dsh", Name: "DeepSeek Harness",
			Specialty:  "DeepSeek 官方编码智能体",
			AcpCommand: "dsh --profile acp",
			// dsh answers session/load with -32601 (the automation-only surface
			// deliberately omits it), so the explicit acp-load endpoint must
			// return 501 rather than attempt it. Automatic recovery after a
			// process restart uses session/resume and is unaffected.
			ACPLoadSession:       false,
			ThinkingEffortLevels: []string{"off", "low", "high", "max"},
			InstallCmd:           "npm install -g @deepseek-ai/dsh",
			SortOrder:            16,
		},
	})
}
