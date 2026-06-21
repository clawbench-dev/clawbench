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
	ID        int64           `json:"id"`
	MessageID int64           `json:"message_id"`
	SessionID string          `json:"session_id"`
	ToolID    string          `json:"tool_id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	Output    string          `json:"output"`
	Status    string          `json:"status"`
	Done      bool            `json:"done"`
	Summary   string          `json:"summary"`
	CreatedAt time.Time       `json:"created_at"`
}

// UpsertToolCall inserts or updates a tool call record in chat_tool_calls.
// On conflict (same tool_id + message_id), input is overwritten,
// output is only overwritten if non-empty, and status/done/summary are always updated.
func UpsertToolCall(messageID int64, sessionID, toolID, name string, input json.RawMessage, output, status, summary string, done bool) error {
	_, err := DB.ExecContext(context.Background(), `
		INSERT INTO chat_tool_calls (message_id, session_id, tool_id, name, input, output, status, done, summary)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tool_id, message_id) DO UPDATE SET
			input = excluded.input,
			output = CASE WHEN excluded.output != '' THEN excluded.output ELSE chat_tool_calls.output END,
			status = excluded.status,
			done = excluded.done,
			summary = excluded.summary
	`, messageID, sessionID, toolID, name, string(input), output, status, done, summary)
	if err != nil {
		return fmt.Errorf("UpsertToolCall: %w", err)
	}
	return nil
}

// GetToolCall retrieves a tool call record by tool_id and message_id.
// Returns nil if not found. Uses DBRead for WAL-mode concurrent reads.
func GetToolCall(toolID string, messageID int64) (*ToolCallRecord, error) {
	var r ToolCallRecord
	var doneInt int
	var inputStr string
	err := DBRead.QueryRowContext(context.Background(), `
		SELECT id, message_id, session_id, tool_id, name, input, output, status, done, summary, created_at
		FROM chat_tool_calls WHERE tool_id = ? AND message_id = ?
	`, toolID, messageID).Scan(
		&r.ID, &r.MessageID, &r.SessionID, &r.ToolID, &r.Name,
		&inputStr, &r.Output, &r.Status, &doneInt, &r.Summary, &r.CreatedAt,
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
