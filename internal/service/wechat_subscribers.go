package service

import (
	"database/sql"
	"log/slog"

	"clawbench/internal/store"
)

// WeChatSubscriber represents a personal-WeChat (iLink Bot) user subscribed to
// push notifications.
type WeChatSubscriber struct {
	ID        int64  `json:"id"`
	UserID    string `json:"user_id"`
	UserName  string `json:"user_name"`
	Source    string `json:"source"` // always "stream": WeChat has no manual subscribers
	CreatedAt string `json:"created_at"`
	// ContextToken is the per-user reply token the platform mints on an inbound
	// message. WeChat refuses an outbound message without it, so it is
	// persisted here rather than left to the SDK's process-local cache.
	ContextToken string `json:"-"`
}

// GetWeChatSubscribers returns all subscribed WeChat users, including their
// context tokens (needed for outbound push after a restart).
func GetWeChatSubscribers() ([]WeChatSubscriber, error) {
	if !store.ReadDBReady() {
		return nil, nil
	}
	rows, err := store.ReadDB().Query(
		`SELECT id, user_id, user_name, source, created_at, context_token
		 FROM wechat_subscribers ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var subs []WeChatSubscriber
	for rows.Next() {
		var s WeChatSubscriber
		if err := rows.Scan(&s.ID, &s.UserID, &s.UserName, &s.Source, &s.CreatedAt, &s.ContextToken); err != nil {
			return nil, err
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

// UpsertWeChatSubscriber inserts or updates a WeChat subscriber.
//
// The ON CONFLICT clause deliberately does NOT touch context_token: this runs
// on every inbound message, and a concurrent SetWeChatContextToken must not be
// blanked by it. context_token is written only by SetWeChatContextToken.
func UpsertWeChatSubscriber(userID, conversationID, userName, source string) error {
	if !store.DBReady() {
		return nil
	}
	_, err := store.WriteExec(
		`INSERT INTO wechat_subscribers (user_id, conversation_id, user_name, source)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET
			conversation_id = excluded.conversation_id,
			user_name = excluded.user_name,
			source = excluded.source`,
		userID, conversationID, userName, source,
	)
	if err != nil {
		slog.Warn("wechat_subscribers: upsert failed", "error", err, "user_id", userID)
	}
	return err
}

// DeleteWeChatSubscriber removes a subscriber by WeChat user id.
func DeleteWeChatSubscriber(userID string) error {
	if !store.DBReady() {
		return nil
	}
	result, err := store.WriteExec(`DELETE FROM wechat_subscribers WHERE user_id = ?`, userID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetWeChatLastSessionID returns the session the user last addressed from
// WeChat, or "" when none was recorded. This is the sticky target for messages
// that carry no "@{shortID}" prefix.
//
// An empty result is a normal state (never addressed a session, or a database
// from before the column existed) and the caller must fall back to the "/ls"
// hint rather than pick a session on the user's behalf.
func GetWeChatLastSessionID(userID string) (string, error) {
	if !store.ReadDBReady() {
		return "", nil
	}
	var sessionID string
	err := store.ReadDB().QueryRow(
		`SELECT last_session_id FROM wechat_subscribers WHERE user_id = ?`, userID,
	).Scan(&sessionID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return sessionID, nil
}

// SetWeChatLastSessionID records the user's sticky target. It is called only
// after a message was successfully routed to that session, so a failed send
// never moves the target.
func SetWeChatLastSessionID(userID, sessionID string) error {
	if !store.DBReady() {
		return nil
	}
	result, err := store.WriteExec(
		`UPDATE wechat_subscribers SET last_session_id = ? WHERE user_id = ?`,
		sessionID, userID,
	)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SetWeChatContextToken stores the reply token from the user's latest inbound
// message. Persisting it is what keeps outbound push working across a server
// restart; the SDK's own cache is process-local.
//
// The row is expected to exist (UpsertSubscriber runs on every inbound
// message, before this). A missing row is reported as an error rather than
// silently inserting a partial subscriber.
func SetWeChatContextToken(userID, token string) error {
	if !store.DBReady() {
		return nil
	}
	result, err := store.WriteExec(
		`UPDATE wechat_subscribers SET context_token = ? WHERE user_id = ?`,
		token, userID,
	)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ClearWeChatContextTokens blanks every stored reply token. Called on logout:
// context tokens are scoped to the bot account that received them, so they are
// useless (and misleading) once a different account logs in.
//
// Subscriber rows are kept on purpose — their last_session_id is a user
// preference that survives a re-login of the same bot.
func ClearWeChatContextTokens() error {
	if !store.DBReady() {
		return nil
	}
	_, err := store.WriteExec(`UPDATE wechat_subscribers SET context_token = ''`)
	return err
}
