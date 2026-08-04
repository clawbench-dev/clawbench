package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// ToolCallRecord represents a row in the chat_tool_calls table.
type ToolCallRecord struct {
	ID         int64           `json:"id"`
	MessageID  int64           `json:"message_id"`
	SessionID  string          `json:"session_id"`
	ToolID     string          `json:"tool_id"`
	Name       string          `json:"name"`
	Input      json.RawMessage `json:"input"`
	Output     string          `json:"output"`
	Status     string          `json:"status"`
	Done       bool            `json:"done"`
	Summary    string          `json:"summary"`
	DurationMs int             `json:"duration_ms"` // Wall-clock execution time in milliseconds (0 = unknown)
	CreatedAt  time.Time       `json:"created_at"`
}

// UpsertToolCall inserts or updates a tool call record in chat_tool_calls.
// On conflict (same tool_id + message_id), input is overwritten,
// output is only overwritten if non-empty, duration is only overwritten if
// non-zero (so intermediate tool_use events never wipe a computed duration),
// and status/done/summary are always updated.
func UpsertToolCall(messageID int64, sessionID, toolID, name string, input json.RawMessage, output, status, summary string, done bool, durationMs int) error {
	_, err := WriteExecContext(context.Background(), `
		INSERT INTO chat_tool_calls (message_id, session_id, tool_id, name, input, output, status, done, summary, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tool_id, message_id) DO UPDATE SET
			input = excluded.input,
			output = CASE WHEN excluded.output != '' THEN excluded.output ELSE chat_tool_calls.output END,
			status = excluded.status,
			done = excluded.done,
			summary = excluded.summary,
			duration_ms = CASE WHEN excluded.duration_ms > 0 THEN excluded.duration_ms ELSE chat_tool_calls.duration_ms END
	`, messageID, sessionID, toolID, name, string(input), output, status, done, summary, durationMs)
	if err != nil {
		return fmt.Errorf("UpsertToolCall: %w", err)
	}
	return nil
}

// GetToolCall retrieves a tool call record by tool_id and message_id.
// Returns nil if not found. Uses dbRead for WAL-mode concurrent reads.
func GetToolCall(toolID string, messageID int64) (*ToolCallRecord, error) {
	var r ToolCallRecord
	var doneInt int
	var inputStr string
	err := dbRead.QueryRowContext(context.Background(), `
		SELECT id, message_id, session_id, tool_id, name, input, output, status, done, summary, duration_ms, created_at
		FROM chat_tool_calls WHERE tool_id = ? AND message_id = ?
	`, toolID, messageID).Scan(
		&r.ID, &r.MessageID, &r.SessionID, &r.ToolID, &r.Name,
		&inputStr, &r.Output, &r.Status, &doneInt, &r.Summary, &r.DurationMs, &r.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetToolCall: %w", err)
	}
	r.Input = json.RawMessage(inputStr)
	r.Done = doneInt != 0
	return &r, nil
}

// GetToolCallsBySession retrieves all tool call records for a session.
// Used by BuildForkContext to batch-fetch tool details without N+1 queries.
func GetToolCallsBySession(sessionID string) ([]ToolCallRecord, error) {
	rows, err := dbRead.QueryContext(context.Background(), `
		SELECT id, message_id, session_id, tool_id, name, input, output, status, done, summary, duration_ms, created_at
		FROM chat_tool_calls WHERE session_id = ?
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("GetToolCallsBySession: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var records []ToolCallRecord
	for rows.Next() {
		var r ToolCallRecord
		var doneInt int
		var inputStr string
		if err := rows.Scan(
			&r.ID, &r.MessageID, &r.SessionID, &r.ToolID, &r.Name,
			&inputStr, &r.Output, &r.Status, &doneInt, &r.Summary, &r.DurationMs, &r.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("GetToolCallsBySession scan: %w", err)
		}
		r.Input = json.RawMessage(inputStr)
		r.Done = doneInt != 0
		records = append(records, r)
	}
	return records, rows.Err()
}

// GetToolCallBySession retrieves a tool call record by tool_id and session_id.
// This is a fallback for task executions where the session has multiple assistant
// messages and the tool call may be stored
// under a different message_id than the one the frontend knows about.
// Returns nil if not found.
func GetToolCallBySession(toolID, sessionID string) (*ToolCallRecord, error) {
	var r ToolCallRecord
	var doneInt int
	var inputStr string
	err := dbRead.QueryRowContext(context.Background(), `
		SELECT id, message_id, session_id, tool_id, name, input, output, status, done, summary, duration_ms, created_at
		FROM chat_tool_calls WHERE tool_id = ? AND session_id = ?
		ORDER BY created_at DESC LIMIT 1
	`, toolID, sessionID).Scan(
		&r.ID, &r.MessageID, &r.SessionID, &r.ToolID, &r.Name,
		&inputStr, &r.Output, &r.Status, &doneInt, &r.Summary, &r.DurationMs, &r.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetToolCallBySession: %w", err)
	}
	r.Input = json.RawMessage(inputStr)
	r.Done = doneInt != 0
	return &r, nil
}
