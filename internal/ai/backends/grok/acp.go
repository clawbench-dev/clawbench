package grok

// GrokACPTCIDPrefixes maps Grok tool IDs to ClawBench canonical tool names.
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
