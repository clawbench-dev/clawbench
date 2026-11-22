package claude

import (
	"clawbench/internal/ai"
)

// ParseACPToolCall extracts a ToolCall from a Claude/Qoder ACP ToolCall event.
// Claude ACP specifics:
//   - Tool name from _meta.claudeCode.toolName (preferred), fallback to ExtractToolName
//   - rawInput uses snake_case field names (file_path, old_string, new_string)
//   - Content may include diff-type blocks
//   - rawOutput is plain text string
//   - _meta.claudeCode.toolResponse contains stdout/stderr/interrupted on completed
//
// The call/update sequence is shared with codebuddy (see
// ai.NewMetaToolCallParser); only the _meta key layout differs.
//
// Input normalization still runs the shared generic table even though Claude's
// own fields are already canonical: a plugin/MCP tool that emits camelCase
// (oldString, dirPath) is normalized the same way every other backend is.
var (
	ParseACPToolCall, ParseACPToolCallUpdate = ai.NewMetaToolCallParser("claude", ai.ExtractMetaToolName)
)
