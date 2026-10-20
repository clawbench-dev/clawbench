package ai

import (
	"encoding/json"
	"log/slog"
	"strings"
)

// OpenCodeStreamMessage represents a single JSON line from `opencode run --format json`
type OpenCodeStreamMessage struct {
	Type      string          `json:"type"` // "step_start", "text", "reasoning", "tool_use", "step_finish"
	Timestamp float64         `json:"timestamp"`
	SessionID string          `json:"sessionID"`
	Part      json.RawMessage `json:"part"` // Varies by type — parse separately
}

// OpenCodeTextPart is the part for type="text" and type="reasoning" messages
type OpenCodeTextPart struct {
	Type string `json:"type"` // "text" or "reasoning"
	Text string `json:"text"`
}

// OpenCodeToolPart is the part for type="tool_use" messages
type OpenCodeToolPart struct {
	Type   string             `json:"type"` // "tool"
	Tool   string             `json:"tool"`
	CallID string             `json:"callID"`
	State  *OpenCodeToolState `json:"state"`
}

// OpenCodeToolState holds tool execution status and I/O
type OpenCodeToolState struct {
	Status string          `json:"status"` // "completed", "running"
	Input  json.RawMessage `json:"input"`
	Output string          `json:"output"`
}

// OpenCodeFinishPart is the part for type="step_finish" messages
type OpenCodeFinishPart struct {
	Reason string          `json:"reason"` // "stop" or "tool-calls"
	Tokens *OpenCodeTokens `json:"tokens,omitempty"`
	Cost   float64         `json:"cost"`
}

// OpenCodeTokens holds token usage from step_finish.
//
// The identity verified over 42,538 real steps in OpenCode's own storage is
//
//	total = input + output + cache.read + cache.write + reasoning
//
// i.e. the cached prompt is reported OUTSIDE input (same convention as
// Claude/Codex), which the ledger normalizes at ingestion.
type OpenCodeTokens struct {
	Total     int              `json:"total"`
	Input     int              `json:"input"`
	Output    int              `json:"output"`
	Reasoning int              `json:"reasoning"`
	Cache     OpenCodeCacheUse `json:"cache"`
}

// OpenCodeCacheUse is the cache split carried inside step_finish tokens.
type OpenCodeCacheUse struct {
	Read  int `json:"read"`
	Write int `json:"write"`
}

// OpenCodeStreamParser parses JSON Lines output from `opencode run --format json`.
// It is separate from the shared StreamParser because OpenCode's format is
// fundamentally different (different top-level types, nesting in "part", multi-step lifecycle).
type OpenCodeStreamParser struct {
	sessionID string // captured from any message that has a sessionID

	// ToolNameMap maps backend-specific tool names to canonical names.
	// When set, ParseLine uses this map instead of the global normalizeToolName().
	ToolNameMap map[string]string

	// InputRemaps maps input field names for tool input normalization.
	// When set, ParseLine uses this map for normalizing tool input fields.
	InputRemaps map[string]string

	// turn accumulates the usage of every step in the CURRENT turn.
	//
	// OpenCode emits one step_finish per model call — a tool-using turn has one
	// per round, and only the LAST one carries reason="stop". Recording just
	// that step (the previous behavior) discarded every intermediate call:
	// measured against OpenCode's own storage, one 812-step session reports
	// 267,293,125 tokens across its steps but only 11,705,915 in its stop
	// steps — 22.8x under. The turn total is the SUM over steps.
	turn openCodeTurnUsage
}

// openCodeTurnUsage is the running total for one turn.
type openCodeTurnUsage struct {
	input      int
	output     int
	total      int
	reasoning  int
	cacheRead  int
	cacheWrite int
	// steps counts the step_finish parts folded in so far. Used to decide
	// whether a turn reported anything at all.
	steps int
}

// reset clears the accumulator for a new turn.
func (u *openCodeTurnUsage) reset() { *u = openCodeTurnUsage{} }

// GetCapturedSessionID returns the OpenCode session ID (ses_xxx) captured from
// parsed stream messages. Available as soon as the first message (e.g., step_start) is parsed.
func (p *OpenCodeStreamParser) GetCapturedSessionID() string { return p.sessionID }

// ParseLine parses a single JSON line from OpenCode's stream-json output and sends
// StreamEvent(s) to the provided channel.
func (p *OpenCodeStreamParser) ParseLine(line string, ch chan<- StreamEvent) {
	var msg OpenCodeStreamMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		slog.Debug("opencode stream: skipping unparseable line", "line", line, "error", err)
		return
	}

	// Capture session ID from any message that has one
	if msg.SessionID != "" {
		p.sessionID = msg.SessionID
	}

	switch msg.Type {
	case "text":
		var part OpenCodeTextPart
		if err := json.Unmarshal(msg.Part, &part); err != nil {
			slog.Debug("opencode stream: skipping unparseable text part", "error", err)
			return
		}
		if part.Text == "" {
			return
		}
		// Strip the leading \n\n that OpenCode prepends to text responses
		text := strings.TrimPrefix(part.Text, "\n\n")
		if text != "" {
			emitStreamEvent(ch, "opencode", StreamEvent{Type: "content", Content: text})
		}

	case "reasoning":
		var part OpenCodeTextPart // same structure as text
		if err := json.Unmarshal(msg.Part, &part); err != nil {
			slog.Debug("opencode stream: skipping unparseable reasoning part", "error", err)
			return
		}
		if part.Text == "" {
			return
		}
		text := strings.TrimPrefix(part.Text, "\n\n")
		if text != "" {
			emitStreamEvent(ch, "opencode", StreamEvent{Type: "thinking", Content: text})
		}

	case "tool_use":
		if tc := parseOpenCodeToolEvent(&msg, p.ToolNameMap, p.InputRemaps); tc != nil {
			emitStreamEvent(ch, "opencode", StreamEvent{Type: "tool_use", Tool: tc})
		}

	case "step_finish":
		p.parseStepFinish(msg.Part, ch)
		// reason="tool-calls" means more steps coming — no event

	case "step_start":
		// Structural — no event

	default:
		slog.Debug("opencode stream: skipping unknown message type", "type", msg.Type)
	}
}

// parseStepFinish handles a step_finish part, emitting the turn's terminal
// metadata + done events when the step ended the turn.
//
// Split out of ParseLine to keep that dispatch within the complexity budget:
// the token/cost mapping has enough conditionals of its own.
func (p *OpenCodeStreamParser) parseStepFinish(raw json.RawMessage, ch chan<- StreamEvent) {
	var part OpenCodeFinishPart
	if err := json.Unmarshal(raw, &part); err != nil {
		slog.Debug("opencode stream: skipping unparseable step_finish part", "error", err)
		return
	}
	// EVERY step counts, not just the terminal one. A tool-using turn emits one
	// step_finish per model call and only the last carries reason="stop";
	// keeping just that step threw away all the intermediate calls (22.8x
	// under-count measured against OpenCode's own storage). Fold this step into
	// the turn accumulator and only emit when the turn actually ends.
	if part.Tokens != nil {
		p.turn.input += part.Tokens.Input
		p.turn.output += part.Tokens.Output
		p.turn.total += part.Tokens.Total
		p.turn.reasoning += part.Tokens.Reasoning
		p.turn.cacheRead += part.Tokens.Cache.Read
		p.turn.cacheWrite += part.Tokens.Cache.Write
	}
	p.turn.steps++
	if part.Reason != "stop" {
		return
	}

	meta := &Metadata{
		SessionID:  p.sessionID,
		StopReason: "stop",
		CostUSD:    part.Cost,
	}
	p.applyTurnUsage(meta)
	// step_finish.cost is the SESSION running total, not this step's cost: in a
	// production DB the value rises monotonically across a session
	// (0.017 → 0.413) while per-turn tokens fall (9280 → 719), which no
	// per-turn price can produce. The ledger diffs it against the previous
	// turn's cost_session_total.
	if meta.CostUSD > 0 {
		meta.CostCumulative = true
		meta.CostSessionTotal = meta.CostUSD
	}
	emitStreamEvent(ch, "opencode", StreamEvent{Type: "metadata", Meta: meta})
	emitStreamEvent(ch, "opencode", StreamEvent{Type: "done"})
	p.turn.reset()
}

// applyTurnUsage copies the accumulated turn totals onto a Metadata.
//
// CACHE NORMALIZATION: OpenCode reports the cached prompt OUTSIDE input
// (verified over 42,538 steps in its own storage: total = input + output +
// cache.read + cache.write + reasoning). The stats panel assumes the CodeBuddy
// shape where input is the FULL prompt, so the cache counters are folded into
// input — otherwise OpenCode's cached traffic is invisible and its cache-hit
// rate has no denominator, exactly the defect fixed for Claude/Codex.
func (p *OpenCodeStreamParser) applyTurnUsage(meta *Metadata) {
	t := &p.turn
	meta.InputTokens = t.input
	meta.OutputTokens = t.output
	meta.ThoughtTokens = t.reasoning
	meta.CachedReadTokens = t.cacheRead
	meta.CachedWriteTokens = t.cacheWrite
	if t.cacheRead > 0 || t.cacheWrite > 0 {
		meta.CacheHitTokens = t.cacheRead
		meta.CacheMissTokens = t.input
		meta.InputTokens += t.cacheRead + t.cacheWrite
	}
	// Prefer the agent's own total; fall back to the normalized components when
	// it did not report one (older builds), and never let it fall below
	// input+output — the ledger's SUM-ability invariant.
	meta.TotalTokens = t.total
	if meta.TotalTokens < meta.InputTokens+meta.OutputTokens {
		meta.TotalTokens = meta.InputTokens + meta.OutputTokens
	}
}
