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
	acp.ToolKindOther:      "Skill", // uncategorized tools → Skill category
}

// extractToolName resolves the canonical frontend tool name from ACP tool identifiers
// and input formatting. We try backend-specific toolCallId prefix first (Kimi pattern),
// then shared title prefix/alias matching, then kind-to-canonical,
// then fall back to the title itself.
func extractToolName(title string, kind acp.ToolKind, backendID string, toolCallID ...string) string {
	// Backend-specific toolCallId prefix lookup (e.g. Kimi ACP uses "read_file-<ts>-<n>").
	if len(toolCallID) > 0 && toolCallID[0] != "" && backendID != "" && LookupACPToolCallIDPrefixesFn != nil {
		tid := toolCallID[0]
		if dashIdx := strings.Index(tid, "-"); dashIdx > 0 {
			prefix := tid[:dashIdx]
			if prefixes := LookupACPToolCallIDPrefixesFn(backendID); prefixes != nil {
				if canonical, ok := prefixes[prefix]; ok {
					return canonical
				}
			}
		}
	}

	// Legacy fallback: try the global acpToolCallIDPrefix map if backend-specific lookup failed.
	if len(toolCallID) > 0 && toolCallID[0] != "" {
		tid := toolCallID[0]
		if dashIdx := strings.Index(tid, "-"); dashIdx > 0 {
			prefix := tid[:dashIdx]
			if canonical, ok := acpToolCallIDPrefix[prefix]; ok {
				return canonical
			}
		}
	}

	if title != "" {
		// Fast path: case-insensitive alias lookup for single-word titles.
		// Some ACP agents send lowercase names (e.g. "bash", "terminal")
		// while the frontend expects PascalCase ("Bash").
		if !strings.Contains(title, " ") {
			if canonical, ok := acpLowerAlias[strings.ToLower(title)]; ok {
				return canonical
			}
		}
		// Try matching title against known canonical tool name prefixes.
		// Longer/more-specific prefixes must appear before shorter ones
		// (e.g. "MultiEdit" before "Edit", "WebSearch" before "Web").
		for _, p := range acpToolNamePatterns {
			if strings.HasPrefix(title, p.prefix) {
				return p.canonical
			}
		}
		// If title is a single word (no spaces), use it directly — it may already be canonical.
		// But file paths with dots/slashes (e.g., "README.md", "cmd/server") are not canonical
		// tool names — fall through to kind mapping instead.
		// Exception: known Agent sub-type names (e.g., "Explore", "Plan") are not standalone
		// tools — they are always Agent calls with a subagent_type field. Map them to "Agent"
		// so the frontend uses the correct icon/category.
		if !strings.Contains(title, " ") && !strings.Contains(title, ".") && !strings.Contains(title, "/") {
			if acpIsAgentSubtype(title) {
				return "Agent"
			}
			return title
		}
	}
	// Map ACP ToolKind to canonical PascalCase names expected by the frontend.
	// Without this, string(kind) returns lowercase ("read", "execute", "search")
	// which won't match TOOL_ICONS in the frontend.
	if canonical, ok := acpKindToCanonical[kind]; ok {
		return canonical
	}
	return string(kind)
}

// ExtractToolNameForTest exports extractToolName for use in integration tests.
// Production code must not use this.
func ExtractToolNameForTest(title string, kind acp.ToolKind, backendID string, toolCallID ...string) string {
	return extractToolName(title, kind, backendID, toolCallID...)
}
