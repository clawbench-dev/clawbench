package handler

import "time"

// formatShareTime renders a share row's created_at as an RFC3339 UTC instant.
//
// The column is written by SQLite's DEFAULT CURRENT_TIMESTAMP, which produces
// bare UTC text ("2006-01-02 15:04:05", no zone suffix). Sent as-is, the
// browser parses that as a LOCAL time, so a client east/west of UTC reads the
// share as hours younger/older than it is — the relative-time label in the
// shared-files / shared-conversations drawers would be wrong by the offset.
//
// Emitting an explicit RFC3339 instant (with the Z suffix) keeps the timezone
// decision in one place and matches every other timestamp the API serves
// (chat messages, sessions, forge events — see formatBtwTime). The frontend
// then localizes for display.
//
// Unparseable input is passed through unchanged rather than dropped: a value
// the UI cannot format is still better than a silently empty timestamp.
func formatShareTime(raw string) string {
	if raw == "" {
		return ""
	}
	t, err := time.Parse("2006-01-02 15:04:05", raw)
	if err != nil {
		return raw
	}
	// The stored text is UTC; .UTC() is explicit intent, not a conversion.
	return t.UTC().Format(time.RFC3339)
}
