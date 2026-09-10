package ai

import (
	"strings"

	acp "github.com/coder/acp-go-sdk"
)

// ---------------------------------------------------------------------------
// Tool name heuristics — maps ACP tool identifiers to canonical frontend names
// ---------------------------------------------------------------------------

// LookupACPToolCallIDPrefixesFn is a function variable that returns the
// ACP toolCallID prefix map for the given backendID. Set by the backends
// package during init() to enable backend-specific prefix lookup.
// Returns nil if no backend-specific prefixes are registered.
var LookupACPToolCallIDPrefixesFn func(backendID string) map[string]string

// LookupACPRemapsFn is a function variable that returns the ACP input
// remapping map for the given backendID. Set by the backends package during
// init() to enable backend-specific remap lookup. Falls back to the generic
// 6-field map if no backend-specific remaps are registered.
var LookupACPRemapsFn func(backendID string) map[string]string

// acpToolCallIDPrefix maps Kimi-style toolCallID prefixes to canonical tool names.
// Kimi ACP uses toolCallID formats like "read_file-<ts>-<n>", "list_directory-<ts>-<n>",
// "glob-<ts>-<n>", "run_shell_command-<ts>-<n>", "ask-<uuid>".
// The prefix before the first dash encodes the tool type.
var acpToolCallIDPrefix = map[string]string{
	"read_file":         "Read",
	"list_directory":    "LS",
	"glob":              "Glob",
	"run_shell_command": "Bash",
	"ask":               "AskUserQuestion",
	"write_file":        "Write",
	"edit_file":         "Edit",
	"replace":           "Edit",
	"search_file":       "Grep",
	"search_directory":  "Grep",
}

// acpLowerAlias maps lowercase single-word tool titles to canonical PascalCase names.
// Used for case-insensitive matching of titles like "bash" → "Bash", "terminal" → "Bash".
var acpLowerAlias = map[string]string{
	"bash":     "Bash",
	"terminal": "Bash",
	"shell":    "Bash",
	"read":     "Read",
	"write":    "Write",
	"edit":     "Edit",
	"glob":     "Glob",
	"grep":     "Grep",
	"ls":       "LS",
	"list":     "LS",
	"agent":    "Agent",
	"skill":    "Skill",
}

// acpAgentSubtypes lists known Agent sub-type names that ACP agents use as
// tool call titles. These are not standalone tools — they represent Agent
// delegation calls with a subagent_type input field. Without this mapping,
// extractToolName returns them as-is (e.g. "Explore"), which has no frontend
// icon and falls back to the wrench. Map them to "Agent" so the frontend
// uses the Bot icon + agent category color, and reads subagent_type from
// input for the display name.
var acpAgentSubtypes = map[string]bool{
	"explore":          true,
	"plan":             true,
	"general-purpose":  true,
	"general":          true,
	"claude":           true,
	"code-reviewer":    true,
	"statusline-setup": true,
	"fork":             true,
}

func acpIsAgentSubtype(title string) bool {
	return acpAgentSubtypes[strings.ToLower(title)]
}

// acpToolNamePatterns maps ACP tool title prefixes to canonical tool names.
// ACP agents send titles like "Read file contents", "Edit file", "Run command"
// but the frontend expects "Read", "Edit", "Bash" for icon/summary matching.
// Longer/more-specific prefixes MUST appear before shorter ones to avoid
// incorrect prefix matches (e.g. "WebSearch" before "Web", "MultiEdit" before "Edit").
var acpToolNamePatterns = []struct{ prefix, canonical string }{
	// Multi-word / compound tools first
	{"NotebookEdit", "NotebookEdit"},
	{"MultiEdit", "MultiEdit"},
	{"TodoWrite", "TodoWrite"},
	{"TodoRead", "TodoRead"},
	{"WebSearch", "WebSearch"},
	{"WebFetch", "WebFetch"},
	{"AskUserQuestion", "AskUserQuestion"},
	{"EnterPlanMode", "EnterPlanMode"},
	{"ExitPlanMode", "ExitPlanMode"},
	{"EnterWorktree", "EnterWorktree"},
	{"LeaveWorktree", "LeaveWorktree"},
	{"SendMessage", "SendMessage"},
	{"TaskCreate", "TaskCreate"},
	{"TaskUpdate", "TaskUpdate"},
	{"TaskList", "TaskList"},
	{"TaskGet", "TaskGet"},
	{"TaskStop", "TaskStop"},
	{"TaskOutput", "TaskOutput"},
	{"TaskCreate", "TaskCreate"},
	{"TaskUpdate", "TaskUpdate"},
	{"TaskList", "TaskList"},
	{"TaskGet", "TaskGet"},
	{"Task", "Agent"}, // ACP generic "Task" tool → Agent (sub-agent delegation)
	{"ComputerUse", "ComputerUse"},
	{"TeamCreate", "TeamCreate"},
	{"TeamDelete", "TeamDelete"},
	{"StructuredOutput", "StructuredOutput"},
	{"SkillManage", "SkillManage"},
	{"DeepThink", "DeepThink"},
	{"ImageGen", "ImageGen"},
	{"PermissionApproval", "PermissionApproval"},
	{"WeChatReply", "WeChatReply"},
	{"WeComReply", "WeComReply"},
	{"save_memory", "save_memory"},
	// Single-word tools — must come after compound prefixes above
	{"Read", "Read"},
	{"Write", "Write"},
	{"Edit", "Edit"},
	{"Bash", "Bash"},
	{"Terminal", "Bash"},
	{"Glob", "Glob"},
	{"Grep", "Grep"},
	{"LS", "LS"},
	{"List", "LS"},
	{"Agent", "Agent"},
	{"Skill", "Skill"},
	{"LSP", "LSP"},
	{"Monitor", "Monitor"},
	{"PowerShell", "PowerShell"},
	{"Git", "Git"},
}

// acpKindToCanonical maps ACP ToolKind enum values to the PascalCase
// canonical names expected by the frontend TOOL_ICONS mapping.
//
// ToolKindOther is deliberately ABSENT: it used to map to "Skill", which turned
// every unrecognized kind=other tool into a bogus Skill pill. Real Skill tools
// resolve via the title/alias tables (title "skill"/"Skill" → "Skill"), so no
// kind fallback is needed for them. Unknown kind=other tools fall through to a
// title-preserving passthrough in extractToolName instead (honest label over a
// misleading one).
var acpKindToCanonical = map[acp.ToolKind]string{
	acp.ToolKindRead:       "Read",
	acp.ToolKindEdit:       "Edit",
	acp.ToolKindDelete:     "Edit", // delete operations → Edit category
	acp.ToolKindMove:       "Edit", // move/rename → Edit category
	acp.ToolKindSearch:     "Grep", // search → Grep category
	acp.ToolKindExecute:    "Bash", // execute/run → Bash category
	acp.ToolKindThink:      "DeepThink",
	acp.ToolKindFetch:      "WebFetch",
	acp.ToolKindSwitchMode: "EnterPlanMode",
}

// extractToolName resolves the canonical frontend tool name from ACP tool identifiers
// and input formatting. We try backend-specific toolCallId prefix first (Kimi pattern),
// then shared title prefix/alias matching, then kind-to-canonical,
// then fall back to the title itself.
func extractToolName(title string, kind acp.ToolKind, backendID string, toolCallID ...string) string {
	// Try toolCallID prefix lookup first (backend-specific then legacy global)
	if name := lookupByToolCallID(toolCallID, backendID); name != "" {
		return name
	}

	if title != "" {
		if name := lookupByTitle(title); name != "" {
			return name
		}
	}

	// Map ACP ToolKind to canonical PascalCase names expected by the frontend.
	if canonical, ok := acpKindToCanonical[kind]; ok {
		return canonical
	}

	// Unresolved tool. kind=other with an unrecognized title must NOT become
	// "Skill" (the old catch-all mislabeled subagent/unknown tools — codex
	// "Start subagent X" etc.). Prefer the raw title so the UI shows something
	// truthful, falling back to the kind enum only when no title exists.
	if kind == acp.ToolKindOther && title != "" {
		return title
	}
	return string(kind)
}

// lookupByToolCallID tries to resolve a tool name from the toolCallID prefix.
// Checks backend-specific prefix map first, then falls back to the global map.
func lookupByToolCallID(toolCallID []string, backendID string) string {
	if len(toolCallID) == 0 || toolCallID[0] == "" {
		return ""
	}
	tid := toolCallID[0]
	dashIdx := strings.Index(tid, "-")
	if dashIdx <= 0 {
		return ""
	}
	prefix := tid[:dashIdx]

	// Backend-specific lookup
	if backendID != "" && LookupACPToolCallIDPrefixesFn != nil {
		if prefixes := LookupACPToolCallIDPrefixesFn(backendID); prefixes != nil {
			if canonical, ok := prefixes[prefix]; ok {
				return canonical
			}
		}
	}

	// Legacy global fallback
	if canonical, ok := acpToolCallIDPrefix[prefix]; ok {
		return canonical
	}
	return ""
}

// lookupByTitle resolves a tool name from the title string using alias matching,
// prefix patterns, or single-word passthrough (with agent subtype detection).
func lookupByTitle(title string) string {
	// Fast path: case-insensitive alias lookup for single-word titles.
	if !strings.Contains(title, " ") {
		if canonical, ok := acpLowerAlias[strings.ToLower(title)]; ok {
			return canonical
		}
	}

	// Try matching title against known canonical tool name prefixes.
	for _, p := range acpToolNamePatterns {
		if strings.HasPrefix(title, p.prefix) {
			return p.canonical
		}
	}

	// Single word without dots/slashes may be canonical already,
	// but Agent subtypes (Explore, Plan, etc.) should map to "Agent".
	if !strings.Contains(title, " ") && !strings.Contains(title, ".") && !strings.Contains(title, "/") {
		if acpIsAgentSubtype(title) {
			return "Agent"
		}
		return title
	}

	return ""
}

// ExtractToolNameForTest exports extractToolName for use in integration tests.
// Production code must not use this.
func ExtractToolNameForTest(title string, kind acp.ToolKind, backendID string, toolCallID ...string) string {
	return extractToolName(title, kind, backendID, toolCallID...)
}
