package dingtalk

import (
	"fmt"
	"regexp"
	"strings"
)

// sessionCmdRe matches "@{8-hex-chars}" followed by optional message text.
var sessionCmdRe = regexp.MustCompile(`^@([0-9a-fA-F]{8})(?:[\s]|$)(.*)`)

// parseSessionCommand parses the "@{shortID} message" format from DingTalk messages.
// Returns (shortID, message, true) if matched, or ("", "", false) if not.
func parseSessionCommand(text string) (string, string, bool) {
	m := sessionCmdRe.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return "", "", false
	}
	return m[1], strings.TrimSpace(m[2]), true
}

// resolveShortSessionID resolves an 8-char short session ID to a full session ID and title.
// It first checks running sessions, then falls back to all sessions.
// Matching is case-insensitive (UUIDs are lowercase in DB, user may type uppercase).
// Returns error on ambiguity (multiple matches) or not found.
func resolveShortSessionID(shortID string) (string, string, error) {
	if sessionMessenger == nil {
		return "", "", fmt.Errorf("session messenger not available")
	}

	// Priority 1: running sessions
	running, err := sessionMessenger.FindSessionsByPrefix(shortID, true)
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
	all, err := sessionMessenger.FindSessionsByPrefix(shortID, false)
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
