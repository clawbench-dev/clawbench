package ai

import (
	"encoding/json"
	"log/slog"
)

// GrokStreamMessage represents a single JSON line from
// `grok -p ... --output-format streaming-json`.
//
// Documented event types:
//
//	{"type":"text","data":"..."}
//	{"type":"thought","data":"..."}
//	{"type":"end","stopReason":"EndTurn","sessionId":"...","usage":{...}}
//	{"type":"error","message":"..."}
//
// The event list is non-exhaustive (e.g. max_turns_reached, auto_compact_*).
type GrokStreamMessage struct {
	Type       string          `json:"type"`
	Data       string          `json:"data,omitempty"`
	Message    string          `json:"message,omitempty"`
	StopReason string          `json:"stopReason,omitempty"`
	SessionID  string          `json:"sessionId,omitempty"`
	RequestID  string          `json:"requestId,omitempty"`
	NumTurns   int             `json:"num_turns,omitempty"`
	Usage      json.RawMessage `json:"usage,omitempty"`
	ModelUsage json.RawMessage `json:"modelUsage,omitempty"`
}

// grokUsage is a best-effort decode of the usage object on end/error events.
// Grok documents snake_case token fields on the json/streaming-json spend shape.
type grokUsage struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	TotalTokens  int     `json:"total_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	// camelCase fallbacks
	InputTokensCamel  int     `json:"inputTokens"`
	OutputTokensCamel int     `json:"outputTokens"`
	TotalTokensCamel  int     `json:"totalTokens"`
	CostUSDCamel      float64 `json:"costUsd"`
}

// GrokStreamParser parses JSON Lines output from Grok headless streaming-json.
type GrokStreamParser struct {
	sessionID string
}

// GetCapturedSessionID returns the session ID captured from an end event.
// CLIBackend.ExecuteStream emits session_capture after each ParseLine call.
func (p *GrokStreamParser) GetCapturedSessionID() string {
	return p.sessionID
}

// ParseLine parses one streaming-json event and emits StreamEvent(s).
func (p *GrokStreamParser) ParseLine(line string, ch chan<- StreamEvent) {
	var msg GrokStreamMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		slog.Debug("grok stream: skipping unparseable line", "line", line, "error", err)
		return
	}

	switch msg.Type {
	case "text":
		if msg.Data != "" {
			ch <- StreamEvent{Type: "content", Content: msg.Data}
		}

	case "thought":
		if msg.Data != "" {
			ch <- StreamEvent{Type: "thinking", Content: msg.Data}
		}

	case "end":
		if msg.SessionID != "" {
			p.sessionID = msg.SessionID
			slog.Debug("grok stream: captured session ID", "session_id", msg.SessionID)
		}
		if meta := grokMetadataFromEnd(&msg); meta != nil {
			ch <- StreamEvent{Type: "metadata", Meta: meta}
		}
		ch <- StreamEvent{Type: "done"}

	case "error":
		errMsg := msg.Message
		if errMsg == "" {
			errMsg = msg.Data
		}
		if errMsg == "" {
			errMsg = "grok error"
		}
		// Still capture session ID if present on error payloads.
		if msg.SessionID != "" {
			p.sessionID = msg.SessionID
		}
		ch <- StreamEvent{Type: "error", Error: errMsg}

	default:
		// Non-exhaustive protocol: ignore unknown event types.
		slog.Debug("grok stream: skipping unknown message type", "type", msg.Type)
	}
}

func grokMetadataFromEnd(msg *GrokStreamMessage) *Metadata {
	if msg == nil {
		return nil
	}
	meta := &Metadata{
		SessionID:  msg.SessionID,
		StopReason: msg.StopReason,
	}
	if len(msg.Usage) > 0 {
		var u grokUsage
		if err := json.Unmarshal(msg.Usage, &u); err == nil {
			meta.InputTokens = firstNonZero(u.InputTokens, u.InputTokensCamel)
			meta.OutputTokens = firstNonZero(u.OutputTokens, u.OutputTokensCamel)
			meta.CostUSD = firstNonZeroFloat(u.CostUSD, u.CostUSDCamel)
		}
	}
	// Emit only when we have something useful beyond empty defaults.
	if meta.SessionID == "" && meta.StopReason == "" &&
		meta.InputTokens == 0 && meta.OutputTokens == 0 && meta.CostUSD == 0 {
		return nil
	}
	return meta
}

func firstNonZero(vals ...int) int {
	for _, v := range vals {
		if v != 0 {
			return v
		}
	}
	return 0
}

func firstNonZeroFloat(vals ...float64) float64 {
	for _, v := range vals {
		if v != 0 {
			return v
		}
	}
	return 0
}
