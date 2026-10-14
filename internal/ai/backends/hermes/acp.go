package hermes

// HermesACPTitlePrefixes maps the tool name at the START of a Hermes ACP
// tool-call title to the canonical name the frontend uses for icons/summaries.
//
// Hermes builds every title as "<tool_name>: <preview>" (acp_adapter/tools.py
// build_tool_title) and its toolCallID is an opaque "tc-<hex>" carrying no tool
// name, so neither the toolCallID-prefix path nor the shared title patterns can
// see the native name. Hermes does send an ACP `kind`, which resolves most tools
// correctly on its own (read_file→Read, terminal→Bash, search_files→Grep,
// browser_navigate→WebFetch, …). This table carries the tools whose kind
// resolves to the wrong canonical name:
//
//   - kind=other has no canonical mapping at all, so the raw native name leaks
//     into the UI ("todo_list: planning 1 task(s)", "memory: Memory manage").
//     The whole kind=other set the hermes-acp toolset enables is covered here.
//   - a few kind=read/edit/execute tools have a more specific frontend entry
//     (skills → Skill/SkillManage; web_search → WebSearch rather than the
//     kind-derived WebFetch; delegate_task → Agent rather than Bash).
//
// Entries whose kind already yields the right name are intentionally omitted.
// The key set is scoped to the tools the `hermes-acp` toolset actually enables
// (toolsets.py: _CODING_TOOLS minus clarify), so it stays in sync with what a
// ClawBench session can see. Names were read off a live `hermes acp` session.
// TestHermesAllEnabledToolsResolveCanonically fails if a tool from that set is
// left to leak its native name.
var HermesACPTitlePrefixes = map[string]string{
	// kind=other — raw native name would otherwise reach the UI.
	"todo_list":      "TodoWrite",
	"todo":           "TodoWrite", // legacy alias still present in older histories
	"memory":         "save_memory",
	"session_search": "Grep", // closest existing canonical for a conversation search
	// process_manage (not "process") is the tool name Hermes registers; it is
	// absent from TOOL_KIND_MAP, so kind=other. Bash matches its terminal
	// sibling and the shared convention for command/process tools.
	"process_manage": "Bash",
	// The browser actions below are absent from TOOL_KIND_MAP (kind=other),
	// unlike their browser_click/type/scroll/press/back siblings which are
	// kind=execute → Bash. Map them to Bash for the same convention rather
	// than leaking the raw native name.
	"browser_console": "Bash",
	"browser_cdp":     "Bash",
	"browser_dialog":  "Bash",
	"browser_exec":    "Bash",
	"browser_vault":   "Bash",
	// manage_connections drives external-service connector lifecycle; it has no
	// action-shaped frontend entry, so it uses the generic capability bucket
	// (the same "skill" category as save_memory / LSP) rather than leaking.
	"manage_connections": "Skill",

	// kind=read/edit — the frontend has dedicated Skill entries.
	"skill_view":   "Skill",
	"skills_list":  "Skill",
	"skill_manage": "SkillManage",

	// kind=execute — would otherwise all collapse to Bash.
	"delegate_task": "Agent",

	// kind=fetch — a *search* should show the WebSearch icon, not WebFetch.
	"web_search": "WebSearch",
}

// HermesACPInputRemaps maps Hermes ACP rawInput field names to canonical names.
//
// Hermes' tool arguments are already canonical (path / old_string / new_string /
// command / pattern — verified against the tool schemas and a live session), and
// rawInput is null for most tools because Hermes renders structured content
// instead. The map therefore carries only the generic camelCase normalizations
// other ACP backends also need, so a plugin/MCP tool that does emit camelCase
// rawInput is still normalized.
var HermesACPInputRemaps = map[string]string{
	"filePath":   "file_path",
	"dirPath":    "path",
	"oldString":  "old_string",
	"newString":  "new_string",
	"replaceAll": "replace_all",
	"cellIndex":  "cell_index",
	"cellType":   "cell_type",
}
