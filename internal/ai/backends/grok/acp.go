package grok

// GrokACPRemaps contains the generic ACP normalization fields.
// Grok ACP uses the standard ACP tool protocol; the generic
// camelCase→snake_case mappings cover edge cases.
var GrokACPRemaps = map[string]string{
	"oldString": "old_string", "newString": "new_string",
	"dirPath": "path", "filePath": "file_path",
	"cellIndex": "cell_index", "cellType": "cell_type",
}
