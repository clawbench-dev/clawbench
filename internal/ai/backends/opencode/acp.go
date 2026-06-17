package opencode

// OpenCodeACPInputRemaps maps OpenCode ACP camelCase field names to snake_case.
// Includes both OpenCode-specific overrides and the generic ACP normalization
// fields that apply to all ACP rawInput (filePath, dirPath, cellIndex, cellType).
var OpenCodeACPInputRemaps = map[string]string{
	// OpenCode-specific overrides
	"oldString":  "old_string",
	"newString":  "new_string",
	"replaceAll": "replace_all",
	// Generic ACP normalization (applied for all ACP rawInput)
	"dirPath":   "path",
	"filePath":  "file_path",
	"cellIndex": "cell_index",
	"cellType":  "cell_type",
}
