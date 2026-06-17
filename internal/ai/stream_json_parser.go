package ai

import (
	"encoding/json"
	"log/slog"
)

// StreamJSONMessage represents a single JSON line from stream-json format (Kimi CLI).
// Fields are shared across event types — only relevant fields are populated per type.
type StreamJSONMessage struct {
	Type      string `json:"type"`       // "init", "message", "tool_use", "tool_result", "error", "result"
	Timestamp string `json:"timestamp"`  // ISO 8601
	SessionID string `json:"session_id"` // from init event
	Model     string `json:"model"`      // from init event

	// message event fields
	Role    string `json:"role"`    // "user" | "assistant"
	Content string `json:"content"` // text content
	Delta   bool   `json:"delta"`   // true for incremental assistant messages

	// tool_use / tool_result shared field
	ToolID string `json:"tool_id"` // tool call ID

	// tool_use event fields
	ToolName   string          `json:"tool_name"`  // tool name
	Parameters json.RawMessage `json:"parameters"` // tool input parameters

	// tool_result / result shared field
	Status string `json:"status"` // "success" | "error"

	// tool_result event fields
	ToolOutput string `json:"output"` // display output

	// error event fields (also used by tool_result for error details)
	Severity string `json:"severity"` // "warning" | "error"
	Message  string `json:"message"`  // error message

	// result event fields
	Error *ResultError `json:"error"` // only when status="error"
	Stats *StreamStats `json:"stats"`
}

// ResultError represents the error field in a result event
type ResultError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// StreamStats represents the stats field in a result event
type StreamStats struct {
	TotalTokens  int                    `json:"total_tokens"`
	InputTokens  int                    `json:"input_tokens"`
	OutputTokens int                    `json:"output_tokens"`
	Cached       int                    `json:"cached"`
	Input        int                    `json:"input"`
	DurationMs   int                    `json:"duration_ms"`
	ToolCalls    int                    `json:"tool_calls"`
	Models       map[string]ModelTokens `json:"models"`
}

// ModelTokens represents per-model token usage
type ModelTokens struct {
	TotalTokens  int `json:"total_tokens"`
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	Cached       int `json:"cached"`
	Input        int `json:"input"`
}

// StreamJSONParser parses JSON Lines output from stream-json format (Kimi CLI).
type StreamJSONParser struct {
	sessionID string // captured from init event
	model     string // captured from init event

	// ToolNameMap maps backend-specific tool names to canonical names.
	// When set, ParseLine uses this map instead of the global normalizeToolName().
	// key: backend raw tool name → value: canonical name (e.g. "read_file" → "Read")
	ToolNameMap map[string]string

	// InputRemaps maps input field names for tool input normalization.
	// Injected at parser construction time by the backend sub-package.
	InputRemaps map[string]string
}

// GetCapturedSessionID implements LineParser — returns empty string
// (session ID is captured internally for metadata but not exposed for external resume).
func (p *StreamJSONParser) GetCapturedSessionID() string { return "" }

// ParseLine parses a single JSON line from stream-json output and sends
// StreamEvent(s) to the provided channel.
//
//nolint:gocognit,gocyclo // complex stream parsing logic
func (p *StreamJSONParser) ParseLine(line string, ch chan<- StreamEvent) {
	var msg StreamJSONMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		slog.Debug("stream-json parser: skipping unparseable line", "line", line, "error", err)
		return
	}

	switch msg.Type {
	case "init":
		// Capture session ID and model from init event
		if msg.SessionID != "" {
			p.sessionID = msg.SessionID
		}
		if msg.Model != "" {
			p.model = msg.Model
		}

	case "message":
		if msg.Role == "assistant" && msg.Content != "" {
			ch <- StreamEvent{Type: "content", Content: msg.Content}
		}
		// Skip user messages — they echo back the input prompt

	case "tool_use":
		inputStr := "{}"
		if len(msg.Parameters) > 0 {
			// Normalize input field names to canonical snake_case
			normalized, err := normalizeToolInput(msg.Parameters, p.InputRemaps)
			if err != nil {
				inputStr = string(msg.Parameters)
			} else {
				inputStr = string(normalized)
			}
		}
		toolName := msg.ToolName
		if p.ToolNameMap != nil {
			if canonical, ok := p.ToolNameMap[toolName]; ok {
				toolName = canonical
			} else {
				toolName = normalizeToolName(toolName)
			}
		} else {
			toolName = normalizeToolName(toolName)
		}
		ch <- StreamEvent{Type: "tool_use", Tool: &ToolCall{
			Name:  toolName,
			ID:    msg.ToolID,
			Input: inputStr,
			Done:  true, // stream-json format sends full tool input in one event
		}}

	case "tool_result":
		// Emit tool_result event so the frontend can display tool output
		if msg.ToolID != "" {
			ch <- StreamEvent{Type: "tool_result", Tool: &ToolCall{
				ID:     msg.ToolID,
				Output: truncateToolOutput(msg.ToolOutput),
				Status: msg.Status, // "success" or "error"
			}}
		}

	case "error":
		// Emit as warning for severity="warning", error for "error"
		if msg.Message != "" {
			if msg.Severity == "error" {
				ch <- StreamEvent{Type: "error", Error: msg.Message}
			} else {
				ch <- StreamEvent{Type: "warning", Content: msg.Message}
			}
		}

	case "result":
		meta := &Metadata{
			SessionID: p.sessionID,
			Model:     p.model,
		}
		if msg.Stats != nil {
			meta.InputTokens = msg.Stats.InputTokens
			meta.OutputTokens = msg.Stats.OutputTokens
			meta.DurationMs = msg.Stats.DurationMs
		}
		if msg.Status == "error" {
			meta.IsError = true
			if msg.Error != nil {
				meta.ErrorMessage = msg.Error.Message
			}
			if meta.ErrorMessage != "" {
				ch <- StreamEvent{Type: "warning", Content: meta.ErrorMessage}
			}
		} else {
			meta.StopReason = "stop"
		}
		ch <- StreamEvent{Type: "metadata", Meta: meta}
		ch <- StreamEvent{Type: "done"}

	default:
		slog.Debug("stream-json parser: skipping unknown message type", "type", msg.Type)
	}
}
