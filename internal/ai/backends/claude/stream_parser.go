package claude

import (
	"encoding/json"
	"log/slog"
	"strings"

	"clawbench/internal/ai"
)

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
	activeTools map[int]*ai.ToolCall
	// activeToolResults tracks in-progress tool_result content blocks keyed
	// by content block index. When tool_result content_block_start is received,
	// subsequent text_delta events for that index are accumulated into the
	// tool result output instead of being emitted as content events.
	activeToolResults map[int]*ai.ToolResultAccum
	// activeThinkingBlocks tracks content block indices that are thinking blocks.
	// When content_block_stop arrives for a thinking index, we emit thinking_done
	// so the frontend can stop the thinking spinner immediately.
	activeThinkingBlocks map[int]bool
	// emittedToolInputEmpty tracks tool_use IDs that were emitted via stream_event
	// but had empty Input (partial_json was empty). When the assistant verbose
	// message arrives with the full Input, we re-emit a tool_use event so that
	// AccumulateBlock can update the block with the correct input data.
	emittedToolInputEmpty map[string]bool
	// compactReported is set once a compaction signal has been emitted for this
	// parser's stream. The CLI repeats the flag on every item emitted while
	// compacting, so this keeps the service layer from being told the same
	// compaction happened many times. The parser is per-turn, so it resets
	// naturally when the next turn builds a new parser.
	compactReported bool
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
// ai.StreamEvent(s) to the provided channel. This is the shared parser used by
// both Claude and Codebuddy streaming implementations.
//
//nolint:gocognit,gocyclo // complex stream parsing logic
func (p *StreamParser) ParseLine(line string, ch chan<- ai.StreamEvent) {
	var msg ai.ClaudeStreamMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		slog.Debug("stream: skipping unparseable line", "line", line, "error", err)
		return
	}

	// Compaction detection — checked before the type switch because the signal
	// rides on several message shapes (CodeBuddy: providerData/_meta flag on any
	// item, or system/status="compacting"; Claude: system/compact_boundary).
	// Reported at most once per compaction; the parser is per-turn, so the flag
	// resets naturally on the next turn. The latch is set only on delivery so a
	// drop on a full channel is retried by the next matching line.
	if !p.compactReported && ai.IsCLICompactionMessage(&msg) {
		if ai.TryEmitStreamEvent(ch, "cli", ai.CompactDetectedEvent("cli_meta")) {
			p.compactReported = true
		}
	}

	switch msg.Type {
	case "assistant":
		state := p.toolState()
		events := parseClaudeAssistantToolUse(&msg, &state)
		for _, ev := range events {
			ai.EmitStreamEvent(ch, "cli", ev)
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
			ai.EmitStreamEvent(ch, "cli", ai.StreamEvent{Type: "content", Content: msg.Text})
		}

	case "result":
		meta := &ai.Metadata{
			SessionID:  msg.SessionID,
			DurationMs: msg.DurationMs,
			CostUSD:    msg.TotalCostUSD,
			StopReason: msg.StopReason,
			IsError:    msg.IsError,
		}
		if msg.Usage != nil {
			meta.InputTokens = msg.Usage.InputTokens
			meta.OutputTokens = msg.Usage.OutputTokens
			// The CLI's result usage reports cache counters OUTSIDE
			// input_tokens (total = input + output + cacheRead + cacheWrite).
			// Normalize to the OpenAI/CodeBuddy shape so the stats panel's
			// "输入 Tokens" and cache hit rate mean the same thing for every
			// backend; without it Claude's cached traffic was invisible.
			if msg.Usage.CacheReadInputTokens > 0 || msg.Usage.CacheCreationInputTokens > 0 {
				meta.CachedReadTokens = msg.Usage.CacheReadInputTokens
				meta.CachedWriteTokens = msg.Usage.CacheCreationInputTokens
				meta.CacheHitTokens = msg.Usage.CacheReadInputTokens
				meta.CacheMissTokens = msg.Usage.InputTokens
				meta.InputTokens += msg.Usage.CacheReadInputTokens + msg.Usage.CacheCreationInputTokens
			}
			meta.TotalTokens = meta.InputTokens + meta.OutputTokens
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
				ai.EmitStreamEvent(ch, "cli", ai.StreamEvent{Type: "warning", Content: errMsg})
			}
		}
		slog.Info("stream: emitting done event", "is_error", msg.IsError)
		ai.EmitStreamEvent(ch, "cli", ai.StreamEvent{Type: "metadata", Meta: meta})
		ai.EmitStreamEvent(ch, "cli", ai.StreamEvent{Type: "done"})

	case "system":
		// System messages (e.g., init, tool use) - skip

	case "user":
		events := parseClaudeUserToolResult(&msg)
		for _, ev := range events {
			ai.EmitStreamEvent(ch, "cli", ev)
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

// NewStreamParser returns a parser for the Claude Code stream-json format.
//
// Four backends share this format (claude, codebuddy, copilot, qoder) — they
// are Claude Code forks or emit the same wire shapes — so the parser lives here
// and the others import it rather than carrying a copy.
func NewStreamParser() ai.LineParser { return &StreamParser{} }
