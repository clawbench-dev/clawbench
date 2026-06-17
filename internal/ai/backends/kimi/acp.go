package kimi

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
