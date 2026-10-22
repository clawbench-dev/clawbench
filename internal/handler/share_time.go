package handler

import "time"

// shareTimeLayouts lists the shapes a share row's created_at can arrive in.
//
// The column is a DATETIME with DEFAULT CURRENT_TIMESTAMP, so what comes back
// depends on who wrote it and how it was scanned:
//   - SQLite's bare UTC text ("2006-01-02 15:04:05") when the value is read as
//     the stored string,
//   - RFC3339 ("2006-10-22T15:18:10Z") when the driver parses the column into a
//     time.Time and the caller formats it back out — which is what
//     GetSessionShareByToken does today.
//
// Both must be accepted. Missing the RFC3339 case silently produced a zero
// createdAt in the share payload, so the exported JSON claimed the share was
// created in year 1.
var shareTimeLayouts = []string{time.RFC3339, "2006-01-02 15:04:05"}

// formatShareTime renders a share row's created_at as an RFC3339 UTC instant.
//
// Sent as-is, a bare-UTC value would be parsed by the browser as a LOCAL time, so
// a client east/west of UTC reads the share as hours younger/older than it is —
// the relative-time label in the shared-files / shared-conversations drawers
// would be wrong by the offset.
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
	t, ok := parseShareTime(raw)
	if !ok {
		return raw
	}
	return t.Format(time.RFC3339)
}

// parseShareTime recovers the instant from a share row's created_at, in either
// shape the column can arrive in (see shareTimeLayouts).
//
// ok=false means the value matches neither layout; callers must decide what to do
// rather than silently substituting the zero time, which is what made the payload
// carry "0001-01-01T00:00:00Z" for every share.
func parseShareTime(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range shareTimeLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			// Stored values are UTC; .UTC() is explicit intent, not a conversion.
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}
