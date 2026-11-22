package codebuddy

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"

	"clawbench/internal/ai"
)

// spawnHooks returns CodeBuddy's spawn-time argv customization.
//
// Both entries are workarounds that only make sense for this CLI, which is why
// they live here rather than in the shared ACP lifecycle.
func spawnHooks() *ai.ACPSpawnHooks {
	return &ai.ACPSpawnHooks{Args: spawnArgs}
}

func spawnArgs(sc ai.ACPSpawnContext) []string {
	var args []string

	// Workaround for https://github.com/clawbench-dev/clawbench/issues/270:
	// When Codebuddy starts with --acp, it sets strictDynamic=true which causes
	// McpConfigManager.shouldLoadFromFilesystem() to return false for ALL
	// filesystem scopes (user/project/local), resulting in 0 MCP servers loaded.
	// User's MCP services (websearch, tavily, chrome-devtools, etc.) configured
	// in ~/.codebuddy/.mcp.json become completely unavailable.
	//
	// We read ~/.codebuddy/.mcp.json and inject it via --mcp-config so the ACP
	// process can still load MCP tools.
	//
	// COMPATIBILITY PERIOD: this flag is only used while ClawBench has no MCP
	// servers of its own. Once mcp.servers is non-empty the protocol field
	// carries the configuration (probed working on codebuddy), and passing both
	// would register the same server twice. Keeping the old path for the empty
	// case is what stops an upgrade from silently deleting the tools of every
	// user who configured ~/.codebuddy/.mcp.json by hand.
	if sc.MCPServerCount == 0 {
		if mcpConfigJSON := readUserMCPConfig(); mcpConfigJSON != "" {
			args = append(args, "--mcp-config", mcpConfigJSON)
			slog.Info("acp conn: injecting user MCP config via --mcp-config (workaround for issue #270; legacy path, mcp.servers is empty)",
				slog.String("clawbench_sid", sc.SessionID))
		}
	}

	// An own-service provider may point the CLI at a customer-defined address
	// (CodeBuddy's dedicated / self-hosted editions). The environment has no
	// variable for it, and writing settings.json would fight the user's own
	// file, so it rides along as the documented per-process override.
	if sc.Agent != nil {
		if payload := ai.OwnServiceSettingsArg(sc.Agent.ID); payload != "" {
			args = append(args, "--settings", payload)
			slog.Info("acp conn: pointing codebuddy at an own-service endpoint",
				slog.String("agent_id", sc.Agent.ID),
				slog.String("clawbench_sid", sc.SessionID))
		}
	}

	return args
}

// readUserMCPConfig reads ~/.codebuddy/.mcp.json and returns the mcpServers
// JSON string if non-empty, suitable for --mcp-config CLI injection.
// Returns "" on any error or if no servers configured.
func readUserMCPConfig() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(home, ".codebuddy", ".mcp.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var cfg struct {
		McpServers map[string]any `json:"mcpServers"`
	}
	if unmarshalErr := json.Unmarshal(data, &cfg); unmarshalErr != nil {
		return ""
	}
	if len(cfg.McpServers) == 0 {
		return ""
	}
	// Re-marshal just the mcpServers object as --mcp-config expects
	serversJSON, err := json.Marshal(cfg.McpServers)
	if err != nil {
		return ""
	}
	return string(serversJSON)
}
