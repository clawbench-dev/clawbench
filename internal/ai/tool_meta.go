package ai

import (
	"encoding/json"
	"sort"
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
// (deterministic: lexicographically-first key).
func ExtractSummary(name string, input map[string]any) string {
	if input == nil {
		return ""
	}

	nameLower := strings.ToLower(name)

	// AskUserQuestion special case
	if nameLower == "askuserquestion" {
		return extractAskUserQuestionSummary(input)
	}

	// TaskUpdate: CodeBuddy's TaskUpdate input is {status, taskId} — it has no
	// subject/description, so the generic chain below would fall through to the
	// (deterministic, but opaque) sorted-string fallback. Format it explicitly
	// as "#<taskId> · <status>" so the pill shows e.g. "#3 · in_progress".
	// TaskUpdate also accepts optional subject/description overrides, which
	// must keep priority over the derived form.
	if nameLower == "taskupdate" {
		if s := extractTaskUpdateSummary(input); s != "" {
			return s
		}
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

	// Fallback: first string value. Go map iteration order is random, so sort
	// the keys first to make the result deterministic (and to mirror the
	// frontend's Object.keys ordering, which is always lexicographic for
	// string keys). Previously the same input could summarize to different
	// strings on consecutive runs (e.g. TaskUpdate's {status, taskId}).
	keys := make([]string, 0, len(input))
	for k := range input {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if s, ok := input[k].(string); ok {
			return truncateStr(s)
		}
	}

	return ""
}

// extractTaskUpdateSummary summarizes a CodeBuddy TaskUpdate call. The usual
// input {status, taskId} has no descriptive field, so derive a deterministic
// "#<taskId> · <status>" label. If the model passed an explicit subject or
// description override it wins; bare taskId is only shown as a fallback when
// status is missing. Returns "" when neither subject/description nor taskId is
// present (defer to the generic chain / empty).
func extractTaskUpdateSummary(input map[string]any) string {
	if v, _ := input["subject"].(string); v != "" {
		return truncateStr(v)
	}
	if v, _ := input["description"].(string); v != "" {
		return truncateStr(v)
	}
	id, _ := input["taskId"].(string)
	if id == "" {
		return ""
	}
	status, _ := input["status"].(string)
	if status == "" {
		return "#" + truncateStr(id)
	}
	return "#" + truncateStr(id) + " · " + status
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

// fileTools is the set of canonical tool names whose primary target is a
// single file path. Only these tools have a detected FilePath promoted to
// ToolCallMeta/block metadata; other tools may carry coincidental path-like
// input fields (e.g. a Bash command string containing "filename=…", or a
// Glob/LS "path" that is a directory or pattern) that must not be treated as
// the file identity of the call. Consumers of FilePath (file-modification
// detection, preview refresh) only act on Write/Edit; Read is kept so file
// reads still surface the path they operated on.
var fileTools = map[string]bool{
	"Write": true,
	"Edit":  true,
	"Read":  true,
}

// isFileTool reports whether the canonical tool name operates on a file path.
func isFileTool(name string) bool {
	return fileTools[name]
}

// ExtractFilePath extracts the file path from a tool call input.
// Only file tools (Write/Edit/Read) are considered; other tools return "" so
// coincidental path-like input fields are not promoted to a file identity.
// Priority order for a direct string path field:
//
//	file_path > new_file_path > old_file_path > path > filename > file_name
//
// followed by camelCase variants of the same (filePath, newFilePath, …) for
// inputs that bypassed normalizeToolInput. When no direct string field exists,
// it digs into container fields that some backends use instead of a flat
// path key:
//
//   - file_paths / filePaths (array) — first element (used for batch tools)
//   - locations (array of {path|file_path}) — first element's path (ACP read/edit)
//   - location (single {path|file_path}) — nested object path
//
// Container/array forms only yield a path when the element itself looks like a
// file reference, so a command string never accidentally becomes a "path".
func ExtractFilePath(name string, input map[string]any) string {
	if input == nil || !isFileTool(name) {
		return ""
	}

	// 1. Flat string path fields — priority order, snake_case first then camelCase.
	for _, key := range []string{
		"file_path", "new_file_path", "old_file_path", "path", "filename", "file_name",
		"filePath", "newFilePath", "oldFilePath", "fileName",
	} {
		if v, _ := input[key].(string); v != "" {
			return v
		}
	}

	// 2. Array container fields — take the first element that carries a path.
	for _, key := range []string{"file_paths", "filePaths"} {
		if arr, ok := input[key].([]any); ok {
			if p := firstPathFromArray(arr); p != "" {
				return p
			}
		}
	}

	// 3. ACP-style location containers (read/edit tools report the target file).
	if locs, ok := input["locations"].([]any); ok {
		if p := firstPathFromLocations(locs); p != "" {
			return p
		}
	}
	if loc, ok := input["location"].(map[string]any); ok {
		if p := pathFromLocationMap(loc); p != "" {
			return p
		}
	}

	return ""
}

// firstPathFromArray returns the first non-empty path string in a []any that
// contains either plain path strings or {"path"/"file_path": …} objects.
func firstPathFromArray(arr []any) string {
	for _, item := range arr {
		switch v := item.(type) {
		case string:
			if v != "" {
				return v
			}
		case map[string]any:
			if p := pathFromLocationMap(v); p != "" {
				return p
			}
		}
	}
	return ""
}

// firstPathFromLocations returns the first non-empty path from an ACP-style
// locations array (elements may be {"path": …} or {"file_path": …}).
func firstPathFromLocations(locs []any) string {
	for _, item := range locs {
		if m, ok := item.(map[string]any); ok {
			if p := pathFromLocationMap(m); p != "" {
				return p
			}
		}
	}
	return ""
}

// pathFromLocationMap pulls the path key out of a single location-style map.
func pathFromLocationMap(m map[string]any) string {
	for _, key := range []string{"file_path", "path"} {
		if v, _ := m[key].(string); v != "" {
			return v
		}
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
