package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	acp "github.com/coder/acp-go-sdk"
)

// mapACPSessionUpdate converts an ACP SessionUpdate to StreamEvent(s) and
// sends them to the stream channel. Called from ClawBenchACPClient.SessionUpdate,
// which runs on the SDK's internal goroutine.
func mapACPSessionUpdate(update acp.SessionUpdate, ch chan<- StreamEvent, ctx context.Context) {
	switch {
	case update.AgentMessageChunk != nil:
		content := update.AgentMessageChunk.Content
		if content.Text != nil {
			forwardACPEvent(ch, StreamEvent{Type: "content", Content: content.Text.Text})
		}

	case update.AgentThoughtChunk != nil:
		content := update.AgentThoughtChunk.Content
		if content.Text != nil {
			forwardACPEvent(ch, StreamEvent{Type: "thinking", Content: content.Text.Text})
		}

	case update.ToolCall != nil:
		tc := update.ToolCall
		event := mapACPToolCall(*tc)
		forwardACPEvent(ch, event)

	case update.ToolCallUpdate != nil:
		tcu := update.ToolCallUpdate
		event := mapACPToolCallUpdate(*tcu)
		forwardACPEvent(ch, event)

	case update.Plan != nil:
		entries := make([]PlanEntry, 0, len(update.Plan.Entries))
		for _, e := range update.Plan.Entries {
			entries = append(entries, PlanEntry{
				Content:  e.Content,
				Priority: string(e.Priority),
				Status:   string(e.Status),
			})
		}
		forwardACPEvent(ch, StreamEvent{Type: "plan_update", Plan: &PlanState{Entries: entries}})

	case update.AvailableCommandsUpdate != nil:
		cmds := update.AvailableCommandsUpdate.AvailableCommands
		slog.Info("acp: available commands update", "count", len(cmds))
		infos := make([]AvailableCommandInfo, 0, len(cmds))
		for _, c := range cmds {
			info := AvailableCommandInfo{
				Name:        c.Name,
				Description: c.Description,
			}
			if c.Input != nil && c.Input.Unstructured != nil {
				info.InputHint = c.Input.Unstructured.Hint
			}
			infos = append(infos, info)
		}
		forwardACPEvent(ch, StreamEvent{
			Type:     "commands_update",
			Commands: infos,
		})

	case update.CurrentModeUpdate != nil:
		// v1 mode update: only currentModeId; available modes were sent in session/new
		mu := update.CurrentModeUpdate
		modeState := &ModeState{
			CurrentModeID: string(mu.CurrentModeId),
		}
		forwardACPEvent(ch, StreamEvent{Type: "mode_update", Mode: modeState})

	case update.ConfigOptionUpdate != nil:
		// v2 config option update: extract mode and thought_level options
		cu := update.ConfigOptionUpdate
		for _, opt := range cu.ConfigOptions {
			if opt.Select == nil {
				continue
			}
			sel := opt.Select
			if sel.Category == nil {
				continue
			}

			switch *sel.Category {
			case acp.SessionConfigOptionCategoryMode:
				configState := buildConfigOptionStateFromSelect(sel, "mode")
				forwardACPEvent(ch, StreamEvent{Type: "config_update", Config: configState})

			case acp.SessionConfigOptionCategoryThoughtLevel:
				effortState := buildThinkingEffortStateFromSelect(sel)
				if effortState != nil {
					forwardACPEvent(ch, StreamEvent{Type: "thinking_effort_update", ThinkingEffort: effortState})
				}
			}
		}

	case update.SessionInfoUpdate != nil:
		slog.Debug("acp: session info update")
	}
}

// mapACPToolCall converts an ACP ToolCall start event to a StreamEvent.
func mapACPToolCall(tc acp.SessionUpdateToolCall) StreamEvent {
	tool := &ToolCall{
		Name: extractToolName(tc.Title, tc.Kind),
		ID:   string(tc.ToolCallId),
		Done: false,
	}

	// Extract raw input as JSON string
	if tc.RawInput != nil {
		if inputBytes, err := json.Marshal(tc.RawInput); err == nil {
			tool.Input = string(inputBytes)
		}
	}

	return StreamEvent{Type: "tool_use", Tool: tool}
}

// mapACPToolCallUpdate converts an ACP ToolCallUpdate to a StreamEvent.
func mapACPToolCallUpdate(tcu acp.SessionToolCallUpdate) StreamEvent {
	tool := &ToolCall{
		ID: string(tcu.ToolCallId),
	}

	// Map status
	if tcu.Status != nil {
		switch *tcu.Status {
		case acp.ToolCallStatusCompleted:
			tool.Done = true
			tool.Status = "success"
		case acp.ToolCallStatusFailed:
			tool.Done = true
			tool.Status = "error"
		case acp.ToolCallStatusPending, acp.ToolCallStatusInProgress:
			tool.Done = false
		}
	}

	// Extract raw output
	if tcu.RawOutput != nil {
		if outputBytes, err := json.Marshal(tcu.RawOutput); err == nil {
			tool.Output = truncateToolOutput(string(outputBytes))
		}
	}

	// Determine event type: if tool is done, emit tool_result; otherwise update tool_use
	eventType := "tool_use"
	if tool.Done {
		eventType = "tool_result"
	}

	return StreamEvent{Type: eventType, Tool: tool}
}

// extractToolName returns a canonical tool name from an ACP tool call.
// Uses the title field, falling back to kind string.
func extractToolName(title string, kind acp.ToolKind) string {
	if title != "" {
		return title
	}
	return string(kind)
}

// mapACPError maps a JSON-RPC error code to a StreamEvent.
func mapACPError(code int, message string) StreamEvent {
	reason := ReasonBackendExit
	switch code {
	case -32700:
		reason = ReasonParseError
	case -32600, -32602:
		reason = ReasonParseError // invalid request/params
	case -32601:
		reason = ReasonBackendExit // method not found
	case -32603:
		reason = ReasonBackendExit // internal error
	case -32000:
		reason = ReasonRequestFailed // auth required
	case -32800:
		reason = ReasonContextCancel // request cancelled
	}
	return StreamEvent{
		Type:    "error",
		Error:   fmt.Sprintf("ACP error %d: %s", code, message),
		Reason:  reason,
	}
}

// buildConfigOptionStateFromSelect builds a ConfigOptionState from an ACP SessionConfigOptionSelect.
func buildConfigOptionStateFromSelect(sel *acp.SessionConfigOptionSelect, category string) *ConfigOptionState {
	configState := &ConfigOptionState{
		ConfigID:  string(sel.Id),
		CurrentID: string(sel.CurrentValue),
	}

	optDef := ConfigOptionDef{
		ID:       string(sel.Id),
		Name:     sel.Name,
		Category: category,
	}

	mapACPSelectOptions(sel.Options, &optDef)
	configState.Options = append(configState.Options, optDef)
	return configState
}

// buildThinkingEffortStateFromSelect builds a ThinkingEffortState from an ACP thought_level config option.
func buildThinkingEffortStateFromSelect(sel *acp.SessionConfigOptionSelect) *ThinkingEffortState {
	state := &ThinkingEffortState{
		CurrentID: string(sel.CurrentValue),
	}

	if sel.Options.Ungrouped != nil {
		for _, v := range *sel.Options.Ungrouped {
			state.AvailableLevels = append(state.AvailableLevels, ThinkingEffortDef{
				ID:   string(v.Value),
				Name: v.Name,
			})
		}
	}
	if sel.Options.Grouped != nil {
		for _, g := range *sel.Options.Grouped {
			for _, v := range g.Options {
				state.AvailableLevels = append(state.AvailableLevels, ThinkingEffortDef{
					ID:   string(v.Value),
					Name: v.Name,
				})
			}
		}
	}

	if len(state.AvailableLevels) == 0 && state.CurrentID == "" {
		return nil
	}

	return state
}

// mapACPConfigOptionUpdate converts an ACP SessionConfigOptionUpdate to a ConfigOptionState.
// Returns nil if the update doesn't contain mode-relevant information.
// Deprecated: Use the per-category extraction in mapACPSessionUpdate instead.
func mapACPConfigOptionUpdate(cu *acp.SessionConfigOptionUpdate) *ConfigOptionState {
	if cu == nil || len(cu.ConfigOptions) == 0 {
		return nil
	}

	// Look for mode-relevant config options
	for _, opt := range cu.ConfigOptions {
		// SessionConfigOption is a union: .Select or .Boolean
		if opt.Select == nil {
			continue
		}
		sel := opt.Select

		// Check if this is a mode-related config option
		if sel.Category != nil && *sel.Category == acp.SessionConfigOptionCategoryMode {
			configState := &ConfigOptionState{
				ConfigID:  string(sel.Id),
				CurrentID: string(sel.CurrentValue),
			}

			optDef := ConfigOptionDef{
				ID:       string(sel.Id),
				Name:     sel.Name,
				Category: "mode",
			}

			// Extract option values from SessionConfigSelectOptions
			mapACPSelectOptions(sel.Options, &optDef)

			configState.Options = append(configState.Options, optDef)
			return configState
		}
	}

	return nil
}

// mapACPSelectOptions extracts ConfigOptionValue entries from ACP SessionConfigSelectOptions.
func mapACPSelectOptions(opts acp.SessionConfigSelectOptions, optDef *ConfigOptionDef) {
	if opts.Ungrouped != nil {
		for _, v := range *opts.Ungrouped {
			optDef.Values = append(optDef.Values, ConfigOptionValue{
				ID:   string(v.Value),
				Name: v.Name,
			})
		}
	}
	if opts.Grouped != nil {
		for _, g := range *opts.Grouped {
			for _, v := range g.Options {
				optDef.Values = append(optDef.Values, ConfigOptionValue{
					ID:   string(v.Value),
					Name: v.Name,
				})
			}
		}
	}
}

// extractACPModeState extracts ModeState from an ACP NewSessionResponse.
// Returns nil if no modes are available.
func extractACPModeState(sessResp *acp.NewSessionResponse) *ModeState {
	if sessResp == nil || sessResp.Modes == nil {
		return nil
	}

	modeState := &ModeState{
		CurrentModeID: string(sessResp.Modes.CurrentModeId),
	}

	for _, m := range sessResp.Modes.AvailableModes {
		modeState.AvailableModes = append(modeState.AvailableModes, ModeDef{
			ID:   string(m.Id),
			Name: m.Name,
		})
	}

	if len(modeState.AvailableModes) == 0 && modeState.CurrentModeID == "" {
		return nil
	}

	return modeState
}

// extractACPConfigOptions extracts mode-relevant ConfigOptionState from an ACP NewSessionResponse.
// Returns nil if no mode-relevant config options are available.
func extractACPConfigOptions(sessResp *acp.NewSessionResponse) *ConfigOptionState {
	if sessResp == nil || len(sessResp.ConfigOptions) == 0 {
		return nil
	}

	// Find the "mode" config option
	for _, opt := range sessResp.ConfigOptions {
		if opt.Select == nil {
			continue
		}
		sel := opt.Select

		// Check if this is a mode-related config option
		if sel.Category != nil && *sel.Category == acp.SessionConfigOptionCategoryMode {
			configState := &ConfigOptionState{
				ConfigID:  string(sel.Id),
				CurrentID: string(sel.CurrentValue),
			}

			optDef := ConfigOptionDef{
				ID:       string(sel.Id),
				Name:     sel.Name,
				Category: "mode",
			}

			mapACPSelectOptions(sel.Options, &optDef)

			configState.Options = append(configState.Options, optDef)
			return configState
		}
	}

	return nil
}

// extractACPThinkingEffort extracts ThinkingEffortState from an ACP NewSessionResponse.
// Looks for config options with Category "thought_level". Returns nil if none found.
func extractACPThinkingEffort(sessResp *acp.NewSessionResponse) *ThinkingEffortState {
	if sessResp == nil || len(sessResp.ConfigOptions) == 0 {
		return nil
	}

	for _, opt := range sessResp.ConfigOptions {
		if opt.Select == nil {
			continue
		}
		sel := opt.Select

		if sel.Category != nil && *sel.Category == acp.SessionConfigOptionCategoryThoughtLevel {
			return buildThinkingEffortStateFromSelect(sel)
		}
	}

	return nil
}

// forwardACPEvent sends a StreamEvent to the channel with non-blocking send.
// Used by ACP event mapping to avoid blocking the SDK's internal goroutine.
func forwardACPEvent(ch chan<- StreamEvent, event StreamEvent) {
	select {
	case ch <- event:
	default:
		// Channel full, drop event (same as CLIBackend pattern)
		slog.Warn("acp: stream channel full, dropping event", "type", event.Type)
	}
}
