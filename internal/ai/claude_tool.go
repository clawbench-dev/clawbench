package ai

import (
	"log/slog"
)

// ClaudeStreamToolState encapsulates StreamParser's tool-call-related state,
// passed as a parameter to tool parsing functions so they remain stateless
// and testable.
type ClaudeStreamToolState struct {
	ActiveTools             map[int]*ToolCall
	ActiveToolResults       map[int]*toolResultAccum
	ActiveThinkingBlocks    map[int]bool
	EmittedToolInputEmpty   map[string]bool
	ReceivedPartialToolUse  bool
	ReceivedPartial         bool
	ReceivedPartialThinking bool
}

// parseClaudeStreamToolEvent handles stream_event messages (content_block_start/delta/stop)
// for tool calls, tool results, and thinking blocks. Returns StreamEvents to emit.
func parseClaudeStreamToolEvent(msg *ClaudeStreamMessage, state *ClaudeStreamToolState, ch chan<- StreamEvent) {
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
func handleClaudeContentBlockDelta(msg *ClaudeStreamMessage, state *ClaudeStreamToolState, ch chan<- StreamEvent) {
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
			ch <- StreamEvent{Type: "content", Content: msg.Event.Delta.Text}
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
			ch <- StreamEvent{Type: "thinking", Content: msg.Event.Delta.Thinking}
		}
	}
}

// handleClaudeContentBlockStart processes content_block_start events.
func handleClaudeContentBlockStart(msg *ClaudeStreamMessage, state *ClaudeStreamToolState, ch chan<- StreamEvent) {
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
func handleClaudeToolUseStart(msg *ClaudeStreamMessage, state *ClaudeStreamToolState, ch chan<- StreamEvent) {
	state.ReceivedPartialToolUse = true
	tool := &ToolCall{
		Name: msg.Event.ContentBlock.Name,
		ID:   msg.Event.ContentBlock.ID,
	}
	if len(msg.Event.ContentBlock.Input) > 0 &&
		string(msg.Event.ContentBlock.Input) != "{}" {
		tool.Input = string(msg.Event.ContentBlock.Input)
	}
	if state.ActiveTools == nil {
		state.ActiveTools = make(map[int]*ToolCall)
	}
	if existing, ok := state.ActiveTools[msg.Event.Index]; ok {
		slog.Debug("stream: auto-closing tool at reused index", "index", msg.Event.Index, "tool_id", existing.ID, "tool_name", existing.Name)
		closed := *existing
		closed.Done = true
		ch <- StreamEvent{Type: "tool_use", Tool: &closed}
	}
	state.ActiveTools[msg.Event.Index] = tool
	startCopy := *tool
	ch <- StreamEvent{Type: "tool_use", Tool: &startCopy}
	if tool.Input == "" {
		if state.EmittedToolInputEmpty == nil {
			state.EmittedToolInputEmpty = make(map[string]bool)
		}
		state.EmittedToolInputEmpty[tool.ID] = true
	}
}

// handleClaudeToolResultStart processes a tool_result content_block_start.
func handleClaudeToolResultStart(msg *ClaudeStreamMessage, state *ClaudeStreamToolState) {
	if state.ActiveToolResults == nil {
		state.ActiveToolResults = make(map[int]*toolResultAccum)
	}
	accum := &toolResultAccum{
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
func handleClaudeContentBlockStop(msg *ClaudeStreamMessage, state *ClaudeStreamToolState, ch chan<- StreamEvent) {
	// Check if this is a tool_result block being finalized
	if state.ActiveToolResults != nil {
		if accum, ok := state.ActiveToolResults[msg.Event.Index]; ok {
			status := "success"
			if accum.IsError {
				status = "error"
			}
			ch <- StreamEvent{Type: "tool_result", Tool: &ToolCall{
				ID:     accum.ToolUseID,
				Output: truncateToolOutput(accum.Output.String()),
				Status: status,
			}}
			delete(state.ActiveToolResults, msg.Event.Index)
		}
	}
	// Check if this is a tool_use block being finalized
	if tool, ok := state.ActiveTools[msg.Event.Index]; ok {
		closed := *tool
		closed.Done = true
		ch <- StreamEvent{Type: "tool_use", Tool: &closed}
		if closed.Input == "" {
			if state.EmittedToolInputEmpty == nil {
				state.EmittedToolInputEmpty = make(map[string]bool)
			}
			state.EmittedToolInputEmpty[closed.ID] = true
		}
		delete(state.ActiveTools, msg.Event.Index)
	} else if state.ActiveThinkingBlocks != nil && state.ActiveThinkingBlocks[msg.Event.Index] {
		ch <- StreamEvent{Type: "thinking_done"}
		delete(state.ActiveThinkingBlocks, msg.Event.Index)
	} else {
		slog.Debug("stream: content_block_stop for unknown index", "index", msg.Event.Index)
	}
}

// parseClaudeAssistantToolUse handles "assistant" type messages with tool_use
// and tool_result blocks in msg.Message.Content. When stream_event partial
// mode was active, it supplements missing input data for tools emitted with
// empty Input.
func parseClaudeAssistantToolUse(msg *ClaudeStreamMessage, state *ClaudeStreamToolState) []StreamEvent {
	if msg.Message == nil {
		return nil
	}

	var events []StreamEvent
	for _, block := range msg.Message.Content {
		switch block.Type {
		case "tool_use":
			events = appendClaudeToolUseEvents(events, block, state)
		case "tool_result":
			events = append(events, claudeToolResultEvent(block))
		case "thinking":
			if block.Thinking != "" && !state.ReceivedPartialThinking {
				events = append(events, StreamEvent{Type: "thinking", Content: block.Thinking})
			}
		case "text":
			if block.Text != "" && !state.ReceivedPartial {
				events = append(events, StreamEvent{Type: "content", Content: block.Text})
			}
		}
	}
	return events
}

// appendClaudeToolUseEvents appends tool_use events for an assistant content block.
func appendClaudeToolUseEvents(events []StreamEvent, block ClaudeContentBlock, state *ClaudeStreamToolState) []StreamEvent {
	if state.ReceivedPartialToolUse {
		// Supplement empty input from the complete assistant message
		if state.EmittedToolInputEmpty != nil && state.EmittedToolInputEmpty[block.ID] {
			inputStr := string(block.Input)
			if inputStr != "" && inputStr != "{}" {
				slog.Debug("stream: supplementing empty tool_use input from assistant message",
					"tool_id", block.ID, "tool_name", block.Name, "input_len", len(inputStr))
				events = append(events, StreamEvent{Type: "tool_use", Tool: &ToolCall{
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
	events = append(events, StreamEvent{Type: "tool_use", Tool: &ToolCall{
		Name:  block.Name,
		ID:    block.ID,
		Input: inputStr,
		Done:  true,
	}})
	return events
}

// claudeToolResultEvent creates a tool_result StreamEvent from a content block.
func claudeToolResultEvent(block ClaudeContentBlock) StreamEvent {
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
	return StreamEvent{Type: "tool_result", Tool: &ToolCall{
		ID:     toolUseID,
		Output: truncateToolOutput(output),
		Status: status,
	}}
}

// parseClaudeUserToolResult handles "user" type messages containing tool_result
// blocks. These appear when tool results are sent back to the model.
func parseClaudeUserToolResult(msg *ClaudeStreamMessage) []StreamEvent {
	if msg.Message == nil {
		return nil
	}

	var events []StreamEvent
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
