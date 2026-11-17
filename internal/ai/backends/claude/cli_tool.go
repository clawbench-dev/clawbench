package claude

import (
	"log/slog"

	"clawbench/internal/ai"
)

// ClaudeStreamToolState encapsulates StreamParser's tool-call-related state,
// passed as a parameter to tool parsing functions so they remain stateless
// and testable.
type ClaudeStreamToolState struct {
	ActiveTools             map[int]*ai.ToolCall
	ActiveToolResults       map[int]*ai.ToolResultAccum
	ActiveThinkingBlocks    map[int]bool
	EmittedToolInputEmpty   map[string]bool
	ReceivedPartialToolUse  bool
	ReceivedPartial         bool
	ReceivedPartialThinking bool
}

// parseClaudeStreamToolEvent handles stream_event messages (content_block_start/delta/stop)
// for tool calls, tool results, and thinking blocks. Returns StreamEvents to emit.
func parseClaudeStreamToolEvent(msg *ai.ClaudeStreamMessage, state *ClaudeStreamToolState, ch chan<- ai.StreamEvent) {
	if msg.Event == nil {
		return
	}

	switch msg.Event.Type {
	case "content_block_delta":
		handleClaudeContentBlockDelta(msg, state, ch)
	case "content_block_start":
		handleClaudeContentBlockStart(msg, state, ch)
	case "content_block_stop":
		handleClaudeContentBlockStop(msg, state, ch)
	case "message_start":
		state.ReceivedPartial = false
		state.ReceivedPartialThinking = false
		state.ReceivedPartialToolUse = false
	case "message_delta", "message_stop":
		// Structural events — no content to emit
	}
}

// handleClaudeContentBlockDelta processes content_block_delta events.
func handleClaudeContentBlockDelta(msg *ai.ClaudeStreamMessage, state *ClaudeStreamToolState, ch chan<- ai.StreamEvent) {
	if msg.Event.Delta == nil {
		return
	}
	switch msg.Event.Delta.Type {
	case "text_delta":
		if state.ActiveToolResults != nil {
			if accum, ok := state.ActiveToolResults[msg.Event.Index]; ok {
				if msg.Event.Delta.Text != "" {
					accum.Output.WriteString(msg.Event.Delta.Text)
				}
				return
			}
		}
		if msg.Event.Delta.Text != "" {
			state.ReceivedPartial = true
			ai.EmitStreamEvent(ch, "cli", ai.StreamEvent{Type: "content", Content: msg.Event.Delta.Text})
		}
	case "input_json_delta":
		if tool, ok := state.ActiveTools[msg.Event.Index]; ok {
			delta := msg.Event.Delta.PartialJSON
			if delta != "" {
				tool.Input += delta
			}
		}
	case "thinking_delta":
		if msg.Event.Delta.Thinking != "" {
			state.ReceivedPartialThinking = true
			ai.EmitStreamEvent(ch, "cli", ai.StreamEvent{Type: "thinking", Content: msg.Event.Delta.Thinking})
		}
	}
}

// handleClaudeContentBlockStart processes content_block_start events.
func handleClaudeContentBlockStart(msg *ai.ClaudeStreamMessage, state *ClaudeStreamToolState, ch chan<- ai.StreamEvent) {
	if msg.Event.ContentBlock == nil {
		return
	}
	switch msg.Event.ContentBlock.Type {
	case "tool_use":
		handleClaudeToolUseStart(msg, state, ch)
	case "tool_result":
		handleClaudeToolResultStart(msg, state)
	case "thinking":
		if state.ActiveThinkingBlocks == nil {
			state.ActiveThinkingBlocks = make(map[int]bool)
		}
		state.ActiveThinkingBlocks[msg.Event.Index] = true
	}
}

// handleClaudeToolUseStart processes a tool_use content_block_start.
func handleClaudeToolUseStart(msg *ai.ClaudeStreamMessage, state *ClaudeStreamToolState, ch chan<- ai.StreamEvent) {
	state.ReceivedPartialToolUse = true
	tool := &ai.ToolCall{
		Name: msg.Event.ContentBlock.Name,
		ID:   msg.Event.ContentBlock.ID,
	}
	if len(msg.Event.ContentBlock.Input) > 0 &&
		string(msg.Event.ContentBlock.Input) != "{}" {
		tool.Input = string(msg.Event.ContentBlock.Input)
	}
	if state.ActiveTools == nil {
		state.ActiveTools = make(map[int]*ai.ToolCall)
	}
	if existing, ok := state.ActiveTools[msg.Event.Index]; ok {
		slog.Debug("stream: auto-closing tool at reused index", "index", msg.Event.Index, "tool_id", existing.ID, "tool_name", existing.Name)
		closed := *existing
		closed.Done = true
		ai.EmitStreamEvent(ch, "cli", ai.StreamEvent{Type: "tool_use", Tool: &closed})
	}
	state.ActiveTools[msg.Event.Index] = tool
	startCopy := *tool
	ai.EmitStreamEvent(ch, "cli", ai.StreamEvent{Type: "tool_use", Tool: &startCopy})
	if tool.Input == "" {
		if state.EmittedToolInputEmpty == nil {
			state.EmittedToolInputEmpty = make(map[string]bool)
		}
		state.EmittedToolInputEmpty[tool.ID] = true
	}
}

// handleClaudeToolResultStart processes a tool_result content_block_start.
func handleClaudeToolResultStart(msg *ai.ClaudeStreamMessage, state *ClaudeStreamToolState) {
	if state.ActiveToolResults == nil {
		state.ActiveToolResults = make(map[int]*ai.ToolResultAccum)
	}
	accum := &ai.ToolResultAccum{
		IsError: msg.Event.ContentBlock.IsError,
	}
	toolUseID := msg.Event.ContentBlock.ToolUseID
	if toolUseID == "" {
		toolUseID = msg.Event.ContentBlock.ID
	}
	accum.ToolUseID = toolUseID
	if msg.Event.ContentBlock.Content != "" {
		accum.Output.WriteString(msg.Event.ContentBlock.Content)
	}
	state.ActiveToolResults[msg.Event.Index] = accum
}

// handleClaudeContentBlockStop processes content_block_stop events.
func handleClaudeContentBlockStop(msg *ai.ClaudeStreamMessage, state *ClaudeStreamToolState, ch chan<- ai.StreamEvent) {
	// Check if this is a tool_result block being finalized
	if state.ActiveToolResults != nil {
		if accum, ok := state.ActiveToolResults[msg.Event.Index]; ok {
			status := "success"
			if accum.IsError {
				status = "error"
			}
			ai.EmitStreamEvent(ch, "cli", ai.StreamEvent{Type: "tool_result", Tool: &ai.ToolCall{
				ID:     accum.ToolUseID,
				Output: ai.TruncateToolOutput(accum.Output.String()),
				Status: status,
			}})
			delete(state.ActiveToolResults, msg.Event.Index)
		}
	}
	// Check if this is a tool_use block being finalized
	if tool, ok := state.ActiveTools[msg.Event.Index]; ok {
		closed := *tool
		closed.Done = true
		ai.EmitStreamEvent(ch, "cli", ai.StreamEvent{Type: "tool_use", Tool: &closed})
		if closed.Input == "" {
			if state.EmittedToolInputEmpty == nil {
				state.EmittedToolInputEmpty = make(map[string]bool)
			}
			state.EmittedToolInputEmpty[closed.ID] = true
		}
		delete(state.ActiveTools, msg.Event.Index)
	} else if state.ActiveThinkingBlocks != nil && state.ActiveThinkingBlocks[msg.Event.Index] {
		ai.EmitStreamEvent(ch, "cli", ai.StreamEvent{Type: "thinking_done"})
		delete(state.ActiveThinkingBlocks, msg.Event.Index)
	} else {
		slog.Debug("stream: content_block_stop for unknown index", "index", msg.Event.Index)
	}
}

// parseClaudeAssistantToolUse handles "assistant" type messages with tool_use
// and tool_result blocks in msg.Message.Content. When stream_event partial
// mode was active, it supplements missing input data for tools emitted with
// empty Input.
func parseClaudeAssistantToolUse(msg *ai.ClaudeStreamMessage, state *ClaudeStreamToolState) []ai.StreamEvent {
	if msg.Message == nil {
		return nil
	}

	var events []ai.StreamEvent
	for _, block := range msg.Message.Content {
		switch block.Type {
		case "tool_use":
			events = appendClaudeToolUseEvents(events, block, state)
		case "tool_result":
			events = append(events, claudeToolResultEvent(block))
		case "thinking":
			if block.Thinking != "" && !state.ReceivedPartialThinking {
				events = append(events, ai.StreamEvent{Type: "thinking", Content: block.Thinking})
			}
		case "text":
			if block.Text != "" && !state.ReceivedPartial {
				events = append(events, ai.StreamEvent{Type: "content", Content: block.Text})
			}
		}
	}
	return events
}

// appendClaudeToolUseEvents appends tool_use events for an assistant content block.
func appendClaudeToolUseEvents(events []ai.StreamEvent, block ai.ClaudeContentBlock, state *ClaudeStreamToolState) []ai.StreamEvent {
	if state.ReceivedPartialToolUse {
		// Supplement empty input from the complete assistant message
		if state.EmittedToolInputEmpty != nil && state.EmittedToolInputEmpty[block.ID] {
			inputStr := string(block.Input)
			if inputStr != "" && inputStr != "{}" {
				slog.Debug("stream: supplementing empty tool_use input from assistant message",
					"tool_id", block.ID, "tool_name", block.Name, "input_len", len(inputStr))
				events = append(events, ai.StreamEvent{Type: "tool_use", Tool: &ai.ToolCall{
					Name:  block.Name,
					ID:    block.ID,
					Input: inputStr,
					Done:  true,
				}})
			}
			delete(state.EmittedToolInputEmpty, block.ID)
		}
		return events
	}
	// Emit tool_use event with full input from the complete message
	inputStr := string(block.Input)
	events = append(events, ai.StreamEvent{Type: "tool_use", Tool: &ai.ToolCall{
		Name:  block.Name,
		ID:    block.ID,
		Input: inputStr,
		Done:  true,
	}})
	return events
}

// claudeToolResultEvent creates a tool_result ai.StreamEvent from a content block.
func claudeToolResultEvent(block ai.ClaudeContentBlock) ai.StreamEvent {
	toolUseID := block.ToolUseID
	if toolUseID == "" {
		toolUseID = block.ID
	}
	status := "success"
	if block.IsError {
		status = "error"
	}
	output := ai.ExtractContentText(block.Content)
	if output == "" && block.Text != "" {
		output = block.Text
	}
	return ai.StreamEvent{Type: "tool_result", Tool: &ai.ToolCall{
		ID:     toolUseID,
		Output: ai.TruncateToolOutput(output),
		Status: status,
	}}
}

// parseClaudeUserToolResult handles "user" type messages containing tool_result
// blocks. These appear when tool results are sent back to the model.
func parseClaudeUserToolResult(msg *ai.ClaudeStreamMessage) []ai.StreamEvent {
	if msg.Message == nil {
		return nil
	}

	var events []ai.StreamEvent
	for _, block := range msg.Message.Content {
		if block.Type != "tool_result" {
			continue
		}
		evt := claudeToolResultEvent(block)
		slog.Debug("stream: emitting tool_result from user message", "tool_use_id", evt.Tool.ID, "output_len", len(evt.Tool.Output), "status", evt.Tool.Status)
		events = append(events, evt)
	}
	return events
}
