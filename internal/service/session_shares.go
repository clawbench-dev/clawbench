package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"clawbench/internal/store"
)

// SessionSharesDDL creates the session_shares table.
// Exported so handler tests and other external packages can create this table
// in their test databases.
//
// A row records only WHICH messages a link exposes — never their content. The
// JSON is assembled on every public request (see session_share_payload.go), so
// a shared conversation keeps reflecting edits, summaries that landed later,
// avatar changes and deletions. Only the SELECTION is frozen: message_ids is
// always the resolved, explicit, chronological id list, never an empty array
// meaning "all" — storing "all" would make the link silently grow as the
// conversation continues, which is exactly what a share must not do.
//
// token is the sole credential (capability URL). session_id is UNIQUE and
// indexed so the session-deletion paths can revoke shares in one statement, and
// so two concurrent creations cannot leave a session with two live tokens (the
// second one would be unreachable by session id). There is no foreign key to
// chat_sessions because a share must survive the session being archived (only a
// hard delete revokes it).
const SessionSharesDDL = `
CREATE TABLE IF NOT EXISTS session_shares (
	token TEXT PRIMARY KEY,
	session_id TEXT NOT NULL,
	message_ids TEXT NOT NULL DEFAULT '[]',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_session_shares_session ON session_shares(session_id);
`

// migrateSessionSharesToIDOnly rebuilds session_shares without the snapshot
// columns and DISCARDS every existing row.
//
// Dropping the rows is not optional: a legacy row's selection lived inside the
// payload being thrown away, so keeping the row would leave message_ids at its
// '[]' default — which the reader interprets as "nothing left to show" (404) at
// best, and as "share everything" at worst if the sentinel ever changed.
//
// Rebuild rather than a chain of ALTER TABLE DROP COLUMN: the table is empty
// afterwards, so the copy is free, and a rebuild makes the result byte-identical
// to what a fresh install gets from SessionSharesDDL (column order, NOT NULL,
// defaults). DROP TABLE takes the index with it, so the index is recreated
// inside the same transaction.
func migrateSessionSharesToIDOnly() error {
	ctx := context.Background()
	tx, err := store.WriteBegin()
	if err != nil {
		return err
	}
	defer store.WriteUnlock()
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM session_shares`); err != nil {
		return fmt.Errorf("clear legacy session shares: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE session_shares_new (
		token TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		message_ids TEXT NOT NULL DEFAULT '[]',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("create session_shares_new: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE session_shares`); err != nil {
		return fmt.Errorf("drop legacy session_shares: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE session_shares_new RENAME TO session_shares`); err != nil {
		return fmt.Errorf("rename session_shares_new: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_session_shares_session ON session_shares(session_id)`); err != nil {
		return fmt.Errorf("recreate idx_session_shares_session: %w", err)
	}
	return tx.Commit()
}

// UpsertSessionShare creates a share for sessionID (created=true) or, when a
// share already exists for that session, rotates it to a fresh token
// (created=false). Rotating invalidates the previous link immediately — only
// the returned token is valid afterwards. Mirrors UpsertFileShare.
//
// messageIDs must be the RESOLVED selection (see ResolveSessionShareSelection):
// non-empty, chronological, de-duplicated. An empty list is rejected rather
// than stored, because the reader treats an empty selection as "nothing left to
// show" and would 404 the link the moment it was created.
//
// The delete+insert runs in one transaction: session_id is UNIQUE, so two
// concurrent creations that both saw "no existing share" would otherwise race
// into a constraint violation — or, before the index existed, leave two live
// tokens where only one is reachable by session id.
func UpsertSessionShare(sessionID string, messageIDs []int64) (token string, created bool, err error) {
	if sessionID == "" {
		return "", false, fmt.Errorf("session id is required")
	}
	if len(messageIDs) == 0 {
		return "", false, fmt.Errorf("session share requires at least one message id")
	}
	encoded, err := json.Marshal(messageIDs)
	if err != nil {
		return "", false, fmt.Errorf("marshal session share message ids: %w", err)
	}

	_, existing, getErr := GetSessionShareBySession(sessionID)
	if getErr != nil {
		return "", false, getErr
	}

	token, err = GenerateShareToken()
	if err != nil {
		return "", false, err
	}

	ctx := context.Background()
	tx, txErr := store.WriteBegin()
	if txErr != nil {
		return "", false, txErr
	}
	defer store.WriteUnlock()
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, "DELETE FROM session_shares WHERE session_id = ?", sessionID); err != nil {
		return "", false, fmt.Errorf("delete stale session share: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO session_shares (token, session_id, message_ids) VALUES (?, ?, ?)",
		token, sessionID, string(encoded),
	); err != nil {
		return "", false, fmt.Errorf("insert session share: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", false, fmt.Errorf("commit session share: %w", err)
	}
	return token, !existing, nil
}

// GetSessionShareByToken resolves a capability token to its share row joined
// with the session's LIVE metadata. Returns ok=false when no share matches
// (link unknown or revoked) or its session row is gone.
//
// The JOIN is INNER, mirroring ListSessionShares: every delete path revokes the
// share in the same transaction that removes the session, so a share without a
// session is indistinguishable from a revoked link.
func GetSessionShareByToken(token string) (SessionShare, bool, error) {
	var s SessionShare
	if token == "" {
		return s, false, nil
	}
	var rawIDs string
	var archived int
	err := store.ReadDB().QueryRow(
		`SELECT sh.token, sh.session_id, sh.message_ids, sh.created_at,
		        cs.title, cs.backend, cs.archived
		   FROM session_shares sh
		   JOIN chat_sessions cs ON cs.id = sh.session_id
		  WHERE sh.token = ?`,
		token,
	).Scan(&s.Token, &s.SessionID, &rawIDs, &s.CreatedAt, &s.Title, &s.Backend, &archived)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SessionShare{}, false, nil
		}
		return SessionShare{}, false, fmt.Errorf("query session share by token: %w", err)
	}
	ids, err := decodeSessionShareMessageIDs(rawIDs)
	if err != nil {
		return SessionShare{}, false, fmt.Errorf("session share %s: %w", token, err)
	}
	s.MessageIDs = ids
	s.MessageCount = len(ids)
	s.Archived = archived != 0
	return s, true, nil
}

// decodeSessionShareMessageIDs parses the stored id array.
//
// A malformed value is an ERROR, never an empty list: "empty" means "the shared
// messages are all gone" (the reader 404s), while silently treating a corrupt
// row as empty would turn a data bug into a dead link with no explanation.
func decodeSessionShareMessageIDs(raw string) ([]int64, error) {
	if raw == "" {
		return nil, fmt.Errorf("empty message_ids")
	}
	var ids []int64
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil, fmt.Errorf("decode message_ids: %w", err)
	}
	if ids == nil {
		return nil, fmt.Errorf("empty message_ids")
	}
	return ids, nil
}

// GetSessionShareBySession returns the active token for a session.
// Returns ok=false when the session has no share.
func GetSessionShareBySession(sessionID string) (token string, ok bool, err error) {
	if sessionID == "" {
		return "", false, nil
	}
	row := store.ReadDB().QueryRow("SELECT token FROM session_shares WHERE session_id = ?", sessionID)
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
	row := store.ReadDB().QueryRow(
		`SELECT COALESCE(p.path, '')
		   FROM session_shares sh
		   JOIN chat_sessions cs ON cs.id = sh.session_id
		   LEFT JOIN projects p ON p.id = cs.project_id
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
	if _, err := store.WriteExec("DELETE FROM session_shares WHERE token = ?", token); err != nil {
		return fmt.Errorf("delete session share by token: %w", err)
	}
	return nil
}

// DeleteSessionShareBySession revokes the share for a single session.
func DeleteSessionShareBySession(sessionID string) error {
	if sessionID == "" {
		return nil
	}
	if _, err := store.WriteExec("DELETE FROM session_shares WHERE session_id = ?", sessionID); err != nil {
		return fmt.Errorf("delete session share by session: %w", err)
	}
	return nil
}

// SessionShare describes one active session share. Title / Backend / Archived
// are read from chat_sessions on every call: nothing about the conversation is
// frozen, so a rename shows up in both the drawer and the public page.
type SessionShare struct {
	Token      string  `json:"token"`
	SessionID  string  `json:"sessionId"`
	Title      string  `json:"title"`
	Backend    string  `json:"backend"`
	MessageIDs []int64 `json:"messageIds"`
	// MessageCount is the SELECTED count (len(MessageIDs)). Messages deleted
	// after the share was created still count here; the public page renders
	// only the survivors, so the two can differ until the next rotation.
	MessageCount int    `json:"messageCount"`
	CreatedAt    string `json:"createdAt"`
	// Archived reports whether the underlying session is archived. The share
	// still works (its messages are still there) and must stay revocable, but
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
// HardDeleteSession and store.PurgeArchivedData revoke shares in the same
// transaction that removes the session row, so every share has a session to
// join against. (Archiving is NOT a delete: an archived session keeps both its
// row and its share.)
func ListSessionShares(projectPath string) ([]SessionShare, error) {
	projectID, idErr := store.ProjectIDForPath(projectPath)
	if idErr != nil {
		return nil, idErr
	}
	rows, err := store.ReadDB().Query(
		`SELECT sh.token, sh.session_id, sh.message_ids, sh.created_at,
		        cs.title, cs.backend, cs.archived
		   FROM session_shares sh
		   JOIN chat_sessions cs ON cs.id = sh.session_id
		  WHERE cs.project_id = ?
		  ORDER BY sh.rowid DESC`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list session shares: %w", err)
	}
	defer func() { _ = rows.Close() }()

	shares := make([]SessionShare, 0)
	for rows.Next() {
		var s SessionShare
		var rawIDs string
		var archived int
		if err := rows.Scan(&s.Token, &s.SessionID, &rawIDs, &s.CreatedAt, &s.Title, &s.Backend, &archived); err != nil {
			return nil, fmt.Errorf("scan session share: %w", err)
		}
		ids, err := decodeSessionShareMessageIDs(rawIDs)
		if err != nil {
			return nil, fmt.Errorf("session share %s: %w", s.Token, err)
		}
		s.MessageIDs = ids
		s.MessageCount = len(ids)
		s.Archived = archived != 0
		shares = append(shares, s)
	}
	return shares, rows.Err()
}
