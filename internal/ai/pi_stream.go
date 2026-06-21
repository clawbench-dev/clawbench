package ai

import (
	"encoding/json"
	"log/slog"
)

// PiStreamMessage represents a single JSON line from `pi --mode json`.
// Fields are shared across event types — only relevant fields are populated per type.
type PiStreamMessage struct {
	Type string `json:"type"` // "session", "message_update", "message_end", "tool_execution_start", "tool_execution_end", "agent_end", etc.

	// session event
	ID string `json:"id,omitempty"`

	// message_update event
	AssistantMessageEvent *PiAssistantMessageEvent `json:"assistantMessageEvent,omitempty"`
	Message               *PiMessage               `json:"message,omitempty"`

	// tool_execution_start / tool_execution_end
	ToolCallID string          `json:"toolCallId,omitempty"`
	ToolName   string          `json:"toolName,omitempty"`
	Args       json.RawMessage `json:"args,omitempty"`
	Result     *PiToolResult   `json:"result,omitempty"`
	IsError    bool            `json:"isError,omitempty"`
}

// PiAssistantMessageEvent represents the assistantMessageEvent field
// in a message_update event from Pi.
type PiAssistantMessageEvent struct {
	Type         string         `json:"type"`         // "thinking_start", "thinking_delta", "thinking_end", "text_start", "text_delta", "text_end", "toolcall_start", "toolcall_delta", "toolcall_end"
	ContentIndex int            `json:"contentIndex"` // index into the message content array
	Delta        string         `json:"delta"`        // incremental content for thinking_delta / text_delta / toolcall_delta
	ToolCall     *PiToolCallEnd `json:"toolCall"`     // populated on toolcall_end
}

// PiMessage represents the message field in Pi stream events.
type PiMessage struct {
	Role         string          `json:"role"`              // "assistant"
	Content      json.RawMessage `json:"content,omitempty"` // array of content items (for toolcall_start/delta)
	Usage        *PiUsage        `json:"usage,omitempty"`
	StopReason   string          `json:"stopReason,omitempty"`
	ErrorMessage string          `json:"errorMessage,omitempty"`
	ResponseID   string          `json:"responseId,omitempty"`
}

// PiUsage represents token usage and cost information.
type PiUsage struct {
	Input       int     `json:"input"`
	Output      int     `json:"output"`
	CacheRead   int     `json:"cacheRead"`
	CacheWrite  int     `json:"cacheWrite"`
	TotalTokens int     `json:"totalTokens"`
	Cost        *PiCost `json:"cost"`
}

// PiCost represents cost breakdown.
type PiCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
	Total      float64 `json:"total"`
}

// PiToolCallEnd represents the toolCall field in a toolcall_end event.
type PiToolCallEnd struct {
	Type      string          `json:"type"`      // "toolCall"
	ID        string          `json:"id"`        // tool call ID
	Name      string          `json:"name"`      // tool name (e.g., "read", "edit", "bash")
	Arguments json.RawMessage `json:"arguments"` // complete tool arguments
}

// PiToolResult represents the result field in tool_execution_end.
type PiToolResult struct {
	Content []PiToolResultContent `json:"content"`
}

// PiToolResultContent represents a single content item in a tool result.
type PiToolResultContent struct {
	Type string `json:"type"` // "text"
	Text string `json:"text"` // output text
}

// PiStreamParser parses JSON Lines output from `pi --mode json`.
type PiStreamParser struct {
	sessionID string

	// InputRemaps maps input field names for tool input normalization.
	// When set, parsePiToolCallEnd uses this as the base remap table.
	InputRemaps map[string]string
}

// GetCapturedSessionID returns the session ID captured from session events.
// This is used for the --resume flow in subsequent requests.
func (p *PiStreamParser) GetCapturedSessionID() string {
	return p.sessionID
}

// ParseLine parses a single JSON line from Pi's --mode json output and sends
// StreamEvent(s) to the provided channel.
func (p *PiStreamParser) ParseLine(line string, ch chan<- StreamEvent) {
	var msg PiStreamMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		slog.Debug("pi stream: skipping unparseable line", "line", line, "error", err)
		return
	}

	switch msg.Type {
	case "session":
		if msg.ID != "" {
			p.sessionID = msg.ID
			slog.Debug("pi stream: captured session ID", "session_id", msg.ID)
			// Don't emit session_capture here — CLIBackend.ExecuteStream()
			// handles emission via GetCapturedSessionID() after each ParseLine call.
		}

	case "message_update":
		p.parseMessageUpdate(&msg, ch)

	case "message_end":
		p.parseMessageEnd(&msg, ch)

	case "tool_execution_start":
		// No event — tool_use already emitted from toolcall_end

	case "tool_execution_end":
		if tc := parsePiToolExecutionEnd(&msg); tc != nil {
			ch <- StreamEvent{Type: "tool_result", Tool: tc}
		}

	case "agent_end":
		ch <- StreamEvent{Type: "done"}

	default:
		slog.Debug("pi stream: skipping unknown message type", "type", msg.Type)
	}
}

// parseMessageUpdate handles message_update events with assistantMessageEvent subtypes.
func (p *PiStreamParser) parseMessageUpdate(msg *PiStreamMessage, ch chan<- StreamEvent) {
	if msg.AssistantMessageEvent == nil {
		return
	}

	evt := msg.AssistantMessageEvent

	switch evt.Type {
	case "thinking_delta":
		if evt.Delta != "" {
			ch <- StreamEvent{Type: "thinking", Content: evt.Delta}
		}

	case "text_delta":
		if evt.Delta != "" {
			ch <- StreamEvent{Type: "content", Content: evt.Delta}
		}

	case "toolcall_start", "toolcall_delta":
		// No event emitted — toolcall_end provides the complete arguments.
		// Pi always provides full arguments in toolcall_end, so tracking
		// partial state during start/delta is unnecessary.

	case "toolcall_end":
		if tc := parsePiToolCallEnd(evt, p.InputRemaps); tc != nil {
			ch <- StreamEvent{Type: "tool_use", Tool: tc}
		}

	case "thinking_start", "thinking_end", "text_start", "text_end":
		// No additional event needed — deltas already streamed
	}
}

// parseMessageEnd handles message_end events — emits metadata and/or error.
func (p *PiStreamParser) parseMessageEnd(msg *PiStreamMessage, ch chan<- StreamEvent) {
	if msg.Message == nil {
		return
	}

	m := msg.Message

	// Emit metadata if usage info is available
	if m.Usage != nil {
		costUSD := 0.0
		if m.Usage.Cost != nil {
			costUSD = m.Usage.Cost.Total
		}
		ch <- StreamEvent{Type: "metadata", Meta: &Metadata{
			InputTokens:  m.Usage.Input,
			OutputTokens: m.Usage.Output,
			CostUSD:      costUSD,
			StopReason:   m.StopReason,
		}}
	}

	// Emit error if stopReason is "error"
	if m.StopReason == "error" {
		errMsg := m.ErrorMessage
		if errMsg == "" {
			errMsg = "unknown error"
		}
		ch <- StreamEvent{Type: "error", Error: errMsg}
	}
}
