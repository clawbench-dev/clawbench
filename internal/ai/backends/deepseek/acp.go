package deepseek

// CodeWhaleACPInputRemaps maps CodeWhale ACP input field names to canonical names.
// CodeWhale (formerly DeepSeek TUI) uses concise snake_case names that differ
// from the canonical names expected by the frontend renderers.
var CodeWhaleACPInputRemaps = map[string]string{
	"path":      "file_path",
	"search":    "old_string",
	"replace":   "new_string",
	"filePaths": "file_paths",
	"dirPath":   "path",
	"dir_path":  "path",
	"oldString": "old_string",
	"newString": "new_string",
	"filePath":  "file_path",
	"cellIndex": "cell_index",
	"cellType":  "cell_type",
}

// CodeWhaleACPToolCallIDPrefixes maps CodeWhale ACP tool names to their
// toolCallID prefix used for display grouping. CodeWhale uses the same
// tool names as the CodeWhale CLI stream format.
var CodeWhaleACPToolCallIDPrefixes = map[string]string{
	"read_file":   "Read",
	"write_file":  "Write",
	"edit_file":   "Edit",
	"replace":     "Edit",
	"bash":        "Bash",
	"list_files":  "LS",
	"list_dir":    "LS",
	"grep_files":  "Grep",
	"file_search": "Glob",
	"glob":        "Glob",
	"ask":         "AskUserQuestion",
}
