package codebuddy

import (
	"clawbench/internal/ai"
)

// ParseACPToolCall extracts a ToolCall from a CodeBuddy ACP ToolCall event.
// CodeBuddy ACP specifics:
//   - Tool name from _meta["codebuddy.ai/toolName"] (preferred), fallback to extractToolName
//   - rawInput is streamed incrementally (each tool_call_update contains partial content)
//   - Field names may be mixed camelCase/snake_case, normalized by the shared
//     generic remap table (ai.GenericACPRemaps)
//
// The call/update sequence is shared with claude (see
// ai.NewMetaToolCallParser); only the _meta key layout differs.
var (
	ParseACPToolCall, ParseACPToolCallUpdate = ai.NewMetaToolCallParser("codebuddy", ai.ExtractMetaToolNameFlat)
)
