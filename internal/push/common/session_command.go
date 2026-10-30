package common

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// SessionCmdRe matches "@{8+hex-chars}" followed by optional message text.
// Allows 8+ hex chars so users can type longer prefixes for disambiguation.
// The (?s) flag makes "." match newlines so multi-line messages are preserved
// in full instead of being truncated at the first line break.
var SessionCmdRe = regexp.MustCompile(`(?s)^@([0-9a-fA-F]{8,})(?:\s|$)(.*)`)

// sessionPrefixRe is the same prefix shape without the "@" and the payload. The
// /show and /stop arguments go through it so a command accepts exactly what the
// "@{shortID}" form accepts — the docs promise an 8+ hex prefix, and these
// commands can stop a turn or reveal its text, so a one-character argument must
// not silently match whichever session happens to start with it.
//
// The optional trailing "-hex" groups keep a FULL session id working: the
// resolver matches by prefix, so "a1b2c3d4-1111-…" is a valid (maximally
// specific) prefix, and rejecting it would take away a capability the "@" form
// and the pre-check code both had.
var sessionPrefixRe = regexp.MustCompile(`^[0-9a-fA-F]{8,}(?:-[0-9a-fA-F]+)*$`)

// ParseSessionCommand parses the "@{shortID} message" format from push messages.
// Returns (shortID, message, true) if matched, or ("", "", false) if not.
func ParseSessionCommand(text string) (string, string, bool) {
	m := SessionCmdRe.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return "", "", false
	}
	return m[1], strings.TrimSpace(m[2]), true
}

// Session list / reply / stop / new commands. Only these exact words are
// commands; everything else is ordinary text for the AI.
const (
	// SessionListCommand lists recent sessions.
	SessionListCommand = "/ls"
	// SessionShowCommand resends the target session's last AI reply.
	SessionShowCommand = "/show"
	// SessionStopCommand stops the target session and clears its queue.
	SessionStopCommand = "/stop"
	// SessionNewCommand creates a session, or lists projects when bare.
	SessionNewCommand = "/new"
)

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
	// RouteShowLastReply asks for the target session's last AI reply ("/show").
	// Arg carries an optional session prefix; empty means the sticky session.
	RouteShowLastReply
	// RouteStopSession asks to stop the target session and clear its queue
	// ("/stop"). Arg carries an optional session prefix; empty means sticky.
	RouteStopSession
	// RouteNewSession creates a session ("/new"). Arg is the project selector
	// (a 1-based index into the project list, or a full path); empty means the
	// user wants the project list.
	RouteNewSession
	// RouteNoTarget means the user sent something routable but has no sticky
	// session yet. The caller must reply with the "/ls" hint.
	RouteNoTarget
)

// IsCommand reports whether this kind is a slash command rather than text to
// deliver to a session.
//
// A command keeps its word in Route.Arg, so Route.Message is empty. Any caller
// that delivers Route.Message (in particular the attachment paths) must consult
// this first: without it a command sent together with an image becomes an empty
// message posted to a session — the command does not run and the text the user
// typed is lost.
func (k RouteKind) IsCommand() bool {
	return k == RouteListSessions || k == RouteShowLastReply ||
		k == RouteStopSession || k == RouteNewSession
}

// CommandWord returns the slash word that produced this kind, or "" when the
// kind is not a command. Callers use it to name the command back to the user
// without re-parsing the message.
func (k RouteKind) CommandWord() string {
	switch k {
	case RouteListSessions:
		return SessionListCommand
	case RouteShowLastReply:
		return SessionShowCommand
	case RouteStopSession:
		return SessionStopCommand
	case RouteNewSession:
		return SessionNewCommand
	}
	return ""
}

// Route is the outcome of classifying an inbound IM message.
type Route struct {
	Kind    RouteKind
	ShortID string // set only for an explicit "@{shortID}" target
	Message string // text payload; empty for a bare file/image
	Arg     string // command argument (session prefix or project selector)
}

// ClassifyIncoming routes an inbound text message from an IM user.
//
// Precedence: an explicit "@{shortID}" always wins — which doubles as the
// escape hatch for sending a literal command word to a session; then a leading
// command word (/ls, /show, /stop, /new); then any other text goes to the
// sticky session (shortID left empty for the caller to resolve). An
// empty/whitespace-only text with no sticky session is RouteNoTarget — the same
// hint a bare file gets when nothing was ever chosen.
//
// stickySessionID is the caller's already-resolved sticky target ("" when
// none). It is passed in rather than looked up so this stays pure and testable.
func ClassifyIncoming(text, stickySessionID string) Route {
	if shortID, msg, ok := ParseSessionCommand(text); ok {
		return Route{Kind: RouteToSession, ShortID: shortID, Message: msg}
	}

	trimmed := strings.TrimSpace(text)
	if cmd, arg := splitCommand(trimmed); cmd != "" {
		switch cmd {
		case SessionListCommand:
			// "/ls" takes no argument; a trailing argument means the user
			// meant something else, so fall through to the session path.
			if arg == "" {
				return Route{Kind: RouteListSessions}
			}
		case SessionShowCommand:
			if arg == "" && stickySessionID == "" {
				// No argument and no sticky target: there is nothing to show.
				// Report it as the standard no-target case so the user gets the
				// same "/ls" hint as any other untargeted message, rather than
				// an internal "no target session" error.
				return Route{Kind: RouteNoTarget}
			}
			return Route{Kind: RouteShowLastReply, Arg: arg}
		case SessionStopCommand:
			if arg == "" && stickySessionID == "" {
				return Route{Kind: RouteNoTarget}
			}
			return Route{Kind: RouteStopSession, Arg: arg}
		case SessionNewCommand:
			return Route{Kind: RouteNewSession, Arg: arg}
		}
	}

	if stickySessionID == "" {
		return Route{Kind: RouteNoTarget}
	}
	return Route{Kind: RouteToSession, Message: trimmed}
}

// splitCommand splits a trimmed message into its leading command word and the
// remaining argument, preserving the argument verbatim (a project path may
// contain spaces). Returns ("", "") when the message is not a leading slash
// word at all.
//
// The argument is the rest of the FIRST line only. A trailing block that is not
// part of the command must not become the argument: WeChat appends a quoted
// message after a blank line (see wechat.parseInbound), so "/show" sent as a
// reply to a push would otherwise parse as "/show <the quote>" and be rejected
// as a malformed session id.
func splitCommand(trimmed string) (cmd, arg string) {
	if !strings.HasPrefix(trimmed, "/") {
		return "", ""
	}
	idx := strings.IndexAny(trimmed, " \t\n")
	if idx < 0 {
		return trimmed, ""
	}
	rest := trimmed[idx+1:]
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[:nl]
	}
	return trimmed[:idx], strings.TrimSpace(rest)
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
		return "", "", errors.New(T("PushMultipleRunningSessions", map[string]any{"ID": shortID}))
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
		return "", "", errors.New(T("PushMultipleSessions", map[string]any{"ID": shortID}))
	}
	if len(all) == 1 {
		return all[0].ID, all[0].Title, nil
	}

	return "", "", errors.New(T("PushSessionNotFound", map[string]any{"ID": shortID}))
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
		// Keep the underlying error reachable via errors.Is/As: the localized
		// message is what the user sees, but callers may still want to match the
		// cause. Wrapping with %w (not errors.New) preserves that.
		return "", "", fmt.Errorf("%s: %w",
			T("PushRecentSessionUnavailable", map[string]any{"Error": err.Error()}), err)
	}
	return info.ID, info.Title, nil
}

// ResolveCommandTarget resolves the target of a /show or /stop command.
//
// An explicit prefix (the command's argument) resolves like "@{shortID}" does —
// including the 8+ hex shape check, so "/stop a" cannot match an arbitrary
// session and "/stop %" cannot reach the LIKE query as a wildcard.
// An empty argument means the sticky session, whose title is read back so the
// reply can name it. Returns an error when there is no sticky target at all —
// the caller turns that into the "/ls" hint rather than acting on a guess.
func ResolveCommandTarget(messenger SessionMessenger, arg, stickySessionID string) (sessionID, title string, err error) {
	if arg != "" {
		if !sessionPrefixRe.MatchString(arg) {
			return "", "", errors.New(T("PushInvalidSessionID", map[string]any{"ID": arg}))
		}
		return ResolveShortSessionID(messenger, arg)
	}
	return ResolveTarget(messenger, Route{Kind: RouteToSession}, stickySessionID)
}

// FormatSessionLabel returns a human-readable label for a session.
func FormatSessionLabel(sessionID, sessionTitle string) string {
	if sessionTitle != "" {
		return sessionTitle
	}
	return T("PushSessionLabelPrefix") + ShortSessionID(sessionID)
}

// ShortSessionID returns the first 8 characters of a session ID for display.
// Session IDs are always ASCII hex (UUID format), so byte slicing is safe.
func ShortSessionID(id string) string {
	if len(id) < 8 {
		return id
	}
	return id[:8]
}
