//nolint:noctx // db global, context not applicable
package service

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"regexp"
)

// validIdentifier validates SQL identifier (table/column name) to prevent injection.
var validIdentifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// generateUUID generates a standard UUID v4 format string with an optional prefix.
// It checks for conflicts in the specified database table and column.
// Returns empty string on exhaustion or error.
func generateUUID(prefix, tableName, column string) string { //nolint:unparam // tableName always "chat_sessions" currently; kept generic for future use
	// Validate identifiers to prevent SQL injection (ISS-009)
	if !validIdentifier.MatchString(tableName) || !validIdentifier.MatchString(column) {
		slog.Error("generateUUID: invalid identifier", slog.String("table", tableName), slog.String("column", column))
		return ""
	}

	for range 10 {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			slog.Error("generateUUID: rand.Read failed", slog.String("err", err.Error()))
			return ""
		}
		// Set version (4) and variant (2) bits according to UUID v4 spec
		b[6] = (b[6] & 0x0f) | 0x40
		b[8] = (b[8] & 0x3f) | 0x80
		uuid := fmt.Sprintf("%s%x-%x-%x-%x-%x",
			prefix, b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])

		var exists bool
		err := dbRead.QueryRow(
			fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE %s = ?)", tableName, column),
			uuid,
		).Scan(&exists)
		if err != nil {
			slog.Warn("generateUUID: DB check failed", slog.String("err", err.Error()))
			continue
		}
		if !exists {
			return uuid
		}
	}
	return ""
}
