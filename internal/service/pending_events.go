//nolint:noctx,govet // DB global singleton, context not applicable
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
		return status == "completed" || status == "cancelled" || status == "permission_pending"
	case "task_update":
		return status == "completed" || status == "failed" || status == "cancelled"
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
	if DB == nil {
		return nil
	}
	_, err := DB.Exec(
		`INSERT OR IGNORE INTO pending_events (event_id, event_type, payload, expires_at) VALUES (?, ?, ?, ?)`,
		eventID, eventType, payload, expiresAt,
	)
	if err != nil {
		return err
	}
	// Evict expired events
	_, _ = DB.Exec(`DELETE FROM pending_events WHERE expires_at < datetime('now')`)
	// Cap total rows
	_, _ = DB.Exec(
		`DELETE FROM pending_events WHERE id NOT IN (
			SELECT id FROM pending_events ORDER BY created_at DESC LIMIT ?
		)`,
		pendingEventMaxRows,
	)
	return nil
}

// GetPendingEvents returns non-expired events optionally after a cursor event_id.
// Results are ordered by id ASC.
func GetPendingEvents(afterEventID string) ([]PendingEvent, error) {
	if DB == nil || DBRead == nil {
		return nil, nil
	}

	var rows *sql.Rows
	var err error
	if afterEventID != "" {
		rows, err = DBRead.Query(
			`SELECT event_id, event_type, payload, expires_at, created_at
			 FROM pending_events
			 WHERE expires_at >= datetime('now')
			   AND id > (SELECT COALESCE(id, 0) FROM pending_events WHERE event_id = ?)
			 ORDER BY id ASC`,
			afterEventID,
		)
	} else {
		rows, err = DBRead.Query(
			`SELECT event_id, event_type, payload, expires_at, created_at
			 FROM pending_events
			 WHERE expires_at >= datetime('now')
			 ORDER BY id ASC`,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

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

// CleanupPendingEvents removes expired events.
func CleanupPendingEvents() {
	if DB == nil {
		return
	}
	result, err := DB.Exec(`DELETE FROM pending_events WHERE expires_at < datetime('now')`)
	if err != nil {
		slog.Warn("pending_events: cleanup failed", "error", err)
	} else if n, _ := result.RowsAffected(); n > 0 {
		slog.Debug("pending_events: cleaned up expired", "count", n)
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
	}
	expiresAt := pendingEventExpiresAt(msg.Event, status)
	if err := StorePendingEvent(msg.ID, msg.Event, string(payload), expiresAt); err != nil {
		slog.Warn("pending_events: store failed", "error", err)
	}
}
