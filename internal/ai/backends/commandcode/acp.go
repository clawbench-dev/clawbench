package commandcode

// CommandCodeACPTitlePrefixes maps the tool-call TITLE prefix to the canonical
// name the frontend uses for icons/summaries.
//
// Command Code puts the tool name at the START of the title, but as a *human
// label* rather than the native tool name (cli.mjs toolTitleFor): read_file →
// "Read: /path", grep → "Search: pattern", shell_command → "Shell: cmd". Its
// toolCallID is an opaque "call_function_<hex>_<n>" carrying no tool name, so
// neither the toolCallID-prefix path nor the shared title patterns can see the
// native name.
//
// Most titles already resolve correctly through the shared tables:
//
//	"Shell: …"  → kind=execute → Bash      "Read: …"   → Read
//	"List: …"   → LS                        "Edit: …"   → Edit
//	"Write: …"  → Write                     "Glob: …"   → Glob
//	"Fetch: …"  → WebFetch                  "Agent: …"  → Agent
//	"Monitor: …"→ Monitor                   "Command: …"→ Bash
//	"Kill: …"   → Bash                      "Diagnostics"→ Diagnostics
//
// The entries below are the ones that DO NOT resolve. Their titles are a single
// token with no ".", "/" or ":" (or, for todo_write, a natural-language phrase),
// so extractToolName's lookupByTitle short-circuits on "single token, return the
// title as-is" and never reaches the kind → canonical mapping. Verified by
// running ai.ExtractToolNameForTest over every tool (see register_test.go):
//
//	"enter_plan_mode"   kind=switch_mode → "enter_plan_mode"   (leaks)
//	"exit_plan_mode"    kind=switch_mode → "exit_plan_mode"    (leaks)
//	"plan_review"       kind=think       → "plan_review"       (leaks)
//	"ask_user_question" kind=think       → "ask_user_question" (leaks)
//	"monitor_events"    kind=execute     → "monitor_events"    (leaks)
//	"shell_tasks"       kind=execute     → "shell_tasks"       (leaks)
//	"bash_output"       kind=execute     → "bash_output"       (leaks)
//	"Updating todos"    kind=think       → "DeepThink"         (wrong bucket)
//
// NOTE on "Search": grep and web_search BOTH use the label "Search" with
// kind=search, so they are indistinguishable from title+kind alone. Both land on
// Grep, which is acceptable (both are searches); mapping "Search" → WebSearch
// would mislabel every grep. Telling them apart would need a rawInput-based hook
// (grep has `pattern`, web_search has `query`) — not worth the complexity.
var CommandCodeACPTitlePrefixes = map[string]string{
	// Single-token native names that lookupByTitle would pass through verbatim.
	"enter_plan_mode":   "EnterPlanMode",
	"exit_plan_mode":    "ExitPlanMode",
	"plan_review":       "ExitPlanMode",
	"ask_user_question": "AskUserQuestion",
	"monitor_events":    "Bash",
	"shell_tasks":       "Bash",
	"bash_output":       "BashOutput",
	// Natural-language title (no colon); kind=think would land in DeepThink.
	"Updating todos": "TodoWrite",
}

// CommandCodeACPInputRemaps maps Command Code ACP rawInput field names to
// canonical names.
//
// Command Code's own tool arguments are already canonical (path / file_path /
// old_string / new_string / command / pattern — read off the tool schemas), so
// this table only carries the generic camelCase normalizations other ACP
// backends also need. A plugin/MCP tool that emits camelCase rawInput still gets
// normalized.
var CommandCodeACPInputRemaps = map[string]string{
	"filePath":   "file_path",
	"dirPath":    "path",
	"oldString":  "old_string",
	"newString":  "new_string",
	"replaceAll": "replace_all",
	"cellIndex":  "cell_index",
	"cellType":   "cell_type",
}
