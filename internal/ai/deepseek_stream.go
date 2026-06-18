package ai

import (
	"encoding/json"
	"log/slog"
)

// DeepSeekStreamMessage represents a single JSON line from
// `codewhale exec --output-format stream-json`.
// Fields are shared across event types — only relevant fields are populated per type.
type DeepSeekStreamMessage struct {
	Type    string `json:"type"`    // "content", "thinking", "tool_use", "tool_result", "metadata", "session_capture", "done", "error"
	Content string `json:"content"` // for content/thinking/session_capture events

	// tool_use event fields
	Name  string          `json:"name"`  // tool name
	ID    string          `json:"id"`    // tool call ID
	Input json.RawMessage `json:"input"` // tool input parameters
	Done  bool            `json:"done"`  // whether input accumulation is complete

	// tool_result event fields
	Output string `json:"output"` // tool output text
	Status string `json:"status"` // "success" | "error"

	// metadata event fields
	Meta *DeepSeekStreamMeta `json:"meta"`

	// error event fields
	Error string `json:"error"` // error message
}

// DeepSeekStreamMeta represents the meta field in a metadata event from CodeWhale.
type DeepSeekStreamMeta struct {
	Model        string `json:"model"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	SessionID    string `json:"session_id"`
}

// DeepSeekStreamParser parses JSON Lines output from
// `codewhale exec --output-format stream-json`.
type DeepSeekStreamParser struct {
	sessionID string // captured from session_capture event
	model     string // captured from metadata event

	// InputRemaps maps input field names for tool input normalization.
	// When set, normalizeDeepSeekInput uses this as the base remap table.
	InputRemaps map[string]string
}

// GetCapturedSessionID returns the session ID captured from session_capture events.
// This is used for the --resume flow in subsequent requests.
func (p *DeepSeekStreamParser) GetCapturedSessionID() string {
	return p.sessionID
}

// ParseLine parses a single JSON line from CodeWhale's stream-json output and sends
// StreamEvent(s) to the provided channel.
//
//nolint:gocyclo // complex stream parsing logic
func (p *DeepSeekStreamParser) ParseLine(line string, ch chan<- StreamEvent) {
	var msg DeepSeekStreamMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		slog.Debug("deepseek stream: skipping unparseable line", "line", line, "error", err)
		return
	}

	switch msg.Type {
	case "content":
		if msg.Content != "" {
			ch <- StreamEvent{Type: "content", Content: msg.Content}
		}

	case "thinking":
		if msg.Content != "" {
			ch <- StreamEvent{Type: "thinking", Content: msg.Content}
		}

	case "tool_use":
		ch <- StreamEvent{Type: "tool_use", Tool: &ToolCall{
			Name:  normalizeToolName(msg.Name),
			ID:    msg.ID,
			Input: normalizeDeepSeekInput(msg.Name, msg.Input, p.InputRemaps),
			Done:  msg.Done,
		}}

	case "tool_result":
		if msg.ID != "" {
			ch <- StreamEvent{Type: "tool_result", Tool: &ToolCall{
				ID:     msg.ID,
				Output: truncateToolOutput(msg.Output),
				Status: msg.Status,
			}}
		}

	case "session_capture":
		if msg.Content != "" {
			p.sessionID = msg.Content
			slog.Debug("deepseek stream: captured session ID", "session_id", msg.Content)
			ch <- StreamEvent{Type: "session_capture", Content: msg.Content}
		}

	case "metadata":
		if msg.Meta != nil {
			p.model = msg.Meta.Model
			ch <- StreamEvent{Type: "metadata", Meta: &Metadata{
				Model:        msg.Meta.Model,
				InputTokens:  msg.Meta.InputTokens,
				OutputTokens: msg.Meta.OutputTokens,
				SessionID:    msg.Meta.SessionID,
			}}
		}

	case "done":
		ch <- StreamEvent{Type: "done"}

	case "error":
		if msg.Error != "" {
			ch <- StreamEvent{Type: "error", Error: msg.Error}
		}

	default:
		slog.Debug("deepseek stream: skipping unknown message type", "type", msg.Type)
	}
}

// normalizeDeepSeekInput normalizes tool input field names from CodeWhale's
// native names to the canonical names expected by the frontend renderers.
//
// CodeWhale uses concise snake_case names that differ from the canonical
// Claude-style names: path→file_path, search→old_string, replace→new_string,
// command→command (no change), content→content (no change).
func normalizeDeepSeekInput(toolName string, rawInput json.RawMessage, baseRemaps map[string]string) string {
	// Start with base remaps from the sub-package (injected at parser construction)
	remaps := map[string]string{
		"filePaths": "file_paths", // camelCase fallback
		"oldString": "old_string", // camelCase fallback
		"newString": "new_string", // camelCase fallback
		"dirPath":   "path",       // camelCase fallback
	}
	// Merge base remaps (sub-package overrides take precedence)
	for k, v := range baseRemaps {
		remaps[k] = v
	}

	switch toolName {
	case "edit_file":
		// DeepSeek: {path, search, replace} → canonical: {file_path, old_string, new_string}
		remaps["path"] = "file_path"
		remaps["search"] = "old_string"
		remaps["replace"] = "new_string"
	case "read_file", "write_file", "list_dir":
		// DeepSeek: {path, ...} → canonical: {file_path, ...}
		remaps["path"] = "file_path"
	case "grep_files", "file_search":
		// DeepSeek: {path, ...} → canonical: {path, ...} (grep uses 'path', not 'file_path')
		// No remap needed — 'path' is already canonical for Grep/Glob
	}

	normalized, err := normalizeToolInput(rawInput, remaps)
	if err != nil {
		return string(rawInput)
	}
	return string(normalized)
}
