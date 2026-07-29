//nolint:noctx // db global singleton, context not applicable
package service

import (
	"database/sql"
	"log/slog"
)

// FeishuSubscriber represents a Feishu user subscribed to push notifications.
type FeishuSubscriber struct {
	ID        int64  `json:"id"`
	UserID    string `json:"user_id"`
	ChatID    string `json:"chat_id"`
	UserName  string `json:"user_name"`
	Source    string `json:"source"` // "stream" (auto) or "manual" (config/panel)
	CreatedAt string `json:"created_at"`
}

// GetFeishuSubscribers returns all subscribed Feishu users.
func GetFeishuSubscribers() ([]FeishuSubscriber, error) {
	if dbRead == nil {
		return nil, nil
	}
	rows, err := dbRead.Query(
		`SELECT id, user_id, chat_id, user_name, source, created_at
		 FROM feishu_subscribers ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var subs []FeishuSubscriber
	for rows.Next() {
		var s FeishuSubscriber
		if err := rows.Scan(&s.ID, &s.UserID, &s.ChatID, &s.UserName, &s.Source, &s.CreatedAt); err != nil {
			return nil, err
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

// UpsertFeishuSubscriber inserts or updates a Feishu subscriber.
// If the user already exists, chat_id and user_name are updated.
func UpsertFeishuSubscriber(userID, chatID, userName, source string) error {
	if db == nil {
		return nil
	}
	_, err := WriteExec(
		`INSERT INTO feishu_subscribers (user_id, chat_id, user_name, source)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET
			chat_id = excluded.chat_id,
			user_name = excluded.user_name,
			source = CASE WHEN feishu_subscribers.source = 'manual' THEN 'manual' ELSE excluded.source END`,
		userID, chatID, userName, source,
	)
	if err != nil {
		slog.Warn("feishu_subscribers: upsert failed", "error", err, "user_id", userID)
	}
	return err
}

// DeleteFeishuSubscriber removes a subscriber by Feishu open_id.
func DeleteFeishuSubscriber(userID string) error {
	if db == nil {
		return nil
	}
	result, err := WriteExec(`DELETE FROM feishu_subscribers WHERE user_id = ?`, userID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// MergeFeishuConfigSubscribers merges config.yaml static users into the DB.
// Users from config are upserted with source='manual'. Users already in DB
// with source='manual' but no longer in config are removed.
func MergeFeishuConfigSubscribers(users []string) {
	if db == nil {
		return
	}

	// Get existing manual subscribers
	rows, err := dbRead.Query(
		`SELECT user_id FROM feishu_subscribers WHERE source = 'manual'`,
	)
	if err != nil {
		slog.Warn("feishu_subscribers: query manual failed", "error", err)
		return
	}
	defer func() { _ = rows.Close() }()

	var existingManual []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return
		}
		existingManual = append(existingManual, uid)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("feishu_subscribers: rows iteration failed", "error", err)
		return
	}

	configSet := make(map[string]bool, len(users))
	for _, u := range users {
		configSet[u] = true
	}

	// Upsert config users
	for _, u := range users {
		if u == "" {
			continue
		}
		if err := UpsertFeishuSubscriber(u, "", "", "manual"); err != nil {
			slog.Warn("feishu_subscribers: merge upsert failed", "error", err, "user_id", u)
		}
	}

	// Remove manual subscribers no longer in config
	for _, u := range existingManual {
		if !configSet[u] {
			if _, err := WriteExec(`DELETE FROM feishu_subscribers WHERE user_id = ? AND source = 'manual'`, u); err != nil {
				slog.Warn("feishu_subscribers: merge delete failed", "error", err, "user_id", u)
			}
		}
	}

	if len(users) > 0 {
		slog.Info("feishu_subscribers: merged config users", "count", len(users))
	}
}
