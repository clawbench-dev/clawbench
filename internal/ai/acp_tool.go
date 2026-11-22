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

// parseACPToolCall dispatches ACP ToolCall parsing to the backend's registered
// behavior hook, falling back to the generic mapper.
//
// The fallback is the intended path, not a leftover: a backend that speaks
// plain ACP (cursor, dsh, grok, …) declares nothing and works through the
// generic parser. Only a backend with a private wire shape registers its own.
func parseACPToolCall(backend string, tc acp.SessionUpdateToolCall) *ToolCall {
	if b := acpBehavior(backend); b != nil && b.ParseToolCall != nil {
		return b.ParseToolCall(tc)
	}
	return parseGenericACPToolCall(tc, backend)
}

// parseACPToolCallUpdate dispatches ACP ToolCallUpdate parsing to the backend's
// registered behavior hook, falling back to the generic mapper.
func parseACPToolCallUpdate(backend string, tcu acp.SessionToolCallUpdate) *ToolCall {
	if b := acpBehavior(backend); b != nil && b.ParseToolCallUpdate != nil {
		return b.ParseToolCallUpdate(tcu)
	}
	return parseGenericACPToolCallUpdate(tcu, backend)
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
func parseGenericACPToolCall(tc acp.SessionUpdateToolCall, backendID string) *ToolCall {
	tool := &ToolCall{
		Name: extractToolName(tc.Title, tc.Kind, backendID, string(tc.ToolCallId)),
		ID:   string(tc.ToolCallId),
		Done: false,
	}

	resolveACPToolInput(tc, tool, acpRemapsForBackend(backendID))
	return tool
}

// parseGenericACPToolCallUpdate creates a ToolCall from an ACP ToolCallUpdate
// using generic extraction logic.
func parseGenericACPToolCallUpdate(tcu acp.SessionToolCallUpdate, backendID string) *ToolCall {
	tool := &ToolCall{
		ID: string(tcu.ToolCallId),
	}

	mapToolCallStatus(tcu.Status.Ptr(), tool)
	mapToolCallInput(tcu, tool, backendID)
	mapToolCallName(tcu, tool, backendID)
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
// Falls back to GenericACPRemaps only when the backends package is not loaded
// (e.g., isolated test runs) — the same table the registry would have returned.
func acpRemapsForBackend(backendID string) map[string]string {
	if LookupACPRemapsFn != nil {
		return LookupACPRemapsFn(backendID)
	}
	return GenericACPRemaps
}

// mapACPToolCall creates a StreamEvent from an ACP ToolCall start event.
// Delegates to parseACPToolCall for per-agent routing.
func mapACPToolCall(tc acp.SessionUpdateToolCall, backendID string, conn *ACPConn) StreamEvent {
	tool := parseACPToolCall(backendID, tc)
	meta := MetaAny(tc.Meta)
	attr := frameAttribution(backendID, conn, meta)
	attachParentToolCallIDToTool(tool, attr)
	member, color := "", ""
	if attr != nil {
		member, color = attr.MemberName, attr.MemberColor
	}
	return StreamEvent{Type: "tool_use", Tool: tool, MemberName: member, MemberColor: color}
}

// mapACPToolCallUpdate creates a StreamEvent from an ACP ToolCallUpdate.
// Delegates to parseACPToolCallUpdate for per-agent routing.
func mapACPToolCallUpdate(tcu acp.SessionToolCallUpdate, backendID string, conn *ACPConn) StreamEvent {
	tool := parseACPToolCallUpdate(backendID, tcu)
	updateMeta := PatchMetaAny(tcu.Meta)
	attr := frameAttribution(backendID, conn, updateMeta)
	attachParentToolCallIDToTool(tool, attr)

	eventType := "tool_use"
	if tool.Done {
		eventType = "tool_result"
	}

	tcuContent, _ := tcu.Content.Value()
	slog.Debug("acp: tool_call_update", "tool_call_id", tool.ID, "done", tool.Done, "event_type", eventType, "has_output", tool.Output != "",
		"status", fmt.Sprintf("%v", tcu.Status.Ptr()), "content_count", len(tcuContent), "title", tcu.Title.Ptr(),
		"raw_input", fmt.Sprintf("%v", tcu.RawInput.Ptr()))

	member, color := "", ""
	if attr != nil {
		member, color = attr.MemberName, attr.MemberColor
	}
	return StreamEvent{Type: eventType, Tool: tool, MemberName: member, MemberColor: color}
}

// attachParentToolCallIDToTool stamps the sub-agent parent link (if any) onto a
// parsed tool call. Shared by the start/update mappers so the debouncer path
// (which also routes through mapACPToolCallUpdate) carries the link too.
//
// The link comes from the backend's frame-attribution hook: a backend whose
// members carry no parentToolCallId resolves one from its own join key there.
func attachParentToolCallIDToTool(tool *ToolCall, attr *FrameAttribution) {
	if tool == nil || attr == nil {
		return
	}
	if attr.ParentToolCallID != "" {
		tool.ParentToolCallID = attr.ParentToolCallID
	}
}
