package ai

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

const maxSummaryLen = 200

// ToolCallMeta holds extracted metadata from a tool call event,
// used by the WS handler to include summary/display info in slim events.
type ToolCallMeta struct {
	ToolID      string `json:"tool_id"`
	Summary     string `json:"summary"`
	DisplayName string `json:"display_name"`
	FilePath    string `json:"file_path"`
	DurationMs  int    `json:"duration_ms,omitempty"`
}

// ExtractToolCallMeta extracts metadata (summary, display_name, file_path, duration_ms)
// from a StreamEvent. This is called before WS forwarding so that
// slim WS events can include display information without waiting for
// AccumulateBlock.
func ExtractToolCallMeta(event StreamEvent) ToolCallMeta {
	if event.Tool == nil {
		return ToolCallMeta{}
	}

	var input map[string]any
	if event.Tool.Input != "" {
		_ = json.Unmarshal([]byte(event.Tool.Input), &input)
	}

	return ToolCallMeta{
		ToolID:      event.Tool.ID,
		Summary:     ExtractSummary(event.Tool.Name, input),
		DisplayName: ExtractDisplayName(event.Tool.Name, input),
		FilePath:    ExtractFilePath(event.Tool.Name, input),
		DurationMs:  event.Tool.DurationMs,
	}
}

// ExtractToolCallMetaFromInput extracts metadata from already-parsed tool input.
// Used by AccumulateBlock after input has been merged/updated.
func ExtractToolCallMetaFromInput(name, toolID string, input map[string]any) ToolCallMeta {
	return ToolCallMeta{
		ToolID:      toolID,
		Summary:     ExtractSummary(name, input),
		DisplayName: ExtractDisplayName(name, input),
		FilePath:    ExtractFilePath(name, input),
	}
}

// ExtractSummary generates a human-readable summary for a tool call,
// mirroring the frontend toolCallSummary() priority chain:
// description > file_path > command > pattern > query > url > skill >
// prompt (agent only) > path > src_path+dst_path > first string value
func ExtractSummary(name string, input map[string]any) string {
	if input == nil {
		return ""
	}

	nameLower := strings.ToLower(name)

	// AskUserQuestion special case
	if nameLower == "askuserquestion" {
		return extractAskUserQuestionSummary(input)
	}

	// Agent / wait special cases before the generic priority chain.
	if nameLower == "agent" {
		if s := extractAgentSummary(input); s != "" {
			return s
		}
	}
	if nameLower == "wait" && len(input) > 0 {
		if _, hasStates := input["agentsStates"]; hasStates {
			return extractWaitSummary(input)
		}
	}

	// Priority chain — check fields in order
	for _, field := range summaryPriorityFields {
		if v, _ := input[field.key].(string); v != "" {
			return field.format(v)
		}
	}

	// src_path + dst_path pair
	if src, srcOk := input["src_path"].(string); srcOk {
		if dst, dstOk := input["dst_path"].(string); dstOk {
			return truncateStr(baseName(src) + " → " + baseName(dst))
		}
	}

	// Fallback: first string value
	for _, v := range input {
		if s, ok := v.(string); ok {
			return truncateStr(s)
		}
	}

	return ""
}

// extractAgentSummary summarizes an Agent/Agent-like tool call. Priority order
// preserves the pre-existing UX:
//   - description (claude Task / Agent delegation shows the short description)
//   - prompt (sub-agent delegation instructions when no description)
//   - Codex sub-agent lifecycle frames (Start/Complete/Interact/Interrupt
//     subagent) carry {activityKind, agentPath, agentThreadId} — summarize
//     deterministically as "<activity> <agent basename>" (e.g.
//     "started codebase_research") instead of the random map-iteration fallback.
func extractAgentSummary(input map[string]any) string {
	if v, _ := input["description"].(string); v != "" {
		return truncateStr(v)
	}
	if v, _ := input["prompt"].(string); v != "" {
		return truncateStr(v)
	}
	if activity, _ := input["activityKind"].(string); activity != "" {
		name := ""
		if p, _ := input["agentPath"].(string); p != "" {
			name = baseName(p)
		}
		return truncateStr(activity + " " + name)
	}
	return ""
}

// extractWaitSummary summarizes a Codex collaboration wait (title "wait"):
// leave empty (deterministic) unless agentsStates carries child statuses.
func extractWaitSummary(input map[string]any) string {
	if states, ok := input["agentsStates"].(map[string]any); ok && len(states) > 0 {
		return "waiting for subagent"
	}
	return ""
}

// summaryField defines a priority-ordered field for summary extraction.
type summaryField struct {
	key    string
	format func(string) string
}

// summaryPriorityFields lists fields checked in order for ExtractSummary.
var summaryPriorityFields = []summaryField{
	{key: "description", format: truncateStr},
	{key: "file_path", format: func(v string) string { return truncateStr(baseName(v)) }},
	{key: "command", format: truncateStr},
	{key: "pattern", format: truncateStr},
	{key: "query", format: truncateStr},
	{key: "url", format: truncateStr},
	{key: "skill", format: truncateStr},
	{key: "path", format: func(v string) string { return truncateStr(baseName(v)) }},
}

// extractAskUserQuestionSummary extracts summary from AskUserQuestion input.
func extractAskUserQuestionSummary(input map[string]any) string {
	questions, ok := input["questions"]
	if !ok {
		return ""
	}
	qSlice, ok := questions.([]any)
	if !ok || len(qSlice) == 0 {
		return ""
	}
	first, ok := qSlice[0].(map[string]any)
	if !ok {
		return ""
	}
	if header, _ := first["header"].(string); header != "" {
		return truncateStr(header)
	}
	if question, _ := first["question"].(string); question != "" {
		return truncateStr(question)
	}
	return ""
}

// ExtractDisplayName extracts the display name for a tool call.
// For Agent and DeepThink tools, returns the subagent_type (e.g., "Explore").
// For Codex sub-agent lifecycle frames (activityKind present), returns the agent
// basename (e.g. "codebase_research") so the Agent pill shows the child name.
func ExtractDisplayName(name string, input map[string]any) string {
	if input == nil {
		return ""
	}
	nameLower := strings.ToLower(name)
	if nameLower == "agent" || nameLower == "deepthink" {
		if v, _ := input["subagent_type"].(string); v != "" {
			return v
		}
	}
	if nameLower == "agent" {
		if v, _ := input["activityKind"].(string); v != "" {
			if p, _ := input["agentPath"].(string); p != "" {
				return baseName(p)
			}
			return v
		}
	}
	return ""
}

// ExtractFilePath extracts the file path from a tool call input.
// Checks file_path first, then path as fallback.
func ExtractFilePath(name string, input map[string]any) string {
	if input == nil {
		return ""
	}
	if v, _ := input["file_path"].(string); v != "" {
		return v
	}
	if v, _ := input["path"].(string); v != "" {
		return v
	}
	return ""
}

// baseName returns the last segment of a path (filename or directory name).
// Mirrors the frontend baseName() function from utils/path.ts.
func baseName(p string) string {
	// Handle both / and \ separators
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimRight(p, "/")
	if p == "" {
		return ""
	}
	idx := strings.LastIndex(p, "/")
	if idx < 0 {
		return p
	}
	return p[idx+1:]
}

// truncateStr truncates a string to maxSummaryLen runes and appends "..." if truncated.
func truncateStr(s string) string {
	if utf8.RuneCountInString(s) <= maxSummaryLen {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxSummaryLen])
}
