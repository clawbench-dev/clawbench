package ai

import (
	"encoding/json"
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

// streamErrorReporter is an OPTIONAL interface a LineParser may implement to
// tell the stream driver that it already emitted the turn's terminal error.
//
// It exists because the driver independently reports an abnormal exit
// ("AI backend exited abnormally") whenever the process exits non-zero. For a
// parser that already classified the failure from the agent's own output — e.g.
// OpenCode's `{"type":"error"}` frame, which carries the provider's 402/quota
// message — that second warning is not just redundant: it is tagged
// ReasonBackendExit, which is RETRYABLE, so auto-continue would re-send a
// request that can only fail again.
//
// Optional rather than a required method: only backends whose CLI reports
// failures in-band need it, and forcing the other seven parsers to grow a
// no-op method would be noise.
type streamErrorReporter interface {
	ReportedStreamError() bool
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
	// Cache counters are reported by the CLI's result event. They are NOT part
	// of InputTokens, so the ledger normalizes total from all four.
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
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
		// Compaction flags: CodeBuddy stamps these on the providerData of every
		// item emitted while it compacts the session context.
		IsCompactInternal bool   `json:"isCompactInternal,omitempty"`
		CompactType       string `json:"compactType,omitempty"`
	} `json:"providerData,omitempty"`

	// Meta carries the stream-json _meta block. CodeBuddy puts the compaction
	// flag here as well (codebuddy.ai/isCompactInternal), mirroring ACP.
	Meta map[string]any `json:"_meta,omitempty"`

	// Status is the payload of `system`/`status` messages; CodeBuddy emits
	// status="compacting" while it compacts the session context.
	Status string `json:"status,omitempty"`

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
