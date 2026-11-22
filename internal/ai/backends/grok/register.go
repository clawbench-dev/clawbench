package grok

import (
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

// GrokACPTCIDPrefixes maps Grok ACP tool IDs to ClawBench canonical tool names.
// Grok's ACP toolCallID encodes the tool type before the first dash; these
// prefixes are backend-specific additions on top of the generic map.
var GrokACPTCIDPrefixes = map[string]string{
	"read_file":         "Read",
	"write_file":        "Write",
	"search_replace":    "Edit",
	"edit_file":         "Edit",
	"run_terminal_cmd":  "Bash",
	"bash":              "Bash",
	"shell":             "Bash",
	"list_dir":          "LS",
	"list_directory":    "LS",
	"grep":              "Grep",
	"glob":              "Glob",
	"web_search":        "WebSearch",
	"web_fetch":         "WebFetch",
	"ask_user_question": "AskUserQuestion",
	"ask":               "AskUserQuestion",
}

// GrokACPRemaps normalizes Grok ACP rawInput field names to canonical snake_case.
var GrokACPRemaps = map[string]string{
	"oldString":  "old_string",
	"newString":  "new_string",
	"replaceAll": "replace_all",
	"dirPath":    "path",
	"filePath":   "file_path",
	"cellIndex":  "cell_index",
	"cellType":   "cell_type",
}

func init() {
	// Grok runs over ACP only (grok agent stdio).
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
		ProviderEnv: &model.ProviderEnvSpec{
			OpenAI: model.ProtoEnv("GROK_XAI_API_BASE_URL", "XAI_API_KEY", ""),
		},
	})
}
