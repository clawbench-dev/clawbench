package openclaw

// OpenClawACPTitlePrefixes maps the tool-call TITLE prefix to the canonical name
// the frontend uses for icons/summaries.
//
// OpenClaw's bridge puts the tool name at the START of the title followed by
// ": " and a preview: "read: path: /a/b.go", "exec: command: ls", "ls: path: /x".
// Its toolCallId is an opaque "call_<hex>" with no tool name, so the
// toolCallID-prefix path cannot see it.
//
// Most titles already resolve through the shared tables:
//
//	"read: …"       → read        → lowercase alias → Read
//	"write: …"      → write       → Write
//	"edit: …"       → edit        → Edit
//	"exec: …"       → exec        → (kind=execute) → Bash
//	"web_fetch: …"  → web_fetch   → (kind=fetch)   → WebFetch
//	"web_search: …" → web_search  → (kind=search)  → Grep
//
// The entries below are the ones that do NOT resolve. Two shapes fail:
//   - single-token titles with no ":" (lookupByTitle's passthrough returns them
//     verbatim), and
//   - "ls: …" / "apply_patch: …", whose prefix is not a known canonical name and
//     whose kind (other) has no canonical mapping.
//
// Verified by running ai.ExtractToolNameForTest over every frame captured by
// TestOpenClawACPProbe_L4_ToolFrameShapes:
//
//	"ls: path: /x"       kind=other  → "ls: path: /x"      (leaks the whole title)
//	"tool_search"        kind=search → "tool_search"       (leaks; it is a tool
//	                                                        DISCOVERY call, not a
//	                                                        file search, so Grep
//	                                                        is a lie but renders)
//	"memory_search"      kind=search → "memory_search"     (leaks; not a file search)
//	"apply_patch"        kind=other  → "apply_patch"       (leaks; it is an edit)
//	"progress_card"      kind=other  → "progress_card"     (leaks; no renderer)
//	"sessions_list"      kind=other  → "sessions_list"     (leaks)
//	"tool_describe"      kind=other  → "tool_describe"     (leaks)
//
// Mappings chosen for renderer fit rather than literal accuracy:
//   - apply_patch → Edit: its rawInput is a unified patch, and Edit's card
//     renders a file header + diff, which is the closest fit.
//   - tool_search / memory_search → Grep: both are searches over non-file data.
//     Grep's card shows a pattern row, which reads correctly; there is no
//     "discovery" or "memory" category in the frontend.
//   - progress_card / sessions_list / tool_describe → DeepThink: control-plane
//     calls with no dedicated renderer. DeepThink's card renders arbitrary input
//     without pretending to be a file/terminal operation.
var OpenClawACPTitlePrefixes = map[string]string{
	// "ls: …" would otherwise leak the whole title; bare "ls" already resolves
	// via the lowercase alias, so this only adds the decorated form.
	"ls": "LS",
	// Single-token native names that lookupByTitle would pass through verbatim.
	"tool_search":   "Grep",
	"memory_search": "Grep",
	"apply_patch":   "Edit",
	"progress_card": "DeepThink",
	"sessions_list": "DeepThink",
	"tool_describe": "DeepThink",
}

// OpenClawACPInputRemaps maps OpenClaw ACP rawInput field names to canonical
// names.
//
// Measured shapes (TestOpenClawACPProbe_L4_ToolFrameShapes):
//
//	read    {"path": "/a/b.go"}                          → file_path
//	ls      {"path": "/x"}                               → path (LS reads path)
//	exec    {"command": "…", "workdir": "…"}             → command
//	write   {"path": "/x", "content": "…"}               → file_path
//	edit    {"path": "/x", "edits": [{"oldText","newText"}]} → file_path + old_string/new_string
//	web_fetch {"url": "…"}                               → url
//
// Two gaps that a flat remap table CANNOT close, and which are therefore handled
// in acp.go's tool parser instead:
//   - `read`/`write`/`edit` use "path", but Read/Write/Edit cards read
//     "file_path". LS reads "path", so remapping path→file_path globally would
//     break LS. The remap is applied per tool name, not globally.
//   - `edit` carries a nested edits[] array with oldText/newText and NO flat
//     old_string/new_string. The renderer only reads the flat fields, so the
//     first edit must be promoted (same treatment pi needs).
var OpenClawACPInputRemaps = map[string]string{
	// Generic camelCase normalizations other ACP backends also need.
	"filePath":   "file_path",
	"dirPath":    "path",
	"oldString":  "old_string",
	"newString":  "new_string",
	"replaceAll": "replace_all",
	"cellIndex":  "cell_index",
	"cellType":   "cell_type",
}
