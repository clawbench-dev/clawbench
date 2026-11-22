package opencode

import (
	"clawbench/internal/ai"
)

// ParseACPToolCall extracts a ToolCall from an OpenCode ACP ToolCall event.
// OpenCode ACP specifics:
//   - No _meta, so the name comes from the title + kind (nil name source)
//   - rawInput uses camelCase (filePath → file_path needs normalization)
//   - rawOutput is a nested object {metadata: {...}, output: "..."}
//
// The call/update sequence is shared with claude/codebuddy (see
// ai.NewMetaToolCallParser); opencode simply has no _meta name source.
var (
	ParseACPToolCall, ParseACPToolCallUpdate = ai.NewMetaToolCallParser("opencode", nil)
)

// IsChildSessionUpdate reports whether a frame was projected from an OpenCode
// v2 sub-agent (child) session into the parent's stream. Such frames carry no
// parent tool-call link, so the host drops them unless one was resolved.
func IsChildSessionUpdate(meta map[string]any) bool {
	_, ok := meta["opencode/child-session"]
	return ok
}
