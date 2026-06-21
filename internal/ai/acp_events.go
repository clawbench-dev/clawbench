package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	acp "github.com/coder/acp-go-sdk"
)

// mapACPSessionUpdate converts an ACP SessionUpdate to StreamEvent(s) and
// sends them to the stream channel. Called from ClawBenchACPClient.SessionUpdate,
// which runs on the SDK's internal goroutine.
// If conn is non-nil, mode/config/thinking cache updates are applied to the connection
// so that re-emitted SSE events reflect the latest state.
func mapACPSessionUpdate(update acp.SessionUpdate, ch chan<- StreamEvent, ctx context.Context, conn *ACPConn, deb *toolCallDebouncer) { //nolint:gocognit,gocyclo,revive,unparam // ACP protocol has many event types, each branch is simple; ctx position follows ACP SDK convention; ctx reserved for future use
	// Extract backendID once for all downstream ACP event mapping.
	// conn.agent.Backend provides the backend identifier (e.g. "kimi", "claude").
	backendID := ""
	if conn != nil {
		backendID = conn.BackendID()
	}
	// Emit raw_output event for each ACP notification so the handler can
	// persist the original protocol data to ai_raw_responses for debugging.
	// This mirrors how CLIBackend collects raw stdout lines.
	if rawJSON, err := json.Marshal(update); err == nil {
		forwardACPEvent(ch, StreamEvent{Type: "raw_output", RawOutput: string(rawJSON)})
	}

	switch {
	case update.AgentMessageChunk != nil:
		// When the agent transitions from thinking to content output, emit
		// thinking_done so the frontend can stop the thinking spinner immediately.
		forwardACPEvent(ch, StreamEvent{Type: "thinking_done"})
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
		// When the agent transitions from thinking to tool use, emit
		// thinking_done so the frontend can stop the thinking spinner.
		forwardACPEvent(ch, StreamEvent{Type: "thinking_done"})
		tc := update.ToolCall
		// Flush any pending debounce batch for this tool ID before the new call.
		if deb != nil {
			deb.handleToolCall(*tc)
		}
		event := mapACPToolCall(*tc, backendID)
		forwardACPEvent(ch, event)

	case update.ToolCallUpdate != nil:
		tcu := update.ToolCallUpdate

		// Debounce non-terminal ToolCallUpdate events to reduce SSE traffic.
		// ACP agents emit ToolCallUpdate deltas every ~30ms during tool input
		// streaming. Batching these into a single event per 50ms window cuts
		// the event rate by ~95% without losing any information.
		if deb != nil {
			buffered := deb.handleToolCallUpdate(*tcu)
			if buffered {
				// Event was buffered — check if it's a think tool completion
				// which needs immediate thinking_done forwarding.
				if tcu.Kind != nil && *tcu.Kind == acp.ToolKindThink && tcu.Status != nil {
					switch *tcu.Status {
					case acp.ToolCallStatusCompleted, acp.ToolCallStatusFailed:
						forwardACPEvent(ch, StreamEvent{Type: "thinking_done"})
					}
				}
				break
			}
			// Terminal event was already forwarded by the debouncer.
			// But we still need to emit thinking_done for think tools.
			if tcu.Kind != nil && *tcu.Kind == acp.ToolKindThink && tcu.Status != nil {
				switch *tcu.Status {
				case acp.ToolCallStatusCompleted, acp.ToolCallStatusFailed:
					forwardACPEvent(ch, StreamEvent{Type: "thinking_done"})
				}
			}
			break
		}

		// Fallback: no debouncer, forward directly (original behavior).
		event := mapACPToolCallUpdate(*tcu, backendID)
		forwardACPEvent(ch, event)

		// When a think tool completes, also emit thinking_done so the frontend
		// can stop the thinking spinner immediately — without this, the spinner
		// stays until the entire AI response finishes because thinking blocks
		// have no per-block "done" signal.
		if tcu.Kind != nil && *tcu.Kind == acp.ToolKindThink && tcu.Status != nil {
			switch *tcu.Status {
			case acp.ToolCallStatusCompleted, acp.ToolCallStatusFailed:
				forwardACPEvent(ch, StreamEvent{Type: "thinking_done"})
			}
		}
	case update.Plan != nil:
		entries := make([]PlanEntry, 0, len(update.Plan.Entries))
		for _, e := range update.Plan.Entries {
			entries = append(entries, PlanEntry{
				Content:  e.Content,
				Priority: string(e.Priority),
				Status:   string(e.Status),
			})
		}
		planState := &PlanState{Entries: entries}
		forwardACPEvent(ch, StreamEvent{Type: "plan_update", Plan: planState})
		if conn != nil {
			conn.SetCachedPlanState(planState)
		}

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
		// Update agent-level commands in registry
		if conn != nil {
			agentID := conn.AgentID()
			if agentID != "" {
				GetAgentCapabilityRegistry().UpdateCommands(agentID, infos)
			}
		}
		forwardACPEvent(ch, StreamEvent{
			Type:     "commands_update",
			Commands: infos,
		})

	case update.CurrentModeUpdate != nil:
		// v1 mode update: only currentModeId; available modes were sent in session/new.
		// Update session-level current value and forward SSE event so the frontend can reflect
		// agent-initiated mode changes. Only accept the mode if it's in availableModes
		// to filter out invalid mode reports from bridge adapters.
		mu := update.CurrentModeUpdate
		modeID := string(mu.CurrentModeId)
		if conn != nil {
			if modeID != "" && !GetAgentCapabilityRegistry().IsModeAvailable(conn.AgentID(), modeID) {
				// Agent reported a mode not in availableModes — likely a bridge adapter
				// artifact. Skip updating currentModeId but still update cache for
				// availableModes if needed.
				slog.Debug("acp: ignoring CurrentModeUpdate with unrecognized mode",
					"mode_id", modeID, "clawbench_sid", conn.clawbenchSID)
			} else {
				if conn.HasCurrentModeChanged(modeID) {
					conn.UpdateCachedCurrentMode(modeID)
					// Build mode state from registry + session current value
					if ms := GetAgentCapabilityRegistry().GetModeState(conn.AgentID(), modeID); ms != nil {
						forwardACPEvent(ch, StreamEvent{Type: "mode_update", Mode: ms})
					}
				} else {
					conn.UpdateCachedCurrentMode(modeID)
				}
			}
		} else {
			forwardACPEvent(ch, StreamEvent{
				Type: "mode_update",
				Mode: &ModeState{CurrentModeID: modeID},
			})
		}

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
				if conn != nil {
					derived := modeStateFromConfigState(configState)
					agentID := conn.AgentID()
					reg := GetAgentCapabilityRegistry()
					newModes := derived != nil && reg.HasNewAvailableModes(agentID, derived.AvailableModes)
					modeChanged := derived != nil && conn.HasCurrentModeChanged(derived.CurrentModeID)
					// Forward config_update SSE if available modes or currentModeId changed.
					if newModes || modeChanged {
						forwardACPEvent(ch, StreamEvent{Type: "config_update", Config: configState})
					}
					// Update agent-level config state in registry
					reg.UpdateConfigState(agentID, configState)
					if derived != nil {
						if newModes {
							// Available modes changed — update registry
							reg.UpdateModes(agentID, derived.AvailableModes)
							// Also update session current mode
							conn.UpdateCachedCurrentMode(derived.CurrentModeID)
						} else if modeChanged {
							// Only currentModeId changed — validate before updating cache.
							// Only accept the mode if it's in availableModes to filter out
							// invalid mode reports from bridge adapters.
							if derived.CurrentModeID != "" && !reg.IsModeAvailable(agentID, derived.CurrentModeID) {
								slog.Debug("acp: ignoring ConfigOptionUpdate with unrecognized mode",
									"mode_id", derived.CurrentModeID, "clawbench_sid", conn.clawbenchSID)
							} else {
								conn.UpdateCachedCurrentMode(derived.CurrentModeID)
							}
						}
					}
				} else {
					forwardACPEvent(ch, StreamEvent{Type: "config_update", Config: configState})
				}

			case acp.SessionConfigOptionCategoryThoughtLevel:
				effortState := buildThinkingEffortStateFromSelect(sel)
				if effortState != nil {
					if conn != nil {
						agentID := conn.AgentID()
						reg := GetAgentCapabilityRegistry()
						// Diff-check: only forward SSE if available levels actually changed.
						if reg.HasNewAvailableThinkingEfforts(agentID, effortState.AvailableLevels) {
							// Update agent-level thinking efforts in registry
							reg.UpdateThinkingEfforts(agentID, effortState.AvailableLevels)
							forwardACPEvent(ch, StreamEvent{Type: "thinking_effort_update", ThinkingEffort: effortState})
						}
						conn.UpdateCachedCurrentThinkingEffort(string(sel.CurrentValue))
					} else {
						forwardACPEvent(ch, StreamEvent{Type: "thinking_effort_update", ThinkingEffort: effortState})
					}
				}

			case acp.SessionConfigOptionCategoryModel:
				modelList := buildModelListStateFromSelect(sel)
				if modelList != nil {
					if conn != nil {
						agentID := conn.AgentID()
						reg := GetAgentCapabilityRegistry()
						// Diff-check: only forward SSE if available models actually changed.
						if reg.HasNewAvailableModels(agentID, modelList.Models) {
							// Update agent-level models in registry
							reg.UpdateModels(agentID, modelList.Models)
							forwardACPEvent(ch, StreamEvent{Type: "model_list_update", ModelList: modelList})
						}
						conn.SetCachedModelListState(modelList)
					} else {
						forwardACPEvent(ch, StreamEvent{Type: "model_list_update", ModelList: modelList})
					}
				}
			}
		}

	case update.SessionInfoUpdate != nil:
		slog.Debug("acp: session info update")

	case update.UsageUpdate != nil:
		usageState := &UsageState{
			Used: update.UsageUpdate.Used,
			Size: update.UsageUpdate.Size,
		}
		if update.UsageUpdate.Cost != nil {
			usageState.Cost = update.UsageUpdate.Cost.Amount
			usageState.Currency = update.UsageUpdate.Cost.Currency
		}
		forwardACPEvent(ch, StreamEvent{Type: "usage_update", Usage: usageState})
		if conn != nil {
			conn.SetCachedUsageState(usageState)
		}
	}
}

// extractInputFromContent extracts tool input parameters from ACP Content blocks.
// Terminal content blocks contain the command being executed; text content blocks
// may contain the description or command text.
func extractInputFromContent(tc acp.SessionUpdateToolCall) map[string]any {
	input := make(map[string]any)
	for _, c := range tc.Content {
		if c.Terminal != nil {
			// Terminal content — the command text is typically in the title
			// For Terminal/Bash tools, use the tool call title as the command
			if tc.Title != "" {
				input["command"] = tc.Title
			}
			return input
		}
		if c.Content != nil {
			// Text content block — extract text as description
			cb := c.Content.Content
			if cb.Text != nil && cb.Text.Text != "" {
				input["description"] = cb.Text.Text
			}
		}
	}
	if len(input) == 0 {
		return nil
	}
	return input
}

// extractInputFromContentUpdate extracts tool input from Content in tool_call_update events.
// Same logic as extractInputFromContent but works with SessionToolCallUpdate (Title is *string).
func extractInputFromContentUpdate(tcu acp.SessionToolCallUpdate) map[string]any {
	input := make(map[string]any)
	for _, c := range tcu.Content {
		if c.Terminal != nil {
			// Terminal content — use title as command
			if tcu.Title != nil && *tcu.Title != "" {
				input["command"] = *tcu.Title
			}
			return input
		}
		if c.Content != nil {
			cb := c.Content.Content
			if cb.Text != nil && cb.Text.Text != "" {
				input["description"] = cb.Text.Text
			}
		}
	}
	if len(input) == 0 {
		return nil
	}
	return input
}

// extractInputFromLocationsAndTitle extracts tool input from ACP locations and title fields.
// Kimi ACP sends file paths in `locations` (for read-kind tools) and search targets in `title`
// (for search-kind tools) instead of `rawInput`. Without this extraction, the frontend shows
// empty tool bars with no summary text.
//
// Mapping logic:
//   - kind=read + locations → {"file_path": locations[0].path}
//   - kind=search + toolCallId prefix "glob-" → {"pattern": title}
//   - kind=search + toolCallId prefix "list_directory-" → {"path": title}
//   - kind=search + other → {"path": title} (generic search)
func extractInputFromLocationsAndTitle(locations []acp.ToolCallLocation, title string, kind acp.ToolKind, toolCallID string) map[string]any {
	input := make(map[string]any)

	switch kind {
	case acp.ToolKindRead:
		// Read tools: extract file_path from locations (Kimi pattern)
		if len(locations) > 0 {
			input["file_path"] = locations[0].Path
		} else if title != "" {
			// Fallback: title is the file name/path
			input["file_path"] = title
		}
	case acp.ToolKindSearch:
		// Search tools: determine glob vs list_directory from toolCallID prefix
		prefix := ""
		if dashIdx := strings.Index(toolCallID, "-"); dashIdx > 0 {
			prefix = toolCallID[:dashIdx]
		}
		switch prefix {
		case "glob":
			if title != "" {
				input["pattern"] = title
			}
		case "list_directory", "search_directory":
			if title != "" {
				input["path"] = title
			}
		default:
			// Generic search: use title as path/query
			if title != "" {
				input["path"] = title
			}
		}
	case acp.ToolKindEdit:
		// Edit tools: extract file_path from locations
		if len(locations) > 0 {
			input["file_path"] = locations[0].Path
		}
	}

	if len(input) == 0 {
		return nil
	}
	return input
}

// mapToolCallStatus sets the tool's Done and Status fields based on ACP status.
func mapToolCallStatus(status *acp.ToolCallStatus, tool *ToolCall) {
	if status == nil {
		return
	}
	switch *status {
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

// mapToolCallInput extracts tool input from RawInput, Content, or Locations/Title.
func mapToolCallInput(tcu acp.SessionToolCallUpdate, tool *ToolCall, backendID string) {
	if tcu.RawInput != nil {
		if inputBytes, err := json.Marshal(tcu.RawInput); err == nil && string(inputBytes) != "{}" {
			remaps := acpRemapsForBackend(backendID)
			normalized, normErr := normalizeToolInput(inputBytes, remaps)
			if normErr == nil {
				tool.Input = string(normalized)
			} else {
				tool.Input = string(inputBytes)
			}
		}
		return
	}

	// For execute-kind tools without RawInput, try title as command (Kimi CLI)
	// or Content terminal blocks. Do NOT extract text Content as description —
	// that carries output, not input.
	if tcu.Kind != nil && *tcu.Kind == acp.ToolKindExecute {
		mapToolCallInputFromExecute(tcu, tool)
		return
	}

	// Kimi ACP: extract input from locations for read/search tools.
	mapToolCallInputFromLocations(tcu, tool)
}

// mapToolCallInputFromExecute handles input extraction for execute-kind tools.
func mapToolCallInputFromExecute(tcu acp.SessionToolCallUpdate, tool *ToolCall) {
	if tcu.Title != nil && *tcu.Title != "" {
		input := map[string]any{"command": *tcu.Title}
		if inputBytes, err := json.Marshal(input); err == nil {
			tool.Input = string(inputBytes)
		}
		return
	}
	// Check for terminal content blocks (rare, but some agents may use them)
	for _, c := range tcu.Content {
		if c.Terminal != nil {
			input := make(map[string]any)
			if tcu.Title != nil && *tcu.Title != "" {
				input["command"] = *tcu.Title
			}
			if inputBytes, err := json.Marshal(input); err == nil && len(input) > 0 {
				tool.Input = string(inputBytes)
			}
			break
		}
	}
}

// mapToolCallInputFromLocations extracts input from locations/title for read/search tools.
func mapToolCallInputFromLocations(tcu acp.SessionToolCallUpdate, tool *ToolCall) {
	if tool.Input != "" {
		return
	}
	title := ""
	if tcu.Title != nil {
		title = *tcu.Title
	}
	kind := acp.ToolKindOther
	if tcu.Kind != nil {
		kind = *tcu.Kind
	}
	if input := extractInputFromLocationsAndTitle(tcu.Locations, title, kind, string(tcu.ToolCallId)); input != nil {
		if inputBytes, err := json.Marshal(input); err == nil {
			tool.Input = string(inputBytes)
		}
	}
}

// mapToolCallName sets the tool name from title when the tool is not yet done.
func mapToolCallName(tcu acp.SessionToolCallUpdate, tool *ToolCall, backendID string) {
	if tcu.Title == nil || *tcu.Title == "" || tool.Done {
		return
	}
	// If tool already has a recognized canonical name (from the initial ToolCall
	// event), don't let a later ToolCallUpdate with a different title overwrite
	// it. ACP agents send progressive title updates (e.g., "Agent" → "Explore
	// project structure"), and extractToolName would return "Explore" which has
	// no frontend icon mapping — causing a fallback wrench icon. Keep the
	// original canonical name; the frontend uses input.subagent_type to display
	// the sub-agent's specific name.
	if tool.Name != "" && tool.Name != strings.ToLower(tool.Name) {
		return
	}
	kind := acp.ToolKindExecute // default kind for title-based name extraction
	if tcu.Kind != nil {
		kind = *tcu.Kind
	}
	tool.Name = extractToolName(*tcu.Title, kind, backendID, string(tcu.ToolCallId))
}

// mapToolCallOutput extracts human-readable output from RawOutput or Content blocks.
func mapToolCallOutput(tcu acp.SessionToolCallUpdate, tool *ToolCall) {
	if tcu.RawOutput != nil {
		tool.Output = truncateToolOutput(extractACPToolOutput(tcu.RawOutput))
	} else if len(tcu.Content) > 0 {
		tool.Output = truncateToolOutput(extractACPToolOutputFromContent(tcu.Content))
	}
}

// extractACPToolOutputFromContent extracts human-readable output text from ACP
// Content blocks. Kimi ACP sends tool results in Content blocks (text, terminal)
// instead of RawOutput. This function joins text from all content blocks into a
// single string, similar to how extractACPToolOutput works for RawOutput.
func extractACPToolOutputFromContent(contents []acp.ToolCallContent) string {
	var parts []string
	for _, c := range contents {
		if c.Content != nil {
			cb := c.Content.Content
			if cb.Text != nil && cb.Text.Text != "" {
				parts = append(parts, cb.Text.Text)
			}
		}
		// Terminal content is streamed to the terminal widget — no text to extract
	}
	return strings.Join(parts, "\n")
}

// extractACPToolOutput converts ACP RawOutput (any) to a human-readable string.
// ACP agents return structured output (e.g. map[string]any{"result": "file contents"}),
// but the frontend expects plain text like CLI mode produces. This function extracts
// the text content from known keys and falls back to pretty-printed JSON.
func extractACPToolOutput(rawOutput any) string {
	// Direct string — already human-readable
	if s, ok := rawOutput.(string); ok {
		return s
	}

	// Boolean or number — convert directly
	switch v := rawOutput.(type) {
	case bool:
		return fmt.Sprintf("%v", v)
	case float64, float32, int, int64, int32:
		return fmt.Sprintf("%v", v)
	}

	// Map — try known content keys to extract text
	if m, ok := rawOutput.(map[string]any); ok {
		return extractMapOutput(m)
	}

	// Array — join string elements or pretty-print
	if arr, ok := rawOutput.([]any); ok {
		return extractArrayOutput(arr)
	}

	// Fallback: pretty-print as JSON
	if bytes, err := json.MarshalIndent(rawOutput, "", "  "); err == nil {
		return string(bytes)
	}
	return fmt.Sprintf("%v", rawOutput)
}

// acpOutputKeyPriority defines the order of keys to try when extracting text
// from a map[string]any tool output. Earlier keys take priority.
var acpOutputKeyPriority = []string{
	"result",  // Most common: {"result": "file contents"}
	"output",  // {"output": "command output"}
	"content", // {"content": "file content"}
	"text",    // {"text": "plain text"}
	"message", // {"message": "success"}
	"stdout",  // Bash-like: {"stdout": "...", "stderr": "..."}
}

// extractMapOutput extracts human-readable text from a map output.
func extractMapOutput(m map[string]any) string { //nolint:gocognit,gocyclo // many output format branches, each is trivial
	// Try known content keys in priority order
	for _, key := range acpOutputKeyPriority {
		if val, ok := m[key]; ok && val != nil {
			switch v := val.(type) {
			case string:
				if v != "" {
					// For Bash-like stdout, also append stderr if present
					if key == "stdout" {
						if stderr, ok2 := m["stderr"]; ok2 {
							if s, ok3 := stderr.(string); ok3 && s != "" {
								return v + "\n" + s
							}
						}
					}
					return v
				}
			case map[string]any, []any:
				// Nested structure — pretty-print it
				if bytes, err := json.MarshalIndent(v, "", "  "); err == nil {
					return string(bytes)
				}
			default:
				if fmt.Sprintf("%v", v) != "" {
					return fmt.Sprintf("%v", v)
				}
			}
		}
	}

	// Try "error" key for failed tools
	if errVal, ok := m["error"]; ok && errVal != nil {
		switch v := errVal.(type) {
		case string:
			return v
		case map[string]any:
			if msg, ok2 := v["message"]; ok2 {
				return fmt.Sprintf("%v", msg)
			}
		}
		return fmt.Sprintf("%v", errVal)
	}

	// No known key — pretty-print entire object
	if bytes, err := json.MarshalIndent(m, "", "  "); err == nil {
		return string(bytes)
	}
	return fmt.Sprintf("%v", m)
}

// extractArrayOutput extracts human-readable text from an array output.
func extractArrayOutput(arr []any) string {
	// If all elements are strings, join them
	allStrings := true
	var parts []string
	for _, elem := range arr {
		if s, ok := elem.(string); ok {
			parts = append(parts, s)
		} else {
			allStrings = false
			break
		}
	}
	if allStrings && len(parts) > 0 {
		return strings.Join(parts, "\n")
	}

	// Fallback: pretty-print as JSON
	if bytes, err := json.MarshalIndent(arr, "", "  "); err == nil {
		return string(bytes)
	}
	return fmt.Sprintf("%v", arr)
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
		Type:   "error",
		Error:  fmt.Sprintf("ACP error %d: %s", code, message),
		Reason: reason,
	}
}

// forwardACPEvent sends a StreamEvent to the channel with non-blocking send.
// Used by ACP event mapping to avoid blocking the SDK's internal goroutine.
// Recovers from send-on-closed-channel: the ACP SDK's internal goroutines may
// outlive the channel close in ExecuteStream (e.g., on context cancellation),
// so a panic from sending to a closed channel is safe to ignore.
func forwardACPEvent(ch chan<- StreamEvent, event StreamEvent) {
	defer func() {
		if r := recover(); r != nil {
			slog.Debug("acp: send on closed stream channel, ignoring", "type", event.Type)
		}
	}()
	select {
	case ch <- event:
	default:
		// Channel full, drop event (same as CLIBackend pattern)
		slog.Warn("acp: stream channel full, dropping event", "type", event.Type)
	}
}

// MapACPSessionUpdateForTest exports mapACPSessionUpdate for use in handler-level
// tests that verify LoadSession replay parsing. Production code must not use this.
func MapACPSessionUpdateForTest(update acp.SessionUpdate, ch chan<- StreamEvent) {
	mapACPSessionUpdate(update, ch, nil, nil, nil)
}
