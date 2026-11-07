package ai

import (
	"strings"

	acp "github.com/coder/acp-go-sdk"
)

// ---------------------------------------------------------------------------
// OpenCode v2 sub-agent attribution
// ---------------------------------------------------------------------------
//
// OpenCode v2 runs the `subagent` tool in a child session. The child's frames
// are projected into the parent session's stream with only
// `_meta["opencode/child-session"]` = {id, parentID, depth, title}; there is no
// parent tool-call id on the wire.
//
// The probe in opencode_acp_child_mapping_probe_integration_test.go established:
//   - child.title == the subagent tool call's input.description (foreground),
//   - the tool result carries rawOutput.metadata.sessionID == child.id,
//   - foreground: child frames arrive BEFORE the result (title is the only live
//     signal; the exact id arrives too late),
//   - background: child frames arrive on a separate channel after the parent
//     turn (not handled here — no active route by then).
//
// So: remember (session, description) → toolCallID when the subagent call is
// seen, and childSessionID → toolCallID from the tool result. A child frame is
// attributed by exact child id first, then by title. Unattributed child frames
// are dropped (no main-timeline pollution).

// noteOpenCodeSubagentCall records attribution data from a `subagent` tool call
// or one of its updates. rootSessionID is the ACP session id the frame arrived
// under; a nested subagent call overrides it with its own child session id.
func (c *ClawBenchACPClient) noteOpenCodeSubagentCall(rootSessionID string, update acp.SessionUpdate) {
	callID, title, rawInput, rawOutput := openCodeToolCallFields(update)
	if callID == "" || !looksLikeOpenCodeSubagent(title, rawInput) {
		return
	}
	sessionKey := rootSessionID
	if childID := openCodeChildSessionID(update); childID != "" {
		sessionKey = childID
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if desc := rawInputString(rawInput, "description"); desc != "" {
		c.subagentByTitle[subagentTitleKey(sessionKey, desc)] = callID
	}
	if sid := rawOutputSessionID(rawOutput); sid != "" {
		c.subagentByChild[sid] = callID
	}
}

// resolveOpenCodeChildParent returns the tool call id that spawned the child
// session this update came from, or "" when it cannot be attributed yet.
func (c *ClawBenchACPClient) resolveOpenCodeChildParent(update acp.SessionUpdate) string {
	meta := acpUpdateMeta(update)
	ns, _ := meta[metaKeyOpenCodeChildSession].(map[string]any)
	if ns == nil {
		return ""
	}
	childID := metaString(ns["id"])
	parentSessionID := metaString(ns["parentID"])
	title := metaString(ns["title"])

	c.mu.Lock()
	defer c.mu.Unlock()
	if parent := c.subagentByChild[childID]; parent != "" {
		return parent
	}
	if title != "" {
		return c.subagentByTitle[subagentTitleKey(parentSessionID, title)]
	}
	return ""
}

// openCodeToolCallFields pulls the fields shared by tool_call and
// tool_call_update frames.
func openCodeToolCallFields(update acp.SessionUpdate) (id, title string, rawInput, rawOutput any) {
	switch {
	case update.ToolCall != nil:
		return string(update.ToolCall.ToolCallId), update.ToolCall.Title, update.ToolCall.RawInput, update.ToolCall.RawOutput
	case update.ToolCallUpdate != nil:
		if update.ToolCallUpdate.Title != nil {
			title = *update.ToolCallUpdate.Title
		}
		return string(update.ToolCallUpdate.ToolCallId), title, update.ToolCallUpdate.RawInput, update.ToolCallUpdate.RawOutput
	}
	return "", "", nil, nil
}

// looksLikeOpenCodeSubagent reports whether a tool call is the delegation tool.
// The title is "subagent"; the input shape (agent + description + prompt) is the
// fallback when an agent renames the title.
func looksLikeOpenCodeSubagent(title string, rawInput any) bool {
	l := strings.ToLower(title)
	if strings.Contains(l, "subagent") || strings.Contains(l, "sub agent") {
		return true
	}
	m, _ := rawInput.(map[string]any)
	if m == nil {
		return false
	}
	_, hasAgent := m["agent"]
	_, hasDesc := m["description"]
	_, hasPrompt := m["prompt"]
	return hasAgent && hasDesc && hasPrompt
}

// openCodeChildSessionID returns the child session id carried by a child frame.
func openCodeChildSessionID(update acp.SessionUpdate) string {
	meta := acpUpdateMeta(update)
	ns, _ := meta[metaKeyOpenCodeChildSession].(map[string]any)
	return metaString(ns["id"])
}

func rawInputString(raw any, key string) string {
	m, _ := raw.(map[string]any)
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

// rawOutputSessionID extracts rawOutput.metadata.sessionID from a completed
// subagent tool call (the exact child session id).
func rawOutputSessionID(raw any) string {
	m, _ := raw.(map[string]any)
	if m == nil {
		return ""
	}
	meta, _ := m["metadata"].(map[string]any)
	if meta == nil {
		return ""
	}
	s, _ := meta["sessionID"].(string)
	return s
}

func subagentTitleKey(sessionID, title string) string {
	return sessionID + "\x00" + title
}
