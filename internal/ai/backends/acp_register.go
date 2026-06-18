package backends

import (
	"clawbench/internal/ai/backends/claude"
	"clawbench/internal/ai/backends/codebuddy"
	"clawbench/internal/ai/backends/deepseek"
	"clawbench/internal/ai/backends/kimi"
	"clawbench/internal/ai/backends/opencode"
	"clawbench/internal/ai/backends/pi"
)

func init() {
	registerACP("claude", &ACPPlugin{
		InputRemaps: claude.ClaudeACPRemaps,
	})
	registerACP("codebuddy", &ACPPlugin{
		InputRemaps: codebuddy.CodebuddyACPRemaps,
	})
	registerACP("deepseek", &ACPPlugin{
		ToolCallIDPrefixes: deepseek.CodeWhaleACPToolCallIDPrefixes,
		InputRemaps:        deepseek.CodeWhaleACPInputRemaps,
	})
	registerACP("kimi", &ACPPlugin{
		ToolCallIDPrefixes: kimi.KimiACPTCIDPrefixes,
		InputRemaps:        map[string]string{},
	})
	registerACP("opencode", &ACPPlugin{
		InputRemaps: opencode.OpenCodeACPInputRemaps,
	})
	registerACP("pi", &ACPPlugin{
		InputRemaps: pi.PiACPInputRemaps,
	})
}

// registerACP adds ACP mapping data to an existing backend plugin.
// If the plugin already exists, it sets the ACP field.
// If not, it creates a minimal plugin with just the ACP data.
func registerACP(id string, acp *ACPPlugin) {
	pluginsMu.Lock()
	defer pluginsMu.Unlock()
	if p, ok := plugins[id]; ok {
		p.ACP = acp
	} else {
		plugins[id] = &BackendPlugin{ID: id, ACP: acp}
	}
}
