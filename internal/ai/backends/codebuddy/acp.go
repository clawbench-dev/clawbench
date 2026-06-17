package codebuddy

// CodebuddyACPRemaps contains the generic ACP normalization fields.
// Codebuddy ACP rawInput already uses snake_case for most fields, but the
// generic camelCase→snake_case mappings are still needed for edge cases.
var CodebuddyACPRemaps = map[string]string{
	"oldString": "old_string", "newString": "new_string",
	"dirPath": "path", "filePath": "file_path",
	"cellIndex": "cell_index", "cellType": "cell_type",
}
