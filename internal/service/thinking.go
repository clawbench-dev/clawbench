package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

// ThinkingRecord represents a row in the chat_thinking table.
type ThinkingRecord struct {
	ID        int64     `json:"id"`
	MessageID int64     `json:"message_id"`
	SessionID string    `json:"session_id"`
	ThinkID   string    `json:"think_id"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// generateThinkingID returns a think_id ("th_" + 32 hex chars).
func generateThinkingID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("th_%d", time.Now().UnixNano())
	}
	return "th_" + hex.EncodeToString(b)
}

// UpsertThinking inserts or updates a thinking record in chat_thinking.
// No-op when think_id or text is empty.
func UpsertThinking(messageID int64, sessionID, thinkID, text string) error {
	if thinkID == "" || text == "" {
		return nil
	}
	_, err := WriteExecContext(context.Background(), `
		INSERT INTO chat_thinking (message_id, session_id, think_id, text)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(think_id, message_id) DO UPDATE SET text = excluded.text
	`, messageID, sessionID, thinkID, text)
	if err != nil {
		return fmt.Errorf("UpsertThinking: %w", err)
	}
	return nil
}

// DeleteThinkingByMessage removes thinking records for a message.
// Called before insert in the Finalize write path for idempotency.
func DeleteThinkingByMessage(messageID int64) error {
	_, err := WriteExecContext(context.Background(), "DELETE FROM chat_thinking WHERE message_id = ?", messageID)
	if err != nil {
		return fmt.Errorf("DeleteThinkingByMessage: %w", err)
	}
	return nil
}

// GetThinking retrieves a thinking record by think_id and message_id.
// Returns nil if not found.
func GetThinking(thinkID string, messageID int64) (*ThinkingRecord, error) {
	var r ThinkingRecord
	err := dbRead.QueryRowContext(context.Background(), `
		SELECT id, message_id, session_id, think_id, text, created_at
		FROM chat_thinking WHERE think_id = ? AND message_id = ?
	`, thinkID, messageID).Scan(&r.ID, &r.MessageID, &r.SessionID, &r.ThinkID, &r.Text, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetThinking: %w", err)
	}
	return &r, nil
}

// GetThinkingBySession retrieves a thinking record by think_id and session_id.
// Fallback for ACP multi-assistant-message sessions where the frontend may not
// know the exact message_id (mirrors GetToolCallBySession).
func GetThinkingBySession(thinkID, sessionID string) (*ThinkingRecord, error) {
	var r ThinkingRecord
	err := dbRead.QueryRowContext(context.Background(), `
		SELECT id, message_id, session_id, think_id, text, created_at
		FROM chat_thinking WHERE think_id = ? AND session_id = ?
		ORDER BY created_at DESC LIMIT 1
	`, thinkID, sessionID).Scan(&r.ID, &r.MessageID, &r.SessionID, &r.ThinkID, &r.Text, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetThinkingBySession: %w", err)
	}
	return &r, nil
}
