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
// so that re-emitted WS events reflect the latest state.
func mapACPSessionUpdate(update acp.SessionUpdate, ch chan<- StreamEvent, ctx context.Context, conn *ACPConn, deb *toolCallDebouncer) { //nolint:gocognit,gocyclo,revive,unparam // ACP protocol has many event types, each branch is simple; ctx position follows ACP SDK convention; ctx reserved for future use
	// Extract backendID once for all downstream ACP event mapping.
	// conn.agent.Backend provides the backend identifier (e.g. "kimi", "claude").
	backendID := ""
	if conn != nil {
		backendID = conn.BackendID()
	}
	// Accumulate raw ACP notification payloads for debugging (ai_raw_responses).
	// Previously sent as raw_output StreamEvent through the channel, but this
	// consumed channel buffer space and caused content events to be dropped when
	// the channel was full (~27K drops/day on busy sessions). Now accumulated
	// directly on the ACPConn buffer instead.
	if conn != nil {
		if rawJSON, err := json.Marshal(update); err == nil {
			conn.AppendRawOutput(string(rawJSON))
		}
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
		// A tool call is starting — mark it in-flight so the stall watchdog
		// treats the agent as active while it runs the tool.
		if conn != nil {
			conn.SetToolInFlight(true)
		}
		tc := update.ToolCall
		// Flush any pending debounce batch for this tool ID before the new call.
		if deb != nil {
			deb.handleToolCall(*tc)
		}
		event := mapACPToolCall(*tc, backendID)
		forwardACPEvent(ch, event)

	case update.ToolCallUpdate != nil:
		tcu := update.ToolCallUpdate

		// Track in-flight tool state from the status regardless of debouncing,
		// so the stall watchdog knows whether a tool is still executing.
		if conn != nil && tcu.Status != nil {
			switch *tcu.Status {
			case acp.ToolCallStatusCompleted, acp.ToolCallStatusFailed:
				conn.SetToolInFlight(false)
			case acp.ToolCallStatusPending, acp.ToolCallStatusInProgress:
				conn.SetToolInFlight(true)
			}
		}

		// Debounce non-terminal ToolCallUpdate events to reduce WS traffic.
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
		// Update agent-level commands in registry.
		// For CodeBuddy: merge with any pre-scanned plugin commands (issue #383).
		// The first AvailableCommandsUpdate only has built-in commands (plugins not
		// loaded yet), so we must preserve pre-scanned commands that ACP doesn't include.
		//
		// MergeCommands(acpCommands=infos, pluginCommands=existing) puts ACP commands
		// first, then adds commands from `existing` (registry: may contain pre-scanned
		// plugin commands + prior ACP updates) that are not already in the ACP list.
		// This assumes CodeBuddy never removes previously-available commands from one
		// AvailableCommandsUpdate to the next — only adds new ones (plugin skills).
		if conn != nil {
			agentID := conn.AgentID()
			if agentID != "" {
				if isCodeBuddyBackend(conn.agent) {
					existing := GetAgentCapabilityRegistry().GetCommands(agentID)
					infos = MergeCommands(infos, existing)
				}
				GetAgentCapabilityRegistry().UpdateCommands(agentID, infos)
			}
		}
		forwardACPEvent(ch, StreamEvent{
			Type:     "commands_update",
			Commands: infos,
		})

	case update.CurrentModeUpdate != nil:
		// v1 mode update: only currentModeId; available modes were sent in session/new.
		// Update session-level current value and forward WS event so the frontend can reflect
		// agent-initiated mode changes. Only accept the mode if it's in availableModes
		// to filter out invalid mode reports from bridge adapters.
		mu := update.CurrentModeUpdate
		modeID := string(mu.CurrentModeId)
		if conn != nil {
			if modeID != "" && !GetAgentCapabilityRegistry().IsOptionAvailable(conn.AgentID(), "mode", modeID) {
				// Agent reported a mode not in availableModes — likely a bridge adapter
				// artifact. Skip updating currentModeId but still update cache for
				// availableModes if needed.
				slog.Debug("acp: ignoring CurrentModeUpdate with unrecognized mode",
					"mode_id", modeID, "clawbench_sid", conn.clawbenchSID)
			} else {
				if conn.HasCurrentChanged("mode", modeID) {
					conn.UpdateCachedCurrent("mode", modeID)
					// Build mode state from registry + session current value
					if ms := GetAgentCapabilityRegistry().GetModeState(conn.AgentID(), modeID); ms != nil {
						forwardACPEvent(ch, StreamEvent{Type: "mode_update", Mode: ms})
					}
				} else {
					conn.UpdateCachedCurrent("mode", modeID)
				}
			}
		} else {
			forwardACPEvent(ch, StreamEvent{
				Type: "mode_update",
				Mode: &ModeState{CurrentModeID: modeID},
			})
		}

	case update.ConfigOptionUpdate != nil:
		// v2 config option update: extract mode, thought_level, and model options
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
				// Build SelectState and delegate to unified handler
				selState := buildSelectStateFromACPSelect(sel, "mode")
				// Also maintain ConfigOptionState for agent capability tracking
				configState := buildConfigOptionStateFromSelect(sel, "mode")
				handleConfigOptionSelect(selState, conn, ch)
				// Update agent-level config state in registry (used by REST API / agent list)
				if conn != nil && configState != nil {
					GetAgentCapabilityRegistry().UpdateConfigState(conn.AgentID(), configState)
				}

			case acp.SessionConfigOptionCategoryThoughtLevel:
				// Build SelectState and delegate to unified handler
				selState := buildSelectStateFromACPSelect(sel, "thought_level")
				handleConfigOptionSelect(selState, conn, ch)

			case acp.SessionConfigOptionCategoryModel:
				modelList := buildModelListStateFromSelect(sel)
				if modelList != nil {
					if conn != nil {
						agentID := conn.AgentID()
						reg := GetAgentCapabilityRegistry()
						// Diff-check: only forward WS if available models actually changed.
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

// handleConfigOptionSelect processes a ConfigOptionSelect update for a given
// category in a unified way. It handles diff-checking, registry updates,
// WS event forwarding, and session cache updates for both mode and
// thought_level categories.
//
// Returns the list of StreamEvent types that were forwarded (for testing).
// Category-specific behaviors:
//   - "mode": validates currentID against available modes (bridge adapter filter)
//   - "thought_level": conditionally updates cache only on change
func handleConfigOptionSelect(sel SelectState, conn *ACPConn, ch chan<- StreamEvent) []string {
	if sel.IsEmpty() {
		return nil
	}

	forwarded := make([]string, 0, 1)

	if conn == nil {
		// No connection → always forward
		forwardACPEvent(ch, buildStreamEventFromSelectState(sel))
		return []string{streamEventTypeFromCategory(sel.Category)}
	}

	agentID := conn.AgentID()
	reg := GetAgentCapabilityRegistry()

	newOpts := reg.HasNewAvailableOptions(agentID, sel.Category, sel.Available)
	currentChanged := conn.HasCurrentChanged(sel.Category, sel.CurrentID)

	if !newOpts && !currentChanged {
		return nil
	}

	// Update available options in registry only when options actually changed.
	if newOpts {
		reg.UpdateAvailableOptions(agentID, sel.Category, sel.Available)
	}

	// Forward WS event
	evt := buildStreamEventFromSelectState(sel)
	forwardACPEvent(ch, evt)
	forwarded = append(forwarded, streamEventTypeFromCategory(sel.Category))

	// Category-specific cache update logic
	switch sel.Category {
	case "mode":
		if newOpts {
			conn.UpdateCachedCurrent(sel.Category, sel.CurrentID)
		} else if currentChanged {
			// Validate mode change (bridge adapter filter)
			if sel.CurrentID != "" && !reg.IsOptionAvailable(agentID, "mode", sel.CurrentID) {
				slog.Debug("acp: ignoring ConfigOptionSelect with unrecognized mode",
					"mode_id", sel.CurrentID, "clawbench_sid", conn.clawbenchSID)
				return forwarded
			}
			conn.UpdateCachedCurrent(sel.Category, sel.CurrentID)
		}
	case "thought_level":
		// Only update session cache when the value actually changed,
		// to avoid overwriting the user's selection with the agent's default
		if currentChanged {
			conn.UpdateCachedCurrent(sel.Category, sel.CurrentID)
		}
	default:
		if newOpts || currentChanged {
			conn.UpdateCachedCurrent(sel.Category, sel.CurrentID)
		}
	}

	return forwarded
}

// buildStreamEventFromSelectState creates the appropriate StreamEvent for a
// SelectState based on its category. This preserves JSON API compatibility
// by using the domain-specific serialization types.
func buildStreamEventFromSelectState(sel SelectState) StreamEvent {
	switch sel.Category {
	case "mode":
		return StreamEvent{Type: "mode_update", Mode: sel.ToModeState()}
	case "thought_level":
		return StreamEvent{Type: "thinking_effort_update", ThinkingEffort: sel.ToThinkingEffortState()}
	default:
		// For unknown categories, use config_update as a generic carrier.
		// Populate Values from SelectState.Available so the client receives
		// the full option list, not just an empty ConfigOptionDef.
		values := make([]ConfigOptionValue, len(sel.Available))
		for i, opt := range sel.Available {
			values[i] = ConfigOptionValue(opt)
		}
		return StreamEvent{Type: "config_update", Config: &ConfigOptionState{
			ConfigID:  sel.Category,
			CurrentID: sel.CurrentID,
			Options: []ConfigOptionDef{{
				ID:       sel.Category,
				Category: sel.Category,
				Values:   values,
			}},
		}}
	}
}

// streamEventTypeFromCategory returns the WS event type string for a category.
func streamEventTypeFromCategory(category string) string {
	switch category {
	case "mode":
		return "mode_update"
	case "thought_level":
		return "thinking_effort_update"
	default:
		return "config_update"
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
