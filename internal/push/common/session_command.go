package common

import (
	"fmt"
	"regexp"
	"strings"
)

// SessionCmdRe matches "@{8+hex-chars}" followed by optional message text.
// Allows 8+ hex chars so users can type longer prefixes for disambiguation.
// The (?s) flag makes "." match newlines so multi-line messages are preserved
// in full instead of being truncated at the first line break.
var SessionCmdRe = regexp.MustCompile(`(?s)^@([0-9a-fA-F]{8,})(?:\s|$)(.*)`)

// ParseSessionCommand parses the "@{shortID} message" format from push messages.
// Returns (shortID, message, true) if matched, or ("", "", false) if not.
func ParseSessionCommand(text string) (string, string, bool) {
	m := SessionCmdRe.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return "", "", false
	}
	return m[1], strings.TrimSpace(m[2]), true
}

// SessionListCommand is the command a user sends to list recent sessions.
// It replaces the old implicit reply (any message without an "@{shortID}"
// prefix listed sessions), which is now the sticky-session send path.
const SessionListCommand = "/ls"

// RouteKind identifies how an inbound IM message should be handled.
type RouteKind int

const (
	// RouteToSession targets a specific session. ShortID is set when the user
	// named one with "@{shortID}"; when empty the caller uses the sticky
	// session instead. Message is the text to send (may be empty for a bare
	// file/image).
	RouteToSession RouteKind = iota
	// RouteListSessions asks for the recent-session list ("/ls").
	RouteListSessions
	// RouteNoTarget means the user sent something routable but has no sticky
	// session yet. The caller must reply with the "/ls" hint.
	RouteNoTarget
)

// Route is the outcome of classifying an inbound IM message.
type Route struct {
	Kind    RouteKind
	ShortID string // set only for an explicit "@{shortID}" target
	Message string // text payload; empty for a bare file/image
}

// ClassifyIncoming routes an inbound text message from an IM user.
//
// Precedence: an explicit "@{shortID}" always wins; then the "/ls" command;
// then any other text goes to the sticky session (shortID left empty for the
// caller to resolve). An empty/whitespace-only text with no sticky session is
// RouteNoTarget — the same hint a bare file gets when nothing was ever chosen.
//
// stickySessionID is the caller's already-resolved sticky target ("" when
// none). It is passed in rather than looked up so this stays pure and testable.
func ClassifyIncoming(text, stickySessionID string) Route {
	if shortID, msg, ok := ParseSessionCommand(text); ok {
		return Route{Kind: RouteToSession, ShortID: shortID, Message: msg}
	}

	trimmed := strings.TrimSpace(text)
	if trimmed == SessionListCommand {
		return Route{Kind: RouteListSessions}
	}

	if stickySessionID == "" {
		return Route{Kind: RouteNoTarget}
	}
	return Route{Kind: RouteToSession, Message: trimmed}
}

// ResolveShortSessionID resolves an 8-char short session ID to a full session ID and title.
// It first checks running sessions, then falls back to all sessions.
// Matching is case-insensitive (UUIDs are lowercase in DB, user may type uppercase).
// Returns error on ambiguity (multiple matches) or not found.
func ResolveShortSessionID(messenger SessionMessenger, shortID string) (string, string, error) {
	if messenger == nil {
		return "", "", fmt.Errorf("session messenger not available")
	}

	// Priority 1: running sessions
	running, err := messenger.FindSessionsByPrefix(shortID, true)
	if err != nil {
		return "", "", fmt.Errorf("find running sessions: %w", err)
	}
	if len(running) > 1 {
		return "", "", fmt.Errorf("匹配到多个正在运行的会话，请使用更长的 ID（%s…）", shortID)
	}
	if len(running) == 1 {
		return running[0].ID, running[0].Title, nil
	}

	// Priority 2: all sessions
	all, err := messenger.FindSessionsByPrefix(shortID, false)
	if err != nil {
		return "", "", fmt.Errorf("find sessions: %w", err)
	}
	if len(all) > 1 {
		return "", "", fmt.Errorf("匹配到多个会话，请使用更长的 ID（%s…）", shortID)
	}
	if len(all) == 1 {
		return all[0].ID, all[0].Title, nil
	}

	return "", "", fmt.Errorf("未找到会话 %s", shortID)
}

// ResolveTarget resolves a Route to a concrete session.
//
// An explicit "@{shortID}" (route.ShortID non-empty) is resolved by prefix and
// wins outright. Otherwise the sticky session is used: stickySessionID is the
// user's recorded target and its title is read back for the confirmation
// reply. A sticky ID pointing at a deleted/archived session yields an error so
// the caller can ask the user to pick again rather than send into the void.
func ResolveTarget(messenger SessionMessenger, route Route, stickySessionID string) (sessionID, title string, err error) {
	if route.ShortID != "" {
		return ResolveShortSessionID(messenger, route.ShortID)
	}

	if stickySessionID == "" {
		return "", "", fmt.Errorf("no target session")
	}
	if messenger == nil {
		return "", "", fmt.Errorf("session messenger not available")
	}
	info, err := messenger.GetSessionInfo(stickySessionID)
	if err != nil {
		return "", "", fmt.Errorf("最近会话不可用，请用 /ls 重新选择：%w", err)
	}
	return info.ID, info.Title, nil
}

// FormatSessionLabel returns a human-readable label for a session.
func FormatSessionLabel(sessionID, sessionTitle string) string {
	if sessionTitle != "" {
		return sessionTitle
	}
	return "会话 " + ShortSessionID(sessionID)
}

// ShortSessionID returns the first 8 characters of a session ID for display.
// Session IDs are always ASCII hex (UUID format), so byte slicing is safe.
func ShortSessionID(id string) string {
	if len(id) < 8 {
		return id
	}
	return id[:8]
}
