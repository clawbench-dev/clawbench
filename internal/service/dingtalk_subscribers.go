//nolint:noctx // db global singleton, context not applicable
package service

import (
	"database/sql"
	"log/slog"
)

// DingTalkSubscriber represents a DingTalk user subscribed to push notifications.
type DingTalkSubscriber struct {
	ID             int64  `json:"id"`
	UserID         string `json:"user_id"`
	ConversationID string `json:"conversation_id"`
	UserName       string `json:"user_name"`
	Source         string `json:"source"` // "stream" (auto) or "manual" (config/panel)
	CreatedAt      string `json:"created_at"`
}

// GetDingTalkSubscribers returns all subscribed DingTalk users.
func GetDingTalkSubscribers() ([]DingTalkSubscriber, error) {
	if dbRead == nil {
		return nil, nil
	}
	rows, err := dbRead.Query(
		`SELECT id, user_id, conversation_id, user_name, source, created_at
		 FROM dingtalk_subscribers ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var subs []DingTalkSubscriber
	for rows.Next() {
		var s DingTalkSubscriber
		if err := rows.Scan(&s.ID, &s.UserID, &s.ConversationID, &s.UserName, &s.Source, &s.CreatedAt); err != nil {
			return nil, err
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

// UpsertDingTalkSubscriber inserts or updates a DingTalk subscriber.
// If the user already exists, conversation_id and user_name are updated.
func UpsertDingTalkSubscriber(userID, conversationID, userName, source string) error {
	if db == nil {
		return nil
	}
	_, err := WriteExec(
		`INSERT INTO dingtalk_subscribers (user_id, conversation_id, user_name, source)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET
			conversation_id = excluded.conversation_id,
			user_name = excluded.user_name,
			source = excluded.source`,
		userID, conversationID, userName, source,
	)
	if err != nil {
		slog.Warn("dingtalk_subscribers: upsert failed", "error", err, "user_id", userID)
	}
	return err
}

// DeleteDingTalkSubscriber removes a subscriber by DingTalk userId.
func DeleteDingTalkSubscriber(userID string) error {
	if db == nil {
		return nil
	}
	result, err := WriteExec(`DELETE FROM dingtalk_subscribers WHERE user_id = ?`, userID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// MergeDingTalkConfigSubscribers merges config.yaml static users into the DB.
// Users from config are upserted with source='manual'. Users already in DB
// with source='manual' but no longer in config are removed.
func MergeDingTalkConfigSubscribers(users []string) {
	if db == nil {
		return
	}

	// Get existing manual subscribers
	rows, err := dbRead.Query(
		`SELECT user_id FROM dingtalk_subscribers WHERE source = 'manual'`,
	)
	if err != nil {
		slog.Warn("dingtalk_subscribers: query manual failed", "error", err)
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
		slog.Warn("dingtalk_subscribers: rows iteration failed", "error", err)
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
		if err := UpsertDingTalkSubscriber(u, "", "", "manual"); err != nil {
			slog.Warn("dingtalk_subscribers: merge upsert failed", "error", err, "user_id", u)
		}
	}

	// Remove manual subscribers no longer in config
	for _, u := range existingManual {
		if !configSet[u] {
			if _, err := WriteExec(`DELETE FROM dingtalk_subscribers WHERE user_id = ? AND source = 'manual'`, u); err != nil {
				slog.Warn("dingtalk_subscribers: merge delete failed", "error", err, "user_id", u)
			}
		}
	}

	if len(users) > 0 {
		slog.Info("dingtalk_subscribers: merged config users", "count", len(users))
	}
}
