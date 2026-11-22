package ai

// GenericACPRemaps is the input-field remap table applied to every ACP backend
// that declares none of its own, and the base a backend's own table extends.
//
// Keys are the camelCase spellings ACP agents use; values are the canonical
// snake_case fields the frontend renderers read. cmd→command and exec→command are
// NOT listed: normalizeToolInput applies those to every backend already (see
// defaultMappings in common_stream.go). filePath→file_path is listed even though
// defaultMappings also covers it, because it is the field every ACP tool
// carries and leaving it out makes this table look incomplete.
//
// Invariant (pinned by TestNormalizeToolInput_RemapsNeverCascade): remaps do not
// cascade. normalizeToolInput only consumes a key the frame actually carried, so
// a table may safely map dirPath→path alongside path→file_path — a directory
// argument stays a path instead of turning into a file_path on some runs.
//
// This table lives in internal/ai, not in the backend registry, because it is
// not per-backend data: it is the shared fallback, and internal/ai needs it when
// the registry is not wired (isolated package tests).
var GenericACPRemaps = map[string]string{
	"filePath":   "file_path",
	"oldString":  "old_string",
	"newString":  "new_string",
	"replaceAll": "replace_all",
	"dirPath":    "path",
	"cellIndex":  "cell_index",
	"cellType":   "cell_type",
}
