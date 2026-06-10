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

// GetCapturedSessionID returns empty string for Claude/Codebuddy/Gemini backends
// which use ClawBench UUIDs natively and don't need external session ID mapping.
func (p *StreamParser) GetCapturedSessionID() string { return "" }

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
		// Claude verbose format: content blocks in msg.Message.Content
		if msg.Message != nil {
			for _, block := range msg.Message.Content {
				switch block.Type {
				case "tool_use":
					if p.receivedPartialToolUse {
						// We already emitted this tool_use via stream_event.
						// However, some CLIs/models send empty input_json_delta,
						// resulting in tool_use blocks with no Input. If the
						// verbose assistant message contains the full Input,
						// re-emit a tool_use event so AccumulateBlock can update
						// the existing block with the correct input data.
						if p.emittedToolInputEmpty != nil && p.emittedToolInputEmpty[block.ID] {
							inputStr := string(block.Input)
							if inputStr != "" && inputStr != "{}" {
								slog.Debug("stream: supplementing empty tool_use input from assistant message",
									"tool_id", block.ID, "tool_name", block.Name, "input_len", len(inputStr))
								ch <- StreamEvent{Type: "tool_use", Tool: &ToolCall{
									Name:  block.Name,
									ID:    block.ID,
									Input: inputStr,
									Done:  true,
								}}
							}
							delete(p.emittedToolInputEmpty, block.ID)
						}
						continue
					}
					// Emit tool_use event with full input from the complete message
					// Claude/Codebuddy already use canonical tool names and field names
					inputStr := string(block.Input)
					ch <- StreamEvent{Type: "tool_use", Tool: &ToolCall{
						Name:  block.Name,
						ID:    block.ID,
						Input: inputStr,
						Done:  true,
					}}
				case "tool_result":
					// Tool result in assistant verbose format — emit tool_result event
					toolUseID := block.ToolUseID
					if toolUseID == "" {
						toolUseID = block.ID
					}
					status := "success"
					if block.IsError {
						status = "error"
					}
					output := extractContentText(block.Content)
					if output == "" && block.Text != "" {
						output = block.Text
					}
					ch <- StreamEvent{Type: "tool_result", Tool: &ToolCall{
						ID:     toolUseID,
						Output: truncateToolOutput(output),
						Status: status,
					}}
				case "thinking":
					if block.Thinking != "" && !p.receivedPartialThinking {
						ch <- StreamEvent{Type: "thinking", Content: block.Thinking}
					}
				case "text":
					if block.Text != "" && !p.receivedPartial {
						ch <- StreamEvent{Type: "content", Content: block.Text}
					}
				}
			}
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
		// Claude/Codebuddy verbose format: tool_result blocks appear in user messages
		// (tool results are sent back to the model as "user" role). Extract them
		// so that tool_use blocks get their output/status populated.
		if msg.Message != nil {
			for _, block := range msg.Message.Content {
				if block.Type != "tool_result" {
					continue
				}
				toolUseID := block.ToolUseID
				if toolUseID == "" {
					toolUseID = block.ID
				}
				status := "success"
				if block.IsError {
					status = "error"
				}
				output := extractContentText(block.Content)
				if output == "" && block.Text != "" {
					output = block.Text
				}
				slog.Debug("stream: emitting tool_result from user message", "tool_use_id", toolUseID, "output_len", len(output), "status", status)
				ch <- StreamEvent{Type: "tool_result", Tool: &ToolCall{
					ID:     toolUseID,
					Output: truncateToolOutput(output),
					Status: status,
				}}
			}
		}

	case "stream_event":
		// Codebuddy --include-partial-messages: incremental token streaming
		if msg.Event == nil {
			return
		}
		switch msg.Event.Type {
		case "content_block_delta":
			if msg.Event.Delta == nil {
				return
			}
			switch msg.Event.Delta.Type {
			case "text_delta":
				// Check if this text_delta belongs to an active tool_result block.
				// If so, accumulate the text into the tool result output instead of
				// emitting it as a content event — this prevents tool output from
				// leaking into the assistant's text response.
				if p.activeToolResults != nil {
					if accum, ok := p.activeToolResults[msg.Event.Index]; ok {
						if msg.Event.Delta.Text != "" {
							accum.Output.WriteString(msg.Event.Delta.Text)
						}
						return
					}
				}
				if msg.Event.Delta.Text != "" {
					p.receivedPartial = true
					ch <- StreamEvent{Type: "content", Content: msg.Event.Delta.Text}
				}
			case "input_json_delta":
				// Accumulate tool input JSON via partial_json field (canonical format).
				// Use the index field to find the correct tool in activeTools,
				// since parallel sub-agents may interleave events for different tools.
				if tool, ok := p.activeTools[msg.Event.Index]; ok {
					delta := msg.Event.Delta.PartialJSON
					if delta != "" {
						tool.Input += delta
					}
				}
			case "thinking_delta":
				if msg.Event.Delta.Thinking != "" {
					p.receivedPartialThinking = true
					ch <- StreamEvent{Type: "thinking", Content: msg.Event.Delta.Thinking}
				}
			}
		case "content_block_start":
			if msg.Event.ContentBlock != nil {
				switch msg.Event.ContentBlock.Type {
				case "tool_use":
					p.receivedPartialToolUse = true
					tool := &ToolCall{
						Name: msg.Event.ContentBlock.Name,
						ID:   msg.Event.ContentBlock.ID,
					}
					// Capture input from content_block_start if provided.
					// Some CLIs (e.g., Claude CLI with certain models) include the
					// full tool input in the content_block_start event instead of
					// sending separate input_json_delta events.
					// Skip empty input "{}" — it's a placeholder; the real input
					// will arrive via input_json_delta events. Setting it would
					// cause JSON corruption when deltas are appended.
					if len(msg.Event.ContentBlock.Input) > 0 &&
						string(msg.Event.ContentBlock.Input) != "{}" {
						tool.Input = string(msg.Event.ContentBlock.Input)
					}
					// Track by content block index so that interleaved events from
					// parallel sub-agents can be correctly routed to their tool.
					if p.activeTools == nil {
						p.activeTools = make(map[int]*ToolCall)
					}
					// If there's already a tool at this index that hasn't received
					// content_block_stop (e.g., CLI reused the same index for a
					// replacement tool without emitting stop), auto-close it first.
					if existing, ok := p.activeTools[msg.Event.Index]; ok {
						slog.Debug("stream: auto-closing tool at reused index", "index", msg.Event.Index, "tool_id", existing.ID, "tool_name", existing.Name)
						closed := *existing // copy before mutating
						closed.Done = true
						ch <- StreamEvent{Type: "tool_use", Tool: &closed}
					}
					p.activeTools[msg.Event.Index] = tool
					// Send a copy to the channel so that later mutations (Input accumulation,
					// Done=true) don't affect events already queued for SSE consumption.
					startCopy := *tool
					ch <- StreamEvent{Type: "tool_use", Tool: &startCopy}
					// Track tool IDs emitted with empty Input so that when the
					// assistant verbose message arrives, we can supplement the
					// missing input data. This handles CLIs/models where
					// input_json_delta.partial_json is always empty.
					if tool.Input == "" {
						if p.emittedToolInputEmpty == nil {
							p.emittedToolInputEmpty = make(map[string]bool)
						}
						p.emittedToolInputEmpty[tool.ID] = true
					}
				case "tool_result":
					// Track tool_result block to suppress text_delta leak.
					// When a tool_result content_block_start is received, subsequent
					// text_delta events for this index should be accumulated as tool
					// output rather than emitted as content events.
					if p.activeToolResults == nil {
						p.activeToolResults = make(map[int]*toolResultAccum)
					}
					accum := &toolResultAccum{
						IsError: msg.Event.ContentBlock.IsError,
					}
					// The tool_use_id field links this result back to the original tool_use call.
					// Fall back to the ID field if tool_use_id is not present.
					toolUseID := msg.Event.ContentBlock.ToolUseID
					if toolUseID == "" {
						toolUseID = msg.Event.ContentBlock.ID
					}
					accum.ToolUseID = toolUseID
					// Some CLIs may include the full output in content_block_start
					if msg.Event.ContentBlock.Content != "" {
						accum.Output.WriteString(msg.Event.ContentBlock.Content)
					}
					p.activeToolResults[msg.Event.Index] = accum
				case "thinking":
					// Track thinking block index so we can emit thinking_done
					// when content_block_stop arrives for it.
					if p.activeThinkingBlocks == nil {
						p.activeThinkingBlocks = make(map[int]bool)
					}
					p.activeThinkingBlocks[msg.Event.Index] = true
				}
			}
		case "content_block_stop":
			// Check if this is a tool_result block being finalized
			if p.activeToolResults != nil {
				if accum, ok := p.activeToolResults[msg.Event.Index]; ok {
					status := "success"
					if accum.IsError {
						status = "error"
					}
					ch <- StreamEvent{Type: "tool_result", Tool: &ToolCall{
						ID:     accum.ToolUseID,
						Output: truncateToolOutput(accum.Output.String()),
						Status: status,
					}}
					delete(p.activeToolResults, msg.Event.Index)
				}
			}
			// Check if this is a tool_use block being finalized
			if tool, ok := p.activeTools[msg.Event.Index]; ok {
				closed := *tool // copy before mutating
				closed.Done = true
				ch <- StreamEvent{Type: "tool_use", Tool: &closed}
				// Track tool IDs where the final Input is still empty after
				// content_block_stop. The assistant verbose message may
				// contain the full input data.
				if closed.Input == "" {
					if p.emittedToolInputEmpty == nil {
						p.emittedToolInputEmpty = make(map[string]bool)
					}
					p.emittedToolInputEmpty[closed.ID] = true
				}
				delete(p.activeTools, msg.Event.Index)
			} else if p.activeThinkingBlocks != nil && p.activeThinkingBlocks[msg.Event.Index] {
				// Thinking block completed — emit thinking_done so the
				// frontend spinner disappears immediately instead of
				// waiting for the entire response to finish.
				ch <- StreamEvent{Type: "thinking_done"}
				delete(p.activeThinkingBlocks, msg.Event.Index)
			} else {
				slog.Debug("stream: content_block_stop for unknown index", "index", msg.Event.Index)
			}
		case "message_start":
			// Extract model name from message_start
			if msg.Event.Message != nil && msg.Event.Message.Model != "" {
				p.model = msg.Event.Message.Model
			}
			// Reset partial flags for a new assistant turn (ISS-028).
			// Each message_start begins a fresh turn — previous partial
			// content flags must not suppress content from the new turn.
			p.receivedPartial = false
			p.receivedPartialThinking = false
			p.receivedPartialToolUse = false
		case "message_delta", "message_stop":
			// Structural events - no content to emit
		}

	case "file-history-snapshot":
		// File history snapshot events - skip

	default:
		slog.Debug("stream: skipping unknown message type", "type", msg.Type)
	}
}
