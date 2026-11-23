package kimi

import (
	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

// KimiACPTCIDPrefixes maps Kimi-style toolCallID prefixes to canonical tool names.
// Kimi ACP uses toolCallID formats like "read_file-<ts>-<n>",
// "list_directory-<ts>-<n>", "glob-<ts>-<n>", "run_shell_command-<ts>-<n>",
// "ask-<uuid>". The prefix before the first dash encodes the tool type.
var KimiACPTCIDPrefixes = map[string]string{
	"read_file":         "Read",
	"list_directory":    "LS",
	"glob":              "Glob",
	"run_shell_command": "Bash",
	"ask":               "AskUserQuestion",
	"write_file":        "Write",
	"edit_file":         "Edit",
	"replace":           "Edit",
	"search_file":       "Grep",
	"search_directory":  "Grep",
}

func init() {
	backends.Register(&backends.BackendPlugin{
		ID: "kimi",
		Spec: model.BackendSpec{
			ID: "kimi", Backend: "kimi", DefaultCmd: "kimi", Name: "Kimi", Specialty: "Kimi AI 编码助手",
			ThinkingEffortLevels: []string{"off", "on"},
			AcpCommand:           "kimi acp",
			ACPLoadSession:       true,
			InstallCmd:           "npm install -g kimiai-cli",
			SortOrder:            10,
		},
		Behavior: &ai.ACPBehavior{
			ParseToolCall:       parseACPToolCall,
			ParseToolCallUpdate: parseACPToolCallUpdate,
		},
		ModelSource: model.StaticSource("kimi", "kimi", KimiCatalog),
		ACP: &backends.ACPPlugin{
			ToolCallIDPrefixes: KimiACPTCIDPrefixes,
		},
		ProviderEnv: &model.ProviderEnvSpec{
			// kimi-cli's own provider is OpenAI-compatible and reads KIMI_*.
			//
			// NO Anthropic row on purpose. llm.py's
			// augment_provider_with_env_vars only rewrites base_url/api_key for
			// provider type "kimi" (KIMI_*) and "openai_legacy" (OPENAI_*); for
			// type "anthropic" it does nothing at all. Verified: with
			// type="anthropic", ANTHROPIC_BASE_URL pointing at a dead host and
			// ANTHROPIC_API_KEY holding a valid key, the request still went to
			// the config file's base_url and authenticated with the config
			// file's key (the 401 named that key). Declaring an Anthropic row
			// here therefore accepted the binding and then silently ignored the
			// endpoint on every turn. The config directory below is the only
			// channel that actually reaches kimi's Anthropic path.
			//
			// KIMI_SHARE_DIR replaces the whole ~/.kimi tree, which is also
			// where the login lives: credentials/ and device_id must be present
			// in the isolated directory or session/new answers -32000
			// Authentication required even with a custom provider configured.
			// The directory setup symlinks them back from the user's own
			// ~/.kimi.
			OpenAI:                  model.ProtoEnv("KIMI_BASE_URL", "KIMI_API_KEY", ""),
			ConfigDirEnv:            "KIMI_SHARE_DIR",
			ConfigDirFile:           "config.toml",
			ConfigDirProtocols:      []string{model.ProviderProtocolOpenAI, model.ProviderProtocolAnthropic},
			ConfigDirProviderID:     "clawbench",
			ConfigDirSecretInFile:   true,
			ConfigDirDeclaresModels: true,
			UserConfigDir:           ".kimi",
			ConfigDirRender:         ConfigTOMLContent,
		},
	})
}
