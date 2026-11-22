package ai

import acp "github.com/coder/acp-go-sdk"

// frameAttribution resolves one frame's attribution through the backend's hook,
// falling back to the generic parent-link decoder.
//
// The fallback matters: every backend that speaks plain ACP draws its sub-agent
// grouping from the standard `parentToolCallId` convention, and that path must
// keep working with no hook declared. Only a backend that carries attribution
// somewhere else (a member name instead of a link) needs the hook.
func frameAttribution(backendID string, conn *ACPConn, meta map[string]any) *FrameAttribution {
	if b := acpBehavior(backendID); b != nil && b.AttributeFrame != nil {
		return b.AttributeFrame(conn, meta)
	}
	if parent := extractParentToolCallID(backendID, meta); parent != "" {
		return &FrameAttribution{ParentToolCallID: parent}
	}
	return nil
}

// frameParentToolCallID is frameAttribution reduced to the parent link, for the
// call sites that only need that one field.
func frameParentToolCallID(backendID string, conn *ACPConn, meta map[string]any) string {
	if a := frameAttribution(backendID, conn, meta); a != nil {
		return a.ParentToolCallID
	}
	return ""
}

// frameMemberName is frameAttribution reduced to the member name.
func frameMemberName(backendID string, conn *ACPConn, meta map[string]any) string {
	if a := frameAttribution(backendID, conn, meta); a != nil {
		return a.MemberName
	}
	return ""
}

// frameMemberColor is frameAttribution reduced to the member colour.
func frameMemberColor(backendID string, conn *ACPConn, meta map[string]any) string {
	if a := frameAttribution(backendID, conn, meta); a != nil {
		return a.MemberColor
	}
	return ""
}

// isCompactionMeta reports whether a frame's _meta announces a compaction.
// Backends that report one through a private flag declare the hook; the shared
// loop learns nothing about the key.
func isCompactionMeta(backendID string, meta map[string]any) bool {
	if b := acpBehavior(backendID); b != nil && b.IsCompactionMeta != nil {
		return b.IsCompactionMeta(meta)
	}
	return false
}

// isCompactionCancelledMeta reports whether a frame's _meta announces a
// compaction that did not go through.
func isCompactionCancelledMeta(backendID string, meta map[string]any) bool {
	if b := acpBehavior(backendID); b != nil && b.IsCompactionCancelledMeta != nil {
		return b.IsCompactionCancelledMeta(meta)
	}
	return false
}

// steerBoundaryID returns the client user-message id a frame echoes for a
// mid-turn injection, or "" when the frame is not an injection echo.
func steerBoundaryID(backendID string, meta map[string]any) string {
	if b := acpBehavior(backendID); b != nil && b.SteerBoundaryID != nil {
		return b.SteerBoundaryID(meta)
	}
	return ""
}

// ExtractParentToolCallIDForHook is the standard parent-link decoder exposed for
// backend hooks. A backend implementing AttributeFrame wants the generic answer
// first (ordinary sub-agent content) and only falls back to its own join key for
// frames the wire leaves unlinked.
func ExtractParentToolCallIDForHook(meta map[string]any) string {
	return parentLinkWithoutBackend(meta)
}

// parentLinkWithoutBackend decodes the two known parent encodings without
// consulting a backend hook — used as the fallback inside hooks themselves, so
// a hook cannot recurse into its own dispatch.
func parentLinkWithoutBackend(meta map[string]any) string {
	if len(meta) == 0 {
		return ""
	}
	if v := metaString(meta[metaKeyClawBenchParentToolCallID]); v != "" {
		return v
	}
	if v := metaString(meta[metaKeyCodeBuddyParentToolCallID]); v != "" {
		return v
	}
	if ns, ok := meta["claudeCode"].(map[string]any); ok {
		return metaString(ns[metaKeyClaudeParentToolUseID])
	}
	return ""
}

// permissionAttribution resolves a permission request's team-member attribution
// through the backend's hook (nil when the backend declares none).
func permissionAttribution(backendID string, meta map[string]any) map[string]any {
	if b := acpBehavior(backendID); b != nil && b.PermissionMemberAttribution != nil {
		return b.PermissionMemberAttribution(meta)
	}
	return nil
}

// toolCallIDOf returns the string id of an ACP tool call.
func toolCallIDOf(id acp.ToolCallId) string { return string(id) }
