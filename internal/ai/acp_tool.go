package ai

import (
	"encoding/json"
	"fmt"
	"log/slog"

	acp "github.com/coder/acp-go-sdk"
)

// ---------------------------------------------------------------------------
// ACP tool call routing — dispatches to per-agent parsing functions
// ---------------------------------------------------------------------------

// parseACPToolCall dispatches ACP ToolCall parsing to the appropriate per-agent
// function based on the backend identifier (e.g. "claude", "codebuddy").
// Backends not listed here (e.g., "mimo", "cline", "copilot", "vecli")
// fall through to parseGenericACPToolCall.
func parseACPToolCall(backend string, tc acp.SessionUpdateToolCall) *ToolCall {
	switch backend {
	case "claude", "qoder":
		return parseClaudeACPToolCall(tc)
	case "codebuddy":
		return parseCodeBuddyACPToolCall(tc)
	case "opencode":
		return parseOpenCodeACPToolCall(tc)
	case "kimi":
		return parseKimiACPToolCall(tc)
	default:
		return parseGenericACPToolCall(tc)
	}
}

// parseACPToolCallUpdate dispatches ACP ToolCallUpdate parsing to the appropriate
// per-agent function based on the backend identifier.
// Backends not listed here fall through to parseGenericACPToolCallUpdate.
func parseACPToolCallUpdate(backend string, tcu acp.SessionToolCallUpdate) *ToolCall {
	switch backend {
	case "claude", "qoder":
		return parseClaudeACPToolCallUpdate(tcu)
	case "codebuddy":
		return parseCodeBuddyACPToolCallUpdate(tcu)
	case "opencode":
		return parseOpenCodeACPToolCallUpdate(tcu)
	case "kimi":
		return parseKimiACPToolCallUpdate(tcu)
	default:
		return parseGenericACPToolCallUpdate(tcu)
	}
}

// ---------------------------------------------------------------------------
// _meta extraction utilities
// ---------------------------------------------------------------------------

// extractMetaToolName extracts the tool name from a nested _meta namespace.
// Used by Claude ACP: _meta.claudeCode.toolName → "Edit"
func extractMetaToolName(meta map[string]any) string {
	if meta == nil {
		return ""
	}
	ns, ok := meta["claudeCode"]
	if !ok {
		return ""
	}
	m, ok := ns.(map[string]any)
	if !ok {
		return ""
	}
	name, _ := m["toolName"].(string)
	return name
}

// extractMetaToolNameFlat extracts the tool name from a top-level _meta key.
// Used by CodeBuddy ACP: _meta["codebuddy.ai/toolName"] → "Bash"
func extractMetaToolNameFlat(meta map[string]any) string {
	if meta == nil {
		return ""
	}
	name, _ := meta["codebuddy.ai/toolName"].(string)
	return name
}

// ---------------------------------------------------------------------------
// Shared input resolution for ACP ToolCall events
// ---------------------------------------------------------------------------

// resolveACPToolInput populates tool.Input using the standard fallback chain:
//  1. RawInput → normalizeToolInput with the given remaps
//  2. Content → extractInputFromContent
//  3. Execute-kind title → {"command": title}
//  4. Locations + title → extractInputFromLocationsAndTitle
//
// Per-agent functions only need to set the tool name and choose remaps;
// this function handles the input extraction uniformly.
func resolveACPToolInput(tc acp.SessionUpdateToolCall, tool *ToolCall, remaps map[string]string) {
	if tc.RawInput != nil {
		if inputBytes, err := json.Marshal(tc.RawInput); err == nil {
			normalized, normErr := normalizeToolInput(inputBytes, remaps)
			if normErr == nil {
				tool.Input = string(normalized)
			} else {
				tool.Input = string(inputBytes)
			}
		}
		return
	}

	if len(tc.Content) > 0 {
		input := extractInputFromContent(tc)
		if input != nil {
			if inputBytes, err := json.Marshal(input); err == nil {
				tool.Input = string(inputBytes)
			}
		}
		return
	}

	// Fallback: for execute-kind tools with no input, use title as command
	if tool.Input == "" && tc.Kind == acp.ToolKindExecute && tc.Title != "" {
		input := map[string]any{"command": tc.Title}
		if inputBytes, err := json.Marshal(input); err == nil {
			tool.Input = string(inputBytes)
		}
	}

	// Fallback: extract input from locations and title
	if tool.Input == "" {
		if input := extractInputFromLocationsAndTitle(tc.Locations, tc.Title, tc.Kind, string(tc.ToolCallId)); input != nil {
			if inputBytes, err := json.Marshal(input); err == nil {
				tool.Input = string(inputBytes)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Generic ACP fallback — works for any agent without specific extraction logic
// ---------------------------------------------------------------------------

// parseGenericACPToolCall creates a ToolCall from an ACP ToolCall using generic
// extraction logic (title/kind/toolCallId prefix for name, RawInput for input,
// Content/Locations/Title fallbacks for input inference).
// Does not attempt _meta extraction — per-agent functions handle their own _meta formats.
func parseGenericACPToolCall(tc acp.SessionUpdateToolCall) *ToolCall {
	tool := &ToolCall{
		Name: extractToolName(tc.Title, tc.Kind, "", string(tc.ToolCallId)),
		ID:   string(tc.ToolCallId),
		Done: false,
	}

	resolveACPToolInput(tc, tool, getRemaps("generic_acp"))
	return tool
}

// parseGenericACPToolCallUpdate creates a ToolCall from an ACP ToolCallUpdate
// using generic extraction logic.
func parseGenericACPToolCallUpdate(tcu acp.SessionToolCallUpdate) *ToolCall {
	tool := &ToolCall{
		ID: string(tcu.ToolCallId),
	}

	mapToolCallStatus(tcu.Status, tool)
	mapToolCallInput(tcu, tool, "")
	mapToolCallName(tcu, tool, "")
	if tool.Done {
		mapToolCallOutput(tcu, tool)
	}

	return tool
}

// ---------------------------------------------------------------------------
// Shared acpRemapsForBackend and mapACP* wrappers
// ---------------------------------------------------------------------------

// acpRemapsForBackend returns the ACP input remapping map for the given backendID.
// Uses LookupACPRemapsFn (wired to backends.LookupACPRemaps) when available,
// which handles backend-specific remaps and generic fallback internally.
// Falls back to getRemaps("generic_acp") only when the backends package
// is not loaded (e.g., isolated test runs).
func acpRemapsForBackend(backendID string) map[string]string {
	if LookupACPRemapsFn != nil {
		return LookupACPRemapsFn(backendID)
	}
	return getRemaps("generic_acp")
}

// mapACPToolCall creates a StreamEvent from an ACP ToolCall start event.
// Delegates to parseACPToolCall for per-agent routing.
func mapACPToolCall(tc acp.SessionUpdateToolCall, backendID string) StreamEvent {
	tool := parseACPToolCall(backendID, tc)
	return StreamEvent{Type: "tool_use", Tool: tool}
}

// mapACPToolCallUpdate creates a StreamEvent from an ACP ToolCallUpdate.
// Delegates to parseACPToolCallUpdate for per-agent routing.
func mapACPToolCallUpdate(tcu acp.SessionToolCallUpdate, backendID string) StreamEvent {
	tool := parseACPToolCallUpdate(backendID, tcu)

	eventType := "tool_use"
	if tool.Done {
		eventType = "tool_result"
	}

	slog.Debug("acp: tool_call_update", "tool_call_id", tool.ID, "done", tool.Done, "event_type", eventType, "has_output", tool.Output != "",
		"status", fmt.Sprintf("%v", tcu.Status), "content_count", len(tcu.Content), "title", tcu.Title,
		"raw_input", fmt.Sprintf("%v", tcu.RawInput))

	return StreamEvent{Type: eventType, Tool: tool}
}
