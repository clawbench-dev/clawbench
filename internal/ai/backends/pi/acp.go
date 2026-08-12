package pi

// PiACPInputRemaps maps Pi ACP tool input field names to canonical names.
// The pi-acp adapter (svkozak/pi-acp) bridges pi's RPC tool schemas to ACP.
// pi's edit tool uses an { path, oldText, newText } schema (or { path, edits })
// rather than the canonical old_string/new_string names, and its read tool
// uses "path" instead of "file_path". These remaps normalize them for the
// frontend renderers.
var PiACPInputRemaps = map[string]string{
	"path":    "file_path",
	"oldText": "old_string",
	"newText": "new_string",
}

// PiACPToolCallIDPrefixes maps Pi ACP tool names to their canonical frontend
// names. pi emits lowercase single-word tool names ("read", "write", "edit",
// "bash", "grep", "glob", "ls") which the shared acpLowerAlias table already
// resolves; this table is registered as defense-in-depth and to document the
// mapping for the pi-acp adapter explicitly.
var PiACPToolCallIDPrefixes = map[string]string{
	"read":  "Read",
	"write": "Write",
	"edit":  "Edit",
	"bash":  "Bash",
	"grep":  "Grep",
	"glob":  "Glob",
	"ls":    "LS",
	"ask":   "AskUserQuestion",
}
