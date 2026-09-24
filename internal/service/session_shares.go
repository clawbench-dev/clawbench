package service

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// SessionSharesDDL creates the session_shares table.
// Exported so handler tests and other external packages can create this table
// in their test databases.
//
// A row freezes one conversation into an opaque JSON snapshot (payload) at
// share time. Unlike file_shares, which stores a live path and reads the file
// on every request, a session share is a point-in-time copy: the conversation
// may keep growing, be rewound, or be archived afterwards without changing what
// the link shows. That is deliberate — the alternative (re-reading chat_history
// on each anonymous request) would make the link's contents drift silently and
// would expose the session to a later-project-membership change.
//
// token is the sole credential (capability URL). session_id is indexed so the
// session-deletion paths can revoke shares in one statement; there is no
// foreign key to chat_sessions because a share must survive the session being
// archived (only a hard delete revokes it).
const SessionSharesDDL = `
CREATE TABLE IF NOT EXISTS session_shares (
	token TEXT PRIMARY KEY,
	session_id TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	backend TEXT NOT NULL DEFAULT '',
	message_count INTEGER NOT NULL DEFAULT 0,
	payload TEXT NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_session_shares_session ON session_shares(session_id);
`

// UpsertSessionShare creates a share for sessionID (created=true) or, when a
// share already exists for that session, rotates it to a fresh token
// (created=false). Rotating invalidates the previous link immediately — only
// the returned token is valid afterwards. Mirrors UpsertFileShare.
func UpsertSessionShare(sessionID, title, backend string, messageCount int, payload string) (token string, created bool, err error) {
	if sessionID == "" {
		return "", false, fmt.Errorf("session id is required")
	}
	_, existing, getErr := GetSessionShareBySession(sessionID)
	if getErr != nil {
		return "", false, getErr
	}

	token, err = GenerateShareToken()
	if err != nil {
		return "", false, err
	}

	if existing {
		// Rotate: delete the old row first so the previous token stops working.
		if _, err := WriteExec("DELETE FROM session_shares WHERE session_id = ?", sessionID); err != nil {
			return "", false, fmt.Errorf("delete stale session share: %w", err)
		}
	}
	if _, err := WriteExec(
		"INSERT INTO session_shares (token, session_id, title, backend, message_count, payload) VALUES (?, ?, ?, ?, ?, ?)",
		token, sessionID, title, backend, messageCount, payload,
	); err != nil {
		return "", false, fmt.Errorf("insert session share: %w", err)
	}
	return token, !existing, nil
}

// GetSessionShareByToken resolves a capability token to its frozen snapshot.
// Returns ok=false when no share matches (link unknown or revoked).
func GetSessionShareByToken(token string) (payload, title string, messageCount int, ok bool, err error) {
	if token == "" {
		return "", "", 0, false, nil
	}
	row := ReadDB().QueryRow("SELECT payload, title, message_count FROM session_shares WHERE token = ?", token)
	if err := row.Scan(&payload, &title, &messageCount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", 0, false, nil
		}
		return "", "", 0, false, fmt.Errorf("query session share by token: %w", err)
	}
	return payload, title, messageCount, true, nil
}

// GetSessionShareBySession returns the active token for a session.
// Returns ok=false when the session has no share.
func GetSessionShareBySession(sessionID string) (token string, ok bool, err error) {
	if sessionID == "" {
		return "", false, nil
	}
	row := ReadDB().QueryRow("SELECT token FROM session_shares WHERE session_id = ?", sessionID)
	if err := row.Scan(&token); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("query session share by session: %w", err)
	}
	return token, true, nil
}

// GetSessionShareProjectByToken returns the project_path of the session a share
// token belongs to. Returns ok=false when the token matches no share.
//
// The JOIN deliberately does not filter archived: a share survives its
// session being archived, so an ownership check that skipped archived rows
// would report a live share as unowned and make it unrevocable.
func GetSessionShareProjectByToken(token string) (projectPath string, ok bool, err error) {
	if token == "" {
		return "", false, nil
	}
	row := ReadDB().QueryRow(
		`SELECT cs.project_path
		   FROM session_shares sh
		   JOIN chat_sessions cs ON cs.id = sh.session_id
		  WHERE sh.token = ?`,
		token,
	)
	if err := row.Scan(&projectPath); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("query session share project by token: %w", err)
	}
	return projectPath, true, nil
}
// DeleteSessionShareByToken revokes a single share link by token.
func DeleteSessionShareByToken(token string) error {
	if token == "" {
		return nil
	}
	if _, err := WriteExec("DELETE FROM session_shares WHERE token = ?", token); err != nil {
		return fmt.Errorf("delete session share by token: %w", err)
	}
	return nil
}

// DeleteSessionShareBySession revokes the share for a single session.
func DeleteSessionShareBySession(sessionID string) error {
	if sessionID == "" {
		return nil
	}
	if _, err := WriteExec("DELETE FROM session_shares WHERE session_id = ?", sessionID); err != nil {
		return fmt.Errorf("delete session share by session: %w", err)
	}
	return nil
}

// DeleteSessionSharesBySessionIDs revokes shares for multiple sessions in one
// statement (used by the archived-session retention purge).
func DeleteSessionSharesBySessionIDs(sessionIDs []string) error {
	clean := make([]string, 0, len(sessionIDs))
	for _, id := range sessionIDs {
		if id != "" {
			clean = append(clean, id)
		}
	}
	if len(clean) == 0 {
		return nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(clean)), ",")
	args := make([]any, 0, len(clean))
	for _, id := range clean {
		args = append(args, id)
	}
	if _, err := WriteExec("DELETE FROM session_shares WHERE session_id IN ("+placeholders+")", args...); err != nil {
		return fmt.Errorf("delete session shares by session ids: %w", err)
	}
	return nil
}

// DeleteAllSessionShares revokes every session share link.
func DeleteAllSessionShares() error {
	if _, err := WriteExec("DELETE FROM session_shares"); err != nil {
		return fmt.Errorf("delete all session shares: %w", err)
	}
	return nil
}

// SessionShare describes one active session share (list view item).
type SessionShare struct {
	Token        string `json:"token"`
	SessionID    string `json:"sessionId"`
	Title        string `json:"title"`
	Backend      string `json:"backend"`
	MessageCount int    `json:"messageCount"`
	CreatedAt    string `json:"createdAt"`
	// Archived reports whether the underlying session is archived. The share
	// still works (the snapshot is a frozen copy) and must stay revocable, but
	// the session itself is no longer addressable, so the UI cannot offer
	// "open conversation" for it.
	Archived bool `json:"archived"`
}

// ListSessionShares returns every active session share for one project, newest
// first. Returns an empty (non-nil) slice when there are none so JSON encodes
// as [].
//
// Scoped by project, unlike ListFileShares: a conversation title is private
// content, so listing another project's shares would disclose it. The JOIN is
// an inner one because a share cannot outlive its session — both
// HardDeleteSession and PurgeArchivedData revoke shares in the same
// transaction that removes the session row, so every share has a session to
// join against. (Archiving is NOT a delete: an archived session keeps both its
// row and its share.)
func ListSessionShares(projectPath string) ([]SessionShare, error) {
	rows, err := ReadDB().Query(
		`SELECT sh.token, sh.session_id, sh.title, sh.backend, sh.message_count, sh.created_at, cs.archived
		   FROM session_shares sh
		   JOIN chat_sessions cs ON cs.id = sh.session_id
		  WHERE cs.project_path = ?
		  ORDER BY sh.rowid DESC`,
		projectPath,
	)
	if err != nil {
		return nil, fmt.Errorf("list session shares: %w", err)
	}
	defer func() { _ = rows.Close() }()

	shares := make([]SessionShare, 0)
	for rows.Next() {
		var s SessionShare
		var archived int
		if err := rows.Scan(&s.Token, &s.SessionID, &s.Title, &s.Backend, &s.MessageCount, &s.CreatedAt, &archived); err != nil {
			return nil, fmt.Errorf("scan session share: %w", err)
		}
		s.Archived = archived != 0
		shares = append(shares, s)
	}
	return shares, rows.Err()
}
