package ai

import (
	"encoding/json"
	"log/slog"
	"strings"
)

// LineParser is the interface for parsing individual JSON lines from CLI output.
type LineParser interface {
	ParseLine(line string, ch chan<- StreamEvent)
	// GetCapturedSessionID returns the externally-identifiable session ID
	// captured from parsed lines (e.g., OpenCode ses_xxx, Codex thread_xxx).
	// Returns empty string if not yet captured or not applicable.
	GetCapturedSessionID() string
}

// ClaudeContentBlock represents a content block within a Claude stream message
type ClaudeContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`     // tool_result blocks: output content (string or array of text blocks)
	ToolUseID string          `json:"tool_use_id,omitempty"` // tool_result blocks: references the tool_use ID this result belongs to
	IsError   bool            `json:"is_error,omitempty"`    // tool_result blocks: whether the tool execution failed
}

// ClaudeStreamMessageBody represents the message body within a Claude stream message
type ClaudeStreamMessageBody struct {
	Content []ClaudeContentBlock `json:"content"`
}

// ClaudeStreamUsage represents token usage in a stream message
type ClaudeStreamUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// ClaudeStreamModelUsage represents per-model token usage
type ClaudeStreamModelUsage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
}

// ClaudeStreamMessage represents a single message in the streaming JSON output
// from both Claude and Codebuddy CLIs (stream-json format).
type ClaudeStreamMessage struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Text    string `json:"text"`

	// Nested message body for assistant messages (Claude verbose format)
	Message *ClaudeStreamMessageBody `json:"message,omitempty"`

	// Fields for result messages
	IsError      bool    `json:"is_error"`
	Result       string  `json:"result"`
	SessionID    string  `json:"session_id"`
	DurationMs   int     `json:"duration_ms"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	StopReason   string  `json:"stop_reason"`

	// Qoder-specific: error_during_execution errors list
	Errors []string `json:"errors,omitempty"`

	// Usage fields (pointer so it can be nil)
	Usage *ClaudeStreamUsage `json:"usage,omitempty"`

	// Per-model usage (Claude-specific)
	ModelUsage map[string]ClaudeStreamModelUsage `json:"modelUsage,omitempty"`

	// Codebuddy-specific: providerData for model info
	ProviderData *struct {
		Model string `json:"model,omitempty"`
		Usage *struct {
			InputTokens  int `json:"inputTokens"`
			OutputTokens int `json:"outputTokens"`
		} `json:"usage,omitempty"`
	} `json:"providerData,omitempty"`

	// stream_event fields (codebuddy --include-partial-messages)
	Event *StreamEventData `json:"event,omitempty"`
}

// StreamEventData represents the event field in a stream_event message
type StreamEventData struct {
	Type         string              `json:"type"`
	Index        int                 `json:"index,omitempty"`
	ContentBlock *StreamContentBlock `json:"content_block,omitempty"`
	Delta        *StreamDelta        `json:"delta,omitempty"`
	Message      *StreamMessageStart `json:"message,omitempty"`
}

// StreamContentBlock represents a content_block_start/stop payload
type StreamContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	Name      string          `json:"name,omitempty"`
	ID        string          `json:"id,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`       // tool_use input (some CLIs include it in content_block_start)
	ToolUseID string          `json:"tool_use_id,omitempty"` // tool_result blocks: references the tool_use ID this result belongs to
	Content   string          `json:"content,omitempty"`     // tool_result blocks: output content (non-streaming format)
	IsError   bool            `json:"is_error,omitempty"`    // tool_result blocks: whether the tool execution failed
}

// StreamDelta represents a content_block_delta payload
type StreamDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"` // input_json_delta uses this field (not "text")
}

// StreamMessageStart represents the message field in a message_start event
type StreamMessageStart struct {
	Model string `json:"model"`
}

const (
	scannerInitial = 64 * 1024   // 64KB initial scanner buffer
	scannerMax     = 1024 * 1024 // 1MB max scanner buffer
	streamChanSize = 512         // channel buffer size
)

// extractContentText extracts text from a Content field that may be a plain
// string or an array of content blocks (e.g., [{"type":"text","text":"..."}]).
// This handles the Claude/Codebuddy API convention where tool_result content
// can be either format depending on the message type and CLI version.
func extractContentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Try string format first
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	// Try array of content blocks format
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var sb strings.Builder
		for i, b := range blocks {
			if b.Type == "text" {
				if i > 0 {
					sb.WriteString("\n")
				}
				sb.WriteString(b.Text)
			}
		}
		return sb.String()
	}
	// Fallback: return raw as string
	return string(raw)
}

// toolResultAccum tracks an in-progress tool_result content block.
// When Claude/Codebuddy CLI emits tool_result blocks via stream_event,
// text_delta events within those blocks must be accumulated here
// (not emitted as content) and finalized on content_block_stop.
type toolResultAccum struct {
	ToolUseID string // the tool_use ID this result belongs to
	Output    strings.Builder
	IsError   bool
}

// StreamParser tracks state across stream lines to avoid duplicate content.
// It implements the LineParser interface and is used by both Claude and Codebuddy backends.
type StreamParser struct {
	// receivedPartial tracks whether we've seen stream_event content_block_delta,
	// so we can skip the full assistant message that follows
	receivedPartial bool
	// receivedPartialThinking tracks whether we've seen thinking_delta events,
	// so we can skip thinking blocks in the full assistant message
	receivedPartialThinking bool
	// receivedPartialToolUse tracks whether we've seen stream_event tool_use
	// events (content_block_start), so we skip the tool_use block in the
	// complete assistant message to avoid duplication
	receivedPartialToolUse bool
	// model stores the model name extracted from message_start events
	model string
	// activeTools tracks in-progress tool calls keyed by content block index.
	// The CLI stream events (content_block_start/delta/stop) all carry an index
	// field identifying which content block they refer to. When AI models invoke
	// multiple tools via parallel sub-agents, the CLI may interleave
	// content_block_start/delta/stop events for different tools. Using a map
	// keyed by index (instead of a single currentTool pointer) ensures that
	// input_json_delta events are accumulated into the correct tool and
	// content_block_stop closes the correct tool, even when events arrive
	// out of the expected sequential order.
	activeTools map[int]*ToolCall
	// activeToolResults tracks in-progress tool_result content blocks keyed
	// by content block index. When tool_result content_block_start is received,
	// subsequent text_delta events for that index are accumulated into the
	// tool result output instead of being emitted as content events.
	activeToolResults map[int]*toolResultAccum
	// activeThinkingBlocks tracks content block indices that are thinking blocks.
	// When content_block_stop arrives for a thinking index, we emit thinking_done
	// so the frontend can stop the thinking spinner immediately.
	activeThinkingBlocks map[int]bool
	// emittedToolInputEmpty tracks tool_use IDs that were emitted via stream_event
	// but had empty Input (partial_json was empty). When the assistant verbose
	// message arrives with the full Input, we re-emit a tool_use event so that
	// AccumulateBlock can update the block with the correct input data.
	emittedToolInputEmpty map[string]bool
}

// GetCapturedSessionID returns empty string for Claude/Codebuddy/Kimi backends
// which use ClawBench UUIDs natively and don't need external session ID mapping.
func (p *StreamParser) GetCapturedSessionID() string { return "" }

// toolState returns a ClaudeStreamToolState populated from the parser's fields.
func (p *StreamParser) toolState() ClaudeStreamToolState {
	return ClaudeStreamToolState{
		ActiveTools:             p.activeTools,
		ActiveToolResults:       p.activeToolResults,
		ActiveThinkingBlocks:    p.activeThinkingBlocks,
		EmittedToolInputEmpty:   p.emittedToolInputEmpty,
		ReceivedPartialToolUse:  p.receivedPartialToolUse,
		ReceivedPartial:         p.receivedPartial,
		ReceivedPartialThinking: p.receivedPartialThinking,
	}
}

// syncToolState writes back tool state fields from the struct to the parser.
func (p *StreamParser) syncToolState(state ClaudeStreamToolState) {
	p.activeTools = state.ActiveTools
	p.activeToolResults = state.ActiveToolResults
	p.activeThinkingBlocks = state.ActiveThinkingBlocks
	p.emittedToolInputEmpty = state.EmittedToolInputEmpty
	p.receivedPartialToolUse = state.ReceivedPartialToolUse
	p.receivedPartial = state.ReceivedPartial
	p.receivedPartialThinking = state.ReceivedPartialThinking
}

// ParseLine parses a single JSON line from stream-json output and sends
// StreamEvent(s) to the provided channel. This is the shared parser used by
// both Claude and Codebuddy streaming implementations.
//
//nolint:gocognit,gocyclo // complex stream parsing logic
func (p *StreamParser) ParseLine(line string, ch chan<- StreamEvent) {
	var msg ClaudeStreamMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		slog.Debug("stream: skipping unparseable line", "line", line, "error", err)
		return
	}

	switch msg.Type {
	case "assistant":
		state := p.toolState()
		events := parseClaudeAssistantToolUse(&msg, &state)
		for _, ev := range events {
			ch <- ev
		}
		p.syncToolState(state)
		if msg.Message != nil {
			return
		}
		// If we already received partial content via stream_event, skip text/thinking
		// (tool_use is handled above with receivedPartialToolUse check)
		if p.receivedPartial {
			return
		}
		// Codebuddy format: simple text field
		if msg.Subtype == "text" && msg.Text != "" {
			ch <- StreamEvent{Type: "content", Content: msg.Text}
		}

	case "result":
		meta := &Metadata{
			SessionID:  msg.SessionID,
			DurationMs: msg.DurationMs,
			CostUSD:    msg.TotalCostUSD,
			StopReason: msg.StopReason,
			IsError:    msg.IsError,
		}
		if msg.Usage != nil {
			meta.InputTokens = msg.Usage.InputTokens
			meta.OutputTokens = msg.Usage.OutputTokens
		}
		// Use model from stream_event message_start if available
		if p.model != "" {
			meta.Model = p.model
		}
		// Claude-specific: extract model from ModelUsage
		for name, usage := range msg.ModelUsage {
			if meta.Model == "" {
				meta.Model = name
			}
			if meta.InputTokens == 0 && meta.OutputTokens == 0 {
				meta.InputTokens = usage.InputTokens
				meta.OutputTokens = usage.OutputTokens
			}
			break
		}
		// Codebuddy-specific: extract model from providerData
		if msg.ProviderData != nil {
			if meta.Model == "" {
				meta.Model = msg.ProviderData.Model
			}
			if msg.ProviderData.Usage != nil {
				if meta.InputTokens == 0 && meta.OutputTokens == 0 {
					meta.InputTokens = msg.ProviderData.Usage.InputTokens
					meta.OutputTokens = msg.ProviderData.Usage.OutputTokens
				}
			}
		}

		if msg.IsError {
			// Build error message: prefer Result field, fall back to Errors array (Qoder)
			errMsg := msg.Result
			if errMsg == "" && len(msg.Errors) > 0 {
				errMsg = strings.Join(msg.Errors, "; ")
			}
			meta.ErrorMessage = errMsg
			// Also emit warning event so error shows as warning block in chat message
			if errMsg != "" {
				slog.Warn("stream: CLI returned is_error result", "result", errMsg)
				ch <- StreamEvent{Type: "warning", Content: errMsg}
			}
		}
		slog.Info("stream: emitting done event", "is_error", msg.IsError)
		ch <- StreamEvent{Type: "metadata", Meta: meta}
		ch <- StreamEvent{Type: "done"}

	case "system":
		// System messages (e.g., init, tool use) - skip

	case "user":
		events := parseClaudeUserToolResult(&msg)
		for _, ev := range events {
			ch <- ev
		}

	case "stream_event":
		state := p.toolState()
		parseClaudeStreamToolEvent(&msg, &state, ch)
		p.syncToolState(state)
		// Extract model name from message_start
		if msg.Event != nil && msg.Event.Type == "message_start" {
			if msg.Event.Message != nil && msg.Event.Message.Model != "" {
				p.model = msg.Event.Message.Model
			}
		}

	case "file-history-snapshot":
		// File history snapshot events - skip

	default:
		slog.Debug("stream: skipping unknown message type", "type", msg.Type)
	}
}
