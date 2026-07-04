//nolint:govet,noctx // db global singleton, context not applicable
package service

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"time"

	"clawbench/internal/ws"
)

// PendingEvent represents a persisted event for offline clients.
type PendingEvent struct {
	ID        int64  `json:"-"`
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	Payload   string `json:"payload"`
	ExpiresAt string `json:"expires_at"`
	CreatedAt string `json:"created_at"`
}

const (
	// pendingEventTTL is the default TTL for terminal events (completed/cancelled/failed).
	pendingEventTTL = 24 * time.Hour
	// pendingEventPermPendTTL is the TTL for permission_pending events (7 days).
	pendingEventPermPendTTL = 7 * 24 * time.Hour
	// pendingEventMaxRows is the maximum total rows in pending_events.
	pendingEventMaxRows = 1000
	// statusCancelled is the cancelled status string used across event types.
	statusCancelled = "cancelled"
	// statusCompleted is the completed status string used across event types.
	statusCompleted = "completed"
)

// IsNotifiableEvent returns true if the event is a terminal state that
// should be persisted for offline clients.
func IsNotifiableEvent(event string, data any) bool {
	var status string
	switch d := data.(type) {
	case *ws.SessionUpdateData:
		status = d.Status
	case *ws.TaskUpdateData:
		status = d.Status
	case map[string]any:
		if s, ok := d["status"].(string); ok {
			status = s
		}
	default:
		return false
	}
	switch event {
	case "session_update":
		return status == statusCompleted || status == statusCancelled || status == "permission_pending"
	case "task_update":
		return status == statusCompleted || status == "failed" || status == statusCancelled
	default:
		return false
	}
}

// pendingEventExpiresAt returns the expires_at value for an event type.
// permission_pending events get 7-day TTL; others get 24h.
func pendingEventExpiresAt(event, status string) string {
	if event == "session_update" && status == "permission_pending" {
		return time.Now().Add(pendingEventPermPendTTL).UTC().Format(time.RFC3339)
	}
	return time.Now().Add(pendingEventTTL).UTC().Format(time.RFC3339)
}

// StorePendingEvent persists a notifiable event to the global event log.
func StorePendingEvent(eventID, eventType, payload, expiresAt string) error {
	if db == nil {
		return nil
	}
	_, err := WriteExec(
		`INSERT OR IGNORE INTO pending_events (event_id, event_type, payload, expires_at) VALUES (?, ?, ?, ?)`,
		eventID, eventType, payload, expiresAt,
	)
	return err
}

// GetPendingEvents returns non-expired events optionally after a cursor event_id.
// Results are ordered by id ASC. If the cursor event_id has expired and been
// cleaned up, returns an empty slice (client should reset cursor and re-fetch).
func GetPendingEvents(afterEventID string) ([]PendingEvent, error) {
	if db == nil || dbRead == nil {
		return nil, nil
	}

	var rows *sql.Rows
	var err error
	if afterEventID != "" {
		// Check if cursor event still exists; if not, return empty
		// to signal client to reset cursor
		var cursorExists int
		if err := dbRead.QueryRow(
			`SELECT COUNT(*) FROM pending_events WHERE event_id = ?`,
			afterEventID,
		).Scan(&cursorExists); err != nil {
			return nil, err
		}
		if cursorExists == 0 {
			return []PendingEvent{}, nil
		}
		rows, err = dbRead.Query(
			`SELECT event_id, event_type, payload, expires_at, created_at
			 FROM pending_events
			 WHERE expires_at >= datetime('now')
			   AND id > (SELECT id FROM pending_events WHERE event_id = ?)
			 ORDER BY id ASC`,
			afterEventID,
		)
	} else {
		rows, err = dbRead.Query(
			`SELECT event_id, event_type, payload, expires_at, created_at
			 FROM pending_events
			 WHERE expires_at >= datetime('now')
			 ORDER BY id ASC`,
		)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var events []PendingEvent
	for rows.Next() {
		var e PendingEvent
		if err := rows.Scan(&e.EventID, &e.EventType, &e.Payload, &e.ExpiresAt, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// CleanupPendingEvents removes expired events and caps total rows.
func CleanupPendingEvents() {
	if db == nil {
		return
	}
	result, err := WriteExec(`DELETE FROM pending_events WHERE expires_at < datetime('now')`)
	if err != nil {
		slog.Warn("pending_events: cleanup failed", "error", err)
	} else if n, _ := result.RowsAffected(); n > 0 {
		slog.Debug("pending_events: cleaned up expired", "count", n)
	}
	// Cap total rows
	capResult, capErr := WriteExec(
		`DELETE FROM pending_events WHERE id NOT IN (
			SELECT id FROM pending_events ORDER BY created_at DESC LIMIT ?
		)`,
		pendingEventMaxRows,
	)
	if capErr != nil {
		slog.Warn("pending_events: row cap failed", "error", capErr)
	} else if n, _ := capResult.RowsAffected(); n > 0 {
		slog.Warn("pending_events: evicted rows to cap", "count", n, "max", pendingEventMaxRows)
	}
}

// StoreNotifiableEvent persists a notifiable WS event if it's a terminal state.
// Only stores when there are disconnected clients (conditional storage).
func StoreNotifiableEvent(msg ws.ServerMessage) {
	if !IsNotifiableEvent(msg.Event, msg.Data) {
		return
	}
	// Conditional storage: only persist if clients are disconnected
	mgr := ws.GetManager()
	if mgr != nil && !mgr.HasDisconnectedClients() {
		return
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		slog.Warn("pending_events: marshal failed", "error", err)
		return
	}
	// Determine status for expires_at calculation
	status := ""
	switch d := msg.Data.(type) {
	case *ws.SessionUpdateData:
		status = d.Status
	case *ws.TaskUpdateData:
		status = d.Status
	case map[string]any:
		if s, ok := d["status"].(string); ok {
			status = s
		}
	}
	expiresAt := pendingEventExpiresAt(msg.Event, status)
	if err := StorePendingEvent(msg.ID, msg.Event, string(payload), expiresAt); err != nil {
		slog.Warn("pending_events: store failed", "error", err)
	}
}
