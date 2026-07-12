//nolint:govet,noctx // db global singleton, context not applicable
package service

import (
	"log/slog"
	"time"
)

// DingTalkOutbox represents a pending DingTalk message in the outbox queue.
type DingTalkOutbox struct {
	ID         int64  `json:"id"`
	UserID     string `json:"user_id"`
	MsgKey     string `json:"msg_key"`
	MsgParam   string `json:"msg_param"`
	Status     string `json:"status"`
	RetryCount int    `json:"retry_count"`
	MaxRetries int    `json:"max_retries"`
	NextRetry  string `json:"next_retry"`
	CreatedAt  string `json:"created_at"`
}

// EnqueueDingTalkMessage adds a message to the outbox for reliable delivery.
func EnqueueDingTalkMessage(userID, msgKey, msgParam string, maxRetries int) error {
	if db == nil {
		return nil
	}
	_, err := WriteExec(
		`INSERT INTO dingtalk_outbox (user_id, msg_key, msg_param, status, retry_count, max_retries, next_retry)
		 VALUES (?, ?, ?, 'pending', 0, ?, datetime('now'))`,
		userID, msgKey, msgParam, maxRetries,
	)
	if err != nil {
		slog.Warn("dingtalk_outbox: enqueue failed", "error", err, "user_id", userID)
	}
	return err
}

// GetPendingDingTalkMessages returns messages that are due for sending or retry.
func GetPendingDingTalkMessages(limit int) ([]DingTalkOutbox, error) {
	if dbRead == nil {
		return nil, nil
	}
	rows, err := dbRead.Query(
		`SELECT id, user_id, msg_key, msg_param, status, retry_count, max_retries, next_retry, created_at
		 FROM dingtalk_outbox
		 WHERE status IN ('pending', 'retry') AND next_retry <= datetime('now')
		 ORDER BY created_at ASC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var msgs []DingTalkOutbox
	for rows.Next() {
		var m DingTalkOutbox
		if err := rows.Scan(&m.ID, &m.UserID, &m.MsgKey, &m.MsgParam, &m.Status,
			&m.RetryCount, &m.MaxRetries, &m.NextRetry, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// MarkDingTalkMessageSent marks a message as successfully sent.
func MarkDingTalkMessageSent(id int64) error {
	if db == nil {
		return nil
	}
	_, err := WriteExec(
		`UPDATE dingtalk_outbox SET status = 'sent', next_retry = NULL WHERE id = ?`,
		id,
	)
	return err
}

// MarkDingTalkMessageFailed marks a message as failed and schedules a retry.
// If retry_count >= max_retries, the message is permanently marked as 'failed'.
// Otherwise, status is set to 'retry' with next_retry = now + 2^retry_count seconds.
func MarkDingTalkMessageFailed(id int64, maxRetries int) error {
	if db == nil {
		return nil
	}
	// Get current retry_count
	var retryCount int
	if err := dbRead.QueryRow(
		`SELECT retry_count FROM dingtalk_outbox WHERE id = ?`, id,
	).Scan(&retryCount); err != nil {
		return err
	}

	retryCount++
	if retryCount >= maxRetries {
		// Permanently failed
		_, err := WriteExec(
			`UPDATE dingtalk_outbox SET status = 'failed', retry_count = ?, next_retry = NULL WHERE id = ?`,
			retryCount, id,
		)
		if err != nil {
			slog.Warn("dingtalk_outbox: mark failed", "error", err, "id", id)
		}
		return err
	}

	// Schedule retry with exponential backoff: 2^retryCount seconds
	delay := time.Duration(1<<uint(retryCount)) * time.Second
	nextRetry := time.Now().Add(delay).UTC().Format(time.RFC3339)
	_, err := WriteExec(
		`UPDATE dingtalk_outbox SET status = 'retry', retry_count = ?, next_retry = ? WHERE id = ?`,
		retryCount, nextRetry, id,
	)
	if err != nil {
		slog.Warn("dingtalk_outbox: mark retry", "error", err, "id", id)
	}
	return err
}

// CleanupDingTalkOutbox removes sent messages older than 24h and permanently
// failed messages older than 7 days.
func CleanupDingTalkOutbox() {
	if db == nil {
		return
	}
	// Remove sent messages older than 24h
	result, err := WriteExec(
		`DELETE FROM dingtalk_outbox WHERE status = 'sent' AND created_at < datetime('now', '-1 day')`,
	)
	if err != nil {
		slog.Warn("dingtalk_outbox: cleanup sent failed", "error", err)
	} else if n, _ := result.RowsAffected(); n > 0 {
		slog.Debug("dingtalk_outbox: cleaned up sent", "count", n)
	}

	// Remove failed messages older than 7 days
	result, err = WriteExec(
		`DELETE FROM dingtalk_outbox WHERE status = 'failed' AND created_at < datetime('now', '-7 days')`,
	)
	if err != nil {
		slog.Warn("dingtalk_outbox: cleanup failed failed", "error", err)
	} else if n, _ := result.RowsAffected(); n > 0 {
		slog.Debug("dingtalk_outbox: cleaned up failed", "count", n)
	}
}

// GetDingTalkOutboxStats returns counts of messages by status.
func GetDingTalkOutboxStats() (pending, sent, failed int) {
	if dbRead == nil {
		return 0, 0, 0
	}
	rows, err := dbRead.Query(
		`SELECT status, COUNT(*) FROM dingtalk_outbox GROUP BY status`,
	)
	if err != nil {
		return 0, 0, 0
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			continue
		}
		switch status {
		case "pending", "retry":
			pending += count
		case "sent":
			sent = count
		case "failed":
			failed = count
		}
	}
	return
}
